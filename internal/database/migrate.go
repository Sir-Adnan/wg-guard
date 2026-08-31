package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"time"

	"github.com/Sir-Adnan/wg-guard/migrations"
)

// PendingCount reports how many embedded migrations are not applied yet —
// the pre-migration automatic backup gate (serve). Forward-only migrations
// mean the applied row count is always a subset of the embedded list.
func (db *DB) PendingCount(ctx context.Context) (int, error) {
	entries, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return 0, fmt.Errorf("database: list migrations: %w", err)
	}
	var applied int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM migrations`).Scan(&applied); err != nil {
		// No migrations table yet: everything is pending.
		return len(entries), nil
	}
	pending := len(entries) - applied
	if pending < 0 {
		pending = 0
	}
	return pending, nil
}

// Migrate applies pending embedded migrations, each inside its own
// transaction. Forward-only; applied versions are recorded in `migrations`
// (docs/architecture/database.md).
func (db *DB) Migrate(ctx context.Context, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	entries, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return fmt.Errorf("database: list migrations: %w", err)
	}
	sort.Strings(entries)

	if err := db.ensureMigrationsTable(ctx); err != nil {
		return err
	}
	applied := map[string]bool{}
	rows, err := db.QueryContext(ctx, `SELECT version FROM migrations`)
	if err != nil {
		return fmt.Errorf("database: read applied migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("database: scan applied migrations: %w", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("database: iterate applied migrations: %w", err)
	}

	for _, name := range entries {
		if applied[name] {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return fmt.Errorf("database: read migration %s: %w", name, err)
		}
		start := time.Now()
		err = db.WithTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx,
				`INSERT INTO migrations (version, applied_at) VALUES (?, ?)`, name, nowUTC())
			return err
		})
		if err != nil {
			return fmt.Errorf("database: apply migration %s: %w", name, err)
		}
		log.Info("migration applied", "version", name, "duration", time.Since(start).Round(time.Millisecond))
	}
	return nil
}

func (db *DB) ensureMigrationsTable(ctx context.Context) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("database: create migrations table: %w", err)
	}
	return nil
}

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// AppliedVersions returns the sorted list of applied migration names
// (diagnostics / doctor).
func (db *DB) AppliedVersions(ctx context.Context) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
