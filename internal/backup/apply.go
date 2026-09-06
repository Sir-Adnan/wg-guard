package backup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// PreviewID is the opaque review identity; confirmation must name the exact preview.
func (p *PendingRestore) PreviewID() string { return filepath.Base(p.Dir) }

func (s *Service) previewDir(id string) (string, error) {
	if !strings.HasPrefix(id, previewPrefix) || filepath.Base(id) != id || strings.ContainsAny(id, "/\\") || len(id) > 80 {
		return "", safetyError("preview_identity", nil)
	}
	return filepath.Join(s.Cfg.DataDir, id), nil
}

func loadStaged(dir string) (*PendingRestore, error) {
	st, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, safetyError("stage_directory", nil)
	}
	raw, err := readSmall(filepath.Join(dir, pendingMeta), maxMetadataBytes)
	if err != nil {
		return nil, err
	}
	var m stagedMeta
	if json.Unmarshal(raw, &m) != nil {
		return nil, safetyError("stage_metadata", nil)
	}
	p := &PendingRestore{Dir: dir, Archive: m.Archive, Size: m.Size, Manifest: m.Manifest, Files: m.Files, Original: m.Original}
	p.StagedAt, err = time.Parse(time.RFC3339, m.StagedAt)
	if err != nil || m.Manifest.Schema != SchemaVersion || !validHash(m.Files[ManifestName]) || !validHash(m.Files[DBMember]) || len(m.Files) != len(m.Manifest.Files)+1 {
		return nil, safetyError("stage_incomplete", nil)
	}
	for name, want := range m.Files {
		limit := memberLimit(name)
		if limit == 0 || !validHash(want) {
			return nil, safetyError("stage_hashes", nil)
		}
		st, err := os.Lstat(filepath.Join(dir, name))
		if err != nil || !st.Mode().IsRegular() || st.Size() > limit {
			return nil, safetyError("stage_member", nil)
		}
		if name == KeyMember && st.Size() != 32 {
			return nil, safetyError("stage_key", nil)
		}
		if fileHash(filepath.Join(dir, name)) != want {
			return nil, safetyError("stage_checksum", nil)
		}
		if name != ManifestName && !validHash(m.Manifest.Files[name]) {
			return nil, safetyError("manifest_incomplete", nil)
		}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		if e.Name() != pendingMeta && m.Files[e.Name()] == "" {
			return nil, safetyError("stage_extra", nil)
		}
	}
	return p, nil
}

func (s *Service) Pending() (*PendingRestore, error) {
	dir := filepath.Join(s.Cfg.DataDir, pendingDirName)
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return loadStaged(dir)
}

// Approve atomically publishes a validated preview for offline apply. It never
// replaces a different pending restore, and original-schema recovery stays CLI-only.
func (s *Service) Approve(id string) (*PendingRestore, error) {
	dir, err := s.previewDir(id)
	if err != nil {
		return nil, err
	}
	p, err := loadStaged(dir)
	if err != nil {
		return nil, err
	}
	if p.Original {
		return nil, safetyError("original_queue", nil)
	}
	dst := filepath.Join(s.Cfg.DataDir, pendingDirName)
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		return nil, safetyError("pending_exists", nil)
	}
	if err := os.Rename(dir, dst); err != nil {
		return nil, err
	}
	p.Dir = dst
	return p, syncDir(s.Cfg.DataDir)
}

