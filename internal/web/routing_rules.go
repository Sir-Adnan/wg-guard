package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
	"github.com/Sir-Adnan/wg-guard/internal/egress/geodata"
	"github.com/Sir-Adnan/wg-guard/internal/egress/routing"
)

// --- rules list ---------------------------------------------------------------------

type presetChip struct {
	ID, Name string
}

type ruleRow struct {
	R           *egress.Rule
	Index       int
	TargetLabel string
	TargetKind  string
	Presets     []presetChip
	Domains     int
	Categories  int
	Addresses   int
	First, Last bool
}

type geoInfo struct {
	Kind       string
	Present    bool
	Size       int64
	ModTime    time.Time
	Categories int
	URL        string
}

type siteCheck struct {
	Query       string
	Valid       bool
	Matched     bool
	RuleName    string
	TargetLabel string
	TargetKind  string
	Default     bool
	Note        string // catalog key for an explanation
}

type rulesData struct {
	Tabs           routingTabs
	Rows           []ruleRow
	DefaultOptions []targetOption
	DefaultLabel   string
	DefaultKind    string
	BlockDNS       bool
	Geo            []geoInfo
	Warnings       []routing.Warning
	ResolverWanted bool
	ResolverActive bool
	Check          *siteCheck
	GeositeURL     string
	GeoIPURL       string
	CanWrite       bool
}

func matcherCounts(r *egress.Rule) (domains, categories, addresses int) {
	ms, err := egress.ParseMatchers(r.Matchers)
	if err != nil {
		return
	}
	for _, m := range ms {
		switch m.Type {
		case egress.MatchGeosite, egress.MatchGeoIP:
			categories++
		case egress.MatchCIDR:
			addresses++
		default:
			domains++
		}
	}
	return
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rules, err := s.Egress.ListRules(ctx)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	settings, err := s.Egress.GetSettings(ctx)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	names := s.targetNames(r)
	d := rulesData{
		Tabs: s.loadRoutingTabs(r, "rules"), BlockDNS: settings.BlockEncryptedDNS,
		DefaultOptions: s.targetOptions(r, settings.DefaultTarget, false),
		DefaultLabel:   s.targetLabel(r, settings.DefaultTarget, names),
		DefaultKind:    string(settings.DefaultTarget.Kind),
		GeositeURL:     settings.GeositeURL, GeoIPURL: settings.GeoIPURL,
		CanWrite: canOperate(r, "interfaces.write"),
	}
	for i, rule := range rules {
		row := ruleRow{R: rule, Index: i + 1, First: i == 0, Last: i == len(rules)-1,
			TargetLabel: s.targetLabel(r, rule.Target, names), TargetKind: string(rule.Target.Kind)}
		for _, id := range rule.Presets {
			if p, ok := egress.PresetByID(id); ok {
				row.Presets = append(row.Presets, presetChip{ID: p.ID, Name: p.Name})
			}
		}
		row.Domains, row.Categories, row.Addresses = matcherCounts(rule)
		d.Rows = append(d.Rows, row)
	}
	if s.EgressRuntime != nil {
		st := s.EgressRuntime.Status()
		d.Warnings, d.ResolverWanted, d.ResolverActive = st.Warnings, st.ResolverWanted, st.ResolverActive
	}
	if s.GeoData != nil {
		urls := map[geodata.Kind]string{geodata.KindGeosite: settings.GeositeURL, geodata.KindGeoIP: settings.GeoIPURL}
		for _, info := range s.GeoData.Info() {
			d.Geo = append(d.Geo, geoInfo{Kind: string(info.Kind), Present: info.Present, Size: info.Size,
				ModTime: info.ModTime, Categories: info.Categories, URL: urls[info.Kind]})
		}
	}
	if q := strings.TrimSpace(r.URL.Query().Get("check")); q != "" {
		d.Check = s.checkSite(r, q, names)
	}
	_ = s.render(w, r, "routing_rules", "app", d)
}

