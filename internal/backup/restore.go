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

// PendingRestore describes the staged payload waiting for the next boot.
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
}

const (
	pendingDirName = "restore.pending"
	pendingMeta    = "meta.json"
)

// Stage verifies an archive end-to-end and stages it for apply: decrypt →
// untar → checksums → schema check → out-of-place migrate → integrity check.
// Nothing on the live node is touched; apply happens with the service
// stopped (CLI ApplyStaged) or at the next boot (serve consumes the staging
// dir before opening the database).
func (s *Service) Stage(ctx context.Context, archivePath, password string) (*PendingRestore, *RestoreReport, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, nil, domain.E(domain.CodeNotFound, "backup: open archive: %v", archivePath)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}

	members, manifest, encrypted, err := readArchive(f, password)
	if err != nil {
		return nil, nil, err
	}
	if manifest.Schema > SchemaVersion {
		return nil, nil, domain.E(domain.CodeInvalidRequest,
			"backup: archive schema %d is newer than this app supports (%d); upgrade first",
			manifest.Schema, SchemaVersion)
	}
	if _, ok := members[DBMember]; !ok {
		return nil, nil, domain.E(domain.CodeInvalidRequest, "backup: archive has no database member")
	}

	// Stage out-of-place, then migrate + integrity-check the staged copy.
	pending := filepath.Join(s.Cfg.DataDir, pendingDirName)
	if err := os.RemoveAll(pending); err != nil {
		return nil, nil, fmt.Errorf("backup: clear staging: %w", err)
	}
	if err := os.MkdirAll(pending, 0o700); err != nil {
		return nil, nil, fmt.Errorf("backup: staging dir: %w", err)
	}
	for name, data := range members {
		p := filepath.Join(pending, filepath.Base(name))
		if err := os.WriteFile(p, data, 0o600); err != nil {
			return nil, nil, fmt.Errorf("backup: stage %s: %w", name, err)
		}
	}

	report := &RestoreReport{
		Archive:    filepath.Base(archivePath),
		Hostname:   manifest.Hostname,
		AppVersion: manifest.AppVersion,
		Encrypted:  encrypted,
		HasKey:     len(members[KeyMember]) > 0,
		Warnings:   []string{},
	}
	if t, err := time.Parse(time.RFC3339, manifest.CreatedAt); err == nil {
		report.CreatedAt = t
	}
	if !report.HasKey {
		report.Warnings = append(report.Warnings,
			"archive has no master key: device private keys cannot be decrypted after restore — peers survive but configs must be re-enrolled")
	}
	if len(members[ConfigMember]) == 0 {
		report.Warnings = append(report.Warnings,
			"archive has no boot config: TLS mode and listen address stay as configured on this host")
	} else {
		report.TLSMode, report.Listen = parseConfigHead(string(members[ConfigMember]))
	}

	if err := s.prepareStagedDB(ctx, pending, report); err != nil {
		os.RemoveAll(pending)
		return nil, nil, err
	}

	pr := &PendingRestore{Dir: pending, Archive: report.Archive, Manifest: *manifest, StagedAt: time.Now().UTC()}
	if err := s.writeStagedMeta(pr, st.Size()); err != nil {
		return nil, nil, err
	}
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
}

func (s *Service) writeStagedMeta(pr *PendingRestore, size int64) error {
	files := map[string]string{}
	for _, name := range []string{ManifestName, DBMember, ConfigMember, KeyMember} {
		if h := fileHash(filepath.Join(pr.Dir, name)); h != "" {
			files[name] = h
		}
	}
	pr.Files = files
	pr.Size = size
	b, err := json.MarshalIndent(stagedMeta{
		Archive: pr.Archive, StagedAt: pr.StagedAt.Format(time.RFC3339),
		Size: size, Manifest: pr.Manifest, Files: files,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pr.Dir, pendingMeta), b, 0o600)
}

