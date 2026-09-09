package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"unsafe"
)

func completeRaw(at time.Time) RawSample {
	cpu, load := 25.0, 0.5
	return RawSample{
		At:                      at,
		HostAvailable:           true,
		CPUPercent:              &cpu,
		MemTotalBytes:           1_000,
		MemAvailableBytes:       400,
		DiskTotalBytes:          2_000,
		DiskFreeBytes:           500,
		Load1:                   &load,
		Uptime:                  time.Hour,
		ProcessRSSBytes:         100,
		ProcessHeapBytes:        50,
		ProcessMetricsAvailable: true,
		Goroutines:              7,
		HostNetwork:             Counter{Identity: "host-a", RXBytes: 100, TXBytes: 200, Available: true},
		VPNNetwork:              Counter{Identity: "vpn-a", RXBytes: 1_000, TXBytes: 2_000, Available: true},
		OnlineUsers:             2,
		ActivePeers:             5,
		ActivityAvailable:       true,
		EnabledInterfaces:       2,
		ObservedInterfaces:      2,
		InterfacesAvailable:     true,
		Ready:                   true,
		ReadinessAvailable:      true,
		AccountingAvailable:     true,
	}
}

func sequenceSource(samples ...RawSample) Source {
	var mu sync.Mutex
	n := 0
	return SourceFunc(func(_ context.Context, _ time.Time) (RawSample, error) {
		mu.Lock()
		defer mu.Unlock()
		if n >= len(samples) {
			return RawSample{}, errors.New("fixture exhausted")
		}
		got := samples[n]
		n++
		return got, nil
	})
}

func sampleOK(t *testing.T, s *Sampler, at time.Time) Point {
	t.Helper()
	p, err := s.Sample(context.Background(), at)
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	return p
}

func TestSamplerDerivesOnlyContinuousMonotonicRates(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	first := completeRaw(t0)
	second := completeRaw(t0.Add(10 * time.Second))
	second.HostNetwork.RXBytes, second.HostNetwork.TXBytes = 1_100, 2_200
	second.VPNNetwork.RXBytes, second.VPNNetwork.TXBytes = 3_000, 5_000
	reset := completeRaw(t0.Add(20 * time.Second))
	reset.HostNetwork.RXBytes, reset.HostNetwork.TXBytes = 10, 20
	reset.VPNNetwork.RXBytes, reset.VPNNetwork.TXBytes = 30, 40
	afterReset := completeRaw(t0.Add(30 * time.Second))
	afterReset.HostNetwork.RXBytes, afterReset.HostNetwork.TXBytes = 110, 220
	afterReset.VPNNetwork.RXBytes, afterReset.VPNNetwork.TXBytes = 230, 440
	replaced := completeRaw(t0.Add(40 * time.Second))
	replaced.HostNetwork = Counter{Identity: "host-b", RXBytes: 1_000, TXBytes: 2_000, Available: true}
	replaced.VPNNetwork = Counter{Identity: "vpn-b", RXBytes: 3_000, TXBytes: 4_000, Available: true}
	late := completeRaw(t0.Add(80 * time.Second))
	late.HostNetwork = Counter{Identity: "host-b", RXBytes: 5_000, TXBytes: 6_000, Available: true}
	late.VPNNetwork = Counter{Identity: "vpn-b", RXBytes: 7_000, TXBytes: 8_000, Available: true}

	s := New(sequenceSource(first, second, reset, afterReset, replaced, late), 10*time.Second)
	if p := sampleOK(t, s, t0); p.HostRXRate.Available || p.VPNRXRate.Available {
		t.Fatal("first sample must not fabricate rates")
	}
	p := sampleOK(t, s, t0.Add(10*time.Second))
	if !p.HostRXRate.Available || p.HostRXRate.Value != 100 || p.HostTXRate.Value != 200 {
		t.Fatalf("host rates = %+v/%+v", p.HostRXRate, p.HostTXRate)
	}
	if !p.VPNRXRate.Available || p.VPNRXRate.Value != 200 || p.VPNTXRate.Value != 300 {
		t.Fatalf("VPN rates = %+v/%+v", p.VPNRXRate, p.VPNTXRate)
	}
	if p := sampleOK(t, s, t0.Add(20*time.Second)); p.HostRXRate.Available || p.VPNRXRate.Available {
		t.Fatal("counter reset must make rates unavailable")
	}
	p = sampleOK(t, s, t0.Add(30*time.Second))
	if p.HostRXRate.Value != 10 || p.HostTXRate.Value != 20 || p.VPNRXRate.Value != 20 || p.VPNTXRate.Value != 40 {
		t.Fatalf("post-reset rates = %+v", p)
	}
	if p := sampleOK(t, s, t0.Add(40*time.Second)); p.HostRXRate.Available || p.VPNRXRate.Available {
		t.Fatal("interface replacement must make rates unavailable")
	}
	if p := sampleOK(t, s, t0.Add(80*time.Second)); p.HostRXRate.Available || p.VPNRXRate.Available {
		t.Fatal("unreasonable sample gap must make rates unavailable")
	}
}

