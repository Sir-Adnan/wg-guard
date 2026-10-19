package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
	"github.com/Sir-Adnan/wg-guard/internal/egress/routing"
)

// Egress routing workspace: three pages (Outbounds, Balancers, Rules) that
// share one sub-navigation. Handlers call the egress store directly and then
// re-render kernel routing through the runtime manager; client tunnels are
// never touched by these mutations.

// routingTabs is the shared sub-navigation of the routing pages.
type routingTabs struct {
	Active    string // outbounds | balancers | rules
	Outbounds int
	Balancers int
	Rules     int
}

func (s *Server) loadRoutingTabs(r *http.Request, active string) routingTabs {
	tabs := routingTabs{Active: active}
	if s.Egress == nil {
		return tabs
	}
	if list, err := s.Egress.ListOutbounds(r.Context()); err == nil {
		tabs.Outbounds = len(list)
	}
	if list, err := s.Egress.ListBalancers(r.Context()); err == nil {
		tabs.Balancers = len(list)
	}
	if list, err := s.Egress.ListRules(r.Context()); err == nil {
		tabs.Rules = len(list)
	}
	return tabs
}

// egressApply re-renders routing after a stored change. The change is
// already durable; a failed apply is reported, and the next runtime pass
// retries it.
func (s *Server) egressApply(r *http.Request) error {
	if s.EgressRuntime == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	err := s.EgressRuntime.Reapply(ctx)
	if err != nil {
		s.logError(r, "egress apply failed", err)
	}
	return err
}

// routingDone applies routing and redirects with the success toast, or the
// "saved, not yet applied" toast when the kernel apply failed.
func (s *Server) routingDone(w http.ResponseWriter, r *http.Request, path, key string, targ ...string) {
	if err := s.egressApply(r); err != nil {
		s.redirectToast(w, r, path, "routing.toast.saved_pending")
		return
	}
	s.redirectToast(w, r, path, key, targ...)
}

// egressFieldError maps a store error to (field, localized message).
func (s *Server) egressFieldError(r *http.Request, err error) (string, string) {
	if v, ok := egress.AsValidation(err); ok {
		msg := s.t(r, "egress.error."+v.Reason)
		if v.Detail != "" {
			msg += " " + s.t(r, "egress.error.detail", v.Detail)
		}
		return v.Field, msg
	}
	if errors.Is(err, egress.ErrWARPUnavailable) {
		return "", s.t(r, "egress.error.warp_unavailable")
	}
	return "", s.t(r, "common.error_generic")
}

// egressActionFailed handles list-page action failures (toggle, delete, move).
func (s *Server) egressActionFailed(w http.ResponseWriter, r *http.Request, path string, err error) {
	if errors.Is(err, egress.ErrNotFound) {
		s.redirectToast(w, r, path, "common.error_not_found")
		return
	}
	if v, ok := egress.AsValidation(err); ok {
		msg := s.t(r, "egress.error."+v.Reason)
		if v.Detail != "" {
			msg += " " + s.t(r, "egress.error.detail", v.Detail)
		}
		s.redirectErrorRaw(w, r, path, msg)
		return
	}
	s.logError(r, "egress action failed", err)
	s.redirectToast(w, r, path, "common.error_generic")
}

func (s *Server) handleRoutingIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/routing/outbounds", http.StatusSeeOther)
}

// --- outbounds -------------------------------------------------------------------

type outboundCard struct {
	O          *egress.Outbound
	KindKey    string
	State      string // healthy | down | checking | disabled | error
	Status     routing.OutboundStatus
	Latency    string
	ExitIP     string
	Country    string
	Flag       string
	Endpoint   string
	RX, TX     int64
	Counters   bool
	Handshake  *time.Time
	UsedBy     []string
	ProbedAt   *time.Time
	ProbeError string
	Colo       string // Cloudflare data centre that served the probe
	WARPState  string // on | plus | off, from the probe (WARP kind only)
	Via        string // name of the outbound this one is chained through
	// Bridge is the IPv6 site bridge state (routing.OutboundStatus.Bridge,
	// or "pending" before the first apply); ExitIP6 the probed IPv6 exit.
	Bridge   string
	ExitIP6  string
	Bridged  routing.BridgeStats
	HasStats bool
}

