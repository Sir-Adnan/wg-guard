// Package database opens the SQLite store with the pragmas required by the
// low-RAM contract and provides transaction helpers. Driver: modernc.org/sqlite
// (pure Go, CGO_ENABLED=0 — ADR-0005).
package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"runtime"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps sql.DB with the write-transaction helper used by every service.
type DB struct {
	*sql.DB
	path string
}

// Options control Open; zero values are the production defaults.
type Options struct {
	// BusyTimeout waits this long for a locked database (default 5s).
	BusyTimeout time.Duration
	// MaxConns caps the pool. Default: 4 (single-process, low-RAM target);
	// SQLite WAL allows concurrent readers, writers serialize anyway.
	MaxConns int
	// ReadOnly opens without writes (diagnostics). Migrations are not run.
	ReadOnly bool
}

func (o Options) busyTimeout() time.Duration {
	if o.BusyTimeout <= 0 {
		return 5 * time.Second
	}
	return o.BusyTimeout
}

func (o Options) maxConns() int {
	if o.MaxConns <= 0 {
		if n := runtime.NumCPU(); n < 4 {
			return 4
		}
		return 8
	}
	return o.MaxConns
}

// Open opens (creating if needed) the database and applies connection
// pragmas. Write transactions run in BEGIN IMMEDIATE mode (dsn _txlock), so
// check-then-write sequences inside WithTx are race-free — this is what makes
// the device-limit and IP-allocation invariants hold (database.md).
func Open(path string, opts Options) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("database: empty path")
	}
	q := url.Values{}
	q.Add("_txlock", "immediate")
	q.Add("_pragma", "busy_timeout("+fmt.Sprint(int(opts.busyTimeout().Milliseconds()))+")")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "foreign_keys(1)")
	// Negative cache_size = KiB; 8 MiB caps page-cache RAM on big tables.
	q.Add("_pragma", "cache_size(-8000)")
	dsn := "file:" + path + "?" + q.Encode()

	var (
		db  *sql.DB
		err error
	)
	if opts.ReadOnly {
		db, err = sql.Open("sqlite", dsn+"&mode=ro")
	} else {
		db, err = sql.Open("sqlite", dsn)
	}
	if err != nil {
		return nil, fmt.Errorf("database: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(opts.maxConns())
	db.SetMaxIdleConns(opts.maxConns())
	db.SetConnMaxIdleTime(2 * time.Minute)

	// Fail fast on an unusable file (bad path, corrupt header) instead of at
	// first query.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: ping %s: %w", path, err)
	}
	return &DB{DB: db, path: path}, nil
}

// Path returns the file path the database was opened with.
func (db *DB) Path() string { return db.path }

// WithTx runs fn inside a write transaction (BEGIN IMMEDIATE via the driver
// DSN), retrying on transient lock contention beyond busy_timeout is left to
// the driver. A panic or error rolls back.
func (db *DB) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("database: begin: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("database: commit: %w", err)
	}
	return nil
}

// Close closes the pool. WAL files persist by design (crash recovery).
func (db *DB) Close() error { return db.DB.Close() }
