package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// backupsData feeds the /backups screen: archive list, schedules, telegram
// state, the pending-restore banner and the restore review card.
type backupsData struct {
	Error                                                  string // localized message or safe engine error text
	Field                                                  string
	Available, ArchivesKnown, SchedulesKnown, PendingKnown bool
	TelegramReady, SettingsKnown                           bool
	ScheduleOpen                                           bool
	CreateOpen                                             bool
	RestoreName                                            string
	Form                                                   operationalForm

	Archives  []backup.ArchiveInfo
	Schedules []*backup.Schedule
	Pending   *backup.PendingRestore

	TelegramSet  bool
	TelegramChat string
	PasswordSet  bool
	Retention    int

	// Review carries the environment report of a freshly staged restore —
	// rendered once between Stage and Confirm/Cancel.
	Review   *backup.RestoreReport
	Warnings []string

	// Submitted schedule form values redisplayed after a validation error.
	SchedForm scheduleForm
}

type scheduleForm struct {
	ID             string
	Name           string
	Kind           string
	TimeOfDay      string
	Weekday        int
	IntervalHours  int
	RetentionCount int
	Enabled        bool
}

func (s *Server) backupsData(r *http.Request) backupsData {
	ctx := r.Context()
	d := backupsData{Available: s.Backup != nil, SettingsKnown: true, RestoreName: r.URL.Query().Get("restore"), Form: scheduleOperationalForm(nil), CreateOpen: r.URL.Query().Get("create") == "1" || r.URL.Path == "/backups/create"}
	if r.URL.Path == "/backups/restore" {
		d.RestoreName = r.PostFormValue("name")
	}
	if s.Backup != nil {
		if arcs, err := s.Backup.List(); err == nil {
			d.Archives = arcs
			d.ArchivesKnown = true
		}
		if pending, err := s.Backup.Pending(); err == nil {
			d.Pending = pending
			d.PendingKnown = true
		}
		if schedules, err := s.Backup.Schedules(ctx); err == nil {
			d.Schedules = schedules
			d.SchedulesKnown = true
		}
	}
	if token, err := s.Settings.GetSecret(ctx, "backup.telegram_token"); err == nil {
		d.TelegramSet = token != ""
	} else {
		d.SettingsKnown = false
	}
	if chat, err := s.Settings.GetString(ctx, "backup.telegram_chat"); err == nil {
		d.TelegramChat = chat
	} else {
		d.SettingsKnown = false
	}
	if pw, err := s.Settings.GetSecret(ctx, "backup.password"); err == nil {
		d.PasswordSet = pw != ""
	} else {
		d.SettingsKnown = false
	}
	if retention, err := s.Settings.GetInt(ctx, "backup.retention_count"); err == nil {
		d.Retention = retention
	} else {
		d.SettingsKnown = false
	}
	d.TelegramReady = d.Available && d.SettingsKnown && d.TelegramSet && strings.TrimSpace(d.TelegramChat) != ""
	return d
}

// handleBackupsPage renders the backups/ops screen.
func (s *Server) handleBackupsPage(w http.ResponseWriter, r *http.Request) {
	d := s.backupsData(r)
	if id := r.URL.Query().Get("schedule"); id != "" && d.Available {
		d.ScheduleOpen = true
		if id != "new" {
			f, err := s.Backup.GetSchedule(r.Context(), id)
			if err != nil {
				s.backupError(w, r, err)
				return
			}
			d.SchedForm.ID = f.ID
			d.Form = scheduleOperationalForm(f)
		}
	}
	_ = s.render(w, r, "backups", "app", d)
}

// handleBackupCreate runs a manual archive (optional explicit password;
// empty falls back to the stored backup password, plain when neither).
func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	password := r.PostFormValue("password")
	res, err := s.Backup.Create(r.Context(), backup.CreateOpts{
		Password: password,
		Reason:   "manual",
		Deliver:  true,
	})
	if err != nil {
		s.backupError(w, r, err)
		return
	}
	s.audit(r, "backup.created", res.Name, map[string]any{
		"size": res.Size, "encrypted": res.Encrypted, "delivered": res.Delivered,
	})
	if len(res.Warnings) > 0 {
		// Only safe, localized public messages enter the existing escaped PRG
		// flash channel; engine causes (including credential URLs) stay private.
		text := s.t(r, "backups.toast.created") + " " + strings.Join(backup.WarningTexts(res.Warnings, s.localeFor(r)), " · ")
		s.redirectToastRaw(w, r, "/backups", text)
		return
	}
	s.redirectToast(w, r, "/backups", "backups.toast.created")
}

// handleBackupDelete removes a local archive.
func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PostFormValue("name")
	if err := s.Backup.Delete(r.Context(), name); err != nil {
		s.backupError(w, r, err)
		return
	}
	s.audit(r, "backup.deleted", name, nil)
	s.redirectToast(w, r, "/backups", "backups.toast.deleted")
}

