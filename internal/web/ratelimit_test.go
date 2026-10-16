package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestIPLimiterConcurrentPublicSub hammers the anonymous /sub/ surface from
// many goroutines with distinct sources. An unguarded limiter map fails here
// with a fatal concurrent map access (or a -race report), which would take
// the whole node process down.
func TestIPLimiterConcurrentPublicSub(t *testing.T) {
	e := newEnv(t)
	h := e.srv.Handler()
	const workers, perWorker = 32, 40
	var wg sync.WaitGroup
	for g := 0; g < workers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/sub/unknown-%d-%d", g, i), nil)
				req.RemoteAddr = fmt.Sprintf("198.51.100.%d:%d", g+1, 20000+i)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != http.StatusNotFound && rec.Code != http.StatusTooManyRequests {
					t.Errorf("GET %s = %d", req.URL.Path, rec.Code)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestIPLimiterConcurrentMixed drives blocked, fail and allow together (the
// login, onboarding and public sub call patterns) across window expiry, so
// sweeps run while other goroutines read and write.
func TestIPLimiterConcurrentMixed(t *testing.T) {
	l := newRateLimiter(time.Millisecond, 4)
	base := time.Now()
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				ip := fmt.Sprintf("203.0.113.%d", (g*7+i)%200)
				now := base.Add(time.Duration(i) * 100 * time.Microsecond)
				switch i % 3 {
				case 0:
					l.blocked(ip, now)
				case 1:
					l.fail(ip, now)
				default:
					l.allow(ip, now)
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestIPLimiterAllowAtomicBudget: concurrent requests from one source never
// exceed the per-window budget (check and count happen under one lock).
func TestIPLimiterAllowAtomicBudget(t *testing.T) {
	l := newRateLimiter(time.Minute, 60)
	now := time.Now()
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				if l.allow("192.0.2.1", now) {
					allowed.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != int64(l.max) {
		t.Fatalf("allowed %d requests, want exactly %d", got, l.max)
	}
}

// TestIPLimiterSweep drops expired windows at most once per window rather
// than scanning the table on every request.
func TestIPLimiterSweep(t *testing.T) {
	l := newRateLimiter(time.Minute, 2)
	t0 := time.Now()
	for i := 0; i < 5; i++ {
		l.fail(fmt.Sprintf("192.0.2.%d", i), t0)
	}
	if len(l.m) != 5 {
		t.Fatalf("tracked %d sources, want 5", len(l.m))
	}
	// Within the window, no sweep runs and every source stays tracked.
	l.fail("192.0.2.100", t0.Add(30*time.Second))
	if len(l.m) != 6 {
		t.Fatalf("tracked %d sources mid-window, want 6", len(l.m))
	}
	// After the first window, expired sources are swept on the next write.
	l.fail("192.0.2.200", t0.Add(61*time.Second))
	if len(l.m) != 2 {
		t.Fatalf("tracked %d sources after sweep, want 2 (live mid-window + new)", len(l.m))
	}
	if l.blocked("192.0.2.0", t0.Add(61*time.Second)) {
		t.Fatal("expired window must not block")
	}
}
