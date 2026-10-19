package web

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
	"github.com/Sir-Adnan/wg-guard/internal/egress/routing"
)

type balancerMember struct {
	Name        string
	Kind        string
	State       string // healthy | down | checking | disabled
	Weight      int
	Missing     bool
	Connections int64
	Counted     bool
}

type balancerCard struct {
	B       *egress.Balancer
	Members []balancerMember
	Live    int
	UsedBy  []string
}

type balancersData struct {
	Tabs      routingTabs
	Cards     []balancerCard
	Outbounds int
}

func memberState(o *egress.Outbound, st routing.Status) string {
	if !o.Enabled {
		return "disabled"
	}
	obs, ok := st.Outbounds[o.ID]
	switch {
	case !ok || (!obs.Probed && obs.LinkError == ""):
		return "checking"
	case obs.Healthy:
		return "healthy"
	}
	return "down"
}

func (s *Server) handleBalancers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	balancers, err := s.Egress.ListBalancers(ctx)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	outbounds, err := s.Egress.ListOutbounds(ctx)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	byID := map[string]*egress.Outbound{}
	for _, o := range outbounds {
		byID[o.ID] = o
	}
	var st routing.Status
	if s.EgressRuntime != nil {
		st = s.EgressRuntime.Status()
	}
	usage := map[string][]string{}
	if rules, err := s.Egress.ListRules(ctx); err == nil {
		for _, rule := range rules {
			if rule.Target.Kind == egress.TargetBalancer {
				usage[rule.Target.ID] = append(usage[rule.Target.ID], rule.Name)
			}
		}
	}
	if settings, err := s.Egress.GetSettings(ctx); err == nil && settings.DefaultTarget.Kind == egress.TargetBalancer {
		usage[settings.DefaultTarget.ID] = append(usage[settings.DefaultTarget.ID], s.t(r, "routing.rules.default_short"))
	}
	d := balancersData{Tabs: s.loadRoutingTabs(r, "balancers"), Outbounds: len(outbounds)}
	for _, b := range balancers {
		card := balancerCard{B: b, UsedBy: usage[b.ID]}
		for _, m := range b.Members {
			o, ok := byID[m.OutboundID]
			if !ok {
				card.Members = append(card.Members, balancerMember{Missing: true, Weight: m.Weight})
				continue
			}
			bm := balancerMember{Name: o.Name, Kind: outboundKindKey(o), State: memberState(o, st), Weight: m.Weight}
			if obs, ok := st.Outbounds[o.ID]; ok && obs.Counted && b.Strategy == egress.StrategyLeastConn {
				bm.Connections, bm.Counted = obs.Connections, true
			}
			if bm.State == "healthy" || bm.State == "checking" {
				card.Live++
			}
			card.Members = append(card.Members, bm)
		}
		d.Cards = append(d.Cards, card)
	}
	_ = s.render(w, r, "routing_balancers", "app", d)
}

type memberOption struct {
	ID, Name, Kind string
	Checked        bool
	Weight         string
	Order          int // failover priority shown in the form (1 = first)
	Enabled        bool
}

type balancerFormData struct {
	Tabs    routingTabs
	B       *egress.Balancer
	Form    operationalForm
	Members []memberOption
}

func (s *Server) balancerFormData(r *http.Request, b *egress.Balancer, form operationalForm, chosen map[string]int) balancerFormData {
	d := balancerFormData{Tabs: s.loadRoutingTabs(r, "balancers"), B: b, Form: form}
	outbounds, _ := s.Egress.ListOutbounds(r.Context())
	byID := map[string]*egress.Outbound{}
	for _, o := range outbounds {
		byID[o.ID] = o
	}
	option := func(o *egress.Outbound) memberOption {
		w, checked := chosen[o.ID]
		opt := memberOption{ID: o.ID, Name: o.Name, Kind: outboundKindKey(o), Checked: checked, Weight: "1", Enabled: o.Enabled}
		if checked && w > 0 {
			opt.Weight = strconv.Itoa(w)
		}
		return opt
	}
	// Stored members first, in their (failover) order; then the rest.
	placed := map[string]bool{}
	if b != nil {
		for _, m := range b.Members {
			if o, ok := byID[m.OutboundID]; ok && !placed[o.ID] {
				placed[o.ID] = true
				d.Members = append(d.Members, option(o))
			}
		}
	}
	for _, o := range outbounds {
		if !placed[o.ID] {
			d.Members = append(d.Members, option(o))
		}
	}
	for i := range d.Members {
		d.Members[i].Order = i + 1
	}
	return d
}

