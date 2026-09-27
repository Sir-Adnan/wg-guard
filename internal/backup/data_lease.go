package backup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// DataLease owns the node's DB/key lifetime. The persistent lock inode lives in
// the shared data volume, outside all archive and restore replacement members.
// Byte 0 serializes admission; byte 1 protects the DB/key pair. An exclusive
// owner keeps admission locked, so downgrade never exposes an unlocked pair.
// Never unlink this file to recover from contention: the kernel releases locks
// when the process exits, including interruption and ungraceful death.
type DataLease struct {
	file      *os.File
	exclusive bool
	once      sync.Once
}

const dataLeaseName = ".wg-guard-data.lock"
const purgedMarker = 'P'

func (l *DataLease) Close() {
	if l != nil {
		l.once.Do(func() { _ = l.file.Close() })
	}
}

func (s *Service) lockData(exclusive bool) (*DataLease, error) {
	if err := os.MkdirAll(s.Cfg.DataDir, 0700); err != nil {
		return nil, safetyError("data_busy", nil)
	}
	if err := s.checkDataLayout(); err != nil {
		return nil, err
	}
	f, err := openLeaseFile(filepath.Join(s.Cfg.DataDir, dataLeaseName))
	if err != nil {
		return nil, safetyError("data_busy", nil)
	}
	fail := func() (*DataLease, error) { f.Close(); return nil, safetyError("data_busy", nil) }
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return fail()
	}
	if err := leaseLock(f, 0, true); err != nil {
		return fail()
	}
	if err := leaseLock(f, 1, exclusive); err != nil {
		return fail()
	}
	if purged, err := isPurged(f); err != nil || purged {
		f.Close()
		return nil, fmt.Errorf("data volume was purged; reinstall the node before opening it")
	}
	if !exclusive {
		if err := leaseUnlock(f, 0); err != nil {
			return fail()
		}
	}
	return &DataLease{file: f, exclusive: exclusive}, nil
}

func isPurged(f *os.File) (bool, error) {
	var marker [1]byte
	n, err := f.ReadAt(marker[:], 2)
	if n == 0 && errors.Is(err, io.EOF) {
		return false, nil
	}
	return n == 1 && marker[0] == purgedMarker, err
}

// PurgeGuard excludes data commands after service stop and before uninstall
// removes its first artifact. The inode survives purge to avoid split locks.
type PurgeGuard struct {
	dir  string
	file *os.File
}

func (g *PurgeGuard) Close() error { return g.file.Close() }

func AcquirePurgeGuard(dir string) (*PurgeGuard, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) == string(filepath.Separator) {
		return nil, fmt.Errorf("invalid data directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if st, err := os.Lstat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("data directory is not a regular directory")
	}
	f, err := openLeaseFile(filepath.Join(dir, dataLeaseName))
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*PurgeGuard, error) { f.Close(); return nil, err }
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return fail(fmt.Errorf("data ownership file is not regular"))
	}
	if err := leaseLock(f, 0, true); err != nil {
		return fail(safetyError("data_busy", err))
	}
	if err := leaseLock(f, 1, true); err != nil {
		return fail(safetyError("data_busy", err))
	}
	return &PurgeGuard{dir: dir, file: f}, nil
}

func withPurgeOwnership(dir string, action func(*os.File) error) error {
	guard, err := AcquirePurgeGuard(dir)
	if err != nil {
		return err
	}
	defer guard.Close()
	return action(guard.file)
}

// PurgeDataDir excludes all admitted commands and prevents new admissions
// before removing any member. A protected tombstone stays in the empty data
// directory until the next fresh installation explicitly resets it.
func PurgeDataDir(dir string) error {
	guard, err := AcquirePurgeGuard(dir)
	if err != nil {
		return err
	}
	defer guard.Close()
	return guard.Purge()
}

func (g *PurgeGuard) Purge() error {
	if _, err := g.file.WriteAt([]byte{purgedMarker}, 2); err != nil {
		return err
	}
	if err := g.file.Sync(); err != nil {
		return err
	}
	entries, err := os.ReadDir(g.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == dataLeaseName {
			continue
		}
		if err := os.RemoveAll(filepath.Join(g.dir, entry.Name())); err != nil {
			return err
		}
	}
	d, err := os.Open(g.dir)
	if err != nil {
		return err
	}
	return joinSyncClose(d)
}

