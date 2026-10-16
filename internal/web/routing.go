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
		s.redirectToastRaw(w, r, path, msg)
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
}

type outboundsData struct {
	Tabs     routingTabs
	Cards    []outboundCard
	Healthy  int
	Problems int
	Warnings []routing.Warning
}

// outboundKindKey labels the transport precisely (WARP vs WARP+, WireGuard
// vs AmneziaWG) for the badge.
func outboundKindKey(o *egress.Outbound) string {
	switch o.Kind {
	case egress.KindWARP:
		if o.WARP != nil && o.WARP.Plus {
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
	if s.EgressRuntime != nil {
		st = s.EgressRuntime.Status()
	}
	usage := s.outboundUsage(r)
	cards := make([]outboundCard, 0, len(outbounds))
	for _, o := range outbounds {
		c := outboundCard{O: o, KindKey: outboundKindKey(o), UsedBy: usage[o.ID]}
		if o.Tunnel != nil {
			c.Endpoint = o.Tunnel.Endpoint
		}
		obs, known := st.Outbounds[o.ID]
		c.Status = obs
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

// outboundUsage names the rules and balancers that reference each outbound.
func (s *Server) outboundUsage(r *http.Request) map[string][]string {
	usage := map[string][]string{}
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
	return usage
}

func (s *Server) handleOutbounds(w http.ResponseWriter, r *http.Request) {
	cards, st, err := s.outboundCards(r)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	d := outboundsData{Tabs: s.loadRoutingTabs(r, "outbounds"), Cards: cards, Warnings: st.Warnings}
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
}

func outboundFormDefaults(kind egress.Kind) map[string]string {
	name := ""
	if kind == egress.KindWARP {
		name = "WARP"
	}
	return map[string]string{"kind": string(kind), "name": name, "enabled": "1", "endpoint": "", "mtu": "", "source_ip": "", "config": "", "license": ""}
}

func (s *Server) handleOutboundNew(w http.ResponseWriter, r *http.Request) {
	kind := egress.Kind(r.URL.Query().Get("kind"))
	if !kind.Valid() {
		_ = s.render(w, r, "routing_outbound_kind", "app", struct{ Tabs routingTabs }{s.loadRoutingTabs(r, "outbounds")})
		return
	}
	_ = s.render(w, r, "routing_outbound_form", "app", outboundFormData{
		Tabs: s.loadRoutingTabs(r, "outbounds"), Kind: kind, Form: operationalForm{Values: outboundFormDefaults(kind), Fields: map[string]string{}},
	})
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
	s.audit(r, "egress.outbound_created", o.ID, map[string]any{"name": o.Name, "kind": string(o.Kind)})
	s.routingDone(w, r, "/routing/outbounds", "routing.toast.outbound_created", o.Name)
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
	_ = s.render(w, r, "routing_outbound_form", "app", outboundFormData{Tabs: s.loadRoutingTabs(r, "outbounds"), Kind: kind, O: o, Form: f})
}

func outboundEditValues(o *egress.Outbound) map[string]string {
	v := outboundFormDefaults(o.Kind)
	v["name"] = o.Name
	if !o.Enabled {
		v["enabled"] = "0"
	}
	if o.Tunnel != nil {
		v["endpoint"], v["mtu"] = o.Tunnel.Endpoint, strconv.Itoa(o.Tunnel.MTU)
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
	_ = s.render(w, r, "routing_outbound_form", "app", outboundFormData{
		Tabs: s.loadRoutingTabs(r, "outbounds"), Kind: o.Kind, O: o, Form: operationalForm{Values: outboundEditValues(o), Fields: map[string]string{}},
	})
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
	if _, err := s.Egress.UpdateOutbound(r.Context(), id, in); err != nil {
		s.outboundFormError(w, r, o.Kind, o, err)
		return
	}
	if o.Kind == egress.KindWireGuard {
		conf, err := readConfigField(r)
		if err == nil && strings.TrimSpace(conf) != "" {
			if _, err := s.Egress.ReplaceWireGuardConfig(r.Context(), id, conf); err != nil {
				s.outboundFormError(w, r, o.Kind, o, err)
				return
			}
		}
	}
	if o.Kind == egress.KindWARP && strings.TrimSpace(r.PostFormValue("license")) != "" {
		if _, err := s.Egress.UpdateWARPLicense(r.Context(), id, r.PostFormValue("license")); err != nil {
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