// readArchive returns the verified members keyed by name.
func readArchive(r io.Reader, password string) (map[string][]byte, *Manifest, bool, error) {
	kind, br, err := sniffContainer(r)
	if err != nil {
		return nil, nil, false, err
	}
	encrypted := kind == "age"
	var src io.Reader = br
	if encrypted {
		src, err = ageDecrypt(br, password)
		if err != nil {
			return nil, nil, true, err
		}
	}
	tr, err := newTarReader(src)
	if err != nil {
		return nil, nil, encrypted, err
	}

	members := map[string][]byte{}
	for {
		hdr, err := tr.tar.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, encrypted, fmt.Errorf("backup: tar: %w", err)
		}
		name := filepath.Base(hdr.Name) // never honor paths inside archives
		switch name {
		case ManifestName, DBMember, ConfigMember, KeyMember:
		default:
			continue // forward compatibility: ignore unknown members
		}
		data, err := io.ReadAll(io.LimitReader(tr.tar, 1<<32)) // 4 GiB ceiling
		if err != nil {
			return nil, nil, encrypted, fmt.Errorf("backup: read %s: %w", name, err)
		}
		members[name] = data
	}

	// The tar stream ends at its end-of-archive marker, before the gzip
	// trailer: drain the remainder so the container checksums (gzip CRC, age
	// chunk MACs) are enforced over every byte, padding included.
	if _, err := io.Copy(io.Discard, tr.gz); err != nil {
		return nil, nil, encrypted, fmt.Errorf("backup: container checksum failed: %w", err)
	}

	raw, ok := members[ManifestName]
	if !ok {
		return nil, nil, encrypted, domain.E(domain.CodeInvalidRequest, "backup: archive has no manifest")
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, nil, encrypted, domain.E(domain.CodeInvalidRequest, "backup: manifest unreadable")
	}
	for name, want := range manifest.Files {
		if got := hashBytes(members[name]); got != want {
			return nil, nil, encrypted, domain.E(domain.CodeInvalidRequest,
				"backup: checksum mismatch for %s (archive is corrupt or was modified)", name)
		}
	}
	return members, &manifest, encrypted, nil
}

