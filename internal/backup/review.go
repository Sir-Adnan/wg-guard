package backup

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MaxPanelPreviews bounds retained decrypted review payloads. Orphaned/incomplete
// previews count as well and can be explicitly discarded, never automatically applied.
const MaxPanelPreviews = 4

type PreviewInfo struct {
	ID      string
	Pending *PendingRestore
}

// StageReview is the panel admission policy on top of the shared restore engine.
func (s *Service) StageReview(ctx context.Context, path, password string) (*PendingRestore, *RestoreReport, error) {
	release, err := s.claimInspection(ctx)
	if err != nil {
		return nil, nil, verificationError(err)
	}
	defer release()
	// A directory may appear before its report is published. Keep cancellation
	// and approval out until staging completes, including across CLI processes.
	finishReview, err := s.claimReview()
	if err != nil {
		return nil, nil, err
	}
	defer finishReview()
	previews, err := s.Previews()
	if err != nil {
		return nil, nil, err
	}
	if len(previews) >= MaxPanelPreviews {
		return nil, nil, safetyError("preview_limit", nil)
	}
	if pending, err := s.PendingSummary(); err != nil {
		return nil, nil, err
	} else if pending != nil {
		return nil, nil, safetyError("pending_exists", nil)
	}
	p, report, err := s.stage(ctx, path, password, false)
	if err == nil && report.Inventory.EnabledOwners == 0 {
		_ = os.RemoveAll(p.Dir)
		return nil, nil, safetyError("restore_owner", nil)
	}
	return p, report, verificationError(err)
}

// ApproveReview applies the panel-specific access preflight to the actual checked
// staged DB, including previews created by a CLI or an older implementation.
func (s *Service) ApproveReview(ctx context.Context, id string) (*PendingRestore, error) {
	release, err := s.claimReview()
	if err != nil {
		return nil, err
	}
	defer release()
	dir, err := s.previewDir(id)
	if err != nil {
		return nil, err
	}
	if _, err := loadStaged(dir); err != nil {
		return nil, err
	}
	inventory, err := inspectStagedData(ctx, dir)
	if err != nil {
		return nil, err
	}
	if inventory.EnabledOwners == 0 {
		return nil, safetyError("restore_owner", nil)
	}
	return s.approve(id)
}

// Preview loads only the bounded saved report. It never hashes/migrates a large
// DB on GET. An explicit approval performs the full payload recheck.
func (s *Service) Preview(id string) (*PendingRestore, error) {
	dir, err := s.previewDir(id)
	if err != nil {
		return nil, err
	}
	p, err := readStagedMetadata(dir)
	if err != nil || p.Report == nil || p.Original {
		return nil, safetyError("preview_unavailable", err)
	}
	return p, nil
}

// Previews reads directory entries in batches and returns at most four receipts.
// Incomplete/older previews have no cached report; UI offers exact cancellation.
func (s *Service) Previews() ([]PreviewInfo, error) {
	dir, err := os.Open(s.Cfg.DataDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	var out []PreviewInfo
	for {
		entries, err := dir.ReadDir(64)
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), previewPrefix) {
				continue
			}
			p, _ := s.Preview(entry.Name())
			out = append(out, PreviewInfo{ID: entry.Name(), Pending: p})
			if len(out) == MaxPanelPreviews {
				return out, nil
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Service) PendingSummary() (*PendingRestore, error) {
	dir := filepath.Join(s.Cfg.DataDir, pendingDirName)
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return readStagedMetadata(dir)
}

// CancelPending cannot let an old tab cancel a later operator's restore.
func (s *Service) CancelPending(expected string) error {
	if !validHash(expected) {
		return safetyError("preview_changed", nil)
	}
	release, err := s.claimReview()
	if err != nil {
		return err
	}
	defer release()
	p, err := s.PendingSummary()
	if err != nil || p == nil || p.Identity != expected {
		return safetyError("preview_changed", err)
	}
	if err := os.RemoveAll(p.Dir); err != nil {
		return err
	}
	return syncDir(s.Cfg.DataDir)
}

func (s *Service) claimReview() (func(), error) {
	if !s.reviewMu.TryLock() {
		return nil, safetyError("archive_busy", ErrArchiveBusy)
	}
	file, err := s.openWorkFile()
	if err != nil {
		s.reviewMu.Unlock()
		return nil, err
	}
	if err := leaseLock(file, reviewLockOffset, true); err != nil {
		file.Close()
		s.reviewMu.Unlock()
		return nil, safetyError("archive_busy", ErrArchiveBusy)
	}
	if purged, err := isPurged(file); err != nil || purged {
		file.Close()
		s.reviewMu.Unlock()
		return nil, safetyError("data_busy", err)
	}
	return func() { file.Close(); s.reviewMu.Unlock() }, nil
}
