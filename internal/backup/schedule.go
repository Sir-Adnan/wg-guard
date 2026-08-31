package backup

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// Schedule kinds (docs/operations/backup-restore.md §Sources): all times are
// stored UTC and displayed server-local by the callers.
const (
	KindDaily    = "daily"
	KindInterval = "interval"
	KindWeekly   = "weekly"
)

// Schedule is one stored backup schedule row.
type Schedule struct {
	ID             string
	Name           string
	Kind           string
	TimeOfDay      string // "HH:MM" UTC (daily/weekly)
	Weekday        int    // 0=Sunday..6=Saturday (weekly)
	IntervalHours  int    // (interval)
	Enabled        bool
	RetentionCount int // 0 = settings default
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastRunAt      *time.Time
	LastStatus     string // "" | ok | failed
	NextRunAt      time.Time
}

var (
	timeOfDayRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	schedNameRe = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,63}$`)
)

// Validate checks one schedule's fields; next-run computation depends on it.
func (sc *Schedule) Validate() error {
	if !schedNameRe.MatchString(sc.Name) {
		return domain.E(domain.CodeInvalidRequest, "backup: schedule name must be 1-64 characters (letters, digits, space, . _ -)")
	}
	switch sc.Kind {
	case KindDaily:
	case KindWeekly:
		if sc.Weekday < 0 || sc.Weekday > 6 {
			return domain.E(domain.CodeInvalidRequest, "backup: weekday must be 0 (Sunday) through 6 (Saturday)")
		}
	case KindInterval:
		if sc.IntervalHours < 1 || sc.IntervalHours > 24*7 {
			return domain.E(domain.CodeInvalidRequest, "backup: interval hours must be 1..168")
		}
	default:
		return domain.E(domain.CodeInvalidRequest, "backup: schedule kind must be daily, interval or weekly")
	}
	if sc.Kind != KindInterval && !timeOfDayRe.MatchString(sc.TimeOfDay) {
		return domain.E(domain.CodeInvalidRequest, "backup: time must be HH:MM (00:00–23:59)")
	}
	if sc.RetentionCount < 0 || sc.RetentionCount > 365 {
		return domain.E(domain.CodeInvalidRequest, "backup: retention must be 0..365 (0 = panel default)")
	}
	return nil
}

// NextRun computes the first fire time strictly after from (UTC).
func NextRun(sc *Schedule, from time.Time) time.Time {
	from = from.UTC()
	switch sc.Kind {
	case KindInterval:
		return from.Add(time.Duration(sc.IntervalHours) * time.Hour)
	case KindDaily:
		return nextWallClock(from, -1, sc.TimeOfDay)
	default: // weekly
		return nextWallClock(from, sc.Weekday, sc.TimeOfDay)
	}
}

// nextWallClock finds the next day matching weekday (-1 = any day) at HH:MM.
func nextWallClock(from time.Time, weekday int, tod string) time.Time {
	var h, m int
	fmt.Sscanf(tod, "%02d:%02d", &h, &m)
	candidate := time.Date(from.Year(), from.Month(), from.Day(), h, m, 0, 0, time.UTC)
	for i := 0; i < 8; i++ { // 7 days always contain the target slot
		if candidate.After(from) && (weekday < 0 || int(candidate.Weekday()) == weekday) {
			return candidate
		}
		candidate = candidate.AddDate(0, 0, 1)
	}
	return from.AddDate(0, 0, 1)
}

// --- store --------------------------------------------------------------------

const scheduleCols = `id, name, kind, time_of_day, weekday, interval_hours, enabled,
	retention_count, created_at, updated_at, last_run_at, last_status, next_run_at`

func scanSchedule(row interface{ Scan(...any) error }) (*Schedule, error) {
	var sc Schedule
	var created, updated, next string
	var lastRun, lastStatus sql.NullString
	var enabled int
	if err := row.Scan(&sc.ID, &sc.Name, &sc.Kind, &sc.TimeOfDay, &sc.Weekday,
		&sc.IntervalHours, &enabled, &sc.RetentionCount,
		&created, &updated, &lastRun, &lastStatus, &next); err != nil {
		return nil, err
	}
	sc.LastStatus = lastStatus.String
	sc.Enabled = enabled == 1
	sc.CreatedAt = parseTime(created)
	sc.UpdatedAt = parseTime(updated)
	sc.NextRunAt = parseTime(next)
	if lastRun.Valid && lastRun.String != "" {
		t := parseTime(lastRun.String)
		sc.LastRunAt = &t
	}
	return &sc, nil
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// Schedules lists all schedules, soonest-due first.
func (s *Service) Schedules(ctx context.Context) ([]*Schedule, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+scheduleCols+` FROM backup_schedules ORDER BY next_run_at, name`)
	if err != nil {
		return nil, fmt.Errorf("backup: list schedules: %w", err)
	}
	defer rows.Close()
	var out []*Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// GetSchedule fetches one schedule.
func (s *Service) GetSchedule(ctx context.Context, id string) (*Schedule, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+scheduleCols+` FROM backup_schedules WHERE id = ?`, id)
	sc, err := scanSchedule(row)
	if err == sql.ErrNoRows {
		return nil, domain.E(domain.CodeNotFound, "backup: unknown schedule")
	}
	return sc, err
}

// CreateSchedule inserts a new schedule (first run computed from now).
func (s *Service) CreateSchedule(ctx context.Context, sc *Schedule) (*Schedule, error) {
	if err := sc.Validate(); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	sc.ID = newNonce()
	sc.CreatedAt, sc.UpdatedAt = now, now
	sc.LastStatus = ""
	sc.NextRunAt = NextRun(sc, now)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO backup_schedules
		(id, name, kind, time_of_day, weekday, interval_hours, enabled, retention_count,
		 created_at, updated_at, last_run_at, last_status, next_run_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sc.ID, sc.Name, sc.Kind, sc.TimeOfDay, sc.Weekday, sc.IntervalHours,
		boolInt(sc.Enabled), sc.RetentionCount,
		formatTime(sc.CreatedAt), formatTime(sc.UpdatedAt), nil, nil, formatTime(sc.NextRunAt))
	if err != nil {
		return nil, fmt.Errorf("backup: create schedule: %w", err)
	}
	s.auditSchedule(ctx, "backup.schedule_created", sc)
	return sc, nil
}

// UpdateSchedule replaces the editable fields and recomputes the next run.
func (s *Service) UpdateSchedule(ctx context.Context, id string, in *Schedule) (*Schedule, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	cur, err := s.GetSchedule(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	cur.Name, cur.Kind, cur.TimeOfDay, cur.Weekday, cur.IntervalHours =
		in.Name, in.Kind, in.TimeOfDay, in.Weekday, in.IntervalHours
	cur.Enabled, cur.RetentionCount = in.Enabled, in.RetentionCount
	cur.UpdatedAt = now
	cur.NextRunAt = NextRun(cur, now)
	_, err = s.DB.ExecContext(ctx, `UPDATE backup_schedules SET
		name=?, kind=?, time_of_day=?, weekday=?, interval_hours=?, enabled=?,
		retention_count=?, updated_at=?, next_run_at=? WHERE id=?`,
		cur.Name, cur.Kind, cur.TimeOfDay, cur.Weekday, cur.IntervalHours,
		boolInt(cur.Enabled), cur.RetentionCount, formatTime(cur.UpdatedAt),
		formatTime(cur.NextRunAt), id)
	if err != nil {
		return nil, fmt.Errorf("backup: update schedule: %w", err)
	}
	s.auditSchedule(ctx, "backup.schedule_updated", cur)
	return cur, nil
}

// DeleteSchedule removes one schedule.
func (s *Service) DeleteSchedule(ctx context.Context, id string) error {
	cur, err := s.GetSchedule(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM backup_schedules WHERE id = ?`, id); err != nil {
		return fmt.Errorf("backup: delete schedule: %w", err)
	}
	s.auditSchedule(ctx, "backup.schedule_deleted", cur)
	return nil
}