// prepareStagedDB forward-migrates the staged copy and fills the report's
// environment fields from the staged data.
func (s *Service) prepareStagedDB(ctx context.Context, dir string, report *RestoreReport) error {
	dbPath := filepath.Join(dir, DBMember)
	if err := checkStagedVersion(dbPath); err != nil {
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
	report.Interfaces = stagedInterfaces(ctx2, db)
	return nil
}

// checkStagedVersion refuses archives written by a newer app and non-WG-Guard
// databases. Migration versions are zero-padded filenames, so lexicographic
// order is the chronological order.
func checkStagedVersion(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
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

func stagedSetting(ctx context.Context, db *database.DB, key string) string {
	var v string
	_ = db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v
}

func stagedInterfaces(ctx context.Context, db *database.DB) []IfaceSummary {
	rows, err := db.QueryContext(ctx, `SELECT name, listen_port, ipv4_subnet FROM interfaces ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []IfaceSummary
	for rows.Next() {
		var f IfaceSummary
		var subnet sql.NullString
		if err := rows.Scan(&f.Name, &f.Port, &subnet); err == nil {
			f.Subnet = subnet.String
			out = append(out, f)
		}
	}
	return out
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

// Pending returns the staged payload if one is waiting, nil otherwise.
func (s *Service) Pending() (*PendingRestore, error) {
	dir := filepath.Join(s.Cfg.DataDir, pendingDirName)
	raw, err := os.ReadFile(filepath.Join(dir, pendingMeta))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: read staged meta: %w", err)
	}
	var meta struct {
		Archive  string            `json:"archive"`
		StagedAt string            `json:"staged_at"`
		Size     int64             `json:"size"`
		Manifest Manifest          `json:"manifest"`
		Files    map[string]string `json:"files"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("backup: staged meta unreadable — inspect %s", dir)
	}
	stagedAt, _ := time.Parse(time.RFC3339, meta.StagedAt)
	return &PendingRestore{
		Dir: dir, Archive: meta.Archive, Size: meta.Size, StagedAt: stagedAt,
		Manifest: meta.Manifest, Files: meta.Files,
	}, nil
}

// DiscardPending removes a staged restore without applying it.
func (s *Service) DiscardPending() error {
	return os.RemoveAll(filepath.Join(s.Cfg.DataDir, pendingDirName))
}

// ApplyStaged swaps the staged payload into place. The caller MUST have the
// service stopped (CLI guards this; serve calls it before opening the DB).
// The replaced state is kept beside the originals as *.pre-restore.
func (s *Service) ApplyStaged(ctx context.Context) (*RestoreReport, error) {
	pending := filepath.Join(s.Cfg.DataDir, pendingDirName)
	stagedDB := filepath.Join(pending, DBMember)
	stagedKey := filepath.Join(pending, KeyMember)
	if _, err := os.Stat(stagedDB); err != nil {
		return nil, domain.E(domain.CodeInvalidRequest, "backup: no staged restore to apply")
	}

	// Re-verify at apply time against the hashes captured at stage time —
	// staged files could have drifted.
	if raw, err := os.ReadFile(filepath.Join(pending, pendingMeta)); err == nil {
		var meta struct {
			Files map[string]string `json:"files"`
		}
		if json.Unmarshal(raw, &meta) == nil && len(meta.Files) > 0 {
			for name, want := range meta.Files {
				if got := fileHash(filepath.Join(pending, name)); got != want {
					return nil, domain.E(domain.CodeInvalidRequest,
						"backup: staged %s failed checksum re-verification", name)
				}
			}
		}
	}

	report := &RestoreReport{Warnings: []string{}}

	// Safety snapshot of the current state (best effort — a fresh node may
	// have nothing to protect), then the swap. A stale -wal/-shm from the
	// replaced database must never survive into the new files.
	if err := setAside(s.Cfg.DatabasePath); err != nil {
		return nil, fmt.Errorf("backup: protect current database: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(s.Cfg.DatabasePath + suffix)
	}
	if err := copyFile(stagedDB, s.Cfg.DatabasePath, 0o600); err != nil {
		return nil, fmt.Errorf("backup: apply database: %w", err)
	}
	if _, err := os.Stat(stagedKey); err == nil {
		if err := setAside(s.Cfg.MasterKeyFile); err != nil {
			return nil, fmt.Errorf("backup: protect current master key: %w", err)
		}
		if err := copyFile(stagedKey, s.Cfg.MasterKeyFile, 0o600); err != nil {
			return nil, fmt.Errorf("backup: apply master key: %w", err)
		}
	} else if s.keyExists() {
		report.Warnings = append(report.Warnings,
			"archive has no master key; the existing master key was kept — device keys encrypted under it still decrypt, but keys written by the source node do not")
	}

	// Archived boot config is staged beside the live one, never applied.
	if data, err := os.ReadFile(filepath.Join(pending, ConfigMember)); err == nil && s.ConfigPath != "" {
		dst := s.ConfigPath + ".restored"
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			report.Warnings = append(report.Warnings, "archived boot config could not be staged: "+err.Error())
		} else {
			report.Warnings = append(report.Warnings,
				"archived boot config staged at "+dst+" — review it before replacing "+s.ConfigPath)
		}
	}

	if err := s.DiscardPending(); err != nil {
		report.Warnings = append(report.Warnings, "staging dir cleanup failed: "+err.Error())
	}
	return report, nil
}

// ConsumePendingRestore is the boot path: if a staged restore is waiting,
// apply it before the database is opened. It returns the applied archive
// name (” when nothing was pending). Failures never abort boot — the
// operator decides what to do with a broken staging dir.
func (s *Service) ConsumePendingRestore() string {
	pending, err := s.Pending()
	if err != nil || pending == nil {
		if err != nil && s.Log != nil {
			s.Log.Warn("restore staging unreadable; leaving it in place", "err", err)
		}
		return ""
	}
	report, err := s.ApplyStaged(context.Background())
	if err != nil {
		if s.Log != nil {
			s.Log.Error("pending restore FAILED verification; boot continues with the existing database", "err", err)
		}
		return ""
	}
	if s.Log != nil {
		s.Log.Info("staged restore applied at boot",
			"archive", pending.Archive, "warnings", len(report.Warnings))
		for _, w := range report.Warnings {
			s.Log.Warn("restore note", "detail", w)
		}
	}
	return pending.Archive
}

// setAside renames path to path.pre-restore (best effort on ENOENT).
func setAside(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.Remove(path + ".pre-restore"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(path, path+".pre-restore")
}

// keyExists reports whether the master key file is present.
func (s *Service) keyExists() bool {
	_, err := os.Stat(s.Cfg.MasterKeyFile)
	return err == nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