type outboundsData struct {
	Tabs     routingTabs
	Cards    []outboundCard
	Healthy  int
	Problems int
	Warnings []routing.Warning
	Site     siteTestData
}

// outboundKindKey labels the transport precisely (WARP vs WARP+, WireGuard
// vs AmneziaWG) for the badge.
func outboundKindKey(o *egress.Outbound) string {
	switch o.Kind {
	case egress.KindWARP:
		// The API reports warp_plus=true for free accounts too; only a paid
		// account type ("limited" quota or "unlimited") is WARP+.
		if o.WARP != nil && (o.WARP.AccountType == "limited" || o.WARP.AccountType == "unlimited") {
			return "routing.kind.warp_plus"
		}
		return "routing.kind.warp"
	case egress.KindWireGuard:
		if o.Tunnel != nil && o.Tunnel.Obfuscation.Enabled {
			return "routing.kind.amneziawg"
		}
		return "routing.kind.wireguard"
	}
	return "routing.kind.source_ip"
}

// countryFlag turns an ISO country code into its regional-indicator emoji.
func countryFlag(code string) string {
	if len(code) != 2 {
		return ""
	}
	var b strings.Builder
	for _, c := range strings.ToUpper(code) {
		if c < 'A' || c > 'Z' {
			return ""
		}
		b.WriteRune(0x1F1E6 + (c - 'A'))
	}
	return b.String()
}

func (s *Server) outboundCards(r *http.Request) ([]outboundCard, routing.Status, error) {
	outbounds, err := s.Egress.ListOutbounds(r.Context())
	if err != nil {
		return nil, routing.Status{}, err
	}
	var st routing.Status
	var bridged map[string]routing.BridgeStats
	if s.EgressRuntime != nil {
		st = s.EgressRuntime.Status()
		bridged = s.EgressRuntime.BridgeStats()
	}
	usage := s.outboundUsage(r)
	names := map[string]string{}
	for _, o := range outbounds {
		names[o.ID] = o.Name
	}
	cards := make([]outboundCard, 0, len(outbounds))
	for _, o := range outbounds {
		c := outboundCard{O: o, KindKey: outboundKindKey(o), UsedBy: usage[o.ID]}
		if o.Tunnel != nil {
			c.Endpoint = o.Tunnel.Endpoint
			if o.Tunnel.Via != "" {
				c.Via = names[o.Tunnel.Via]
			}
		}
		obs, known := st.Outbounds[o.ID]
		c.Status = obs
		if o.Tunnel.Bridges() && o.Enabled {
			c.Bridge = obs.Bridge
			if c.Bridge == "" {
				c.Bridge = "pending"
			}
			if b, ok := bridged[o.ID]; ok {
				c.Bridged, c.HasStats = b, true
			}
		}
		switch {
		case !o.Enabled:
			c.State = "disabled"
		case !known:
			c.State = "checking"
		case obs.LinkError != "":
			c.State = "error"
		case !obs.Probed:
			c.State = "checking"
		case obs.Healthy:
			c.State = "healthy"
		default:
			c.State = "down"
		}
		if obs.Probed {
			if obs.Probe.OK {
				c.Latency = strconv.FormatInt(obs.Probe.Latency.Milliseconds(), 10)
				c.ExitIP, c.Country, c.Flag = obs.Probe.ExitIP, obs.Probe.Country, countryFlag(obs.Probe.Country)
				c.Colo, c.ExitIP6 = obs.Probe.Colo, obs.Probe.ExitIP6
				if o.Kind == egress.KindWARP {
					c.WARPState = obs.Probe.WARP
				}
			} else {
				c.ProbeError = obs.Probe.Error
			}
			at := obs.Probe.At
			c.ProbedAt = &at
		}
		if !obs.LastHandshake.IsZero() {
			hs := obs.LastHandshake
			c.Handshake = &hs
		}
		if obs.Link != "" {
			if rx, tx, ok := routing.LinkCounters(obs.Link); ok {
				c.RX, c.TX, c.Counters = int64(rx), int64(tx), true
			}
		}
		cards = append(cards, c)
	}
	return cards, st, nil
}