func joinSyncClose(d *os.File) error {
	var err error
	if runtime.GOOS != "windows" {
		err = d.Sync()
	}
	if closeErr := d.Close(); err == nil {
		err = closeErr
	}
	return err
}

// ResetPurgedDataDir admits a fresh install only when no old data member
// survived an interrupted purge. Preserved (non-purged) data is untouched.
func ResetPurgedDataDir(dir string) error {
	return withPurgeOwnership(dir, func(f *os.File) error {
		purged, err := isPurged(f)
		if err != nil || !purged {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(entries) != 1 || entries[0].Name() != dataLeaseName {
			return fmt.Errorf("data purge was interrupted; finish the guided reset before installing")
		}
		if _, err := f.WriteAt([]byte{0}, 2); err != nil {
			return err
		}
		return f.Sync()
	})
}

// A configured pair must share its ownership directory. Independent DataDir
// overrides must not invent a second lock for the same explicit DB/key paths.
// Directory aliases are compared by inode, not textual path; existing file
// symlinks must also resolve within that directory and cannot alias the lock.
func (s *Service) checkDataLayout() error {
	cfg := *s.Cfg
	cfg.Complete()
	dir, err := os.Stat(cfg.DataDir)
	if err != nil || !dir.IsDir() {
		return safetyError("data_layout", nil)
	}
	for _, path := range []string{cfg.DatabasePath, cfg.MasterKeyFile} {
		parent, err := os.Stat(filepath.Dir(path))
		if err != nil || !os.SameFile(dir, parent) || filepath.Base(path) == dataLeaseName {
			return safetyError("data_layout", nil)
		}
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return safetyError("data_layout", nil)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || filepath.Base(resolved) == dataLeaseName {
			return safetyError("data_layout", nil)
		}
		parent, err = os.Stat(filepath.Dir(resolved))
		if err != nil || !os.SameFile(dir, parent) {
			return safetyError("data_layout", nil)
		}
	}
	return nil
}

// OpenData must precede every production DB or master-key open. Keep the lease
// until all users and DB handles are closed. Rotation takes exclusive ownership
// before loading any carrier, including while waiting for confirmation.
func (s *Service) OpenData(exclusive bool) (*DataLease, error) {
	lease, err := s.lockData(exclusive)
	if err != nil {
		return nil, err
	}
	if err := s.CheckOpen(); err != nil {
		lease.Close()
		return nil, err
	}
	return lease, nil
}

// OpenKeys additionally excludes concurrent first-key initialization. Call Share
// after loading/creating the key unless exclusive ownership (rotation) is needed.
// Database-only commands use OpenData and never create a key as a side effect.
func (s *Service) OpenKeys(exclusive bool) (*DataLease, error) {
	lease, err := s.OpenData(exclusive)
	if err != nil {
		return nil, err
	}
	if !exclusive {
		cfg := *s.Cfg
		cfg.Complete()
		if _, err := os.Lstat(cfg.MasterKeyFile); os.IsNotExist(err) {
			// No DB/key has been opened yet. Retry admission exclusively so
			// concurrent initializers cannot each publish a different key.
			lease.Close()
			return s.OpenData(true)
		} else if err != nil {
			lease.Close()
			return nil, safetyError("data_busy", nil)
		}
	}
	return lease, nil
}

// Share converts exclusive startup ownership after the DB/key are initialized.
// It must only be called by the single owner, never concurrently with Close.
func (l *DataLease) Share() error {
	if !l.exclusive {
		return nil
	}
	// Admission byte remains exclusive across the portable unlock/relock.
	if err := leaseUnlock(l.file, 1); err != nil {
		return safetyError("data_busy", nil)
	}
	if err := leaseLock(l.file, 1, false); err != nil {
		return safetyError("data_busy", nil)
	}
	if err := leaseUnlock(l.file, 0); err != nil {
		return safetyError("data_busy", nil)
	}
	l.exclusive = false
	return nil
}

// PrepareOpen completes boot-time restore and retains exclusive ownership while
// the server initializes its DB/key. The server then calls Share without an
// admission gap. No DB or key is loaded before this returns.
func (s *Service) PrepareOpen() (string, *DataLease, error) {
	lease, err := s.lockData(true)
	if err != nil {
		return "", nil, err
	}
	archive, err := s.consumePendingRestore()
	if err != nil {
		lease.Close()
		return "", nil, err
	}
	return archive, lease, nil
}