// handleBackupDownload streams a local archive as an attachment.
func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	f, size, err := s.Backup.Open(name)
	if err != nil {
		s.backupError(w, r, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", name))
	http.ServeContent(w, r, name, time.Now(), f)
	_ = size
}

// handleBackupRestore verifies + stages an archive and renders the review
// card. Nothing is applied until the operator confirms AND the service
// restarts (the swap happens before the database is opened).
func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	name := r.PostFormValue("name")
	password := r.PostFormValue("password")
	f, _, err := s.Backup.Open(name)
	if err != nil {
		s.backupError(w, r, err)
		return
	}
	path := f.Name()
	f.Close()

	pr, report, err := s.Backup.Stage(r.Context(), path, password)
	if err != nil {
		switch domain.CodeOf(err) {
		case domain.CodeInvalidRequest, domain.CodeNotFound:
			d := s.backupsData(r)
			d.Error = backup.ErrorText(err, s.localeFor(r))
			d.RestoreName = name
			_ = s.render(w, r, "backups", "app", d)
		default:
			s.backupError(w, r, err)
		}
		return
	}
	s.audit(r, "backup.restore_staged", pr.Archive, nil)
	d := s.backupsData(r)
	d.Pending = pr
	d.Review = report
	d.Warnings = backup.WarningTexts(report.Warnings, s.localeFor(r))
	_ = s.render(w, r, "backups", "app", d)
}

// handleBackupRestoreConfirm acknowledges the review; the staged payload
// applies at the next restart.
func (s *Server) handleBackupRestoreConfirm(w http.ResponseWriter, r *http.Request) {
	pending, err := s.Backup.Approve(r.PostFormValue("preview"))
	if err != nil || pending == nil {
		s.redirectToast(w, r, "/backups", "backups.toast.no_pending")
		return
	}
	s.audit(r, "backup.restore_confirmed", pending.Archive, nil)
	s.redirectToast(w, r, "/backups", "backups.toast.restore_confirmed")
}

// handleBackupRestoreCancel discards the staged restore.
func (s *Server) handleBackupRestoreCancel(w http.ResponseWriter, r *http.Request) {
	var err error
	if id := r.PostFormValue("preview"); id != "" {
		err = s.Backup.DiscardPreview(id)
	} else {
		err = s.Backup.DiscardPending()
	}
	if err != nil {
		s.backupError(w, r, err)
		return
	}
	s.audit(r, "backup.restore_cancelled", "", nil)
	s.redirectToast(w, r, "/backups", "backups.toast.restore_cancelled")
}

// --- schedules -----------------------------------------------------------------

func scheduleOperationalForm(f *backup.Schedule) operationalForm {
	v := map[string]string{"name": "", "kind": "daily", "time_of_day": "03:00", "weekday": "0", "interval_hours": "24", "retention": "0", "enabled": "1"}
	if f != nil {
		v["name"], v["kind"], v["time_of_day"] = f.Name, f.Kind, f.TimeOfDay
		v["weekday"], v["interval_hours"], v["retention"] = strconv.Itoa(f.Weekday), strconv.Itoa(f.IntervalHours), strconv.Itoa(f.RetentionCount)
		if !f.Enabled {
			v["enabled"] = "0"
		}
	}
	return operationalForm{Values: v, Fields: map[string]string{}}
}

func scheduleNumberErrors(r *http.Request) map[string]string {
	fields := map[string]string{}
	keys := []string{"retention"}
	if r.PostFormValue("kind") == "interval" {
		keys = append(keys, "interval_hours")
	}
	if r.PostFormValue("kind") == "weekly" {
		keys = append(keys, "weekday")
	}
	for _, key := range keys {
		if raw := r.PostFormValue(key); raw != "" {
			if _, err := strconv.Atoi(raw); err != nil {
				fields[key] = "forms.error.number"
			}
		}
	}
	return fields
}

func (s *Server) scheduleFromForm(r *http.Request) scheduleForm {
	weekday, _ := strconv.Atoi(r.PostFormValue("weekday"))
	interval, _ := strconv.Atoi(r.PostFormValue("interval_hours"))
	retention, _ := strconv.Atoi(r.PostFormValue("retention"))
	return scheduleForm{
		Name:           strings.TrimSpace(r.PostFormValue("name")),
		Kind:           r.PostFormValue("kind"),
		TimeOfDay:      strings.TrimSpace(r.PostFormValue("time_of_day")),
		Weekday:        weekday,
		IntervalHours:  interval,
		RetentionCount: retention,
		Enabled:        r.PostFormValue("enabled") == "1",
	}
}

func (f scheduleForm) toSchedule() *backup.Schedule {
	return &backup.Schedule{
		Name: f.Name, Kind: f.Kind, TimeOfDay: f.TimeOfDay,
		Weekday: f.Weekday, IntervalHours: f.IntervalHours,
		Enabled: f.Enabled, RetentionCount: f.RetentionCount,
	}
}

