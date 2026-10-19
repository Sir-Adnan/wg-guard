package web

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Sir-Adnan/wg-guard/internal/egress/routing"
)

// siteTestData is the site test panel on the outbounds page: the form, and
// the results once a test ran.
type siteTestData struct {
	Services []siteService
	Service  string // selected service ID, or "url"
	URL      string
	Ran      bool
	Invalid  bool
	Label    string // what was tested (service name or URL host)
	Rows     []siteTestRow
}

type siteService struct {
	ID   string
	Name string
}

type siteTestRow struct {
	Name    string // outbound name; "" for the node's own route
	Direct  bool
	Kind    string
	Result  routing.SiteResult
	Tone    string // badge tone of the verdict
	Latency string
}

// siteTestConcurrency bounds simultaneous site tests of one run.
const siteTestConcurrency = 4

// siteTestBudget bounds one run (every exit, in parallel batches).
const siteTestBudget = 3 * routing.SiteTimeout

func siteServices() []siteService {
	out := make([]siteService, 0, len(routing.SiteServices))
	for _, svc := range routing.SiteServices {
		out = append(out, siteService{ID: svc.ID, Name: svc.Name})
	}
	return out
}

// siteTestForm is the empty form; ?site=<host or URL> (the rules page's
// site check links here) prefills a custom URL.
func (s *Server) siteTestForm(r *http.Request) siteTestData {
	d := siteTestData{Services: siteServices(), Service: routing.SiteServices[0].ID}
	if site := strings.TrimSpace(r.URL.Query().Get("site")); site != "" && len(site) <= 2048 {
		d.Service, d.URL = "url", site
	}
	return d
}

// handleSiteTest opens one site through the node's own route and every
// enabled outbound and judges the answer. HTMX swaps the result panel; a
// plain post renders the outbounds page with it.
func (s *Server) handleSiteTest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	d := siteTestData{Services: siteServices(), Service: r.PostFormValue("service"), URL: strings.TrimSpace(r.PostFormValue("url")), Ran: true}
	svc, known := routing.SiteServiceByID(d.Service)
	var target *routing.SiteService
	rawURL := d.URL
	if known {
		target, rawURL, d.Label = &svc, svc.URL, svc.Name
	} else {
		d.Service = "url"
	}
	u, err := routing.ParseSiteURL(rawURL)
	if err != nil {
		d.Invalid = true
	} else {
		if !known {
			d.Label = u.Host
		}
		d.Rows = s.runSiteTests(r, u, target)
	}
	if isHX(r) {
		_ = s.partial(w, r, "routing_outbounds", "site_test_result_partial", d)
		return
	}
	cards, st, err := s.outboundCards(r)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	page := outboundsData{Tabs: s.loadRoutingTabs(r, "outbounds"), Cards: cards, Warnings: st.Warnings, Site: d}
	for _, c := range cards {
		switch c.State {
		case "healthy":
			page.Healthy++
		case "down", "error":
			page.Problems++
		}
	}
	_ = s.render(w, r, "routing_outbounds", "app", page)
}

// runSiteTests tests the node's own route first, then every enabled
// outbound in name order.
func (s *Server) runSiteTests(r *http.Request, u *url.URL, svc *routing.SiteService) []siteTestRow {
	if s.EgressRuntime == nil {
		return nil
	}
	rows := []siteTestRow{{Direct: true}}
	ids := []string{routing.DirectSite}
	if outbounds, err := s.Egress.ListOutbounds(r.Context()); err == nil {
		sort.SliceStable(outbounds, func(i, j int) bool { return outbounds[i].Name < outbounds[j].Name })
		for _, o := range outbounds {
			if !o.Enabled {
				continue
			}
			rows = append(rows, siteTestRow{Name: o.Name, Kind: string(o.Kind)})
			ids = append(ids, o.ID)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), siteTestBudget)
	defer cancel()
	sem := make(chan struct{}, siteTestConcurrency)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			res, err := s.EgressRuntime.TestSite(ctx, id, u, svc)
			if err != nil {
				res = routing.SiteResult{Verdict: routing.SiteFailed, Error: "unreachable"}
			}
			rows[i].Result, rows[i].Tone = res, siteVerdictTone(res.Verdict)
			if res.Latency > 0 {
				rows[i].Latency = strconv.FormatInt(res.Latency.Milliseconds(), 10)
			}
		}()
	}
	wg.Wait()
	return rows
}

// siteVerdictTone maps a verdict to the badge tone.
func siteVerdictTone(verdict string) string {
	switch verdict {
	case routing.SiteAvailable, routing.SiteReachable:
		return "success"
	case routing.SiteBlocked, routing.SiteCaptcha, routing.SiteFailed:
		return "destructive"
	}
	return "outline"
}