func (s *Service) DiscardPreview(id string) error {
	dir, err := s.previewDir(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
func (s *Service) DiscardPending() error {
	return os.RemoveAll(filepath.Join(s.Cfg.DataDir, pendingDirName))
}

// ApplyOriginal requires an exact private preview and is only called offline by
// the lifecycle recovery coordinator after checking archive/artifact identities.
func (s *Service) ApplyOriginal(ctx context.Context, id string) (*RestoreReport, error) {
	lease, err := s.lockData(true)
	if err != nil {
		return nil, err
	}
	defer lease.Close()
	dir, err := s.previewDir(id)
	if err != nil {
		return nil, err
	}
	p, err := loadStaged(dir)
	if err != nil {
		return nil, err
	}
	if !p.Original || p.Files[KeyMember] == "" {
		return nil, safetyError("original_pair", nil)
	}
	return s.apply(ctx, p)
}

func (s *Service) ApplyStaged(ctx context.Context) (*RestoreReport, error) {
	lease, err := s.lockData(true)
	if err != nil {
		return nil, err
	}
	defer lease.Close()
	return s.applyStaged(ctx)
}

func (s *Service) applyStaged(ctx context.Context) (*RestoreReport, error) {
	p, err := s.Pending()
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, safetyError("pending_missing", nil)
	}
	if p.Original {
		return nil, safetyError("original_boot", nil)
	}
	return s.apply(ctx, p)
}

// Originals are copied and synced before the transaction is published. While
// restore.transaction exists, every opener must recover or refuse to open DB.
type restoreTransaction struct {
	Files map[string]string `json:"files"`
	Stage string            `json:"stage"`
}

const transactionDir = "restore.transaction"

func (s *Service) restoreTargets() map[string]string {
	return map[string]string{
		DBMember: s.Cfg.DatabasePath, "db-wal": s.Cfg.DatabasePath + "-wal", "db-shm": s.Cfg.DatabasePath + "-shm",
		KeyMember: s.Cfg.MasterKeyFile, "key-prev": s.Cfg.MasterKeyFile + ".prev",
	}
}

func (s *Service) apply(ctx context.Context, p *PendingRestore) (*RestoreReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	txdir := filepath.Join(s.Cfg.DataDir, transactionDir)
	if _, err := os.Lstat(txdir); !os.IsNotExist(err) {
		return nil, safetyError("unfinished", nil)
	}
	tmp, err := os.MkdirTemp(s.Cfg.DataDir, "restore.protect-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	tx := restoreTransaction{Files: map[string]string{}, Stage: filepath.Base(p.Dir)}
	for name, target := range s.restoreTargets() {
		st, err := os.Lstat(target)
		if os.IsNotExist(err) {
			tx.Files[name] = ""
			continue
		}
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			return nil, safetyError("active_regular", nil)
		}
		if err := copyFile(target, filepath.Join(tmp, name), 0600); err != nil {
			return nil, err
		}
		tx.Files[name] = fileHash(filepath.Join(tmp, name))
		if !validHash(tx.Files[name]) {
			return nil, safetyError("protect_failed", nil)
		}
	}
	raw, _ := json.Marshal(tx)
	if err := writeSynced(filepath.Join(tmp, pendingMeta), raw); err != nil {
		return nil, err
	}
	if err := syncDir(tmp); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, txdir); err != nil {
		return nil, err
	}
	if err := syncDir(s.Cfg.DataDir); err != nil {
		return nil, err
	}
	fail := func(e error) (*RestoreReport, error) { return nil, errors.Join(e, s.recoverPair()) }
	for _, name := range []string{DBMember, KeyMember} {
		if p.Files[name] == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if err := replaceFile(filepath.Join(p.Dir, name), s.restoreTargets()[name]); err != nil {
			return fail(err)
		}
	}
	for _, name := range []string{"db-wal", "db-shm", "key-prev"} {
		if name == "key-prev" && p.Files[KeyMember] == "" {
			continue
		}
		target := s.restoreTargets()[name]
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return fail(err)
		}
		if err := syncDir(filepath.Dir(target)); err != nil {
			return fail(err)
		}
	}
	report := &RestoreReport{}
	if p.Files[KeyMember] == "" {
		report.Warnings = append(report.Warnings, warning("missing_key"))
	}
	if p.Files[ConfigMember] != "" && s.ConfigPath != "" {
		if err := replaceFile(filepath.Join(p.Dir, ConfigMember), s.ConfigPath+".restored"); err != nil {
			report.Warnings = append(report.Warnings, warning("config_failed"))
		} else {
			report.Warnings = append(report.Warnings, warning("config_review", s.ConfigPath))
		}
	}
	// Keeping the transaction originals also keeps WAL and a rotation-window key.
	// Publish success only after the approved input can no longer be replayed.
	if err := os.RemoveAll(p.Dir); err != nil {
		return fail(err)
	}
	if err := syncDir(s.Cfg.DataDir); err != nil {
		return fail(err)
	}
	previous := filepath.Join(s.Cfg.DataDir, "restore.previous")
	if _, err := os.Lstat(previous); err == nil {
		if err := os.Rename(previous, previous+"-"+newNonce()); err != nil {
			return fail(err)
		}
	} else if !os.IsNotExist(err) {
		return fail(err)
	}
	if err := os.Rename(txdir, previous); err != nil {
		return fail(err)
	}
	if err := syncDir(s.Cfg.DataDir); err != nil {
		return nil, err
	}
	return report, nil
}