// outboundUsage names everything that references each outbound: rules,
// balancers, outbounds chained through it and the default route. These are
// exactly the references that block deletion.
func (s *Server) outboundUsage(r *http.Request) map[string][]string {
	usage := map[string][]string{}
	if outbounds, err := s.Egress.ListOutbounds(r.Context()); err == nil {
		for _, o := range outbounds {
			if o.Tunnel != nil && o.Tunnel.Via != "" {
				usage[o.Tunnel.Via] = append(usage[o.Tunnel.Via], o.Name)
			}
		}
	}
	if rules, err := s.Egress.ListRules(r.Context()); err == nil {
		for _, rule := range rules {
			if rule.Target.Kind == egress.TargetOutbound {
				usage[rule.Target.ID] = append(usage[rule.Target.ID], rule.Name)
			}
		}
	}
	if balancers, err := s.Egress.ListBalancers(r.Context()); err == nil {
		for _, b := range balancers {
			for _, m := range b.Members {
				usage[m.OutboundID] = append(usage[m.OutboundID], b.Name)
			}
		}
	}
	if st, err := s.Egress.GetSettings(r.Context()); err == nil && st.DefaultTarget.Kind == egress.TargetOutbound {
		usage[st.DefaultTarget.ID] = append(usage[st.DefaultTarget.ID], s.t(r, "routing.rules.default"))
	}
	return usage
}

func (s *Server) handleOutbounds(w http.ResponseWriter, r *http.Request) {
	cards, st, err := s.outboundCards(r)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	d := outboundsData{Tabs: s.loadRoutingTabs(r, "outbounds"), Cards: cards, Warnings: st.Warnings, Site: s.siteTestForm(r)}
	for _, c := range cards {
		switch c.State {
		case "healthy":
			d.Healthy++
		case "down", "error":
			d.Problems++
		}
	}
	_ = s.render(w, r, "routing_outbounds", "app", d)
}

// handleOutboundTest probes one outbound now. HTMX swaps the refreshed card;
// a plain form post returns to the list with a toast.
func (s *Server) handleOutboundTest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.EgressRuntime != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*routing.ProbeTimeout)
		_, err := s.EgressRuntime.Test(ctx, id)
		cancel()
		if err != nil && !errors.Is(err, egress.ErrNotFound) {
			s.logError(r, "egress test failed", err)
		}
	}
	if !isHX(r) {
		s.redirectToast(w, r, "/routing/outbounds", "routing.toast.tested")
		return
	}
	cards, _, err := s.outboundCards(r)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	for _, c := range cards {
		if c.O.ID == id {
			_ = s.partial(w, r, "routing_outbounds", "outbound_card_partial", c)
			return
		}
	}
	http.NotFound(w, r)
}

type outboundFormData struct {
	Tabs routingTabs
	Kind egress.Kind
	O    *egress.Outbound // nil on create
	Form operationalForm
	// Via lists the outbounds this one can be chained through.
	Via []viaOption
	// Endpoints are suggested WARP endpoints.
	Endpoints []string
	// Chained is set when other outbounds go through this one (it cannot be
	// chained itself).
	Chained bool
	// AccountKey labels the WARP account type ("" when unknown).
	AccountKey string
	// IPv6Capable reports that the IPv6 site bridge can be turned on: a WARP
	// outbound (its address is fetched when missing), a config not imported
	// yet, or an imported config with an IPv6 address.
	IPv6Capable bool
}

// warpAccountKey is the catalog key of a WARP account type, or "" for a type
// this version does not know (shown verbatim).
func warpAccountKey(accountType string) string {
	switch accountType {
	case "free", "limited", "unlimited", "team":
		return "routing.warp_account." + accountType
	}
	return ""
}

