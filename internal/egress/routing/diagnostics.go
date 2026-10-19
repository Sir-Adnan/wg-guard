package routing

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
)

// Panel diagnostics: probing every outbound at once, site tests and speed
// tests. They never touch the kernel, so a read-only viewer can run them.
// Their cost is bounded per node, whatever the number of open panels:
// SiteTestSlots site tests run at once (the rest wait their turn), one speed
// test runs at a time, and TestAll probes testAllConcurrency outbounds at once.

// SiteTestSlots bounds the site tests running at once on the node.
const SiteTestSlots = 6

// testAllConcurrency bounds the probes of one TestAll.
const testAllConcurrency = 4

// DirectSite names the node's own route in site and speed tests.
const DirectSite = "direct"

// ErrSpeedBusy reports that another speed test is running.
var ErrSpeedBusy = errors.New("routing: a speed test is already running")

type diagnostics struct {
	once  sync.Once
	slots chan struct{}
	speed sync.Mutex // held for the duration of one speed test

	mu     sync.Mutex
	speeds map[string]SpeedResult // last result per outbound (DirectSite included)
}

func (d *diagnostics) siteSlots() chan struct{} {
	d.once.Do(func() { d.slots = make(chan struct{}, SiteTestSlots) })
	return d.slots
}

// diagTarget resolves an outbound (or DirectSite) to the mark and family
// order its traffic uses; ok is false when it cannot carry a test now.
func (m *Manager) diagTarget(id string) (mark uint32, ipv6, ok bool, err error) {
	if id == DirectSite {
		return 0, false, true, nil
	}
	o, found := m.Status().Outbounds[id]
	if !found {
		return 0, false, false, egress.ErrNotFound
	}
	if !o.Enabled || o.LinkError != "" || (o.Link == "" && o.Kind != egress.KindSourceIP) {
		return 0, false, false, nil
	}
	return o.Mark, o.IPv6, true, nil
}

// TestSite opens a site through one outbound (or the node's own route, for
// DirectSite). With FamilyAuto it dials the way client traffic would: an
// outbound whose link carries IPv6 is tested IPv6 first, like the bridge.
// FamilyIPv6 through an outbound whose link carries no IPv6 fails at once
// (no_ipv6): its IPv6 would leave through the node's own route instead. It
// waits for a free site test slot as long as ctx allows.
func (m *Manager) TestSite(ctx context.Context, id, family string, u *url.URL, svc *SiteService) (SiteResult, error) {
	now := time.Now().UTC()
	mark, ipv6, ok, err := m.diagTarget(id)
	if err != nil {
		return SiteResult{}, err
	}
	if m.Sites == nil {
		return SiteResult{Verdict: SiteFailed, Error: "unsupported", TestedAt: now}, nil
	}
	if !ok {
		return SiteResult{Verdict: SiteFailed, Error: "unreachable", TestedAt: now}, nil
	}
	if !ValidFamily(family) {
		family = FamilyAuto
	}
	if family == FamilyIPv6 && id != DirectSite && !ipv6 {
		return SiteResult{Verdict: SiteFailed, Error: "no_ipv6", TestedAt: now}, nil
	}
	slots := m.diag.siteSlots()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return SiteResult{Verdict: SiteFailed, Error: "timeout", TestedAt: now}, nil
	}
	return m.Sites.Test(ctx, mark, ipv6, family, u, svc), nil
}

// SpeedTest measures one outbound's exit (or the node's own route, for
// DirectSite) and keeps the result for SpeedResults. Only one speed test
// runs at a time on the node; a second one gets ErrSpeedBusy at once.
func (m *Manager) SpeedTest(ctx context.Context, id string) (SpeedResult, error) {
	now := time.Now().UTC()
	mark, ipv6, ok, err := m.diagTarget(id)
	if err != nil {
		return SpeedResult{}, err
	}
	if m.Speeds == nil {
		return SpeedResult{Error: "unsupported", TestedAt: now}, nil
	}
	if !ok {
		return SpeedResult{Error: "unreachable", TestedAt: now}, nil
	}
	if !m.diag.speed.TryLock() {
		return SpeedResult{Error: "busy", TestedAt: now}, ErrSpeedBusy
	}
	res := m.Speeds.Test(ctx, mark, ipv6)
	m.diag.speed.Unlock()
	m.recordSpeed(id, res)
	return res, nil
}

// recordSpeed stores a result and drops those of outbounds that no longer
// exist, so the map never outgrows the outbound list.
func (m *Manager) recordSpeed(id string, res SpeedResult) {
	live := m.Status().Outbounds
	m.diag.mu.Lock()
	defer m.diag.mu.Unlock()
	if m.diag.speeds == nil {
		m.diag.speeds = map[string]SpeedResult{}
	}
	m.diag.speeds[id] = res
	for k := range m.diag.speeds {
		if _, ok := live[k]; !ok && k != DirectSite {
			delete(m.diag.speeds, k)
		}
	}
}

// SpeedResults returns the last speed test of each outbound.
func (m *Manager) SpeedResults() map[string]SpeedResult {
	m.diag.mu.Lock()
	defer m.diag.mu.Unlock()
	out := make(map[string]SpeedResult, len(m.diag.speeds))
	for k, v := range m.diag.speeds {
		out[k] = v
	}
	return out
}

// TestAll probes every enabled outbound now (the panel's "Test all") and
// records the results like Test.
func (m *Manager) TestAll(ctx context.Context) {
	var ids []string
	for id, o := range m.Status().Outbounds {
		if o.Enabled {
			ids = append(ids, id)
		}
	}
	sem := make(chan struct{}, testAllConcurrency)
	var wg sync.WaitGroup
	for _, id := range ids {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			_, _ = m.Test(ctx, id)
		}()
	}
	wg.Wait()
}