// RecoverInterrupted must run before opening any active database handle.
func (s *Service) RecoverInterrupted() error {
	lease, err := s.lockData(true)
	if err != nil {
		return err
	}
	defer lease.Close()
	return s.recoverInterrupted()
}

func (s *Service) recoverInterrupted() error {
	if _, err := os.Lstat(filepath.Join(s.Cfg.DataDir, transactionDir)); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := s.recoverPair(); err != nil {
		return err
	}
	return safetyError("interrupted", nil)
}
func (s *Service) recoverPair() error {
	dir := filepath.Join(s.Cfg.DataDir, transactionDir)
	raw, err := readSmall(filepath.Join(dir, pendingMeta), maxMetadataBytes)
	if err != nil {
		return err
	}
	var tx restoreTransaction
	if json.Unmarshal(raw, &tx) != nil || len(tx.Files) != 5 {
		return safetyError("recovery_metadata", nil)
	}
	if tx.Stage != pendingDirName {
		if _, err := s.previewDir(tx.Stage); err != nil {
			return err
		}
	}
	targets := s.restoreTargets()
	// Validate all originals first. Never restore one half from unverified data.
	for name, want := range tx.Files {
		if targets[name] == "" || want != "" && !validHash(want) {
			return safetyError("recovery_originals", nil)
		}
		if want != "" {
			st, err := os.Lstat(filepath.Join(dir, name))
			if err != nil || !st.Mode().IsRegular() || st.Size() > maxExpandedBytes {
				return safetyError("recovery_unsafe", nil)
			}
		}
		if want != "" && fileHash(filepath.Join(dir, name)) != want {
			return safetyError("recovery_originals", nil)
		}
	}
	for _, name := range []string{DBMember, KeyMember, "db-wal", "db-shm", "key-prev"} {
		target := targets[name]
		if tx.Files[name] == "" {
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return err
			}
		} else if err := replaceFile(filepath.Join(dir, name), target); err != nil {
			return err
		}
		if err := syncDir(filepath.Dir(target)); err != nil {
			return err
		}
	}
	if tx.Stage == pendingDirName {
		if err := s.DiscardPending(); err != nil {
			return err
		}
	} else if _, err := s.previewDir(tx.Stage); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return syncDir(s.Cfg.DataDir)
}

func (s *Service) ConsumePendingRestore() (string, error) {
	lease, err := s.lockData(true)
	if err != nil {
		return "", err
	}
	defer lease.Close()
	return s.consumePendingRestore()
}

func (s *Service) consumePendingRestore() (string, error) {
	if _, err := os.Lstat(filepath.Join(s.Cfg.DataDir, RestoreGuardName)); !os.IsNotExist(err) {
		return "", safetyError("lifecycle_blocked", nil)
	}
	if err := s.recoverInterrupted(); err != nil {
		return "", err
	}
	p, err := s.Pending()
	if err != nil || p == nil {
		return "", err
	}
	if _, err := s.applyStaged(context.Background()); err != nil {
		return "", err
	}
	return p.Archive, nil
}

func syncDir(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func writeSynced(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
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
	n, err := io.Copy(out, io.LimitReader(in, maxExpandedBytes+1))
	if n > maxExpandedBytes {
		err = safetyError("recovery_limit", nil)
	}
	if err == nil {
		err = out.Sync()
	}
	return errors.Join(err, out.Close())
}
func replaceFile(src, dst string) error {
	f, err := os.CreateTemp(filepath.Dir(dst), ".restore-swap-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp)
	if err := copyFile(src, tmp, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return syncDir(filepath.Dir(dst))
}
