package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

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
	s.redirectToast(w, r, "/backups?tab=schedules", "backups.toast.schedule_created")
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
	s.redirectToast(w, r, "/backups?tab=schedules", "backups.toast.schedule_updated")
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
	s.redirectToast(w, r, "/backups?tab=schedules", "backups.toast.schedule_deleted")
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
		s.redirectToast(w, r, "/backups?tab=schedules", "backups.toast.schedule_enabled")
	} else {
		s.redirectToast(w, r, "/backups?tab=schedules", "backups.toast.schedule_disabled")
	}
}