// checkSite evaluates one domain or IPv4 address against the current rules
// exactly as the kernel and resolver would (same compiled plan).
func (s *Server) checkSite(r *http.Request, query string, names map[string]string) *siteCheck {
	if len(query) > 253 {
		query = query[:253]
	}
	c := &siteCheck{Query: query}
	snap, err := s.Egress.LoadSnapshot(r.Context())
	if err != nil {
		return c
	}
	var geo routing.Geo
	if s.GeoData != nil {
		geo = s.GeoData
	}
	health := routing.Health{}
	if s.EgressRuntime != nil {
		for id, o := range s.EgressRuntime.Status().Outbounds {
			if o.Probed {
				health[id] = o.Healthy
			}
		}
	}
	plan := routing.Compile(snap, []routing.Interface{{Name: "check"}}, routing.Inputs{Health: health}, geo)
	ruleNames := map[string]*egress.Rule{}
	for _, rule := range snap.Rules {
		ruleNames[rule.ID] = rule
	}
	finish := func(rule *egress.Rule) *siteCheck {
		c.Valid, c.Matched = true, rule != nil
		target := snap.Settings.DefaultTarget
		if rule != nil {
			c.RuleName, target = rule.Name, rule.Target
		} else {
			c.Default = true
		}
		c.TargetLabel, c.TargetKind = s.targetLabel(r, target, names), string(target.Kind)
		return c
	}
	if addr, err := netip.ParseAddr(query); err == nil {
		if !addr.Is4() {
			c.Note = "routing.check.ipv6"
			return c
		}
		for _, cl := range plan.Classify {
			if prefixes, ok := plan.StaticSets[cl.Set]; ok {
				for _, p := range prefixes {
					if p.Contains(addr) {
						return finish(ruleNames[cl.RuleID])
					}
				}
			}
		}
		c.Note = "routing.check.address_note"
		return finish(nil)
	}
	// Accept a pasted URL: drop the scheme, path and port.
	host := strings.TrimPrefix(strings.TrimPrefix(query, "https://"), "http://")
	if i := strings.IndexAny(host, "/:?#"); i > 0 {
		host = host[:i]
	}
	name, ok := egress.NormalizeDomain(host)
	if !ok {
		return c
	}
	c.Query = name
	if e, found := plan.Ruleset.Lookup(name); found {
		return finish(ruleNames[e.RuleID])
	}
	return finish(nil)
}

func (s *Server) handleRoutingDefault(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	current, err := s.Egress.GetSettings(r.Context())
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	target, err := egress.ParseTarget(r.PostFormValue("default_target"))
	if err != nil {
		s.egressActionFailed(w, r, "/routing/rules", err)
		return
	}
	current.DefaultTarget = target
	current.BlockEncryptedDNS = r.PostFormValue("block_dns") == "1"
	if err := s.Egress.UpdateSettings(r.Context(), current); err != nil {
		s.egressActionFailed(w, r, "/routing/rules", err)
		return
	}
	s.audit(r, "egress.settings_updated", "egress", map[string]any{"default_target": target.String(), "block_encrypted_dns": current.BlockEncryptedDNS})
	s.routingDone(w, r, "/routing/rules", "routing.toast.saved")
}

func (s *Server) handleRuleToggle(enable bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := s.Egress.SetRuleEnabled(r.Context(), id, enable); err != nil {
			s.egressActionFailed(w, r, "/routing/rules", err)
			return
		}
		s.audit(r, "egress.rule_updated", id, map[string]any{"enabled": enable})
		s.routingDone(w, r, "/routing/rules", "routing.toast.saved")
	}
}

func (s *Server) handleRuleMove(delta int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := s.Egress.MoveRule(r.Context(), id, delta); err != nil {
			s.egressActionFailed(w, r, "/routing/rules", err)
			return
		}
		s.audit(r, "egress.rule_moved", id, map[string]any{"delta": delta})
		s.routingDone(w, r, "/routing/rules", "routing.toast.reordered")
	}
}

func (s *Server) handleRuleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Egress.DeleteRule(r.Context(), id); err != nil {
		s.egressActionFailed(w, r, "/routing/rules", err)
		return
	}
	s.audit(r, "egress.rule_deleted", id, nil)
	s.routingDone(w, r, "/routing/rules", "routing.toast.deleted")
}

