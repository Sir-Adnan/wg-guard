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
	P                *plan.Plan
	Users            int
	CountKnown       bool
	IfaceName        string
	IfaceUnavailable bool
}

type plansData struct {
	Rows    []planRow
	Enabled int
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
	} else {
		// Secondary lookup failure is visible in the list and safe to diagnose
		// without exposing database/driver details or submitted configuration.
		s.logError(r, "plan interface references unavailable", nil)
	}
	enabled := 0
	rows := make([]planRow, 0, len(plans))
	for _, p := range plans {
		if p.Enabled {
			enabled++
		}
		rows = append(rows, planRow{
			CountKnown:       err == nil,
			P:                p,
			Users:            counts[p.ID],
			IfaceName:        ifaceNames[deref(p.InterfaceID)],
			IfaceUnavailable: p.InterfaceID != nil && ifaceNames[*p.InterfaceID] == "",
		})
	}
	_ = s.render(w, r, "plans", "app", plansData{Rows: rows, Enabled: enabled})
}

// planFormData backs the new/edit page. P is nil on create; the duration
// prefills from P.DurationSeconds via the view helpers (value + unit).
type planFormData struct {
	Form   operationalForm
	P      *plan.Plan
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
	_ = s.render(w, r, "plan_form", "app", newPlanFormData(nil, refs))
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
	_ = s.render(w, r, "plan_form", "app", newPlanFormData(p, refs))
}

// planInputFromForm parses the plan form. Forms submit every field, so
// "empty" means "clear" on edit and "absent/unlimited" on create — the same
// tri-state mapping the user form uses.
func planInputFromForm(r *http.Request, isEdit bool) (plan.Input, error) {
	in := plan.Input{Name: strings.TrimSpace(r.PostFormValue("name"))}

	quota, err := quotaFromForm(r)
	if err != nil {
		return in, errInvalid
	}
	in.TrafficLimitBytes = limitOpt64(quota, isEdit)
	down, err := parseSpeedMBps(r.PostFormValue("speed_down"))
	if err != nil {
		return in, errInvalid
	}
	in.SpeedLimitDownKbps = limitOptI(down, isEdit)
	up, err := parseSpeedMBps(r.PostFormValue("speed_up"))
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

	if v := r.PostFormValue("duration_value"); v != "" || r.PostFormValue("duration_days") != "" {
		secs, err := durationFromForm(r)
		if err != nil {
			return in, errInvalid
		}
		in.DurationSeconds = secs
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
		s.planFormError(w, r, err)
		return
	}
	p, err := s.Plans.Create(r.Context(), in)
	if err != nil {
		s.planFormError(w, r, err)
		return
	}
	s.audit(r, "plan.created", p.ID, map[string]any{"name": p.Name})
	s.redirectToast(w, r, operationalReturnPath(r, "/plans", "plans.read"), "plans.toast.created")
}

func (s *Server) handlePlanUpdate(w http.ResponseWriter, r *http.Request) {
	in, err := planInputFromForm(r, true)
	if err != nil {
		s.planFormError(w, r, err)
		return
	}
	p, err := s.Plans.Update(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.planFormError(w, r, err)
		return
	}
	s.audit(r, "plan.updated", p.ID, map[string]any{"name": p.Name})
	s.redirectToast(w, r, operationalReturnPath(r, "/plans", "plans.read"), "plans.toast.updated")
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
	s.redirectToast(w, r, operationalReturnPath(r, "/plans", "plans.read"), "plans.toast.toggled")
}

func (s *Server) handlePlanDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Plans.Delete(r.Context(), id); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "plan.deleted", id, nil)
	s.redirectToast(w, r, operationalReturnPath(r, "/plans", "plans.read"), "plans.toast.deleted")
}

func newPlanFormData(p *plan.Plan, refs []ifaceRef) planFormData {
	values := map[string]string{"name": "", "enabled": "1", "traffic_limit_value": "", "traffic_limit_unit": "gb", "duration_value": "", "duration_unit": "days", "device_limit": "", "speed_down": "", "speed_up": "", "interface": "", "start_policy": "immediate"}
	if p != nil {
		v := View{}
		values["name"], values["traffic_limit_value"], values["traffic_limit_unit"] = p.Name, v.QuotaVal(p.TrafficLimitBytes), v.QuotaUnit(p.TrafficLimitBytes)
		values["duration_value"], values["duration_unit"] = v.DurVal(p.DurationSeconds), v.DurUnit(p.DurationSeconds)
		values["device_limit"], values["speed_down"], values["speed_up"] = rawFormInt(p.DeviceLimit), speedMBpsValue(p.SpeedLimitDownKbps), speedMBpsValue(p.SpeedLimitUpKbps)
		values["interface"], values["start_policy"] = deref(p.InterfaceID), string(p.StartPolicy)
		if !p.Enabled {
			values["enabled"] = "0"
		}
	}
	return planFormData{P: p, Ifaces: refs, Form: operationalForm{Values: values}}
}

func (s *Server) planFormError(w http.ResponseWriter, r *http.Request, err error) {
	var p *plan.Plan
	if id := r.PathValue("id"); id != "" {
		var loadErr error
		p, loadErr = s.Plans.Get(r.Context(), id)
		if loadErr != nil {
			s.actionFailed(w, r, loadErr)
			return
		}
	}
	refs, loadErr := s.ifaceRefs(r)
	if loadErr != nil {
		s.actionFailed(w, r, loadErr)
		return
	}
	d := newPlanFormData(p, refs)
	d.Form = submittedOperationalForm(r, d.Form.Values)
	if d.Form.V("enabled") != "0" {
		d.Form.Values["enabled"] = "1"
	}
	if d.Form.V("traffic_limit_value") == "" && r.PostFormValue("traffic_limit_gb") != "" {
		d.Form.Values["traffic_limit_value"], d.Form.Values["traffic_limit_unit"] = r.PostFormValue("traffic_limit_gb"), "gb"
	}
	if d.Form.V("duration_value") == "" && r.PostFormValue("duration_days") != "" {
		d.Form.Values["duration_value"], d.Form.Values["duration_unit"] = r.PostFormValue("duration_days"), "days"
	}
	field := operationalErrorField(err, true)
	if field != "" {
		d.Form.Fields[field] = "common.error_validation"
	}
	if _, e := quotaFromForm(r); e != nil {
		d.Form.Fields["traffic_limit_value"] = "forms.error.quota"
	}
	if _, e := durationFromForm(r); e != nil {
		d.Form.Fields["duration_value"] = "forms.error.duration"
	}
	for _, key := range []string{"speed_down", "speed_up", "device_limit"} {
		var e error
		if key == "device_limit" {
			_, e = parseInt(r.PostFormValue(key))
		} else {
			_, e = parseSpeedMBps(r.PostFormValue(key))
		}
		if e != nil {
			d.Form.Fields[key] = "forms.error.number"
		} else if key == "device_limit" {
			text := strings.TrimSpace(r.PostFormValue(key))
			if text == "" {
				continue
			}
			n, _ := strconv.Atoi(text)
			if n <= 0 {
				d.Form.Fields[key] = "forms.error.number"
			}
		}
	}
	s.operationalFormStatus(w, r, &d.Form, err)
	_ = s.render(w, r, "plan_form", "app", d)
}

func rawFormInt(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
}
