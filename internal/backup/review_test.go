package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/admin"
)

func TestSavedReviewResumesWithoutTrustingPayloadForApproval(t *testing.T) {
	s := reviewService(t)
	ctx := context.Background()
	if err := s.Reg.SetRaw(ctx, "backup.telegram_token", "synthetic-report-secret"); err != nil {
		t.Fatal(err)
	}
	a, err := s.Create(ctx, CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	p, report, err := s.StageReview(ctx, a.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(raw)
	if report.ArchiveSHA256 != hex.EncodeToString(want[:]) || report.VerifiedAt.IsZero() {
		t.Fatal("input identity or verification time missing")
	}
	meta, err := os.ReadFile(filepath.Join(p.Dir, pendingMeta))
	if err != nil || strings.Contains(string(meta), "synthetic-report-secret") {
		t.Fatal("cached report exposed a stored secret", err)
	}
	resumed, err := (&Service{Cfg: s.Cfg}).Preview(p.PreviewID())
	if err != nil || resumed.Report.Inventory != report.Inventory || resumed.Report.ArchiveSHA256 != report.ArchiveSHA256 {
		t.Fatal("review did not survive service recreation", err)
	}
	if pending, err := s.Pending(); err != nil || pending != nil {
		t.Fatal("review queued a restore", err)
	}
	// The cached report is deliberately inexpensive and not apply authority.
	if err := os.WriteFile(filepath.Join(p.Dir, DBMember), []byte("synthetic-tampering"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Preview(p.PreviewID()); err != nil {
		t.Fatal("reading the cached report inspected the payload", err)
	}
	if _, err := s.Approve(p.PreviewID()); err == nil {
		t.Fatal("approval trusted cached verification after payload tampering")
	}
	if pending, _ := s.Pending(); pending != nil {
		t.Fatal("tampered preview was approved")
	}
}

func TestStalePendingCancellationKeepsTheNewRequest(t *testing.T) {
	s := reviewService(t)
	ctx := context.Background()
	a, err := s.Create(ctx, CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := s.StageReview(ctx, a.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.StageReview(ctx, a.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(first.PreviewID()); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelPending(first.Identity); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(second.PreviewID()); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelPending(first.Identity); err == nil {
		t.Fatal("a stale form cancelled a different request")
	}
	current, err := s.PendingSummary()
	if err != nil || current == nil || current.Identity != second.Identity {
		t.Fatal("current request changed", err)
	}
	if err := s.CancelPending(""); err == nil {
		t.Fatal("unsealed cancellation accepted")
	}
	if err := s.CancelPending(second.Identity); err != nil {
		t.Fatal(err)
	}
}

func TestReviewAdmissionCountsIncompletePreviewsAndAllowsExactCleanup(t *testing.T) {
	s := reviewService(t)
	ctx := context.Background()
	a, err := s.Create(ctx, CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxPanelPreviews; i++ {
		if _, err := os.MkdirTemp(s.Cfg.DataDir, previewPrefix); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.StageReview(ctx, a.Path, ""); err == nil {
		t.Fatal("abandoned previews bypassed the retained payload limit")
	}
	previews, err := s.Previews()
	if err != nil || len(previews) != MaxPanelPreviews || previews[0].Pending != nil {
		t.Fatal("incomplete reviews cannot be managed", err)
	}
	if err := s.DiscardPreview(previews[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.StageReview(ctx, a.Path, ""); err != nil {
		t.Fatal("exact cleanup did not reopen admission", err)
	}
	if err := s.DiscardPreview("../"); err == nil {
		t.Fatal("cleanup escaped its namespace")
	}
}

func TestVerificationUsesDiskVolumeAndLeavesNoRestoreState(t *testing.T) {
	s := reviewService(t)
	ctx := context.Background()
	a, err := s.Create(ctx, CreateOpts{Password: "synthetic-archive-password"})
	if err != nil {
		t.Fatal(err)
	}
	before := fileHash(s.Cfg.DatabasePath)
	report, err := s.Verify(ctx, a.Path, "synthetic-archive-password")
	if err != nil || !report.Encrypted || !report.HasKey {
		t.Fatal("encrypted verification failed", err)
	}
	for _, password := range []string{"wrong-password", ""} {
		if _, err := s.Verify(ctx, a.Path, password); err == nil {
			t.Fatal("verification accepted an unavailable password")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Verify(cancelled, a.Path, "synthetic-archive-password"); !errors.Is(err, context.Canceled) {
		t.Fatal("verification ignored cancellation", err)
	}
	entries, err := os.ReadDir(s.Cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), previewPrefix) || strings.HasPrefix(entry.Name(), "wg-guard-verify-") || entry.Name() == pendingDirName {
			t.Fatal("verification retained decrypted/approved data")
		}
	}
	if before != fileHash(s.Cfg.DatabasePath) {
		t.Fatal("verification changed the active database")
	}
}

func TestInspectionAndReviewClaimsExcludeCompetingProcesses(t *testing.T) {
	s := reviewService(t)
	ctx := context.Background()
	a, err := s.Create(ctx, CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.OpenData(false)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := leaseLock(lease.file, archiveLockOffset, true); err != nil {
		t.Fatal(err)
	}
	for _, action := range []func() error{
		func() error { _, err := s.Verify(ctx, a.Path, ""); return err },
		func() error { _, _, err := s.Stage(ctx, a.Path, ""); return err },
		func() error { _, _, err := s.StageReview(ctx, a.Path, ""); return err },
	} {
		if err := action(); !errors.Is(err, ErrArchiveBusy) {
			t.Fatal("competing crypto admitted", err)
		}
	}
	if err := leaseUnlock(lease.file, archiveLockOffset); err != nil {
		t.Fatal(err)
	}
	p, _, err := s.StageReview(ctx, a.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := leaseLock(lease.file, reviewLockOffset, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(p.PreviewID()); !errors.Is(err, ErrArchiveBusy) {
		t.Fatal("competing approval admitted", err)
	}
	if err := s.DiscardPreview(p.PreviewID()); !errors.Is(err, ErrArchiveBusy) {
		t.Fatal("competing cancellation admitted", err)
	}
}

func reviewService(t *testing.T) *Service {
	t.Helper()
	s, _ := newService(t)
	if _, err := admin.NewService(s.DB, nil).BootstrapOwner(context.Background(), "source-owner", "synthetic-owner-password"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPanelRestoreRefusesAnArchiveWithoutAnEnabledOwner(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	a, err := s.Create(ctx, CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.StageReview(ctx, a.Path, ""); err == nil {
		t.Fatal("ownerless panel review admitted")
	}
	if previews, _ := s.Previews(); len(previews) != 0 {
		t.Fatal("refused review retained a payload")
	}
	// Raw CLI/offline staging remains available for controlled recovery. A direct
	// panel confirmation of that preview must still enforce its own access policy.
	p, _, err := s.Stage(ctx, a.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveReview(ctx, p.PreviewID()); err == nil {
		t.Fatal("confirmation bypassed access preflight")
	}
	if pending, _ := s.Pending(); pending != nil {
		t.Fatal("refused ownerless payload queued")
	}
}

func TestPurgeExcludesPrivateInspectionAndReviewWork(t *testing.T) {
	s := reviewService(t)
	ctx := context.Background()
	a, err := s.Create(ctx, CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.claimInspection(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if guard, err := AcquirePurgeGuard(s.Cfg.DataDir); err == nil {
		guard.Close()
		t.Fatal("purge admitted while inspection owned its private payload")
	}
	release()
	release, err = s.claimReview()
	if err != nil {
		t.Fatal(err)
	}
	if guard, err := AcquirePurgeGuard(s.Cfg.DataDir); err == nil {
		guard.Close()
		t.Fatal("purge admitted during review mutation")
	}
	release()
	guard, err := AcquirePurgeGuard(s.Cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if _, err := s.Verify(ctx, a.Path, ""); !errors.Is(err, ErrArchiveBusy) {
		t.Fatal("inspection bypassed purge ownership", err)
	}
	if _, _, err := s.StageReview(ctx, a.Path, ""); !errors.Is(err, ErrArchiveBusy) {
		t.Fatal("review bypassed purge ownership", err)
	}
	if fileHash(a.Path) == "" {
		t.Fatal("refused purge/inspection damaged the archive")
	}
}
