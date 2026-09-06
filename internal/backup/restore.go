package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/migrations"
)

// tarReader bundles the tar stream with its decompressor so one read pass
// keeps both alive.
type tarReader struct {
	tar *tar.Reader
	gz  *gzip.Reader
}

func newTarReader(r io.Reader) (*tarReader, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("backup: gzip: %w", err)
	}
	return &tarReader{tar: tar.NewReader(gz), gz: gz}, nil
}

// IfaceSummary is one interface row from the staged DB (environment review).
type IfaceSummary struct {
	Name   string
	Port   int
	Subnet string
}

// RestoreReport is the environment review produced at stage time: what the
// archive contains vs what this host looks like, with explicit warnings for
// anything an operator must confirm before apply.
type RestoreReport struct {
	Archive    string
	CreatedAt  time.Time
	AppVersion string
	Hostname   string // source host
	NodeID     string // staged node.id
	Endpoint   string // staged node.endpoint
	TLSMode    string // archived boot config ("" when absent)
	Listen     string // archived http_listen
	Interfaces []IfaceSummary
	HasKey     bool // master key member present
	Encrypted  bool
	Warnings   []string
}

// PendingRestore describes a private preview or explicitly approved payload.
// Files holds the hashes captured AFTER staging prep (opening the staged DB
// for migration rewrites its WAL header, so the archive manifest is not the
// right baseline for apply-time re-verification).
type PendingRestore struct {
	Dir      string
	Archive  string
	Size     int64
	StagedAt time.Time
	Manifest Manifest
	Files    map[string]string
	Original bool // no forward migrations; only for recorded lifecycle recovery
}

const (
	pendingDirName = "restore.pending"
	pendingMeta    = "meta.json"
	previewPrefix  = "restore.preview-"
)

// Stage verifies an archive end-to-end and stages it for apply: decrypt →
// untar → checksums → schema check → out-of-place migrate → integrity check.
// Nothing on the live node is touched. A private preview cannot be boot-applied;
// Approve must explicitly publish it before offline or next-boot application.
func (s *Service) Stage(ctx context.Context, archivePath, password string) (*PendingRestore, *RestoreReport, error) {
	return s.stage(ctx, archivePath, password, false)
}

// StageOriginal preserves the exact archived schema and bytes for rollback.
// It never opens active node data or invokes the forward migrator.
func (s *Service) StageOriginal(ctx context.Context, archivePath, password string) (*PendingRestore, *RestoreReport, error) {
	return s.stage(ctx, archivePath, password, true)
}

func (s *Service) stage(ctx context.Context, archivePath, password string, original bool) (*PendingRestore, *RestoreReport, error) {
	input, err := os.Lstat(archivePath)
	if err != nil || !input.Mode().IsRegular() || input.Size() > 8<<30 {
		return nil, nil, fmt.Errorf("backup: archive must be a bounded regular file")
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, nil, domain.E(domain.CodeNotFound, "backup: open archive: %v", archivePath)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > 8<<30 {
		return nil, nil, fmt.Errorf("backup: archive must be a bounded regular file")
	}

	if err := os.MkdirAll(s.Cfg.DataDir, 0700); err != nil {
		return nil, nil, err
	}
	pending, err := os.MkdirTemp(s.Cfg.DataDir, previewPrefix)
	if err != nil {
		return nil, nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(pending)
		}
	}()
	manifest, encrypted, err := extractArchive(ctx, f, password, pending)
	if err != nil {
		return nil, nil, err
	}
	if manifest.Schema > SchemaVersion {
		return nil, nil, domain.E(domain.CodeInvalidRequest,
			"backup: archive schema %d is newer than this app supports (%d); upgrade first",
			manifest.Schema, SchemaVersion)
	}
	if _, ok := manifest.Files[DBMember]; !ok {
		return nil, nil, domain.E(domain.CodeInvalidRequest, "backup: archive has no database member")
	}

	report := &RestoreReport{
		Archive:    filepath.Base(archivePath),
		Hostname:   manifest.Hostname,
		AppVersion: manifest.AppVersion,
		Encrypted:  encrypted,
		HasKey:     manifest.Files[KeyMember] != "",
		Warnings:   []string{},
	}
	if t, err := time.Parse(time.RFC3339, manifest.CreatedAt); err == nil {
		report.CreatedAt = t
	}
	if !report.HasKey {
		report.Warnings = append(report.Warnings,
			"archive has no master key: device private keys cannot be decrypted after restore — peers survive but configs must be re-enrolled")
	}
	if manifest.Files[ConfigMember] == "" {
		report.Warnings = append(report.Warnings,
			"archive has no boot config: TLS mode and listen address stay as configured on this host")
	} else {
		configBytes, err := readSmall(filepath.Join(pending, ConfigMember), maxConfigBytes)
		if err != nil {
			return nil, nil, err
		}
		report.TLSMode, report.Listen = parseConfigHead(string(configBytes))
	}

	if err := s.prepareStagedDB(ctx, pending, report, original); err != nil {
		os.RemoveAll(pending)
		return nil, nil, err
	}

	if original && !report.HasKey {
		return nil, nil, fmt.Errorf("backup: rollback requires the matching archived master key")
	}
	pr := &PendingRestore{Dir: pending, Archive: report.Archive, Manifest: *manifest, StagedAt: time.Now().UTC(), Original: original}
	if err := s.writeStagedMeta(pr, st.Size()); err != nil {
		return nil, nil, err
	}
	keep = true
	return pr, report, nil
}

