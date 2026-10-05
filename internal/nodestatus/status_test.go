package nodestatus

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/metrics"
	"github.com/Sir-Adnan/wg-guard/internal/runtimeapply"
	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
)

func TestSnapshotSeparatesPendingRuntimeAndCurrentAccounting(t *testing.T) {
	now := time.Now().UTC()
	collector := metrics.New()
	collector.SetLastCycle(time.Second, now.Add(-time.Minute), 4)
	r := runtimeapply.Observation{Status: runtimeapply.Pending, Desired: 2, Applied: 1, At: now.Add(-time.Minute), InFlight: true}
	s := Source{
		Readiness:        func(context.Context) State { return NotReady },
		Runtime:          func() runtimeapply.Observation { return r },
		Accounting:       collector.AccountingObservation,
		AccountingWindow: func(context.Context) time.Duration { return 10 * time.Minute },
	}
	d := s.Read(context.Background(), now)
	if d.Readiness != NotReady || d.Runtime.State != Pending || !d.Runtime.InFlight || d.Runtime.LastResult != Applied || d.Accounting.State != Current || d.Accounting.MaxAgeSeconds != 600 {
		t.Fatal("snapshot erased the distinction between saved/pending/successful observations")
	}
	if d.Runtime.RequestedSequence != 2 || d.Runtime.AppliedSequence != 1 {
		t.Fatal("process-local evidence changed")
	}
	r.InFlight = false
	r.LastFailed = true
	d = s.Read(context.Background(), now)
	if d.Runtime.LastResult != Failed {
		t.Fatal("failed completion reported as applied")
	}
	for _, stamp := range []time.Time{now.Add(-time.Hour), now.Add(time.Minute)} {
		collector.SetLastCycle(time.Second, stamp, 0)
		d = s.Read(context.Background(), now)
		if d.Accounting.State != Unavailable || d.Accounting.ObservedAt == nil {
			t.Fatal("old/future accounting was healthy or provenance lost")
		}
	}
}

func TestUnavailableSnapshotHasExplicitNullsAndNoWork(t *testing.T) {
	d := (Source{}).Read(context.Background(), time.Now())
	if d.Readiness != Unavailable || d.Runtime.State != Unavailable || d.Accounting.State != Unavailable || d.Telemetry.State != Unavailable {
		t.Fatal("missing collaborators implied readiness")
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"last_completed_at":null`, `"observed_at":null`, `"issues":[]`} {
		if !strings.Contains(string(raw), want) {
			t.Fatal("unavailable evidence does not have canonical nulls")
		}
	}
}

func TestStatusReaderDoesNotCollectTelemetry(t *testing.T) {
	calls := 0
	sampler := telemetry.New(telemetry.SourceFunc(func(context.Context, time.Time) (telemetry.RawSample, error) {
		calls++
		return telemetry.RawSample{}, nil
	}), time.Second)
	now := time.Now().UTC()
	if _, err := sampler.Sample(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	s := Source{Telemetry: func(at time.Time) telemetry.History { return sampler.Snapshot(at, 1) }}
	for range 10 {
		_ = s.Read(context.Background(), now)
	}
	if calls != 1 {
		t.Fatal("reading status ran the host collector")
	}
	if d := s.Read(context.Background(), now.Add(3*time.Second)); d.Telemetry.State != State("degraded") {
		t.Fatal("stale sample appeared healthy")
	}
	if d := s.Read(context.Background(), now.Add(-time.Second)); d.Telemetry.State != State("degraded") {
		t.Fatal("future sample appeared healthy")
	}
}
