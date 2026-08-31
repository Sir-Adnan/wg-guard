package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/accounting"
	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

// usersData feeds the users list page.
type usersData struct {
	Items      []userRow
	NextCursor string
	Search     string
	Status     string
	Sort       string
	HasFilters bool
	Plans      []*planRef
	Ifaces     []*ifaceRef
	// Create-drawer preset chips (settings-driven).
	QuotaPresets    []string
	DurationPresets []string
	// Create-form defaults (settings-driven; see createDefaults).
	DefaultQuotaGB   int
	DefaultDurMonths int
	DefaultDeviceLim int
	DefaultIfaceID   string
	// FieldErr is always empty here; the create-fields partial is shared
	// with the user form, which uses it for error redisplay.
	FieldErr string
}

// userRow is one list row: the user plus panel-computed display fields.
type userRow struct {
	U           *user.User
	Used        int64 // RX+TX, precomputed for the meter
	DeviceCount int
	Devices     []*device.Device // id/name summaries for the quick-share menu
	PlanName    string
	SubURL      string // customer subscription URL ("" when no active link)
}

type planRef struct {
	ID   string
	Name string
}

// handleUserList renders the (filterable, cursor-paginated) user list.
func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := strings.TrimSpace(q.Get("q"))
	status := q.Get("status")
	sort := q.Get("sort")
	cursor := q.Get("cursor")

	lq := user.ListQuery{Limit: 50, Cursor: cursor}
	switch sort {
	case "", "created_new":
		lq.Sort, lq.Desc = user.SortCreatedAt, true
	case "created_old":
		lq.Sort, lq.Desc = user.SortCreatedAt, false
	case "username":
		lq.Sort, lq.Desc = user.SortUsername, false
	case "expires":
		lq.Sort, lq.Desc = user.SortExpiresAt, false
	case "used":
		lq.Sort, lq.Desc = user.SortUsed, true
	default:
		s.badRequest(w, r, "unknown sort")
		return
	}
	if search != "" {
		lq.Filter.Username = &search
	}
	if status != "" && status != "all" {
		if !validUserStatus(status) {
			s.badRequest(w, r, "unknown status")
			return
		}
		st := domain.UserStatus(status)
		lq.Filter.Status = &st
	}

	page, err := s.Users.ListPage(r.Context(), lq)
	if err != nil {
		if domain.CodeOf(err) == domain.CodeInvalidRequest {
			s.badRequest(w, r, "invalid query")
			return
		}
		s.logError(r, "user list", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	rows, plans := s.decorateUsers(r, page.Items)
	data := usersData{
		Items:           rows,
		NextCursor:      page.NextCursor,
		Search:          search,
		Status:          status,
		Sort:            sort,
		HasFilters:      search != "" || (status != "" && status != "all"),
		Plans:           plans,
		Ifaces:          s.ifacesForForm(r),
		QuotaPresets:    s.settingList(r, "users.quota_presets_gb"),
		DurationPresets: s.settingList(r, "users.duration_presets_months"),
	}
	data.DefaultQuotaGB, data.DefaultDurMonths, data.DefaultDeviceLim, data.DefaultIfaceID = s.createDefaults(r)
	_ = s.render(w, r, "users", "app", data)
}

// ensureSubLink provisions the subscription link for a freshly created user
// (best effort — a missing link is recoverable from the detail page).
func (s *Server) ensureSubLink(r *http.Request, userID string) {
	if s.Links == nil {
		return
	}
	if _, err := s.Links.Ensure(r.Context(), userID); err != nil {
		s.logError(r, "sub link ensure", err)
	}
}

// createAutoDevices provisions up to 10 devices for a fresh user so the
// configs are ready to deliver immediately (QR/share on the detail page).
// Device creation is per-device transactional; a failure logs and stops —
// the admin sees what exists and can add the rest by hand.
func (s *Server) createAutoDevices(r *http.Request, userID string, n int) {
	if n < 1 {
		n = 1
	}
	if n > 10 {
		n = 10
	}
	created := 0
	for i := 1; i <= n; i++ {
		keys, err := s.generateKeys(r, false)
		if err != nil {
			s.logError(r, "auto device keys", err)
			break
		}
		name := fmt.Sprintf("device-%d", i)
		if _, err := s.Devices.Create(r.Context(), userID, name, *keys, ""); err != nil {
			s.logError(r, "auto device create", err)
			break
		}
		created++
	}
	if created > 0 {
		s.audit(r, "user.devices_auto_created", userID, map[string]any{"count": created})
		s.runReconcile(r)
	}
}

// decorateUsers batch-loads the display fields for one page.
func (s *Server) decorateUsers(r *http.Request, items []*user.User) ([]userRow, []*planRef) {
	ctx := r.Context()
	ids := make([]string, len(items))
	for i, u := range items {
		ids[i] = u.ID
	}
	counts, err := s.Devices.CountForUsers(ctx, ids)
	if err != nil {
		s.logError(r, "device count batch", err)
	}
	planNames := map[string]string{}
	var plans []*planRef
	if plist, err := s.Plans.List(ctx); err == nil {
		for _, p := range plist {
			planNames[p.ID] = p.Name
			plans = append(plans, &planRef{ID: p.ID, Name: p.Name})
		}
	}
	subURLs := map[string]string{}
	if s.Links != nil {
		if links, err := s.Links.ForUsers(ctx, ids); err == nil {
			for id, l := range links {
				if !l.Revoked() && l.Token != "" {
					subURLs[id] = s.subURLFor(r, l.Token)
				}
			}
		} else {
			s.logError(r, "sub link batch", err)
		}
	}
	rows := make([]userRow, len(items))
	deviceLists, err := s.Devices.ListForUsers(ctx, ids)
	if err != nil {
		s.logError(r, "device list batch", err)
	}
	for i, u := range items {
		rows[i] = userRow{U: u, Used: u.TrafficUsedRX + u.TrafficUsedTX,
			DeviceCount: counts[u.ID], Devices: deviceLists[u.ID],
			PlanName: planNames[deref(u.PlanID)], SubURL: subURLs[u.ID]}
	}
	return rows, plans
}

// --- create / edit ------------------------------------------------------------

type userFormData struct {
	IsEdit   bool
	User     *user.User
	Plans    []*planRef
	Ifaces   []*ifaceRef
	Error    string // localized message
	FieldErr string // field name that failed (best effort)
	StartNow bool
	// Raw submitted duration (error redisplay on create; edit prefills from
	// User.DurationSeconds via the view helpers).
	DurationValue string
	DurationUnit  string
	// Quick-preset chips (settings-driven; the settings screen manages values).
	QuotaPresets    []string
	DurationPresets []string
	// Create-form defaults (settings-driven; see createDefaults).
	DefaultQuotaGB   int
	DefaultDurMonths int
	DefaultDeviceLim int
	DefaultIfaceID   string
}

type ifaceRef struct {
	ID      string
	Name    string
	Enabled bool
}

func (s *Server) ifacesForForm(r *http.Request) []*ifaceRef {
	list, err := s.Ifaces.List(r.Context())
	if err != nil {
		return nil
	}
	out := make([]*ifaceRef, 0, len(list))
	for _, f := range list {
		out = append(out, &ifaceRef{ID: f.ID, Name: f.Name, Enabled: f.Enabled})
	}
	return out
}

// createDefaults resolves the create-form prefill: configured default quota
// (GB; 0 = none), default duration (months; 0 = no-expiry), default device
// limit and default interface (configured id when it still exists, otherwise
// the first enabled interface). Best effort — registry failures yield zeros.
func (s *Server) createDefaults(r *http.Request) (quotaGB, durMonths, devLimit int, ifaceID string) {
	ctx := r.Context()
	quotaGB, _ = s.Settings.GetInt(ctx, "users.default_quota_gb")
	durMonths, _ = s.Settings.GetInt(ctx, "users.default_duration_months")
	devLimit, _ = s.Settings.GetInt(ctx, "users.default_device_limit")
	if devLimit <= 0 {
		devLimit = 1
	}
	ifaceID, _ = s.Settings.GetString(ctx, "users.default_iface_id")
	ifaces := s.ifacesForForm(r)
	found := false
	for _, f := range ifaces {
		if f.ID == ifaceID {
			found = true
			break
		}
	}
	if !found {
		ifaceID = ""
		for _, f := range ifaces {
			if f.Enabled {
				ifaceID = f.ID
				break
			}
		}
	}
	return quotaGB, durMonths, devLimit, ifaceID
}

func (s *Server) handleUserNew(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "user_form", "app", s.newUserFormData(r))
}

