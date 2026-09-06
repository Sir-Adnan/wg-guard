// Package backup implements the .wgg archive engine, schedules and delivery
// sinks (docs/operations/backup-restore.md). Administrative surface only:
// the REST API has no backup endpoints by design (ADR-0007).
//
// An archive is a tar.gz — optionally age-encrypted with the single backup
// password (ADR-0008, filippo.io/age) — containing manifest.json, the SQLite
// snapshot (VACUUM INTO), the boot config and the master key. Restore is
// stage-then-swap: the verified payload is staged out-of-place and the swap
// happens with the service stopped (CLI) or at the next boot (panel wizard),
// never against a live WAL handle.
//
// Security: archives and staging dirs are 0600/0700; the backup password and
// Telegram credentials never appear in logs, errors or audit records.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
)

// Archive format constants (docs/operations/backup-restore.md §Archive).
const (
	ManifestName = "manifest.json"
	DBMember     = "db.sqlite"
	ConfigMember = "config.toml"
	KeyMember    = "master_key.wrap"

	// SchemaVersion is the archive schema this build writes and accepts.
	SchemaVersion = 1

	// ArchiveExt is the archive filename extension; every local archive is
	// prefixed with ArchivePrefix (the name validator requires both).
	ArchiveExt    = ".wgg"
	ArchivePrefix = "wg-guard-"
)

// Manifest is the self-describing header inside every archive. File hashes
// are verified before anything is applied.
type Manifest struct {
	Schema     int               `json:"schema"`
	AppVersion string            `json:"app_version"`
	CreatedAt  string            `json:"created_at"`
	Hostname   string            `json:"hostname"`
	Files      map[string]string `json:"files"` // member → sha256 hex
}

// Service is the backup engine. It is safe for concurrent use; the heavy
// work runs on scheduler or request goroutines but every archive write is
// serialized by SQLite's write lock (VACUUM INTO takes a read snapshot of a
// live writer).
type Service struct {
	DB         *database.DB
	Reg        *settings.Registry
	Audit      *audit.Service // scheduler-run archives record as system; handlers audit themselves
	Cfg        *config.Config
	ConfigPath string // source boot config to embed ("" = synthesize from cfg)
	Version    string
	Log        *slog.Logger
	HTTPClient HTTPDoer // Telegram delivery; nil = default client
	Now        func() time.Time
}

// CreateOpts tunes one archive run.
type CreateOpts struct {
	// Password overrides the stored backup password for this archive only
	// (CLI --password). Empty falls back to the stored setting; no password
	// anywhere means a plain archive (ADR-0008).
	Password string
	// Reason records why the archive exists: manual, pre-migration,
	// pre-restore or schedule:<id>. Audit metadata only.
	Reason string
	// ScheduleID links the run to its schedule (retention + audit).
	ScheduleID string
	// Retention keeps the N newest archives in the local sink after this
	// run; 0 = the backup.retention_count setting.
	Retention int
	// Deliver sends the finished archive to configured remote sinks
	// (Telegram when credentials are set). Local always keeps a copy.
	Deliver bool
	// Dir overrides the local sink directory (pre-migration archives use
	// backups-auto); empty = <DataDir>/backups.
	Dir string
}

// Result describes a finished archive run.
type Result struct {
	Name      string
	Path      string
	Size      int64
	Encrypted bool
	Delivered []string
	Warnings  []Message
}

