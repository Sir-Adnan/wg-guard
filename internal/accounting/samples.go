package accounting

// Sample persistence: traffic_samples (fine-grained deltas, bounded
// retention) and traffic_rollups (hourly 30d / daily 1y aggregates), per the
// schema contract in docs/architecture/database.md.
//
// Deliberate design point: deltas are buffered in an in-memory accumulator
// and flushed on `accounting.sample_flush_seconds` (default 300s), NOT
// written every cycle. At 1000 concurrent active devices, per-cycle samples
// would be ~5.8M rows/day (hundreds of MB); a 5-minute flush is ~288k
// rows/day (~20 MB) — same dashboards, bounded churn. The accumulator never
// touches correctness: accumulated totals are persisted every cycle, so a
// crash can only lose chart granularity for one flush interval, never usage.

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

type sampleKey struct {
	device string
	bucket time.Time // aligned to the flush interval (UTC)
}

type sampleValue struct{ rx, tx uint64 }

// accumulator is the bounded in-memory delta buffer. Entries ≤ active
// devices × a few buckets; a flush drains it entirely.
type accumulator struct {
	mu sync.Mutex
	m  map[sampleKey]sampleValue
}

func (a *accumulator) push(deviceID string, at time.Time, interval time.Duration, rx, tx uint64) {
	if rx == 0 && tx == 0 {
		return
	}
	bucket := at
	if interval > 0 {
		bucket = at.Truncate(interval)
	}
	a.mu.Lock()
	if a.m == nil {
		a.m = map[sampleKey]sampleValue{}
	}
	k := sampleKey{deviceID, bucket}
	v := a.m[k]
	v.rx += rx
	v.tx += tx
	a.m[k] = v
	a.mu.Unlock()
}

// clearDevices drops buffered deltas for the given devices (a traffic reset
// makes pending samples stale).
func (a *accumulator) clearDevices(ids []string) {
	if len(ids) == 0 {
		return
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	a.mu.Lock()
	for k := range a.m {
		if set[k.device] {
			delete(a.m, k)
		}
	}
	a.mu.Unlock()
}

// take snapshots and drains the buffer.
func (a *accumulator) take() map[sampleKey]sampleValue {
	a.mu.Lock()
	defer a.mu.Unlock()
	batch := a.m
	a.m = nil
	return batch
}

// FlushSamples persists buffered deltas: one row per device per bucket in
// traffic_samples, and accumulating upserts into the hourly and daily
// rollups — all in ONE transaction, so a retried flush can never double
// count. Returns the number of sample rows written.
func (s *Service) FlushSamples(ctx context.Context) (int, error) {
	batch := s.samples.take()
	if len(batch) == 0 {
		return 0, nil
	}
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		sampleStmt, err := tx.PrepareContext(ctx, `INSERT INTO traffic_samples (device_id, ts, rx_delta, tx_delta)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(device_id, ts) DO UPDATE SET
				rx_delta = rx_delta + excluded.rx_delta, tx_delta = tx_delta + excluded.tx_delta`)
		if err != nil {
			return err
		}
		defer sampleStmt.Close()
		rollupStmt, err := tx.PrepareContext(ctx, `INSERT INTO traffic_rollups (device_id, granularity, bucket_start, rx, tx)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(device_id, granularity, bucket_start) DO UPDATE SET
				rx = rx + excluded.rx, tx = tx + excluded.tx`)
		if err != nil {
			return err
		}
		defer rollupStmt.Close()

		for k, v := range batch {
			ts := k.bucket.UTC().Format(time.RFC3339Nano)
			if _, err := sampleStmt.ExecContext(ctx, k.device, ts, int64(v.rx), int64(v.tx)); err != nil {
				return fmt.Errorf("accounting: sample upsert %s: %w", k.device, err)
			}
			hourly := k.bucket.Truncate(time.Hour).UTC().Format(time.RFC3339Nano)
			if _, err := rollupStmt.ExecContext(ctx, k.device, "hourly", hourly, int64(v.rx), int64(v.tx)); err != nil {
				return fmt.Errorf("accounting: hourly upsert %s: %w", k.device, err)
			}
			daily := k.bucket.Truncate(24 * time.Hour).UTC().Format(time.RFC3339Nano)
			if _, err := rollupStmt.ExecContext(ctx, k.device, "daily", daily, int64(v.rx), int64(v.tx)); err != nil {
				return fmt.Errorf("accounting: daily upsert %s: %w", k.device, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(batch), nil
}

// PruneReport counts removed rows.
type PruneReport struct {
	Samples, Hourly, Daily int64
}

// Prune enforces the retention policy: samples for
// `accounting.sample_retention_hours` (24–48 h), hourly rollups for
// `accounting.rollup_hourly_days`, daily for `accounting.rollup_daily_days`.
func (s *Service) Prune(ctx context.Context, now time.Time) (*PruneReport, error) {
	const (
		defSampleHours = 48
		defHourlyDays  = 30
		defDailyDays   = 365
	)
	sampleHours, hourlyDays, dailyDays := defSampleHours, defHourlyDays, defDailyDays
	if s.reg != nil {
		if v, err := s.reg.GetInt(ctx, "accounting.sample_retention_hours"); err == nil && v > 0 {
			sampleHours = v
		}
		if v, err := s.reg.GetInt(ctx, "accounting.rollup_hourly_days"); err == nil && v > 0 {
			hourlyDays = v
		}
		if v, err := s.reg.GetInt(ctx, "accounting.rollup_daily_days"); err == nil && v > 0 {
			dailyDays = v
		}
	}

	rep := &PruneReport{}
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		if rep.Samples, err = execRowsAffected(tx, `DELETE FROM traffic_samples WHERE ts < ?`,
			now.Add(-time.Duration(sampleHours)*time.Hour).UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("accounting: prune samples: %w", err)
		}
		if rep.Hourly, err = execRowsAffected(tx, `DELETE FROM traffic_rollups WHERE granularity = 'hourly' AND bucket_start < ?`,
			now.AddDate(0, 0, -hourlyDays).UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("accounting: prune hourly: %w", err)
		}
		if rep.Daily, err = execRowsAffected(tx, `DELETE FROM traffic_rollups WHERE granularity = 'daily' AND bucket_start < ?`,
			now.AddDate(0, 0, -dailyDays).UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("accounting: prune daily: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rep, nil
}

func execRowsAffected(tx *sql.Tx, query string, args ...any) (int64, error) {
	res, err := tx.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