func (s *Service) auditSchedule(ctx context.Context, action string, sc *Schedule) {
	if s.Audit == nil {
		return
	}
	_ = s.Audit.Record(ctx, audit.Entry{
		ActorType: audit.ActorSystem, Action: action, Target: sc.Name,
		Metadata: map[string]any{"id": sc.ID, "kind": sc.Kind, "enabled": sc.Enabled},
	})
}

// RunDue fires every enabled schedule whose next_run_at has passed: one
// archive per due schedule, each with its own retention, then the schedule
// advances — a missed window (downtime) runs exactly once. The scheduler
// calls this once per minute.
func (s *Service) RunDue(ctx context.Context) (int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+scheduleCols+` FROM backup_schedules
		WHERE enabled = 1 AND next_run_at <= ? ORDER BY next_run_at`,
		formatTime(s.now()))
	if err != nil {
		return 0, fmt.Errorf("backup: due query: %w", err)
	}
	var due []*Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		due = append(due, sc)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	ran := 0
	for _, sc := range due {
		res, err := s.Create(ctx, CreateOpts{
			Reason:     "schedule:" + sc.ID,
			ScheduleID: sc.ID,
			Retention:  sc.RetentionCount,
			Deliver:    true,
		})
		now := s.now().UTC()
		status := "ok"
		if err != nil {
			status = "failed"
			if s.Log != nil {
				s.Log.Error("scheduled backup failed", "schedule", sc.Name, "err", err)
			}
		} else if len(res.Warnings) > 0 && s.Log != nil {
			s.Log.Warn("scheduled backup completed with warnings", "schedule", sc.Name, "warnings", res.Warnings)
		}
		ran++
		// Advance from the missed slot, not from now: a backlog of missed
		// windows still results in exactly one run per schedule.
		next := NextRun(sc, sc.NextRunAt)
		if !next.After(now) {
			next = NextRun(sc, now)
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE backup_schedules
			SET last_run_at=?, last_status=?, next_run_at=? WHERE id=?`,
			formatTime(now), status, formatTime(next), sc.ID); err != nil {
			return ran, fmt.Errorf("backup: advance schedule %s: %w", sc.ID, err)
		}
	}
	return ran, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
