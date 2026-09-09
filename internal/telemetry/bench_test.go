package telemetry

import (
	"context"
	"testing"
	"time"
)

func BenchmarkSampleAndSnapshot(b *testing.B) {
	base := time.Unix(1_700_000_000, 0)
	s := New(SourceFunc(func(_ context.Context, at time.Time) (RawSample, error) {
		return completeRaw(at), nil
	}), DefaultCadence)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		now := base.Add(time.Duration(i) * DefaultCadence)
		_, _ = s.Sample(context.Background(), now)
		_ = s.Snapshot(now, 60)
	}
}