func TestSamplerPreservesMetricsAndDistinctActivityCounts(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	s := New(sequenceSource(completeRaw(t0)), 10*time.Second)
	p := sampleOK(t, s, t0)
	if p.MemoryUsedBytes.Value != 600 || p.MemoryTotalBytes.Value != 1_000 || p.MemoryPercent.Value != 60 {
		t.Fatalf("memory metrics = %+v/%+v/%+v", p.MemoryUsedBytes, p.MemoryTotalBytes, p.MemoryPercent)
	}
	if p.DiskUsedBytes.Value != 1_500 || p.DiskPercent.Value != 75 {
		t.Fatalf("disk metrics = %+v/%+v", p.DiskUsedBytes, p.DiskPercent)
	}
	if p.OnlineUsers.Value != 2 || p.ActivePeers.Value != 5 {
		t.Fatalf("activity counts = users %d peers %d", p.OnlineUsers.Value, p.ActivePeers.Value)
	}
	if p.ProcessRSSBytes.Value != 100 || p.ProcessHeapBytes.Value != 50 || p.Goroutines.Value != 7 {
		t.Fatalf("process metrics = %+v", p)
	}
}

func TestSamplerRingWrapsInOrderAndReturnsCopies(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	var n int
	s := New(SourceFunc(func(_ context.Context, at time.Time) (RawSample, error) {
		raw := completeRaw(at)
		raw.OnlineUsers = uint64(n)
		n++
		return raw, nil
	}), 10*time.Second)
	for i := 0; i < HistoryCapacity+5; i++ {
		sampleOK(t, s, base.Add(time.Duration(i)*10*time.Second))
	}
	h := s.Snapshot(base.Add(time.Hour), HistoryCapacity+50)
	if len(h.Points) != HistoryCapacity {
		t.Fatalf("history length = %d", len(h.Points))
	}
	if h.Points[0].OnlineUsers.Value != 5 || h.Points[len(h.Points)-1].OnlineUsers.Value != HistoryCapacity+4 {
		t.Fatalf("history order = first %d last %d", h.Points[0].OnlineUsers.Value, h.Points[len(h.Points)-1].OnlineUsers.Value)
	}
	h.Points[0].OnlineUsers.Value = 999_999
	again := s.Snapshot(base.Add(time.Hour), HistoryCapacity)
	if again.Points[0].OnlineUsers.Value != 5 {
		t.Fatal("snapshot mutation leaked into the ring")
	}
	limited := s.Snapshot(base.Add(time.Hour), 3)
	if len(limited.Points) != 3 || limited.Points[0].OnlineUsers.Value != HistoryCapacity+2 {
		t.Fatalf("limited history = %+v", limited.Points)
	}
}

func TestSamplerHealthStatesAndStaleness(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	healthy := completeRaw(t0)
	missingLink := completeRaw(t0.Add(10 * time.Second))
	missingLink.ObservedInterfaces = 1
	failedReady := completeRaw(t0.Add(20 * time.Second))
	failedReady.Ready = false
	accountingError := completeRaw(t0.Add(30 * time.Second))
	accountingError.RecentAccountingError = true
	s := New(sequenceSource(healthy, missingLink, failedReady, accountingError), 10*time.Second)
	if p := sampleOK(t, s, t0); p.Health != HealthHealthy || p.Issues != 0 {
		t.Fatalf("healthy state = %s/%v", p.Health, p.Issues.Codes())
	}
	if p := sampleOK(t, s, t0.Add(10*time.Second)); p.Health != HealthDegraded || !p.Issues.Has(IssueAWGInterfaceMissing) {
		t.Fatalf("missing-link state = %s/%v", p.Health, p.Issues.Codes())
	}
	if p := sampleOK(t, s, t0.Add(20*time.Second)); p.Health != HealthDegraded || !p.Issues.Has(IssueReadinessFailed) {
		t.Fatalf("readiness state = %s/%v", p.Health, p.Issues.Codes())
	}
	if p := sampleOK(t, s, t0.Add(30*time.Second)); p.Health != HealthDegraded || !p.Issues.Has(IssueAccountingError) {
		t.Fatalf("accounting state = %s/%v", p.Health, p.Issues.Codes())
	}
	h := s.Snapshot(t0.Add(60*time.Second), 1)
	if h.Latest.Health != HealthDegraded || !h.Latest.Issues.Has(IssueSampleStale) {
		t.Fatalf("stale state = %s/%v", h.Latest.Health, h.Latest.Issues.Codes())
	}
}

func TestSamplerSourceFailureIsUnavailable(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	s := New(SourceFunc(func(context.Context, time.Time) (RawSample, error) {
		return RawSample{}, errors.New("private source detail")
	}), 10*time.Second)
	p, err := s.Sample(context.Background(), t0)
	if err == nil {
		t.Fatal("source error must be returned to the protected scheduler log boundary")
	}
	if p.Health != HealthUnavailable || !p.Issues.Has(IssueSourceUnavailable) {
		t.Fatalf("source failure = %s/%v", p.Health, p.Issues.Codes())
	}
	if p.At != t0 {
		t.Fatalf("source failure timestamp = %v", p.At)
	}
}

func TestRingStorageStaysBelowBudget(t *testing.T) {
	bytes := uintptr(HistoryCapacity) * unsafe.Sizeof(Point{})
	if bytes > 128<<10 {
		t.Fatalf("ring point storage = %d bytes, budget = %d", bytes, 128<<10)
	}
}

func TestSamplerConcurrentReadersAndWriter(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	s := New(SourceFunc(func(_ context.Context, at time.Time) (RawSample, error) {
		return completeRaw(at), nil
	}), 10*time.Second)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if offset == 0 {
					_, _ = s.Sample(context.Background(), base.Add(time.Duration(i)*10*time.Second))
				} else {
					_ = s.Snapshot(base.Add(time.Duration(i)*10*time.Second), 60)
				}
			}
		}(worker)
	}
	wg.Wait()
}