type viaOption struct {
	ID, Name string
	Off      bool
}

// outboundForm completes the form data with the chain choices: tunneled
// outbounds other than this one that are not chained themselves.
func (s *Server) outboundForm(r *http.Request, kind egress.Kind, o *egress.Outbound, f operationalForm) outboundFormData {
	d := outboundFormData{Tabs: s.loadRoutingTabs(r, "outbounds"), Kind: kind, O: o, Form: f}
	if kind == egress.KindWARP {
		d.Endpoints = egress.WARPEndpoints
		if o != nil && o.WARP != nil {
			d.AccountKey = warpAccountKey(o.WARP.AccountType)
		}
	}
	if !kind.Tunneled() {
		return d
	}
	d.IPv6Capable = kind == egress.KindWARP || o == nil || (o.Tunnel != nil && o.Tunnel.Address6 != "")
	all, err := s.Egress.ListOutbounds(r.Context())
	if err != nil {
		return d
	}
	for _, other := range all {
		if o != nil && other.Tunnel != nil && other.Tunnel.Via == o.ID {
			d.Chained = true
		}
		if (o != nil && other.ID == o.ID) || !other.Kind.Tunneled() || other.Tunnel == nil || other.Tunnel.Via != "" {
			continue
		}
		d.Via = append(d.Via, viaOption{ID: other.ID, Name: other.Name, Off: !other.Enabled})
	}
	return d
}

func outboundFormDefaults(kind egress.Kind) map[string]string {
	name := ""
	if kind == egress.KindWARP {
		name = "WARP"
	}
	return map[string]string{"kind": string(kind), "name": name, "enabled": "1", "endpoint": "", "mtu": "", "source_ip": "", "config": "", "license": "", "via": "", "ipv6_sites": ""}
}

func (s *Server) handleOutboundNew(w http.ResponseWriter, r *http.Request) {
	kind := egress.Kind(r.URL.Query().Get("kind"))
	if !kind.Valid() {
		_ = s.render(w, r, "routing_outbound_kind", "app", struct{ Tabs routingTabs }{s.loadRoutingTabs(r, "outbounds")})
		return
	}
	_ = s.render(w, r, "routing_outbound_form", "app", s.outboundForm(r, kind, nil, operationalForm{Values: outboundFormDefaults(kind), Fields: map[string]string{}}))
}

// readConfigField returns the pasted config or, when a file was chosen, the
// uploaded .conf (bounded).
func readConfigField(r *http.Request) (string, error) {
	if file, _, err := r.FormFile("config_file"); err == nil {
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, egress.MaxConfigBytes+1))
		if err != nil {
			return "", err
		}
		if len(data) > 0 {
			return string(data), nil
		}
	}
	return r.PostFormValue("config"), nil
}

func parseRoutingForm(r *http.Request) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(1 << 20)
	}
	return r.ParseForm()
}

func (s *Server) handleOutboundCreate(w http.ResponseWriter, r *http.Request) {
	if err := parseRoutingForm(r); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	kind := egress.Kind(r.PostFormValue("kind"))
	name := strings.TrimSpace(r.PostFormValue("name"))
	var (
		o   *egress.Outbound
		err error
	)
	switch kind {
	case egress.KindWARP:
		o, err = s.Egress.CreateWARP(r.Context(), name, r.PostFormValue("license"), r.PostFormValue("endpoint"))
	case egress.KindWireGuard:
		var conf string
		if conf, err = readConfigField(r); err == nil {
			o, err = s.Egress.CreateWireGuard(r.Context(), name, conf)
		}
	case egress.KindSourceIP:
		o, err = s.Egress.CreateSourceIP(r.Context(), name, r.PostFormValue("source_ip"))
	default:
		s.badRequest(w, r, "outbound kind")
		return
	}
	if err != nil {
		s.outboundFormError(w, r, kind, nil, err)
		return
	}
	if after := createExtras(r, kind); after != nil {
		// The outbound exists now; a chain that no longer validates (its hop
		// was removed meanwhile) or an IPv6 bridge without an IPv6 address
		// is reported on the edit form.
		if _, err := s.Egress.UpdateOutbound(r.Context(), o.ID, *after); err != nil {
			s.audit(r, "egress.outbound_created", o.ID, map[string]any{"name": o.Name, "kind": string(o.Kind)})
			_ = s.egressApply(r)
			s.outboundFormError(w, r, kind, o, err)
			return
		}
	}
	s.audit(r, "egress.outbound_created", o.ID, map[string]any{"name": o.Name, "kind": string(o.Kind)})
	s.routingDone(w, r, "/routing/outbounds", "routing.toast.outbound_created", o.Name)
}