// Create produces one archive: SQLite snapshot → manifest → tar.gz (→ age)
// written atomically into the local sink, then delivery + retention.
func (s *Service) Create(ctx context.Context, opts CreateOpts) (*Result, error) {
	if s.DB == nil || s.Cfg == nil || s.Reg == nil {
		return nil, domain.E(domain.CodeInternal, "backup: service not wired")
	}
	password := opts.Password
	if password == "" {
		var err error
		password, err = s.Reg.GetSecret(ctx, "backup.password")
		if err != nil {
			return nil, domain.Wrap(safetyError("password_read", err), domain.CodeInternal, "backup")
		}
	}
	if password != "" && len(password) < 8 {
		return nil, domain.E(domain.CodeSettingInvalid, "backup password must be at least 8 characters")
	}

	dir := opts.Dir
	if dir == "" {
		dir = s.localDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("backup: sink dir: %w", err)
	}
	name := archiveName(s.now())
	finalPath := filepath.Join(dir, name)
	tmpPath := finalPath + ".tmp"

	// 1. Consistent SQLite snapshot, compact, out of the writer's way.
	snapPath := finalPath + ".snap"
	if err := s.snapshotDB(ctx, snapPath); err != nil {
		return nil, err
	}
	defer os.Remove(snapPath)

	// 2. Other members (missing master key degrades the archive honestly).
	configBytes := s.configBytes()
	keyBytes, err := readSmall(s.Cfg.MasterKeyFile, 32)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("backup: read master key: %w", err)
	}

	// 3. Manifest with hashes over the exact member bytes.
	manifest := Manifest{
		Schema:     SchemaVersion,
		AppVersion: s.Version,
		CreatedAt:  s.now().UTC().Format(time.RFC3339),
		Hostname:   hostname(),
		Files:      map[string]string{},
	}
	manifest.Files[DBMember] = fileHash(snapPath)
	if len(configBytes) > 0 {
		manifest.Files[ConfigMember] = hashBytes(configBytes)
	}
	if len(keyBytes) > 0 {
		manifest.Files[KeyMember] = hashBytes(keyBytes)
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("backup: manifest: %w", err)
	}

	// 4. Stream tar → gzip → (age) → file, atomically published.
	res := &Result{Name: name, Path: finalPath, Encrypted: password != ""}
	warn := func(w Message) { res.Warnings = append(res.Warnings, w) }
	if len(configBytes) == 0 {
		warn(warning("create_config_missing"))
	}
	if len(keyBytes) == 0 {
		warn(warning("create_key_missing"))
	}
	// Only members that exist go into the archive; a nil data member would
	// fall through to the file-path branch with an empty path.
	members := []member{{name: DBMember, path: snapPath}}
	if len(configBytes) > 0 {
		members = append(members, member{name: ConfigMember, data: configBytes})
	}
	if len(keyBytes) > 0 {
		members = append(members, member{name: KeyMember, data: keyBytes})
	}
	members = append(members, member{name: ManifestName, data: manifestJSON})
	if err := s.writeArchive(tmpPath, password, members); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("backup: chmod archive: %w", err)
	}
	st, err := os.Stat(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("backup: stat archive: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("backup: publish archive: %w", err)
	}
	res.Size = st.Size()

	// 5. Delivery sinks + retention.
	if opts.Deliver {
		s.deliver(ctx, res, password, warn)
	}
	keep := opts.Retention
	if keep == 0 {
		keep, _ = s.Reg.GetInt(ctx, "backup.retention_count")
	}
	if opts.Dir == "" && keep > 0 {
		if n, err := s.pruneDir(dir, keep); err != nil {
			warn(warning("retention_failed"))
		} else if n > 0 && s.Log != nil {
			s.Log.Info("backup retention pruned old archives", "count", n)
		}
	}
	if s.Audit != nil {
		_ = s.Audit.Record(ctx, audit.Entry{
			ActorType: audit.ActorSystem,
			Action:    "backup.created", Target: name,
			Metadata: map[string]any{
				"reason": opts.Reason, "size": res.Size,
				"encrypted": res.Encrypted, "delivered": res.Delivered,
			},
		})
	}
	return res, nil
}

// member is one archive entry: either inline bytes or a file to stream.
type member struct {
	name string
	data []byte
	path string // used when data == nil
}