func (s *Server) handleBalancerNew(w http.ResponseWriter, r *http.Request) {
	form := operationalForm{Values: map[string]string{"name": "", "strategy": "sticky", "fallback": "direct", "enabled": "1"}, Fields: map[string]string{}}
	_ = s.render(w, r, "routing_balancer_form", "app", s.balancerFormData(r, nil, form, map[string]int{}))
}

func (s *Server) handleBalancerEditPage(w http.ResponseWriter, r *http.Request) {
	b, err := s.Egress.GetBalancer(r.Context(), r.PathValue("id"))
	if err != nil {
		s.egressActionFailed(w, r, "/routing/balancers", err)
		return
	}
	enabled := "1"
	if !b.Enabled {
		enabled = "0"
	}
	chosen := map[string]int{}
	for _, m := range b.Members {
		chosen[m.OutboundID] = m.Weight
	}
	form := operationalForm{Values: map[string]string{"name": b.Name, "strategy": string(b.Strategy), "fallback": string(b.Fallback), "enabled": enabled}, Fields: map[string]string{}}
	_ = s.render(w, r, "routing_balancer_form", "app", s.balancerFormData(r, b, form, chosen))
}

// balancerInputFromForm reads members in submitted order (form order is the
// failover order).
func balancerInputFromForm(r *http.Request) (egress.BalancerInput, map[string]int) {
	in := egress.BalancerInput{
		Name: r.PostFormValue("name"), Strategy: egress.Strategy(r.PostFormValue("strategy")),
		Fallback: egress.Fallback(r.PostFormValue("fallback")), Enabled: r.PostFormValue("enabled") != "0",
	}
	chosen := map[string]int{}
	order := map[string]int{}
	for i, id := range r.Form["member"] {
		weight, err := strconv.Atoi(r.PostFormValue("weight_" + id))
		if err != nil {
			weight = 1
		}
		in.Members = append(in.Members, egress.Member{OutboundID: id, Weight: weight})
		chosen[id] = weight
		// Failover priority: the number typed, else the position shown.
		order[id] = i + 1
		if n, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("order_" + id))); err == nil && n > 0 {
			order[id] = n
		}
	}
	sort.SliceStable(in.Members, func(i, j int) bool { return order[in.Members[i].OutboundID] < order[in.Members[j].OutboundID] })
	return in, chosen
}

func (s *Server) handleBalancerSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	id := r.PathValue("id")
	in, chosen := balancerInputFromForm(r)
	var (
		b   *egress.Balancer
		err error
	)
	if id == "" {
		b, err = s.Egress.CreateBalancer(r.Context(), in)
	} else {
		b, err = s.Egress.UpdateBalancer(r.Context(), id, in)
	}
	if err != nil {
		if errors.Is(err, egress.ErrNotFound) {
			s.egressActionFailed(w, r, "/routing/balancers", err)
			return
		}
		field, msg := s.egressFieldError(r, err)
		if _, ok := egress.AsValidation(err); !ok {
			s.logError(r, "egress balancer save failed", err)
		}
		form := submittedOperationalForm(r, map[string]string{"name": "", "strategy": "", "fallback": "", "enabled": ""})
		form.Error = msg
		if field != "" {
			form.Fields[field] = msg
		}
		var existing *egress.Balancer
		if id != "" {
			existing, _ = s.Egress.GetBalancer(r.Context(), id)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = s.render(w, r, "routing_balancer_form", "app", s.balancerFormData(r, existing, form, chosen))
		return
	}
	action := "egress.balancer_created"
	if id != "" {
		action = "egress.balancer_updated"
	}
	s.audit(r, action, b.ID, map[string]any{"name": b.Name, "strategy": string(b.Strategy), "members": len(b.Members)})
	s.routingDone(w, r, "/routing/balancers", "routing.toast.balancer_saved", b.Name)
}

func (s *Server) handleBalancerToggle(enable bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := s.Egress.SetBalancerEnabled(r.Context(), id, enable); err != nil {
			s.egressActionFailed(w, r, "/routing/balancers", err)
			return
		}
		s.audit(r, "egress.balancer_updated", id, map[string]any{"enabled": enable})
		s.routingDone(w, r, "/routing/balancers", "routing.toast.saved")
	}
}

func (s *Server) handleBalancerDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Egress.DeleteBalancer(r.Context(), id); err != nil {
		s.egressActionFailed(w, r, "/routing/balancers", err)
		return
	}
	s.audit(r, "egress.balancer_deleted", id, nil)
	s.routingDone(w, r, "/routing/balancers", "routing.toast.deleted")
}