// createExtras are the settings applied right after an outbound is created
// (they need its ID): the chain and the IPv6 site bridge. nil when none.
func createExtras(r *http.Request, kind egress.Kind) *egress.OutboundInput {
	if !kind.Tunneled() {
		return nil
	}
	var in egress.OutboundInput
	if via := strings.TrimSpace(r.PostFormValue("via")); via != "" {
		in.Via = &via
	}
	if r.PostFormValue("ipv6_sites") == "1" {
		on := true
		in.IPv6Sites = &on
	}
	if in.Via == nil && in.IPv6Sites == nil {
		return nil
	}
	return &in
}

func (s *Server) outboundFormError(w http.ResponseWriter, r *http.Request, kind egress.Kind, o *egress.Outbound, err error) {
	field, msg := s.egressFieldError(r, err)
	if !errors.Is(err, egress.ErrWARPUnavailable) {
		if _, ok := egress.AsValidation(err); !ok {
			s.logError(r, "egress outbound save failed", err)
		}
	} else {
		s.logError(r, "warp registration failed", err)
	}
	f := operationalForm{Values: outboundFormDefaults(kind), Fields: map[string]string{}, Error: msg}
	for key := range f.Values {
		if key == "config" || key == "license" {
			continue // secrets are never echoed back
		}
		if v := r.PostFormValue(key); v != "" || key != "kind" {
			f.Values[key] = v
		}
	}
	f.Values["kind"] = string(kind)
	if field != "" {
		f.Fields[field] = msg
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = s.render(w, r, "routing_outbound_form", "app", s.outboundForm(r, kind, o, f))
}

func outboundEditValues(o *egress.Outbound) map[string]string {
	v := outboundFormDefaults(o.Kind)
	v["name"] = o.Name
	if !o.Enabled {
		v["enabled"] = "0"
	}
	if o.Tunnel != nil {
		v["endpoint"], v["mtu"], v["via"] = o.Tunnel.Endpoint, strconv.Itoa(o.Tunnel.MTU), o.Tunnel.Via
		if o.Tunnel.IPv6Sites {
			v["ipv6_sites"] = "1"
		}
	}
	v["source_ip"] = o.SourceIP
	return v
}

func (s *Server) handleOutboundEditPage(w http.ResponseWriter, r *http.Request) {
	o, err := s.Egress.GetOutbound(r.Context(), r.PathValue("id"))
	if err != nil {
		s.egressActionFailed(w, r, "/routing/outbounds", err)
		return
	}
	_ = s.render(w, r, "routing_outbound_form", "app", s.outboundForm(r, o.Kind, o, operationalForm{Values: outboundEditValues(o), Fields: map[string]string{}}))
}

func (s *Server) handleOutboundUpdate(w http.ResponseWriter, r *http.Request) {
	if err := parseRoutingForm(r); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	id := r.PathValue("id")
	o, err := s.Egress.GetOutbound(r.Context(), id)
	if err != nil {
		s.egressActionFailed(w, r, "/routing/outbounds", err)
		return
	}
	enabled := r.PostFormValue("enabled") != "0"
	in := egress.OutboundInput{Name: r.PostFormValue("name"), Enabled: &enabled, Endpoint: r.PostFormValue("endpoint"), SourceIP: r.PostFormValue("source_ip")}
	if o.Kind.Tunneled() && r.PostForm.Has("via") {
		via := r.PostFormValue("via")
		in.Via = &via
	}
	if o.Kind.Tunneled() && r.PostForm.Has("ipv6_sites_form") {
		on := r.PostFormValue("ipv6_sites") == "1"
		in.IPv6Sites = &on
	}
	if raw := strings.TrimSpace(r.PostFormValue("mtu")); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil {
			s.outboundFormError(w, r, o.Kind, o, &egress.ValidationError{Field: "mtu", Reason: "mtu_invalid"})
			return
		}
		in.MTU = n
	}
	if in.Name == "" {
		in.Name = " " // explicit empty name is a validation error, not "keep"
	}
	conf := ""
	if o.Kind == egress.KindWireGuard {
		if c, err := readConfigField(r); err == nil && strings.TrimSpace(c) != "" {
			conf = c
		}
	}
	license := ""
	if o.Kind == egress.KindWARP {
		license = strings.TrimSpace(r.PostFormValue("license"))
	}
	// Validate the config and license before the first write, so a rejected
	// form leaves the stored outbound, and therefore the kernel, unchanged.
	if conf != "" {
		if _, err := egress.ParseClientConfig(conf); err != nil {
			s.outboundFormError(w, r, o.Kind, o, err)
			return
		}
	}
	if license != "" {
		if err := egress.ValidateLicense(license); err != nil {
			s.outboundFormError(w, r, o.Kind, o, err)
			return
		}
	}
	if _, err := s.Egress.UpdateOutbound(r.Context(), id, in); err != nil {
		s.outboundFormError(w, r, o.Kind, o, err)
		return
	}
	// Later steps can still fail (storage, WARP rejecting the license); the
	// fields saved above are then applied before the form is shown again.
	if conf != "" {
		if _, err := s.Egress.ReplaceWireGuardConfig(r.Context(), id, conf); err != nil {
			_ = s.egressApply(r)
			s.outboundFormError(w, r, o.Kind, o, err)
			return
		}
	}
	if license != "" {
		if _, err := s.Egress.UpdateWARPLicense(r.Context(), id, license); err != nil {
			_ = s.egressApply(r)
			s.outboundFormError(w, r, o.Kind, o, err)
			return
		}
	}
	s.audit(r, "egress.outbound_updated", id, map[string]any{"name": strings.TrimSpace(in.Name)})
	s.routingDone(w, r, "/routing/outbounds", "routing.toast.saved")
}

