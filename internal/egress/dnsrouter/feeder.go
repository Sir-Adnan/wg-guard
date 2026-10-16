package dnsrouter

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/egress/routing"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// Feeder bounds and timing.
const (
	feedQueue     = 4096
	feedBatch     = 512
	feedInterval  = 200 * time.Millisecond
	minTimeout    = 30 * time.Minute
	maxTimeout    = 6 * time.Hour
	maxRemembered = 65536
)

type element struct {
	set  string
	addr netip.Addr
	ttl  uint32
}

// FeederStats are diagnostics counters.
type FeederStats struct {
	Added   uint64
	Skipped uint64
	Dropped uint64
	Failed  uint64
}

// feeder batches resolved addresses into `nft -f` transactions. It keeps a
// bounded memory of recent additions so a popular name costs one element
// write per half timeout rather than one per query.
type feeder struct {
	run subprocess.Runner
	log *slog.Logger
	in  chan element
	wg  sync.WaitGroup

	mu         sync.Mutex
	sets       map[string]bool
	remembered map[string]time.Time // set|addr → refresh-after

	added, skipped, dropped, failed atomic.Uint64
}

func newFeeder(run subprocess.Runner, log *slog.Logger) *feeder {
	return &feeder{run: run, log: log, in: make(chan element, feedQueue), sets: map[string]bool{}, remembered: map[string]time.Time{}}
}

func (f *feeder) stats() FeederStats {
	return FeederStats{Added: f.added.Load(), Skipped: f.skipped.Load(), Dropped: f.dropped.Load(), Failed: f.failed.Load()}
}

// setSets replaces the live set names; memory of removed sets is dropped.
func (f *feeder) setSets(sets map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets = sets
	for k := range f.remembered {
		set, _, _ := strings.Cut(k, "|")
		if !sets[set] {
			delete(f.remembered, k)
		}
	}
}

func (f *feeder) add(set string, addr netip.Addr, ttl uint32) {
	select {
	case f.in <- element{set: set, addr: addr, ttl: ttl}:
	default:
		f.dropped.Add(1)
	}
}

func (f *feeder) start(ctx context.Context) {
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		tick := time.NewTicker(feedInterval)
		defer tick.Stop()
		var batch []element
		flush := func() {
			if len(batch) > 0 {
				f.flush(ctx, batch)
				batch = batch[:0]
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-f.in:
				batch = append(batch, e)
				if len(batch) >= feedBatch {
					flush()
				}
			case <-tick.C:
				flush()
			}
		}
	}()
}

func (f *feeder) wait() { f.wg.Wait() }

func timeoutFor(ttl uint32) time.Duration {
	d := time.Duration(ttl) * time.Second
	return min(max(d, minTimeout), maxTimeout)
}

func (f *feeder) flush(ctx context.Context, batch []element) {
	now := time.Now()
	bySet := map[string][]string{}
	f.mu.Lock()
	if len(f.remembered) > maxRemembered {
		for k, at := range f.remembered {
			if now.After(at) {
				delete(f.remembered, k)
			}
		}
		if len(f.remembered) > maxRemembered {
			f.remembered = map[string]time.Time{}
		}
	}
	for _, e := range batch {
		if !f.sets[e.set] {
			continue
		}
		key := e.set + "|" + e.addr.String()
		if at, ok := f.remembered[key]; ok && now.Before(at) {
			f.skipped.Add(1)
			continue
		}
		timeout := timeoutFor(e.ttl)
		f.remembered[key] = now.Add(timeout / 2)
		bySet[e.set] = append(bySet[e.set], fmt.Sprintf("%s timeout %ds", e.addr, int(timeout.Seconds())))
	}
	f.mu.Unlock()
	if len(bySet) == 0 || f.run == nil {
		return
	}
	sets := make([]string, 0, len(bySet))
	for s := range bySet {
		sets = append(sets, s)
	}
	sort.Strings(sets)
	var b strings.Builder
	n := 0
	for _, s := range sets {
		fmt.Fprintf(&b, "add element ip %s %s { %s }\n", routing.TableName, s, strings.Join(bySet[s], ", "))
		n += len(bySet[s])
	}
	if err := f.apply(ctx, b.String()); err != nil {
		f.failed.Add(uint64(n))
		f.log.Warn("egress resolver: set update failed", "elements", n, "err", err)
		// Forget the batch so the next answer retries it.
		f.mu.Lock()
		for _, e := range batch {
			delete(f.remembered, e.set+"|"+e.addr.String())
		}
		f.mu.Unlock()
		return
	}
	f.added.Add(uint64(n))
}

func (f *feeder) apply(ctx context.Context, script string) error {
	tmp, err := os.CreateTemp("", "wg-guard-egress-feed-*.nft")
	if err != nil {
		return err
	}
	path := tmp.Name()
	defer os.Remove(path)
	_, werr := tmp.WriteString(script)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		return fmt.Errorf("temp script")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = f.run.Run(ctx, []string{"nft", "-f", path})
	return err
}
