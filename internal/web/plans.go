package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/plan"
)

// planRow decorates a plan for the list (usage count + profile name).
type planRow struct {
	P         *plan.Plan
	Users     int
	IfaceName string
}

type plansData struct {
	Rows []planRow
}

func (s *Server) handlePlanList(w http.ResponseWriter, r *http.Request) {
	plans, err := s.Plans.List(r.Context())
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	ids := make([]string, 0, len(plans))
	for _, p := range plans {
		ids = append(ids, p.ID)
	}
	counts, err := s.Users.CountForPlans(r.Context(), ids)
	if err != nil {
		s.logError(r, "plan user counts", err)
	}
	ifaceNames := map[string]string{}
	if refs, err := s.ifaceRefs(r); err == nil {
		for _, ref := range refs {
			ifaceNames[ref.ID] = ref.Name
		}
	}
	rows := make([]planRow, 0, len(plans))
	for _, p := range plans {
		rows = append(rows, planRow{
			P:         p,
			Users:     counts[p.ID],
			IfaceName: ifaceNames[deref(p.InterfaceID)],
		})
	}
	_ = s.render(w, r, "plans", "app", plansData{Rows: rows})
}

// planFormData backs the new/edit page. P is nil on create; Days is the
// raw duration-day number for the input (humanized format hides precision).
type planFormData struct {
	P      *plan.Plan
	Days   string
	Ifaces []ifaceRef
}

func (s *Server) ifaceRefs(r *http.Request) ([]ifaceRef, error) {
	ifaces, err := s.Ifaces.List(r.Context())
	if err != nil {
		return nil, err
	}
	refs := make([]ifaceRef, 0, len(ifaces))
	for _, i := range ifaces {
		refs = append(refs, ifaceRef{ID: i.ID, Name: i.Name})
	}
	return refs, nil
}

func (s *Server) handlePlanNew(w http.ResponseWriter, r *http.Request) {
	refs, err := s.ifaceRefs(r)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	_ = s.render(w, r, "plan_form", "app", planFormData{Ifaces: refs})
}

func (s *Server) handlePlanEditPage(w http.ResponseWriter, r *http.Request) {
	p, err := s.Plans.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	refs, err := s.ifaceRefs(r)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	_ = s.render(w, r, "plan_form", "app", planFormData{P: p, Days: durationDays(p.DurationSeconds), Ifaces: refs})
}

// planInputFromForm parses the plan form. Forms submit every field, so
// "empty" means "clear" on edit and "absent/unlimited" on create — the same
// tri-state mapping the user form uses.
func planInputFromForm(r *http.Request, isEdit bool) (plan.Input, error) {
	in := plan.Input{Name: strings.TrimSpace(r.PostFormValue("name"))}

	gb, err := parseGB(r.PostFormValue("traffic_limit_gb"))
	if err != nil {
		return in, errInvalid
	}
	in.TrafficLimitBytes = limitOpt64(gb, isEdit)
	down, err := parseKbps(r.PostFormValue("speed_down"))
	if err != nil {
		return in, errInvalid
	}
	in.SpeedLimitDownKbps = limitOptI(down, isEdit)
	up, err := parseKbps(r.PostFormValue("speed_up"))
	if err != nil {
		return in, errInvalid
	}
	in.SpeedLimitUpKbps = limitOptI(up, isEdit)
	dl, err := parseInt(r.PostFormValue("device_limit"))
	if err != nil {
		return in, errInvalid
	}
	in.DeviceLimit = limitOptI(dl, isEdit)

	if v := r.PostFormValue("interface"); v == "" {
		in.InterfaceID = clearOpt(isEdit)
	} else {
		in.InterfaceID = domain.OptString{Set: true, Value: v}
	}

	if v := r.PostFormValue("duration_days"); v != "" {
		days, err := strconv.ParseFloat(v, 64)
		if err != nil || days <= 0 || days > 3650 {
			return in, errInvalid
		}
		secs := int64(days*86400 + 0.5)
		in.DurationSeconds = &secs
	}

	switch r.PostFormValue("start_policy") {
	case "first_connection":
		in.StartPolicy = domain.StartFirstConnection
	default:
		in.StartPolicy = domain.StartImmediate
	}
	enabled := r.PostFormValue("enabled") != "0"
	in.Enabled = &enabled
	return in, nil
}

func (s *Server) handlePlanCreate(w http.ResponseWriter, r *http.Request) {
	in, err := planInputFromForm(r, false)
	if err != nil {
		s.badRequest(w, r, "plan form")
		return
	}
	p, err := s.Plans.Create(r.Context(), in)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "plan.created", p.ID, map[string]any{"name": p.Name})
	s.redirectToast(w, r, "/plans", "plans.toast.created")
}

func (s *Server) handlePlanUpdate(w http.ResponseWriter, r *http.Request) {
	in, err := planInputFromForm(r, true)
	if err != nil {
		s.badRequest(w, r, "plan form")
		return
	}
	p, err := s.Plans.Update(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "plan.updated", p.ID, map[string]any{"name": p.Name})
	s.redirectToast(w, r, "/plans", "plans.toast.updated")
}

func (s *Server) handlePlanEnable(w http.ResponseWriter, r *http.Request) {
	s.planToggle(w, r, true)
}

func (s *Server) handlePlanDisable(w http.ResponseWriter, r *http.Request) {
	s.planToggle(w, r, false)
}

func (s *Server) planToggle(w http.ResponseWriter, r *http.Request, enable bool) {
	id := r.PathValue("id")
	if _, err := s.Plans.Update(r.Context(), id, plan.Input{Enabled: &enable}); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "plan.updated", id, nil)
	s.redirectToast(w, r, "/plans", "plans.toast.toggled")
}

func (s *Server) handlePlanDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Plans.Delete(r.Context(), id); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "plan.deleted", id, nil)
	s.redirectToast(w, r, "/plans", "plans.toast.deleted")
}
