package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/operation"
	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

const maxStreamingCSRFBytes = 128

// A stale form/download remains safe if the administrative engine is absent.
// Permission checks precede availability, and no request body is consumed here.
func (s *Server) requireBackup(next http.HandlerFunc) http.HandlerFunc {
	return s.requirePermission(auth.ScopeBackupManage, func(w http.ResponseWriter, r *http.Request) {
		if s.Backup == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = s.render(w, r, "backups", "app", s.backupsData(r))
			return
		}
		next(w, r)
	})
}

// backupsData feeds the /backups screen: archive list, schedules, telegram
// state, the pending-restore banner and the restore review card.
type backupsData struct {
	Receipt                                                operation.Receipt
	Error                                                  string // localized message or safe engine error text
	Field                                                  string
	Available, ArchivesKnown, SchedulesKnown, PendingKnown bool
	TelegramReady, SettingsKnown                           bool
	ScheduleOpen                                           bool
	Tab                                                    string
	Verification                                           bool
	PreviewsKnown                                          bool
	Previews                                               []backup.PreviewInfo
	Preview                                                *backup.PendingRestore
	ArchiveChoices                                         []backup.ArchiveInfo
	Limit                                                  int
	Cursor, NextCursor                                     string
	TargetTLS, TargetListen                                string
	RestoreName                                            string
	Form                                                   operationalForm

	Archives  []backup.ArchiveInfo
	Schedules []*backup.Schedule
	Pending   *backup.PendingRestore

	TelegramSet  bool
	TelegramChat string
	PasswordSet  bool
	Retention    int

	// Review is either a saved private preview report or a non-destructive
	// verification result. Only an exact private preview can be approved.
	Review          *backup.RestoreReport
	ReportCounts    []backupReportCount
	Warnings        []string
	LifecycleBackup *updatequeue.RecoveryBackup

	// Submitted schedule form values redisplayed after a validation error.
	SchedForm scheduleForm
}

type backupReportCount struct {
	Label string
	Value uint64
}

func backupReportCounts(report *backup.RestoreReport) []backupReportCount {
	v := report.Inventory
	return []backupReportCount{
		{"nav.users", v.Users}, {"ops.family.devices", v.Devices},
		{"backups.review_templates", v.Templates}, {"nav.admins", v.Admins},
		{"nav.tokens", v.APITokens}, {"backups.customer_links", v.CustomerLinks},
	}
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
	d := backupsData{Available: s.Backup != nil, SettingsKnown: true, RestoreName: r.URL.Query().Get("restore"), Form: scheduleOperationalForm(nil), Tab: backupTab(r), Limit: 25, Cursor: r.URL.Query().Get("cursor")}
	if limit, _ := strconv.Atoi(r.URL.Query().Get("limit")); limit == 50 || limit == 100 {
		d.Limit = limit
	}
	if s.UpdateQueue != nil && (maintenanceCan(r, "update.read") || maintenanceCan(r, "update.manage")) {
		if inventory, err := s.UpdateQueue.Inventory(); err == nil {
			d.LifecycleBackup = inventory.Backup
		}
	}
	if r.URL.Path == "/backups/restore" || r.URL.Path == "/backups/verify" {
		d.RestoreName = r.PostFormValue("name")
	}
	if s.Backup != nil {
		d.TargetTLS, d.TargetListen = string(s.Backup.Cfg.TLS.Mode), s.Backup.Cfg.HTTPListen
		if page, err := s.Backup.ListPage(d.Limit, d.Cursor); err == nil {
			d.Archives = page.Items
			d.NextCursor = page.NextCursor
			d.ArchivesKnown = true
		}
		d.ArchiveChoices = append([]backup.ArchiveInfo(nil), d.Archives...)
		if d.RestoreName != "" {
			found := false
			for _, archive := range d.ArchiveChoices {
				found = found || archive.Name == d.RestoreName
			}
			if !found {
				if file, size, err := s.Backup.Open(d.RestoreName); err == nil {
					file.Close()
					d.ArchiveChoices = append(d.ArchiveChoices, backup.ArchiveInfo{Name: d.RestoreName, Size: size})
				}
			}
		}
		if pending, err := s.Backup.PendingSummary(); err == nil {
			d.Pending = pending
			d.PendingKnown = true
		}
		if schedules, err := s.Backup.Schedules(ctx); err == nil {
			d.Schedules = schedules
			d.SchedulesKnown = true
		}
		if d.Tab == "restore" {
			if previews, err := s.Backup.Previews(); err == nil {
				d.Previews, d.PreviewsKnown = previews, true
			}
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
	if d.Pending != nil {
		d.Receipt = operation.Present(d.Pending.PreviewID(), operation.AwaitingRestart, false)
	}
	return d
}

func backupTab(r *http.Request) string {
	q := r.URL.Query()
	if q.Get("schedule") != "" || strings.HasPrefix(r.URL.Path, "/backups/schedules") {
		return "schedules"
	}
	if q.Get("restore") != "" || q.Get("preview") != "" || strings.HasPrefix(r.URL.Path, "/backups/restore") || r.URL.Path == "/backups/verify" || r.URL.Path == "/backups/import" {
		return "restore"
	}
	if r.URL.Path == "/backups/telegram-test" {
		return "delivery"
	}
	switch q.Get("tab") {
	case "restore", "schedules", "delivery":
		return q.Get("tab")
	default:
		return "archives"
	}
}

// handleBackupsPage renders the backups/ops screen.
func (s *Server) handleBackupsPage(w http.ResponseWriter, r *http.Request) {
	d := s.backupsData(r)
	if id := r.URL.Query().Get("preview"); id != "" && d.Available {
		preview, err := s.Backup.Preview(id)
		if err != nil {
			d.Error = backup.ErrorText(err, s.localeFor(r))
		} else {
			d.Preview, d.Review = preview, preview.Report
			d.ReportCounts = backupReportCounts(d.Review)
			d.Warnings = backup.WarningTexts(d.Review.Warnings, s.localeFor(r))
			if d.Pending == nil {
				d.Receipt = operation.Present(preview.PreviewID(), operation.Review, false)
			}
		}
	}
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
	download := r.PostFormValue("download") == "1"
	reason := "manual"
	if download {
		reason = "manual-download"
	}
	res, err := s.Backup.Create(r.Context(), backup.CreateOpts{
		Password: password,
		Reason:   reason,
		Deliver:  !download,
	})
	if err != nil {
		s.backupError(w, r, err)
		return
	}
	s.audit(r, "backup.created", res.Name, map[string]any{
		"size": res.Size, "encrypted": res.Encrypted, "delivered": res.Delivered,
	})
	if download {
		s.serveBackupArchive(w, r, res.Name)
		return
	}
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
	s.serveBackupArchive(w, r, r.PathValue("name"))
}

func (s *Server) serveBackupArchive(w http.ResponseWriter, r *http.Request, name string) {
	f, size, err := s.Backup.Open(name)
	if err != nil {
		s.backupError(w, r, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", name))
	http.ServeContent(w, r, name, time.Now(), f)
	_ = size
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
	s.redirectToast(w, r, "/backups?tab=delivery", "backups.toast.telegram_ok")
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
