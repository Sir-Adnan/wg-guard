-- Real backup schedules replace the Phase 1 placeholder (never written by
-- any code): run tracking + the due-scan index join here. All times UTC.
DROP TABLE IF EXISTS backup_schedules;
CREATE TABLE backup_schedules (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    kind            TEXT NOT NULL,               -- daily | interval | weekly
    time_of_day     TEXT NOT NULL DEFAULT '03:00', -- HH:MM UTC (daily/weekly)
    weekday         INTEGER NOT NULL DEFAULT 0,    -- 0=Sunday..6=Saturday (weekly)
    interval_hours  INTEGER NOT NULL DEFAULT 24,   -- every N hours (interval)
    enabled         INTEGER NOT NULL DEFAULT 1,
    retention_count INTEGER NOT NULL DEFAULT 0,    -- 0 = panel default (settings)
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    last_run_at     TEXT,
    last_status     TEXT,                          -- ok | failed
    next_run_at     TEXT NOT NULL
);
CREATE INDEX idx_backup_schedules_due ON backup_schedules (enabled, next_run_at);