// handleScheduleCreate adds a schedule.
func (s *Server) handleScheduleCreate(w http.ResponseWriter, r *http.Request) {
	f := s.scheduleFromForm(r)
	if len(scheduleNumberErrors(r)) > 0 {
		s.scheduleError(w, r, f, domain.E(domain.CodeInvalidRequest, "invalid schedule number"))
		return
	}
	if _, err := s.Backup.CreateSchedule(r.Context(), f.toSchedule()); err != nil {
		s.scheduleError(w, r, f, err)
		return
	}
	s.audit(r, "backup.schedule_created", f.Name, map[string]any{"kind": f.Kind})
	s.redirectToast(w, r, "/backups", "backups.toast.schedule_created")
}

// handleScheduleUpdate replaces one schedule's definition.
func (s *Server) handleScheduleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f := s.scheduleFromForm(r)
	f.ID = id
	if len(scheduleNumberErrors(r)) > 0 {
		s.scheduleError(w, r, f, domain.E(domain.CodeInvalidRequest, "invalid schedule number"))
		return
	}
	if _, err := s.Backup.UpdateSchedule(r.Context(), id, f.toSchedule()); err != nil {
		s.scheduleError(w, r, f, err)
		return
	}
	s.audit(r, "backup.schedule_updated", f.Name, nil)
	s.redirectToast(w, r, "/backups", "backups.toast.schedule_updated")
}

// handleScheduleDelete removes one schedule.
func (s *Server) handleScheduleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cur, err := s.Backup.GetSchedule(r.Context(), id)
	if err != nil {
		s.backupError(w, r, err)
		return
	}
	if err := s.Backup.DeleteSchedule(r.Context(), id); err != nil {
		s.backupError(w, r, err)
		return
	}
	s.audit(r, "backup.schedule_deleted", cur.Name, nil)
	s.redirectToast(w, r, "/backups", "backups.toast.schedule_deleted")
}

// handleScheduleToggle flips the enabled flag.
func (s *Server) handleScheduleToggle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cur, err := s.Backup.GetSchedule(r.Context(), id)
	if err != nil {
		s.backupError(w, r, err)
		return
	}
	next := *cur
	next.Enabled = !cur.Enabled
	if _, err := s.Backup.UpdateSchedule(r.Context(), id, &next); err != nil {
		s.backupError(w, r, err)
		return
	}
	if next.Enabled {
		s.redirectToast(w, r, "/backups", "backups.toast.schedule_enabled")
	} else {
		s.redirectToast(w, r, "/backups", "backups.toast.schedule_disabled")
	}
}

// --- telegram ------------------------------------------------------------------

// handleTelegramTest sends the probe document with the stored credentials.
func (s *Server) handleTelegramTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	chat, _ := s.Settings.GetString(ctx, "backup.telegram_chat")
	if err := s.Backup.TestTelegram(ctx); err != nil {
		s.backupError(w, r, err)
		return
	}
	s.audit(r, "backup.telegram_test", chat, nil)
	s.redirectToast(w, r, "/backups", "backups.toast.telegram_ok")
}

// backupError maps engine errors onto the page; unexpected ones surface the
// generic error toast (never engine internals like paths of the key file).
func (s *Server) backupError(w http.ResponseWriter, r *http.Request, err error) {
	var message backup.Message
	if errors.As(err, &message) {
		d := s.backupsData(r)
		d.Error = message.Localized(s.localeFor(r))
		_ = s.render(w, r, "backups", "app", d)
		return
	}
	switch domain.CodeOf(err) {
	case domain.CodeInvalidRequest, domain.CodeNotFound, domain.CodeSettingInvalid:
		d := s.backupsData(r)
		d.Error = backup.ErrorText(err, s.localeFor(r))
		_ = s.render(w, r, "backups", "app", d)
	default:
		s.logError(r, "backup operation", err)
		s.redirectToast(w, r, "/backups", "common.error_generic")
	}
}

// scheduleError redisplays the page with the schedule form values kept.
func (s *Server) scheduleError(w http.ResponseWriter, r *http.Request, f scheduleForm, err error) {
	d := s.backupsData(r)
	d.SchedForm = f
	d.ScheduleOpen = true
	d.Form = submittedOperationalForm(r, scheduleOperationalForm(nil).Values)
	d.Form.Fields = scheduleNumberErrors(r)
	message := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(err.Error()), "backup: "), "schedule ")
	for _, item := range [][2]string{{"name", "name"}, {"kind", "kind"}, {"time", "time_of_day"}, {"weekday", "weekday"}, {"interval", "interval_hours"}, {"retention", "retention"}} {
		if strings.HasPrefix(message, item[0]) {
			d.Form.Fields[item[1]] = "common.error_validation"
		}
	}
	if domain.CodeOf(err) == domain.CodeInvalidRequest {
		d.Error = backup.ErrorText(err, s.localeFor(r))
	} else {
		d.Error = s.t(r, "common.error_generic")
		s.logError(r, "schedule save", err)
	}
	s.operationalFormStatus(w, r, &d.Form, err)
	d.Form.Error = d.Error
	_ = s.render(w, r, "backups", "app", d)
}