// newUserFormData assembles the create-form data: plans, interfaces, the
// quick-preset lists (quota GB, duration months) and the create defaults
// from the settings registry.
func (s *Server) newUserFormData(r *http.Request) userFormData {
	data := userFormData{
		Plans:  s.plansForForm(r),
		Ifaces: s.ifacesForForm(r),
	}
	data.QuotaPresets = s.settingList(r, "users.quota_presets_gb")
	data.DurationPresets = s.settingList(r, "users.duration_presets_months")
	data.DefaultQuotaGB, data.DefaultDurMonths, data.DefaultDeviceLim, data.DefaultIfaceID = s.createDefaults(r)
	return data
}

// settingList reads a string-list setting (best effort — missing key or
// registry failure renders without presets).
func (s *Server) settingList(r *http.Request, key string) []string {
	if v, err := s.Settings.GetStringList(r.Context(), key); err == nil {
		return v
	}
	return nil
}

func (s *Server) plansForForm(r *http.Request) []*planRef {
	if list, err := s.Plans.List(r.Context()); err == nil {
		out := make([]*planRef, 0, len(list))
		for _, p := range list {
			out = append(out, &planRef{ID: p.ID, Name: p.Name})
		}
		return out
	}
	return nil
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	in, err := s.userInputFromForm(r, false)
	if err != nil {
		s.badRequest(w, r, err.Error())
		return
	}
	in.Username = r.PostFormValue("username")
	u, err := s.Users.Create(r.Context(), in)
	if err != nil {
		if domain.CodeOf(err) == domain.CodeInvalidRequest || domain.CodeOf(err) == domain.CodeUsernameExists {
			d := s.newUserFormData(r)
			d.Error = s.humanizeDomainError(r, err)
			d.FieldErr = "username"
			d.StartNow = r.PostFormValue("start_policy") != "first_connection"
			d.DurationValue = formDurationValue(r)
			d.DurationUnit = r.PostFormValue("duration_unit")
			_ = s.render(w, r, "user_form", "app", d)
			return
		}
		s.logError(r, "user create", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.audit(r, "user.created", u.ID, map[string]any{"username": u.Username})
	s.ensureSubLink(r, u.ID)
	if r.PostFormValue("auto_devices") == "1" {
		n := 1 // unlimited device limit still provisions one ready config
		if u.DeviceLimit != nil {
			n = *u.DeviceLimit
		}
		s.createAutoDevices(r, u.ID, n)
	}
	s.redirectToast(w, r, "/users/"+u.ID, "users.toast.created")
}

func (s *Server) handleUserEditPage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	_ = s.render(w, r, "user_form", "app", userFormData{
		IsEdit: true, User: u,
		Plans: s.plansForForm(r), Ifaces: s.ifacesForForm(r),
	})
}

func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	in, err := s.userInputFromForm(r, true)
	if err != nil {
		s.badRequest(w, r, err.Error())
		return
	}
	updated, err := s.Users.Update(r.Context(), u.ID, in)
	if err != nil {
		if domain.CodeOf(err) == domain.CodeInvalidRequest {
			_ = s.render(w, r, "user_form", "app", userFormData{
				IsEdit: true, User: u,
				Plans: s.plansForForm(r), Ifaces: s.ifacesForForm(r),
				Error: s.humanizeDomainError(r, err),
			})
			return
		}
		s.logError(r, "user update", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.audit(r, "user.updated", u.ID, nil)
	s.redirectToast(w, r, "/users/"+updated.ID, "users.toast.updated")
}

// quotaFromForm parses the traffic limit as value+unit (exact decimal bytes).
// The legacy value-only `traffic_limit_gb` field is still accepted so older
// API form posts keep working.
func quotaFromForm(r *http.Request) (*int64, error) {
	value := r.PostFormValue("traffic_limit_value")
	unit := r.PostFormValue("traffic_limit_unit")
	if value == "" {
		value = r.PostFormValue("traffic_limit_gb")
		unit = "gb"
	}
	return parseQuotaBytes(value, unit)
}

// durationFromForm parses the duration as value+unit (hours/days/months).
// Legacy value-only fields are still accepted: `duration_days` (create,
// bulk, plans) and `days` (renew).
func durationFromForm(r *http.Request) (*int64, error) {
	value := r.PostFormValue("duration_value")
	unit := r.PostFormValue("duration_unit")
	if value == "" {
		value = r.PostFormValue("duration_days")
		if value == "" {
			value = r.PostFormValue("days")
		}
		unit = "days"
	}
	return parseDurationSeconds(value, unit)
}

// userInputFromForm maps the shared fields. Empty limit fields mean
// "unlimited": create sends absent options, edit sends explicit nulls
// (tri-state PATCH semantics, api.md).
func (s *Server) userInputFromForm(r *http.Request, isEdit bool) (user.Input, error) {
	in := user.Input{}
	in.DisplayName = strPtr(r.PostFormValue("display_name"))
	in.Note = strPtr(r.PostFormValue("note"))
	in.Tags = parseTags(r.PostFormValue("tags"))

	quota, err := quotaFromForm(r)
	if err != nil {
		return in, errInvalid
	}
	in.TrafficLimitBytes = limitOpt64(quota, isEdit)
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

	if v := r.PostFormValue("plan"); v == "" {
		in.PlanID = clearOpt(isEdit)
	} else {
		in.PlanID = domain.OptString{Set: true, Value: v}
	}
	if v := r.PostFormValue("interface"); v == "" {
		in.InterfaceID = clearOpt(isEdit)
	} else {
		in.InterfaceID = domain.OptString{Set: true, Value: v}
	}

	// Subscription timing: editable on create; on edit only the duration.
	if !isEdit {
		switch r.PostFormValue("start_policy") {
		case "first_connection":
			in.StartPolicy = domain.StartFirstConnection
		default:
			in.StartPolicy = domain.StartImmediate
		}
	}
	if v := r.PostFormValue("duration_value"); v != "" || r.PostFormValue("duration_days") != "" {
		secs, err := durationFromForm(r)
		if err != nil {
			return in, errInvalid
		}
		in.DurationSeconds = secs
	} else if !isEdit {
		in.DurationSeconds = nil // immediate without duration = no expiry
	}
	// Exact expiry date (calendar pick). An already-past date is rejected at
	// the edge — the service stores what it is given (renew parity).
	if !isEdit {
		expires, err := parseDateOnly(r.PostFormValue("expires_on"))
		if err != nil {
			return in, errInvalid
		}
		if expires != nil && expires.Before(time.Now()) {
			return in, errInvalid
		}
		in.ExpiresAt = expires
	}
	return in, nil
}

// --- lifecycle actions ----------------------------------------------------------

func (s *Server) handleUserEnable(w http.ResponseWriter, r *http.Request) {
	s.userLifecycle(w, r, true)
}

func (s *Server) handleUserDisable(w http.ResponseWriter, r *http.Request) {
	s.userLifecycle(w, r, false)
}

func (s *Server) userLifecycle(w http.ResponseWriter, r *http.Request, enable bool) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	var err error
	if enable {
		_, err = s.Users.SetEnabledStatus(r.Context(), u.ID, true, domain.DisableManual)
	} else {
		_, err = s.Users.SetEnabledStatus(r.Context(), u.ID, false, domain.DisableManual)
	}
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	if enable {
		s.audit(r, "user.enabled", u.ID, nil)
		s.redirectToast(w, r, "/users/"+u.ID, "users.toast.enabled")
	} else {
		s.audit(r, "user.disabled", u.ID, nil)
		s.redirectToast(w, r, "/users/"+u.ID, "users.toast.disabled")
	}
	s.runReconcile(r)
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	if err := s.Users.SoftDelete(r.Context(), u.ID); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.deleted", u.ID, map[string]any{"username": u.Username})
	s.redirectToast(w, r, "/users", "users.toast.deleted", u.Username)
}

func (s *Server) handleUserRestore(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	if _, err := s.Users.Restore(r.Context(), u.ID); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.restored", u.ID, nil)
	s.redirectToast(w, r, "/users/"+u.ID, "users.toast.updated", u.Username)
}

// handleUserRenew applies one of the three renewal modes (days for
// from_expiration/from_now, exact date otherwise).
func (s *Server) handleUserRenew(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	mode := r.PostFormValue("mode")
	var (
		duration *int64
		exact    *time.Time
	)
	switch mode {
	case "from_expiration", "from_now":
		secs, err := durationFromForm(r)
		if err != nil || secs == nil {
			s.badRequest(w, r, "duration")
			return
		}
		duration = secs
	case "exact":
		d, err := parseDateOnly(r.PostFormValue("date"))
		if err != nil || d == nil {
			s.badRequest(w, r, "date")
			return
		}
		exact = d
	default:
		s.badRequest(w, r, "mode")
		return
	}
	if _, err := s.Users.Renew(r.Context(), u.ID, mode, duration, exact); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.renewed", u.ID, map[string]any{"mode": mode})
	s.redirectToast(w, r, "/users/"+u.ID, "users.toast.renewed", u.Username)
}

// handleUserTrafficAdd adds quota bytes (charged-counter correction).
// Value+unit parsing keeps small corrections (0.2 GB) exact.
func (s *Server) handleUserTrafficAdd(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	value := r.PostFormValue("traffic_value")
	unit := r.PostFormValue("traffic_unit")
	if value == "" {
		value = r.PostFormValue("gb")
		unit = "gb"
	}
	quota, err := parseQuotaBytes(value, unit)
	if err != nil || quota == nil {
		s.badRequest(w, r, "traffic")
		return
	}
	a := s.actorFrom(r)
	if err := s.Accounting.AddTraffic(r.Context(), u.ID, *quota, 0, a); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.traffic_added", u.ID, map[string]any{"bytes": *quota})
	s.redirectToast(w, r, "/users/"+u.ID, "users.toast.traffic_added", u.Username)
}

// handleUserTrafficReset zeroes counters and unblocks traffic_exceeded.
func (s *Server) handleUserTrafficReset(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	if err := s.Accounting.ResetTraffic(r.Context(), u.ID, s.actorFrom(r)); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "user.traffic_reset", u.ID, nil)
	s.redirectToast(w, r, "/users/"+u.ID, "users.toast.traffic_reset", u.Username)
}

