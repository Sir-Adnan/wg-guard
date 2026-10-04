package backup

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestArchiveContentionKeepsScheduledWorkDue(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	schedule, err := s.CreateSchedule(ctx, &Schedule{Name: "fixture", Kind: KindInterval, IntervalHours: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().UTC().Add(-time.Minute)
	if _, err := s.DB.Exec(`UPDATE backup_schedules SET next_run_at=? WHERE id=?`, formatTime(due), schedule.ID); err != nil {
		t.Fatal(err)
	}
	other, err := s.OpenData(false)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := leaseLock(other.file, archiveLockOffset, true); err != nil {
		t.Fatal(err)
	}
	if count, err := s.RunDue(ctx); count != 0 || !errors.Is(err, ErrArchiveBusy) {
		t.Fatal("competing host/container archive was not rejected")
	}
	current, err := s.GetSchedule(ctx, schedule.ID)
	if err != nil || current.LastRunAt != nil || !current.NextRunAt.Equal(due) {
		t.Fatal("contention consumed a durable scheduled attempt")
	}
	if err := leaseUnlock(other.file, archiveLockOffset); err != nil {
		t.Fatal(err)
	}
	if count, err := s.RunDue(ctx); count != 1 || err != nil {
		t.Fatal("durable scheduled attempt did not resume", err)
	}
}

func TestArchiveStreamingPreservesCancellation(t *testing.T) {
	s, _ := newService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.writeArchiveContext(ctx, t.TempDir()+"/cancelled.wgg", "", []member{{name: "fixture", data: []byte(strings.Repeat("x", 100))}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("archive stream ignored shutdown cancellation")
	}
}

func TestArchiveClaimExcludesAnotherProcess(t *testing.T) {
	for claim, offset := range map[string]int64{"archive": archiveLockOffset, "schedules": scheduleLockOffset} {
		t.Run(claim, func(t *testing.T) { testWorkClaimExcludesAnotherProcess(t, claim, offset) })
	}
}

func testWorkClaimExcludesAnotherProcess(t *testing.T, claim string, offset int64) {
	t.Helper()
	s, dir := newService(t)
	lease, err := s.OpenData(false)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := leaseLock(lease.file, offset, true); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestDataLeaseProcess$")
	child.Env = append(os.Environ(), "WGG_TEST_LEASE_DIR="+dir, "WGG_TEST_WORK_LOCK="+claim)
	if err := child.Run(); err == nil {
		t.Fatal("a second process entered the active archive/crypto claim")
	}
	if err := leaseUnlock(lease.file, offset); err != nil {
		t.Fatal(err)
	}
	child = exec.Command(os.Args[0], "-test.run=^TestDataLeaseProcess$")
	child.Env = append(os.Environ(), "WGG_TEST_LEASE_DIR="+dir, "WGG_TEST_WORK_LOCK="+claim)
	if err := child.Run(); err != nil {
		t.Fatal("archive claim did not release across processes")
	}
}

func TestScheduledPassExcludesCompetitorsAndPreservesConcurrentEdit(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	for key, value := range map[string]string{"backup.telegram_token": "synthetic-token", "backup.telegram_chat": "123456789"} {
		if err := s.Reg.SetRaw(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	entered, release := make(chan struct{}), make(chan struct{})
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-release:
			_, _ = io.WriteString(w, `{"ok":true}`)
		case <-r.Context().Done():
		}
	}))
	defer remote.Close()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	s.HTTPClient = routingClient{base: remote.URL, inner: remote.Client()}
	sc, err := s.CreateSchedule(ctx, &Schedule{Name: "fixture", Kind: KindInterval, IntervalHours: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE backup_schedules SET next_run_at=? WHERE id=?`, formatTime(time.Now().UTC().Add(-time.Minute)), sc.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.RunDue(ctx); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduled delivery did not start")
	}
	competitor := &Service{DB: s.DB, Reg: s.Reg, Cfg: s.Cfg}
	if count, err := competitor.RunDue(ctx); count != 0 || !errors.Is(err, ErrArchiveBusy) {
		t.Fatal("concurrent scheduled scan admitted", err)
	}
	disabled := *sc
	disabled.Enabled = false
	updated, err := s.UpdateSchedule(ctx, sc.ID, &disabled)
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	released = true
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	current, err := s.GetSchedule(ctx, sc.ID)
	if err != nil || current.Enabled || current.LastRunAt != nil || !current.NextRunAt.Equal(updated.NextRunAt) {
		t.Fatal("finished archive overwrote a changed schedule")
	}
	if count, err := competitor.RunDue(ctx); count != 0 || err != nil {
		t.Fatal("completed scheduled claim leaked", err)
	}
}

func TestScheduledPassBoundsDueRows(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	for range 9 {
		if _, err := s.CreateSchedule(ctx, &Schedule{Name: "fixture", Kind: KindInterval, IntervalHours: 1, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DB.Exec(`UPDATE backup_schedules SET next_run_at=?`, formatTime(time.Now().UTC().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	if count, err := s.RunDue(ctx); count != 8 || err != nil {
		t.Fatal("scheduled pass exceeded its row bound or lost work", err)
	}
	if count, err := s.RunDue(ctx); count != 1 || err != nil {
		t.Fatal("bounded scan lost the remaining durable row", err)
	}
}