func (s *Service) writeArchive(dest, password string, members []member) error {
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("backup: create archive: %w", err)
	}
	defer f.Close()

	var sink io.Writer = f
	var ageWC io.WriteCloser
	if password != "" {
		aw, err := ageEncrypt(f, password)
		if err != nil {
			return fmt.Errorf("backup: age: %w", err)
		}
		ageWC = aw
		sink = aw
	}
	gz := gzip.NewWriter(sink)
	tw := tar.NewWriter(gz)

	for _, m := range members {
		hdr := &tar.Header{Name: m.name, Mode: 0o600, Format: tar.FormatPAX}
		if m.data != nil {
			hdr.Size = int64(len(m.data))
			hdr.ModTime = s.now().UTC()
			if err := tw.WriteHeader(hdr); err != nil {
				return fmt.Errorf("backup: tar header %s: %w", m.name, err)
			}
			if _, err := tw.Write(m.data); err != nil {
				return fmt.Errorf("backup: tar write %s: %w", m.name, err)
			}
			continue
		}
		st, err := os.Stat(m.path)
		if err != nil {
			return fmt.Errorf("backup: stat member %s: %w", m.name, err)
		}
		hdr.Size = st.Size()
		hdr.ModTime = st.ModTime().UTC()
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("backup: tar header %s: %w", m.name, err)
		}
		src, err := os.Open(m.path)
		if err != nil {
			return fmt.Errorf("backup: open member %s: %w", m.name, err)
		}
		_, err = io.Copy(tw, src)
		src.Close()
		if err != nil {
			return fmt.Errorf("backup: tar stream %s: %w", m.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("backup: tar close: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("backup: gzip close: %w", err)
	}
	if ageWC != nil {
		if err := ageWC.Close(); err != nil {
			return fmt.Errorf("backup: age close: %w", err)
		}
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("backup: sync archive: %w", err)
	}
	return f.Close()
}

// snapshotDB writes a consistent compact copy with VACUUM INTO.
func (s *Service) snapshotDB(ctx context.Context, dest string) error {
	q := strings.ReplaceAll(dest, "'", "''")
	if _, err := s.DB.ExecContext(ctx, "VACUUM INTO '"+q+"'"); err != nil {
		return fmt.Errorf("backup: sqlite snapshot: %w", err)
	}
	if err := os.Chmod(dest, 0o600); err != nil {
		return fmt.Errorf("backup: chmod snapshot: %w", err)
	}
	return nil
}

// configBytes returns the boot config member: the source file when readable,
// otherwise a synthesized TOML dump of the loaded config; nil when neither
// works (the warning path).
func (s *Service) configBytes() []byte {
	if s.ConfigPath != "" {
		if b, err := readSmall(s.ConfigPath, maxConfigBytes); err == nil {
			return b
		}
	}
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(s.Cfg); err == nil {
		return []byte(sb.String())
	}
	return nil
}

// deliver pushes the finished archive to configured remote sinks. Failures
// become warnings — the local copy is the source of truth.
func (s *Service) deliver(ctx context.Context, res *Result, password string, warn func(Message)) {
	token, err := s.Reg.GetSecret(ctx, "backup.telegram_token")
	if err != nil {
		warn(warning("telegram_skipped_credentials"))
		return
	}
	if token == "" {
		return
	}
	chat, err := s.Reg.Get(ctx, "backup.telegram_chat")
	if err != nil {
		warn(warning("telegram_skipped_chat"))
		return
	}
	chatID, _ := chat.(string)
	if chatID == "" {
		return
	}
	if password == "" {
		warn(warning("plaintext"))
	}
	if st, err := os.Stat(res.Path); err == nil && st.Size() >= telegramWarnSize {
		warn(warning("telegram_near"))
	}
	tg := &TelegramSink{Token: token, Chat: chatID, HTTP: s.HTTPClient}
	if err := tg.Deliver(ctx, res.Path, res.Name); err != nil {
		warn(warning("telegram_failed", err))
		return
	}
	res.Delivered = append(res.Delivered, "telegram")
}

// pruneDir deletes the oldest archives beyond keep; returns how many went.
func (s *Service) pruneDir(dir string, keep int) (int, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	type arc struct {
		name string
		mod  time.Time
	}
	var arcs []arc
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ArchiveExt) ||
			strings.HasSuffix(e.Name(), ".tmp") || strings.HasSuffix(e.Name(), ".snap") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		arcs = append(arcs, arc{e.Name(), info.ModTime()})
	}
	if len(arcs) <= keep {
		return 0, nil
	}
	sort.Slice(arcs, func(i, j int) bool { return arcs[i].mod.After(arcs[j].mod) })
	removed := 0
	for _, a := range arcs[keep:] {
		if err := os.Remove(filepath.Join(dir, a.name)); err == nil {
			removed++
		}
	}
	return removed, nil
}