// --- bulk -----------------------------------------------------------------------

// handleBulkCreate provisions a batch sharing one form's limits.
func (s *Server) handleBulkCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	count, err := strconv.Atoi(r.PostFormValue("count"))
	if err != nil || count < 1 || count > 500 {
		s.badRequest(w, r, "count")
		return
	}
	startIndex, _ := strconv.Atoi(r.PostFormValue("start_index"))
	width := len(strconv.Itoa(startIndex + count - 1))
	if width < 3 {
		width = 3
	}
	in, err := s.userInputFromForm(r, false)
	if err != nil {
		s.badRequest(w, r, "limits")
		return
	}
	res, err := s.Users.CreateBulk(r.Context(), r.PostFormValue("prefix"), count, startIndex, width, in)
	if err != nil {
		if domain.CodeOf(err) == domain.CodeInvalidRequest || domain.CodeOf(err) == domain.CodeUsernameExists {
			s.redirectToast(w, r, "/users", "common.error_validation")
			return
		}
		s.logError(r, "bulk create", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.audit(r, "user.bulk_created", "", map[string]any{"count": len(res.Users)})
	for _, u := range res.Users {
		s.ensureSubLink(r, u.ID)
	}
	s.redirectToast(w, r, "/users", "users.toast.bulk_created", strconv.Itoa(len(res.Users)))
}

// handleBulkAction applies enable/disable/delete to the selected ids.
func (s *Server) handleBulkAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	ids := parseIDs(r.PostForm["ids"])
	action := r.PostFormValue("action")
	if len(ids) == 0 || len(ids) > 500 {
		s.badRequest(w, r, "ids")
		return
	}
	ok := 0
	for _, id := range ids {
		var err error
		switch action {
		case "enable":
			_, err = s.Users.SetEnabledStatus(r.Context(), id, true, domain.DisableManual)
		case "disable":
			_, err = s.Users.SetEnabledStatus(r.Context(), id, false, domain.DisableManual)
		case "delete":
			err = s.Users.SoftDelete(r.Context(), id)
		default:
			s.badRequest(w, r, "action")
			return
		}
		if err == nil {
			ok++
		}
	}
	s.audit(r, "user.bulk_action", "", map[string]any{"action": action, "total": len(ids), "ok": ok})
	if action == "delete" {
		s.redirectToast(w, r, "/users", "users.toast.bulk_deleted", strconv.Itoa(ok))
	} else {
		s.redirectToast(w, r, "/users", "users.toast.bulk_updated", strconv.Itoa(ok))
	}
	if action == "enable" || action == "disable" {
		s.runReconcile(r)
	}
}