func (s *Server) handleOutboundToggle(enable bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, err := s.Egress.UpdateOutbound(r.Context(), id, egress.OutboundInput{Enabled: &enable}); err != nil {
			s.egressActionFailed(w, r, "/routing/outbounds", err)
			return
		}
		s.audit(r, "egress.outbound_updated", id, map[string]any{"enabled": enable})
		s.routingDone(w, r, "/routing/outbounds", "routing.toast.saved")
	}
}

// handleOutboundRenew registers a new WARP identity for a WARP outbound: new
// keys and account, usually a new exit address (for example after sites
// start blocking the current one). With the routing runtime it keeps
// re-registering, a few times at most, while the probed exit stays the same.
func (s *Server) handleOutboundRenew(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.EgressRuntime == nil {
		o, err := s.Egress.RenewWARP(r.Context(), id)
		if s.renewFailed(w, r, err) {
			return
		}
		s.audit(r, "egress.outbound_renewed", id, map[string]any{"name": o.Name})
		s.routingDone(w, r, "/routing/outbounds", "routing.toast.warp_renewed", o.Name)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), renewBudget)
	defer cancel()
	res, err := s.EgressRuntime.RotateWARP(ctx, id)
	if s.renewFailed(w, r, err) {
		return
	}
	s.audit(r, "egress.outbound_renewed", id, map[string]any{"name": res.Name, "attempts": res.Attempts, "exit_changed": res.Changed})
	// RotateWARP already applied the new identity.
	var msg string
	switch {
	case res.Changed && res.OldIP != "":
		msg = s.t(r, "routing.toast.warp_rotated", res.Name, res.OldIP, res.NewIP)
	case res.Changed:
		msg = s.t(r, "routing.toast.warp_rotated_new", res.Name, res.NewIP)
	case res.NewIP == "":
		msg = s.t(r, "routing.toast.warp_renewed_unchecked", res.Name)
	default:
		msg = s.t(r, "routing.toast.warp_same_ip", res.Name, res.Attempts)
	}
	s.redirectToastRaw(w, r, "/routing/outbounds", msg)
}