// writeStagedMeta records the verified manifest beside the staged payload so
// the boot consumer can see what is pending without re-verifying blindly.
type stagedMeta struct {
	Archive  string            `json:"archive"`
	StagedAt string            `json:"staged_at"`
	Size     int64             `json:"size"`
	Manifest Manifest          `json:"manifest"`
	Files    map[string]string `json:"files"`
	Original bool              `json:"original"`
}

func (s *Service) writeStagedMeta(pr *PendingRestore, size int64) error {
	files := map[string]string{}
	for _, name := range []string{ManifestName, DBMember, ConfigMember, KeyMember} {
		if h := fileHash(filepath.Join(pr.Dir, name)); h != "" {
			f, err := os.OpenFile(filepath.Join(pr.Dir, name), os.O_RDWR, 0600)
			if err != nil {
				return err
			}
			err = f.Sync()
			f.Close()
			if err != nil {
				return err
			}
			files[name] = h
		}
	}
	pr.Files = files
	pr.Size = size
	b, err := json.MarshalIndent(stagedMeta{
		Archive: pr.Archive, StagedAt: pr.StagedAt.Format(time.RFC3339),
		Size: size, Manifest: pr.Manifest, Files: files, Original: pr.Original,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeSynced(filepath.Join(pr.Dir, pendingMeta), b); err != nil {
		return err
	}
	return syncDir(pr.Dir)
}

// prepareStagedDB validates and reports the staged copy. Only ordinary restore
// forward-migrates; original-schema recovery opens it immutable and read-only.
func (s *Service) prepareStagedDB(ctx context.Context, dir string, report *RestoreReport, original bool) error {
	dbPath := filepath.Join(dir, DBMember)
	if err := checkStagedVersion(dbPath); err != nil {
		return err
	}
	if original {
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro&immutable=1")
		if err != nil {
			return err
		}
		defer db.Close()
		var integrity string
		if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
			return fmt.Errorf("backup: original database failed integrity check")
		}
		report.NodeID = stagedSetting(ctx, db, "node.id")
		report.Endpoint = stagedSetting(ctx, db, "node.endpoint")
		report.Interfaces, err = stagedInterfaces(ctx, db)
		return err
	}
	db, err := database.Open(dbPath, database.Options{})
	if err != nil {
		return fmt.Errorf("backup: open staged database: %w", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx, s.Log); err != nil {
		return fmt.Errorf("backup: migrate staged database: %w", err)
	}
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return domain.E(domain.CodeInvalidRequest, "backup: staged database failed integrity_check (%s)", integrity)
	}
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	report.NodeID = stagedSetting(ctx2, db, "node.id")
	report.Endpoint = stagedSetting(ctx2, db, "node.endpoint")
	report.Interfaces, err = stagedInterfaces(ctx2, db)
	if err != nil {
		return err
	}
	return nil
}

// checkStagedVersion refuses archives written by a newer app and non-WG-Guard
// databases. Migration versions are zero-padded filenames, so lexicographic
// order is the chronological order.
func checkStagedVersion(dbPath string) error {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro&immutable=1")
	if err != nil {
		return fmt.Errorf("backup: open staged database: %w", err)
	}
	defer db.Close()
	var v string
	if err := db.QueryRow(`SELECT MAX(version) FROM migrations`).Scan(&v); err != nil || v == "" {
		return domain.E(domain.CodeInvalidRequest, "backup: staged database is not a WG-Guard database")
	}
	if v > latestMigrationName() {
		return domain.E(domain.CodeInvalidRequest,
			"backup: archive was written by a newer version of WG-Guard; upgrade this node first")
	}
	return nil
}

// latestMigrationName is the newest embedded migration filename.
func latestMigrationName() string {
	ents, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return ""
	}
	max := ""
	for _, e := range ents {
		if b := filepath.Base(e); b > max {
			max = b
		}
	}
	return max
}

func stagedSetting(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, key string) string {
	var v string
	_ = db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v
}

func stagedInterfaces(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) ([]IfaceSummary, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, listen_port, ipv4_subnet FROM tunnel_interfaces ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("backup: read staged interfaces: %w", err)
	}
	defer rows.Close()
	var out []IfaceSummary
	for rows.Next() {
		var f IfaceSummary
		var subnet sql.NullString
		if err := rows.Scan(&f.Name, &f.Port, &subnet); err != nil {
			return nil, fmt.Errorf("backup: read staged interface row: %w", err)
		}
		f.Subnet = subnet.String
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("backup: read staged interfaces: %w", err)
	}
	return out, nil
}

// parseConfigHead extracts tls mode + listen from the archived boot config
// (report-only; the live config is never replaced automatically).
func parseConfigHead(body string) (mode, listen string) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "="); i > 0 {
			key := strings.TrimSpace(line[:i])
			val := strings.Trim(strings.TrimSpace(line[i+1:]), `"`)
			switch key {
			case "mode":
				mode = val
			case "http_listen":
				listen = val
			}
		}
	}
	return mode, listen
}