// --- helpers ---------------------------------------------------------------------

// loadUser fetches the path user or writes the error response.
func (s *Server) loadUser(w http.ResponseWriter, r *http.Request) (*user.User, bool) {
	u, err := s.Users.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		if domain.CodeOf(err) == domain.CodeUserNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return nil, false
		}
		s.logError(r, "user load", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	return u, true
}

// actorFrom is the admin audit actor for accounting mutations.
func (s *Server) actorFrom(r *http.Request) accounting.Actor {
	if a := adminFrom(r); a != nil {
		return accounting.Actor{Type: audit.ActorAdmin, ID: a.ID}
	}
	return accounting.Actor{}
}

func (s *Server) redirectToast(w http.ResponseWriter, r *http.Request, path, key string, targ ...string) {
	q := url.Values{"toast": {key}}
	if len(targ) > 0 && targ[0] != "" {
		q.Set("targ", targ[0])
	}
	http.Redirect(w, r, path+"?"+q.Encode(), http.StatusSeeOther)
}

// redirectToastRaw carries an already-rendered message (e.g. a safe domain
// error string) through the same PRG flash channel.
func (s *Server) redirectToastRaw(w http.ResponseWriter, r *http.Request, path, msg string) {
	q := url.Values{"rawmsg": {msg}}
	http.Redirect(w, r, path+"?"+q.Encode(), http.StatusSeeOther)
}