// --- rule form ----------------------------------------------------------------------

type presetOption struct {
	ID, Name string
	Checked  bool
	Lines    int
}

type presetGroup struct {
	Category string
	Presets  []presetOption
}

type ruleFormData struct {
	Tabs    routingTabs
	R       *egress.Rule
	Form    operationalForm
	Targets []targetOption
	Groups  []presetGroup
	Geosite []string
	GeoIP   []string
	HasGeo  bool
}

const maxCategorySuggestions = 800

func (s *Server) ruleFormData(r *http.Request, rule *egress.Rule, form operationalForm, presets []string, target egress.Target) ruleFormData {
	checked := map[string]bool{}
	for _, id := range presets {
		checked[id] = true
	}
	d := ruleFormData{Tabs: s.loadRoutingTabs(r, "rules"), R: rule, Form: form, Targets: s.targetOptions(r, target, true)}
	index := map[egress.PresetCategory]int{}
	for _, p := range egress.Presets() {
		i, ok := index[p.Category]
		if !ok {
			i = len(d.Groups)
			index[p.Category] = i
			d.Groups = append(d.Groups, presetGroup{Category: string(p.Category)})
		}
		d.Groups[i].Presets = append(d.Groups[i].Presets, presetOption{ID: p.ID, Name: p.Name, Checked: checked[p.ID], Lines: len(p.Lines)})
	}
	if s.GeoData != nil {
		if names, err := s.GeoData.Categories(geodata.KindGeosite); err == nil && len(names) > 0 {
			d.HasGeo = true
			d.Geosite = names[:min(len(names), maxCategorySuggestions)]
		}
		if names, err := s.GeoData.Categories(geodata.KindGeoIP); err == nil {
			d.GeoIP = names[:min(len(names), maxCategorySuggestions)]
		}
	}
	return d
}

func (s *Server) handleRuleNew(w http.ResponseWriter, r *http.Request) {
	form := operationalForm{Values: map[string]string{"name": "", "target": "direct", "matchers": "", "enabled": "1"}, Fields: map[string]string{}}
	var presets []string
	if p := r.URL.Query().Get("preset"); p != "" {
		if preset, ok := egress.PresetByID(p); ok {
			presets = []string{p}
			form.Values["name"] = preset.Name
		}
	}
	_ = s.render(w, r, "routing_rule_form", "app", s.ruleFormData(r, nil, form, presets, egress.Direct))
}

func (s *Server) handleRuleEditPage(w http.ResponseWriter, r *http.Request) {
	rule, err := s.Egress.GetRule(r.Context(), r.PathValue("id"))
	if err != nil {
		s.egressActionFailed(w, r, "/routing/rules", err)
		return
	}
	enabled := "1"
	if !rule.Enabled {
		enabled = "0"
	}
	form := operationalForm{Values: map[string]string{"name": rule.Name, "target": rule.Target.String(), "matchers": rule.Matchers, "enabled": enabled}, Fields: map[string]string{}}
	_ = s.render(w, r, "routing_rule_form", "app", s.ruleFormData(r, rule, form, rule.Presets, rule.Target))
}

func ruleInputFromForm(r *http.Request) (egress.RuleInput, error) {
	in := egress.RuleInput{
		Name: r.PostFormValue("name"), Matchers: r.PostFormValue("matchers"),
		Presets: r.Form["preset"], Enabled: r.PostFormValue("enabled") != "0",
	}
	target, err := egress.ParseTarget(r.PostFormValue("target"))
	if err != nil {
		return in, err
	}
	in.Target = target
	return in, nil
}

