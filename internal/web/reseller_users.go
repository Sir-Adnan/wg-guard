package web

import (
	"net/http"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/integration"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

type resellerUserRow struct {
	User *user.User
	Used int64
}

type resellerUsersData struct {
	Rows       []resellerUserRow
	Search     string
	NextCursor string
}

type resellerUserData struct {
	User      *user.User
	Used      int64
	Devices   []*deviceView
	NextPlan  *integration.NextPlan
	NextPlans []*planRef
}

func (s *Server) handleResellerUsers(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	filter := user.ListFilter{ResellerID: adminFrom(r).ResellerID}
	if search != "" {
		filter.Username = &search
	}
	page, err := s.Users.ListPage(r.Context(), user.ListQuery{
		Filter: filter, Sort: user.SortUsername, Limit: 50, Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		if domain.CodeOf(err) == domain.CodeInvalidRequest {
			s.surfaceError(w, r, http.StatusBadRequest, "common.error_validation", "")
			return
		}
		s.logError(r, "reseller user list unavailable", nil)
		s.surfaceError(w, r, http.StatusServiceUnavailable, "common.error_generic", "")
		return
	}
	d := resellerUsersData{Search: search, NextCursor: page.NextCursor}
	for _, u := range page.Items {
		d.Rows = append(d.Rows, resellerUserRow{User: u, Used: u.TrafficUsedRX + u.TrafficUsedTX})
	}
	_ = s.render(w, r, "reseller_users", "app", d)
}

func (s *Server) resellerOwnedUser(w http.ResponseWriter, r *http.Request, id string) (*user.User, bool) {
	u, err := s.Users.Get(r.Context(), id)
	if err != nil {
		if domain.CodeOf(err) != domain.CodeUserNotFound {
			s.logError(r, "reseller user unavailable", nil)
			s.surfaceError(w, r, http.StatusServiceUnavailable, "common.error_generic", "")
		} else {
			s.surfaceError(w, r, http.StatusNotFound, "common.error_not_found", "")
		}
		return nil, false
	}
	if u.ResellerID == nil || *u.ResellerID != *adminFrom(r).ResellerID {
		s.surfaceError(w, r, http.StatusNotFound, "common.error_not_found", "")
		return nil, false
	}
	return u, true
}

func (s *Server) handleResellerUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.resellerOwnedUser(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	d := resellerUserData{User: u, Used: u.TrafficUsedRX + u.TrafficUsedTX}
	if auth.Allows(adminFrom(r).Permissions, auth.ScopeNextPlansRead) {
		queued, err := s.Integration.NextPlanForUser(r.Context(), u.ID, adminFrom(r).ResellerID)
		if err != nil {
			s.logError(r, "reseller next plan unavailable", err)
			s.surfaceError(w, r, http.StatusServiceUnavailable, "common.error_generic", "")
			return
		}
		d.NextPlan = queued
	}
	if auth.Allows(adminFrom(r).Permissions, auth.ScopeNextPlansWrite) {
		ids, err := s.Resellers.Templates(r.Context(), *adminFrom(r).ResellerID)
		if err != nil {
			s.logError(r, "reseller plans unavailable", err)
			s.surfaceError(w, r, http.StatusServiceUnavailable, "common.error_generic", "")
			return
		}
		for _, id := range ids {
			p, err := s.Plans.Get(r.Context(), id)
			if err == nil && p.Enabled {
				d.NextPlans = append(d.NextPlans, &planRef{ID: p.ID, Name: p.Name})
			}
		}
	}
	if auth.Allows(adminFrom(r).Permissions, auth.ScopeDevicesRead) {
		devices, err := s.Devices.ListForUser(r.Context(), u.ID)
		if err != nil {
			s.logError(r, "reseller device list unavailable", nil)
			s.surfaceError(w, r, http.StatusServiceUnavailable, "common.error_generic", "")
			return
		}
		d.Devices = safeDeviceViews(devices)
	}
	_ = s.render(w, r, "reseller_user", "app", d)
}

func (s *Server) handleResellerNextPlanQueue(w http.ResponseWriter, r *http.Request) {
	u, ok := s.resellerOwnedUser(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	queued, err := s.Integration.QueueNextPlan(r.Context(), integration.QueueNextPlanInput{
		UserID: u.ID, TemplateID: r.PostFormValue("template_id"), ResellerID: adminFrom(r).ResellerID,
		CarryUnusedTraffic: r.PostFormValue("carry_unused_traffic") == "on",
	})
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.next_plan_queued", u.ID, map[string]any{"template_id": queued.TemplateID})
	s.redirectToast(w, r, "/reseller/users/"+u.ID, "users.next_plan.queued")
}

func (s *Server) handleResellerNextPlanCancel(w http.ResponseWriter, r *http.Request) {
	u, ok := s.resellerOwnedUser(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	if err := s.Integration.CancelNextPlan(r.Context(), u.ID, adminFrom(r).ResellerID); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.next_plan_canceled", u.ID, nil)
	s.redirectToast(w, r, "/reseller/users/"+u.ID, "users.next_plan.canceled")
}

func (s *Server) handleResellerTrafficReset(w http.ResponseWriter, r *http.Request) {
	u, ok := s.resellerOwnedUser(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	if err := s.Accounting.ResetTraffic(r.Context(), u.ID, s.actorFrom(r)); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.traffic_reset", u.ID, nil)
	s.runReconcile(r)
	s.redirectToast(w, r, "/reseller/users/"+u.ID, "users.toast.traffic_reset", u.Username)
}

func (s *Server) resellerOwnedDevice(w http.ResponseWriter, r *http.Request) (*device.Device, bool) {
	d, err := s.Devices.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		if domain.CodeOf(err) != domain.CodeDeviceNotFound {
			s.logError(r, "reseller device unavailable", nil)
			s.surfaceError(w, r, http.StatusServiceUnavailable, "common.error_generic", "")
		} else {
			s.surfaceError(w, r, http.StatusNotFound, "common.error_not_found", "")
		}
		return nil, false
	}
	if _, ok := s.resellerOwnedUser(w, r, d.UserID); !ok {
		return nil, false
	}
	return d, true
}

func (s *Server) handleResellerDeviceConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resellerOwnedDevice(w, r); ok {
		s.handleDeviceConfig(w, r)
	}
}

func (s *Server) handleResellerDeviceQR(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resellerOwnedDevice(w, r); ok {
		s.handleDeviceQR(w, r)
	}
}