// actionFailed renders a toast-carrying redirect after a failed mutation —
// the operation did not apply, and the message says so (ui-ux.md).
func (s *Server) actionFailed(w http.ResponseWriter, r *http.Request, err error) {
	s.logError(r, "action failed", err)
	http.Redirect(w, r, "/?toast=common.error_generic", http.StatusSeeOther)
}

func (s *Server) badRequest(w http.ResponseWriter, r *http.Request, what string) {
	s.logError(r, "bad request: "+what, nil)
	http.Error(w, "bad request", http.StatusBadRequest)
}

func validUserStatus(s string) bool {
	switch domain.UserStatus(s) {
	case domain.UserActive, domain.UserDisabled, domain.UserSuspended,
		domain.UserExpired, domain.UserTrafficExceeded, domain.UserWaitingFirstConnection:
		return true
	}
	return false
}

func parseTags(raw string) []string {
	var out []string
	for _, t := range strings.Split(raw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func parseIDs(raw []string) []string {
	var out []string
	for _, v := range raw {
		for _, id := range strings.Split(v, ",") {
			if id = strings.TrimSpace(id); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

func strPtr(s string) *string { return &s }

// parseKbps parses a speed limit; "" = unset.
func parseKbps(raw string) (*int, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return nil, err
	}
	return &v, nil
}

// parseInt is the device-limit twin of parseKbps.
func parseInt(raw string) (*int, error) { return parseKbps(raw) }

// limitOpt64 maps a form value onto tri-state semantics for byte limits.
func limitOpt64(v *int64, isEdit bool) domain.OptInt64 {
	switch {
	case v == nil && isEdit:
		return domain.OptInt64{Set: true, Null: true} // cleared field = unlimited
	case v == nil:
		return domain.OptInt64{} // absent = default/unlimited on create
	default:
		return domain.OptInt64{Set: true, Value: *v}
	}
}

// limitOptI is the int-sized twin (speeds, device limit).
func limitOptI(v *int, isEdit bool) domain.OptInt {
	switch {
	case v == nil && isEdit:
		return domain.OptInt{Set: true, Null: true}
	case v == nil:
		return domain.OptInt{}
	default:
		return domain.OptInt{Set: true, Value: *v}
	}
}

func clearOpt(isEdit bool) domain.OptString {
	if isEdit {
		return domain.OptString{Set: true, Null: true}
	}
	return domain.OptString{}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// humanizeDomainError maps known validation codes onto catalog messages.
func (s *Server) humanizeDomainError(r *http.Request, err error) string {
	if domain.CodeOf(err) == domain.CodeUsernameExists {
		return s.t(r, "users.error.username_taken")
	}
	return s.t(r, "common.error_validation")
}

// runReconcile applies structural changes to the backend immediately
// (same contract as the API: errors are logged, the accounting cycle and
// boot re-derive the same state from the DB).
func (s *Server) runReconcile(r *http.Request) {
	if s.Reconciler == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if _, err := s.Reconciler.Run(ctx); err != nil && s.Log != nil {
		s.Log.Warn("reconcile after mutation failed", "error", err)
	}
}
