package routing

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// speedServer mimics Cloudflare's speed endpoints.
func speedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__down":
			n, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
			_, _ = io.CopyN(w, zeroReader{}, int64(n))
		case "/__up":
			_, _ = io.Copy(io.Discard, r.Body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSpeedTestMeasuresAndBounds(t *testing.T) {
	srv := speedServer(t)
	tester := &SpeedTester{Base: srv.URL, Transport: srv.Client().Transport}
	m := &Manager{Speeds: tester}
	res, err := m.SpeedTest(context.Background(), DirectSite)
	if err != nil || !res.OK || res.Down <= 0 || res.Up <= 0 || res.Ping <= 0 {
		t.Fatalf("speed test: %+v %v", res, err)
	}
	if got := m.SpeedResults()[DirectSite]; !got.OK {
		t.Fatalf("result not kept: %+v", got)
	}
	// One speed test at a time per node.
	m.diag.speed.Lock()
	if _, err := m.SpeedTest(context.Background(), DirectSite); !errors.Is(err, ErrSpeedBusy) {
		t.Fatalf("concurrent speed test: %v", err)
	}
	m.diag.speed.Unlock()
	// An unknown outbound is not tested.
	if _, err := m.SpeedTest(context.Background(), "nope"); err == nil {
		t.Fatal("unknown outbound tested")
	}
	// A wrong answer is a failure, not a zero speed.
	bad := &SpeedTester{Base: srv.URL + "/missing", Transport: srv.Client().Transport}
	if res := bad.Test(context.Background(), 0, false); res.OK || res.Error != "http" {
		t.Fatalf("bad endpoint: %+v", res)
	}
}

func TestSiteTestSlotsBoundConcurrency(t *testing.T) {
	m := &Manager{Sites: &SiteTester{}}
	slots := m.diag.siteSlots()
	for i := 0; i < SiteTestSlots; i++ {
		slots <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	u, _ := ParseSiteURL("example.com")
	res, err := m.TestSite(ctx, DirectSite, u, nil)
	if err != nil || res.Verdict != SiteFailed || res.Error != "timeout" {
		t.Fatalf("full slots must wait, then give up with the request: %+v %v", res, err)
	}
}