// renewBudget bounds a rotation (a few registrations, applies and probes)
// inside the panel server's 60-second write timeout.
const renewBudget = 50 * time.Second

func (s *Server) renewFailed(w http.ResponseWriter, r *http.Request, err error) bool {
	if errors.Is(err, egress.ErrWARPUnavailable) {
		s.logError(r, "warp renewal failed", err)
		s.redirectToast(w, r, "/routing/outbounds", "routing.toast.warp_failed")
		return true
	}
	if err != nil {
		s.egressActionFailed(w, r, "/routing/outbounds", err)
		return true
	}
	return false
}

func (s *Server) handleOutboundDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Egress.DeleteOutbound(r.Context(), id); err != nil {
		s.egressActionFailed(w, r, "/routing/outbounds", err)
		return
	}
	s.audit(r, "egress.outbound_deleted", id, nil)
	s.routingDone(w, r, "/routing/outbounds", "routing.toast.deleted")
}

// --- target options shared by rules and the default route ----------------------------

type targetOption struct {
	Value    string
	Label    string
	Group    string // direct | block | outbound | balancer
	Selected bool
	Off      bool // disabled object: still selectable, labelled
}

func (s *Server) targetOptions(r *http.Request, selected egress.Target, allowBlock bool) []targetOption {
	sel := selected.String()
	opts := []targetOption{{Value: "direct", Label: s.t(r, "routing.target.direct"), Group: "direct", Selected: sel == "direct"}}
	if allowBlock {
		opts = append(opts, targetOption{Value: "block", Label: s.t(r, "routing.target.block"), Group: "block", Selected: sel == "block"})
	}
	if outbounds, err := s.Egress.ListOutbounds(r.Context()); err == nil {
		for _, o := range outbounds {
			v := egress.Target{Kind: egress.TargetOutbound, ID: o.ID}.String()
			opts = append(opts, targetOption{Value: v, Label: o.Name, Group: "outbound", Selected: sel == v, Off: !o.Enabled})
		}
	}
	if balancers, err := s.Egress.ListBalancers(r.Context()); err == nil {
		for _, b := range balancers {
			v := egress.Target{Kind: egress.TargetBalancer, ID: b.ID}.String()
			opts = append(opts, targetOption{Value: v, Label: b.Name, Group: "balancer", Selected: sel == v, Off: !b.Enabled})
		}
	}
	return opts
}

// targetLabel renders a target for list rows.
func (s *Server) targetLabel(r *http.Request, t egress.Target, names map[string]string) string {
	switch t.Kind {
	case egress.TargetDirect:
		return s.t(r, "routing.target.direct")
	case egress.TargetBlock:
		return s.t(r, "routing.target.block")
	}
	if n, ok := names[t.String()]; ok {
		return n
	}
	return s.t(r, "routing.target.missing")
}

func (s *Server) targetNames(r *http.Request) map[string]string {
	names := map[string]string{}
	if outbounds, err := s.Egress.ListOutbounds(r.Context()); err == nil {
		for _, o := range outbounds {
			names[egress.Target{Kind: egress.TargetOutbound, ID: o.ID}.String()] = o.Name
		}
	}
	if balancers, err := s.Egress.ListBalancers(r.Context()); err == nil {
		for _, b := range balancers {
			names[egress.Target{Kind: egress.TargetBalancer, ID: b.ID}.String()] = b.Name
		}
	}
	return names
}