// --- archive listing ---------------------------------------------------------

// ArchiveInfo is one local-sink archive (listing is filesystem-backed).
type ArchiveInfo struct {
	Name      string
	Path      string
	Size      int64
	ModTime   time.Time
	Encrypted bool
}

// KiB is the template-friendly size unit (archive sizes are small).
func (a ArchiveInfo) KiB() int64 { return a.Size >> 10 }

// List returns the local-sink archives, newest first.
func (s *Service) List() ([]ArchiveInfo, error) {
	dir := s.localDir()
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: list: %w", err)
	}
	var out []ArchiveInfo
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ArchiveExt) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, ArchiveInfo{
			Name:      e.Name(),
			Path:      filepath.Join(dir, e.Name()),
			Size:      info.Size(),
			ModTime:   info.ModTime(),
			Encrypted: fileEncrypted(filepath.Join(dir, e.Name())),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

// Open returns a read handle for one local archive (panel download).
func (s *Service) Open(name string) (*os.File, int64, error) {
	if !validArchiveName(name) {
		return nil, 0, domain.E(domain.CodeNotFound, "backup: unknown archive")
	}
	p := filepath.Join(s.localDir(), name)
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, domain.E(domain.CodeNotFound, "backup: unknown archive")
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

// Delete removes one local archive (audit-logged as system; handlers audit
// the admin actor themselves).
func (s *Service) Delete(ctx context.Context, name string) error {
	if !validArchiveName(name) {
		return domain.E(domain.CodeNotFound, "backup: unknown archive")
	}
	if err := os.Remove(filepath.Join(s.localDir(), name)); err != nil {
		if os.IsNotExist(err) {
			return domain.E(domain.CodeNotFound, "backup: unknown archive")
		}
		return fmt.Errorf("backup: delete: %w", err)
	}
	if s.Audit != nil {
		_ = s.Audit.Record(ctx, audit.Entry{
			ActorType: audit.ActorSystem, Action: "backup.deleted", Target: name,
		})
	}
	return nil
}

func validArchiveName(name string) bool {
	if !strings.HasSuffix(name, ArchiveExt) || strings.ContainsAny(name, `/\`) ||
		strings.Contains(name, "..") || len(name) > 80 {
		return false
	}
	return strings.HasPrefix(name, ArchivePrefix)
}

// --- helpers -----------------------------------------------------------------

func archiveName(now time.Time) string {
	return ArchivePrefix + now.UTC().Format("20060102-150405") + "-" + newNonce() + ArchiveExt
}

func (s *Service) localDir() string {
	return filepath.Join(s.Cfg.DataDir, "backups")
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}

func fileHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newNonce is 8 random bytes hex — schedule identifiers.
func newNonce() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("backup: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// ServiceRunning reports whether a WG-Guard service answers on the
// configured listen address — restore/doctor --fix guard their mutating
// paths with it.
func ServiceRunning(listenAddr string) bool {
	conn, err := net.DialTimeout("tcp", listenAddr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
