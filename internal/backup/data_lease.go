package backup

import (
	"os"
	"path/filepath"
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
	if !exclusive {
		if err := leaseUnlock(f, 0); err != nil {
			return fail()
		}
	}
	return &DataLease{file: f, exclusive: exclusive}, nil
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
