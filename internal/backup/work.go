package backup

import (
	"context"
	"os"
	"path/filepath"
)

// claimArchiveWork is the single nonblocking admission point for archive/KDF
// work. A shared data lease also excludes offline replacement and purge; the
// additional byte claim excludes host/container competitors without blocking
// the running node's accounting handles. Callers already holding an exclusive
// lease (pre-migration) must pass that lease rather than reopening it.
func (s *Service) claimArchiveWork(ctx context.Context, lease *DataLease) (func(), error) {
	return s.claimWork(ctx, lease, true)
}

// Offline inspection never opens the active pair and may run while rotation
// owns it. Its work-byte claim still excludes crypto competitors and purge.
func (s *Service) claimInspection(ctx context.Context) (func(), error) {
	return s.claimWork(ctx, nil, false)
}

func (s *Service) claimWork(ctx context.Context, lease *DataLease, activeData bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.archiveMu.TryLock() {
		return nil, safetyError("archive_busy", ErrArchiveBusy)
	}
	owned := lease == nil
	var file *os.File
	var closeOwned func()
	if lease != nil {
		file = lease.file
	}
	if owned {
		var err error
		if activeData {
			lease, err = s.OpenData(false)
			if err == nil {
				file, closeOwned = lease.file, lease.Close
			}
		} else {
			file, err = s.openWorkFile()
			closeOwned = func() { _ = file.Close() }
		}
		if err != nil {
			s.archiveMu.Unlock()
			return nil, err
		}
	}
	if err := leaseLock(file, archiveLockOffset, true); err != nil {
		if owned {
			closeOwned()
		}
		s.archiveMu.Unlock()
		return nil, safetyError("archive_busy", ErrArchiveBusy)
	}
	// Purge could have completed between opening and acquiring the work byte.
	if purged, err := isPurged(file); err != nil || purged {
		_ = leaseUnlock(file, archiveLockOffset)
		if owned {
			closeOwned()
		}
		s.archiveMu.Unlock()
		return nil, safetyError("data_busy", err)
	}
	return func() {
		_ = leaseUnlock(file, archiveLockOffset)
		if owned {
			closeOwned()
		}
		s.archiveMu.Unlock()
	}, nil
}

func (s *Service) openWorkFile() (*os.File, error) {
	if err := os.MkdirAll(s.Cfg.DataDir, 0700); err != nil {
		return nil, safetyError("data_busy", err)
	}
	if err := s.checkDataLayout(); err != nil {
		return nil, err
	}
	f, err := openLeaseFile(filepath.Join(s.Cfg.DataDir, dataLeaseName))
	if err != nil {
		return nil, safetyError("data_busy", err)
	}
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, safetyError("data_busy", err)
	}
	if purged, err := isPurged(f); err != nil || purged {
		f.Close()
		return nil, safetyError("data_busy", err)
	}
	return f, nil
}
