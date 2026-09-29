package web

import (
	"net/http"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
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
	User    *user.User
	Used    int64
	Devices []*deviceView
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