func (s *Server) handleRuleSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	id := r.PathValue("id")
	in, err := ruleInputFromForm(r)
	var rule *egress.Rule
	if err == nil {
		if id == "" {
			rule, err = s.Egress.CreateRule(r.Context(), in)
		} else {
			rule, err = s.Egress.UpdateRule(r.Context(), id, in)
		}
	}
	if err != nil {
		if errors.Is(err, egress.ErrNotFound) {
			s.egressActionFailed(w, r, "/routing/rules", err)
			return
		}
		field, msg := s.egressFieldError(r, err)
		if _, ok := egress.AsValidation(err); !ok {
			s.logError(r, "egress rule save failed", err)
		}
		form := submittedOperationalForm(r, map[string]string{"name": "", "target": "", "matchers": "", "enabled": ""})
		form.Error = msg
		if field != "" {
			form.Fields[field] = msg
		}
		var existing *egress.Rule
		if id != "" {
			existing, _ = s.Egress.GetRule(r.Context(), id)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = s.render(w, r, "routing_rule_form", "app", s.ruleFormData(r, existing, form, r.Form["preset"], in.Target))
		return
	}
	action := "egress.rule_created"
	if id != "" {
		action = "egress.rule_updated"
	}
	s.audit(r, action, rule.ID, map[string]any{"name": rule.Name, "target": rule.Target.String()})
	s.routingDone(w, r, "/routing/rules", "routing.toast.rule_saved", rule.Name)
}

// --- geodata ------------------------------------------------------------------------

func geoKind(r *http.Request) (geodata.Kind, bool) {
	k := geodata.Kind(r.PathValue("kind"))
	return k, k == geodata.KindGeosite || k == geodata.KindGeoIP
}

func (s *Server) handleGeoDownload(w http.ResponseWriter, r *http.Request) {
	kind, ok := geoKind(r)
	if !ok || s.GeoData == nil {
		http.NotFound(w, r)
		return
	}
	settings, err := s.Egress.GetSettings(r.Context())
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	url := settings.GeositeURL
	fallback := geodata.DefaultGeositeURL
	if kind == geodata.KindGeoIP {
		url, fallback = settings.GeoIPURL, geodata.DefaultGeoIPURL
	}
	if url == "" {
		url = fallback
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	n, err := s.GeoData.Download(ctx, kind, url)
	if err != nil {
		s.logError(r, "geodata download failed", err)
		s.redirectToast(w, r, "/routing/rules", "routing.toast.geo_download_failed")
		return
	}
	s.audit(r, "egress.geodata_installed", string(kind), map[string]any{"categories": n, "source": "download"})
	s.routingDone(w, r, "/routing/rules", "routing.toast.geo_installed")
}

func (s *Server) handleGeoUpload(w http.ResponseWriter, r *http.Request) {
	kind, ok := geoKind(r)
	if !ok || s.GeoData == nil {
		http.NotFound(w, r)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		s.redirectToast(w, r, "/routing/rules", "routing.toast.geo_invalid")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, geodata.MaxFileBytes+1))
	if err != nil {
		s.redirectToast(w, r, "/routing/rules", "routing.toast.geo_invalid")
		return
	}
	n, err := s.GeoData.Install(kind, data)
	if err != nil {
		s.redirectToast(w, r, "/routing/rules", "routing.toast.geo_invalid")
		return
	}
	s.audit(r, "egress.geodata_installed", string(kind), map[string]any{"categories": n, "source": "upload"})
	s.routingDone(w, r, "/routing/rules", "routing.toast.geo_installed")
}

func (s *Server) handleGeoSources(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	current, err := s.Egress.GetSettings(r.Context())
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	current.GeositeURL, current.GeoIPURL = r.PostFormValue("geosite_url"), r.PostFormValue("geoip_url")
	if err := s.Egress.UpdateSettings(r.Context(), current); err != nil {
		s.egressActionFailed(w, r, "/routing/rules", err)
		return
	}
	s.audit(r, "egress.settings_updated", "egress", map[string]any{"geodata_sources": true})
	s.redirectToast(w, r, "/routing/rules", "routing.toast.saved")
}

func (s *Server) handleGeoDelete(w http.ResponseWriter, r *http.Request) {
	kind, ok := geoKind(r)
	if !ok || s.GeoData == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.GeoData.Remove(kind); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "egress.geodata_removed", string(kind), nil)
	s.routingDone(w, r, "/routing/rules", "routing.toast.deleted")
}
