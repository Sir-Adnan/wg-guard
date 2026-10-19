package web

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/egress/routing"
)

// Site tests page (/routing/tests): one service, every service, or any URL,
// opened through the node's own route and every enabled outbound. With HTMX
// the result grid renders at once and each cell loads itself, so results
// arrive as they finish and the node's site test slots (routing.SiteTestSlots)
// bound the work; a plain post runs the grid before answering.

// siteTestData is the page: the form, and the result grid once a test ran.
type siteTestData struct {
	Tabs    routingTabs
	Groups  []siteServiceGroup
	Service string // a service ID, "all" or "url"
	URL     string
	Family  string // auto | ip4 | ip6 | both
	Ran     bool
	Invalid bool
	// Unavailable: this node runs no site tests (no egress runtime).
	Unavailable bool
	Label       string // what was tested (service name, "all" or URL host)
	Columns     []siteColumn
	Rows        []siteTestRow
	Lazy        bool // cells load themselves
}

type siteServiceGroup struct {
	Key      string // catalog suffix: routing.site.group.<key>
	Services []siteService
}

type siteService struct {
	ID   string
	Name string
}

type siteColumn struct {
	Service string // a service ID or "url"
	Name    string
}

type siteTestRow struct {
	Exit   string // outbound ID or routing.DirectSite
	Name   string // outbound name; "" for the node's own route
	Direct bool
	Kind   string
	Cells  []siteCell
}

// siteCell is one exit × service test: one outcome, or one per family when
// the form asked for both.
type siteCell struct {
	Exit     string
	Service  string
	URL      string // custom URL (Service "url")
	Family   string // auto | ip4 | ip6 | both
	Column   string // column name, the phone layout's label
	Wide     bool   // single-column grid: show the final address
	Vals     string // hx-vals of a lazy cell and of its retry button
	Done     bool
	Outcomes []siteOutcome
}

// siteOutcome is one test result of a cell.
type siteOutcome struct {
	Family  string // ip4 | ip6 when the cell compares both families, else ""
	Result  routing.SiteResult
	Tone    string
	Latency string
	Flag    string
}

// siteFamilyBoth asks for an IPv4 and an IPv6 result side by side.
const siteFamilyBoth = "both"

// siteFamily accepts the form's family choice; anything else is auto.
func siteFamily(raw string) string {
	if raw == siteFamilyBoth || routing.ValidFamily(raw) {
		return raw
	}
	return routing.FamilyAuto
}

// siteCellWait bounds how long one cell waits for a free test slot and runs.
const siteCellWait = 2 * time.Minute

// siteGridBudget bounds a plain (non-HTMX) run of the whole grid.
const siteGridBudget = 2 * time.Minute

func siteServiceGroups() []siteServiceGroup {
	var out []siteServiceGroup
	for _, g := range routing.SiteGroups {
		group := siteServiceGroup{Key: g}
		for _, svc := range routing.SiteServices {
			if svc.Group == g {
				group.Services = append(group.Services, siteService{ID: svc.ID, Name: svc.Name})
			}
		}
		if len(group.Services) > 0 {
			out = append(out, group)
		}
	}
	return out
}

func (s *Server) handleSiteTests(w http.ResponseWriter, r *http.Request) {
	d := siteTestData{Tabs: s.loadRoutingTabs(r, "tests"), Groups: siteServiceGroups(), Service: routing.SiteServices[0].ID, Family: routing.FamilyAuto}
	// ?site=<host or URL> (the rules page's route check links here)
	// prefills a custom URL.
	if site := strings.TrimSpace(r.URL.Query().Get("site")); site != "" && len(site) <= 2048 {
		d.Service, d.URL = "url", site
	}
	_ = s.render(w, r, "routing_tests", "app", d)
}

// handleSiteTestRun builds the result grid. HTMX swaps it in with lazy
// cells; a plain post runs every cell and renders the whole page.
func (s *Server) handleSiteTestRun(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	d := siteTestData{Tabs: s.loadRoutingTabs(r, "tests"), Groups: siteServiceGroups(),
		Service: r.PostFormValue("service"), URL: strings.TrimSpace(r.PostFormValue("url")),
		Family: siteFamily(r.PostFormValue("family")), Ran: true}
	d.Columns, d.Label, d.Invalid = siteColumns(d.Service, d.URL)
	if _, known := routing.SiteServiceByID(d.Service); !known && d.Service != "all" {
		d.Service = "url"
	}
	switch {
	case d.Invalid:
	case s.EgressRuntime == nil:
		d.Unavailable = true
	default:
		d.Rows = s.siteRows(r, d.Columns, d.URL, d.Family)
		if isHX(r) {
			d.Lazy = true
		} else {
			s.runSiteGrid(r, d.Rows)
		}
	}
	if isHX(r) {
		_ = s.partial(w, r, "routing_tests", "site_test_result_partial", d)
		return
	}
	_ = s.render(w, r, "routing_tests", "app", d)
}

// siteColumns resolves the form's choice to the services tested; invalid
// reports a custom URL that is not a site address.
func siteColumns(service, rawURL string) (cols []siteColumn, label string, invalid bool) {
	if service == "all" {
		for _, svc := range routing.SiteServices {
			cols = append(cols, siteColumn{Service: svc.ID, Name: svc.Name})
		}
		return cols, "all", false
	}
	if svc, ok := routing.SiteServiceByID(service); ok {
		return []siteColumn{{Service: svc.ID, Name: svc.Name}}, svc.Name, false
	}
	u, err := routing.ParseSiteURL(rawURL)
	if err != nil {
		return nil, "", true
	}
	return []siteColumn{{Service: "url", Name: u.Host}}, u.Host, false
}

// siteRows lists the node's own route first, then every enabled outbound in
// name order, each with one pending cell per column.
func (s *Server) siteRows(r *http.Request, cols []siteColumn, rawURL, family string) []siteTestRow {
	rows := []siteTestRow{{Exit: routing.DirectSite, Direct: true}}
	if outbounds, err := s.Egress.ListOutbounds(r.Context()); err == nil {
		sort.SliceStable(outbounds, func(i, j int) bool { return outbounds[i].Name < outbounds[j].Name })
		for _, o := range outbounds {
			if o.Enabled {
				rows = append(rows, siteTestRow{Exit: o.ID, Name: o.Name, Kind: string(o.Kind)})
			}
		}
	}
	for i := range rows {
		for _, c := range cols {
			cell := siteCell{Exit: rows[i].Exit, Service: c.Service, Family: family, Column: c.Name, Wide: len(cols) == 1}
			if c.Service == "url" {
				cell.URL = rawURL
			}
			cell.Vals = cellVals(cell)
			rows[i].Cells = append(rows[i].Cells, cell)
		}
	}
	return rows
}

func cellVals(c siteCell) string {
	vals := map[string]string{"exit": c.Exit, "service": c.Service, "family": c.Family}
	if c.URL != "" {
		vals["url"] = c.URL
	}
	if c.Wide {
		vals["wide"] = "1"
	}
	b, _ := json.Marshal(vals)
	return string(b)
}

// runSiteGrid runs every cell of a plain post within siteGridBudget; the
// manager's slots bound the concurrency.
func (s *Server) runSiteGrid(r *http.Request, rows []siteTestRow) {
	ctx, cancel := context.WithTimeout(r.Context(), siteGridBudget)
	defer cancel()
	var wg sync.WaitGroup
	for i := range rows {
		for j := range rows[i].Cells {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.runSiteCell(ctx, &rows[i].Cells[j])
			}()
		}
	}
	wg.Wait()
}

// handleSiteTestCell runs one exit × service test (a lazy cell or its retry).
func (s *Server) handleSiteTestCell(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	cell := siteCell{Exit: r.PostFormValue("exit"), Service: r.PostFormValue("service"),
		URL: strings.TrimSpace(r.PostFormValue("url")), Family: siteFamily(r.PostFormValue("family")),
		Wide: r.PostFormValue("wide") == "1"}
	cols, _, invalid := siteColumns(cell.Service, cell.URL)
	if invalid || len(cols) != 1 || cell.Exit == "" || s.EgressRuntime == nil {
		s.badRequest(w, r, "bad site test")
		return
	}
	cell.Column = cols[0].Name
	if cell.Service != "url" {
		cell.URL = ""
	}
	cell.Vals = cellVals(cell)
	ctx, cancel := context.WithTimeout(r.Context(), siteCellWait)
	defer cancel()
	if !s.runSiteCell(ctx, &cell) {
		s.badRequest(w, r, "bad site test")
		return
	}
	_ = s.partial(w, r, "routing_tests", "site_test_cell_partial", cell)
}

// runSiteCell fills one cell; false means the request names no test. Both
// families run side by side, each holding its own test slot. An exit the
// runtime does not know (removed, or not applied yet) answers "unreachable"
// rather than an error, so a lazy cell never stays pending.
func (s *Server) runSiteCell(ctx context.Context, c *siteCell) bool {
	var svc *routing.SiteService
	target := c.URL
	if known, ok := routing.SiteServiceByID(c.Service); ok {
		svc, target = &known, known.URL
	}
	u, err := routing.ParseSiteURL(target)
	if err != nil {
		return false
	}
	families := []string{c.Family}
	if c.Family == siteFamilyBoth {
		families = []string{routing.FamilyIPv4, routing.FamilyIPv6}
	}
	outcomes := make([]siteOutcome, len(families))
	var wg sync.WaitGroup
	for i, family := range families {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := s.EgressRuntime.TestSite(ctx, c.Exit, family, u, svc)
			if err != nil {
				res = routing.SiteResult{Verdict: routing.SiteFailed, Error: "unreachable"}
			}
			o := siteOutcome{Result: res, Tone: siteVerdictTone(res.Verdict), Flag: countryFlag(res.Region)}
			if len(families) > 1 {
				o.Family = family
			}
			if res.Latency > 0 {
				o.Latency = strconv.FormatInt(res.Latency.Milliseconds(), 10)
			}
			outcomes[i] = o
		}()
	}
	wg.Wait()
	c.Done, c.Outcomes = true, outcomes
	return true
}

// siteVerdictTone maps a verdict to the badge tone: green works, amber is
// "answers, with a catch" (bot check, firewall, partial), red does not work.
func siteVerdictTone(verdict string) string {
	switch verdict {
	case routing.SiteAvailable, routing.SiteReachable:
		return "success"
	case routing.SiteChallenge, routing.SiteRefused, routing.SiteLimited:
		return "warning"
	case routing.SiteBlocked, routing.SiteCaptcha, routing.SiteFailed:
		return "destructive"
	}
	return "outline"
}
