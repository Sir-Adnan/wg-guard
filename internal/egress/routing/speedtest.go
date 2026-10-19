package routing

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// Speed tests measure one exit's round trip, download and upload against
// Cloudflare's speed test endpoints (the ones speed.cloudflare.com uses),
// through the outbound's mark like every other test. Each phase is bounded
// by time and bytes, so a test costs at most speedDownBytes+speedUpBytes of
// the exit's traffic and about SpeedTimeout of wall time.

// SpeedTimeout bounds one whole speed test.
const SpeedTimeout = 25 * time.Second

const (
	speedBase      = "https://speed.cloudflare.com"
	speedPings     = 4
	speedDownBytes = 25 << 20
	speedUpBytes   = 10 << 20
	speedPhase     = 8 * time.Second // per transfer phase
)

// SpeedResult is one speed test.
type SpeedResult struct {
	OK       bool
	Ping     time.Duration // best HTTP round trip on a warm connection
	Down     float64       // Mbit/s
	Up       float64       // Mbit/s
	Family   string        // ip4 | ip6
	Error    string        // fixed category when not OK: timeout | unreachable | tls | dns | http | busy | unsupported
	TestedAt time.Time
}

// SpeedTester runs speed tests through outbound marks.
type SpeedTester struct {
	Lookup Lookup
	// Base and Transport replace the endpoint and the marked dialer in tests.
	Base      string
	Transport http.RoundTripper
}

// Test measures the exit of mark. preferIPv6 dials IPv6 first, like the
// site test of an outbound whose link carries IPv6.
func (t SpeedTester) Test(ctx context.Context, mark uint32, preferIPv6 bool) SpeedResult {
	ctx, cancel := context.WithTimeout(ctx, SpeedTimeout)
	defer cancel()
	res := SpeedResult{TestedAt: time.Now().UTC()}
	client, closeIdle := t.client(mark, preferIPv6, &res.Family)
	defer closeIdle()
	base := t.Base
	if base == "" {
		base = speedBase
	}
	fail := func(err error) SpeedResult {
		res.Error = classifySpeedError(err)
		return res
	}
	// Round trip: the first request opens the connection; the best of the
	// following ones is the exit's latency to Cloudflare's edge.
	for i := 0; i <= speedPings; i++ {
		start := time.Now()
		resp, err := speedGet(ctx, client, base+"/__down?bytes=0", nil)
		if err != nil {
			return fail(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if rtt := time.Since(start); i > 0 && (res.Ping == 0 || rtt < res.Ping) {
			res.Ping = rtt
		}
	}
	down, err := speedDownload(ctx, client, base)
	if err != nil {
		return fail(err)
	}
	up, err := speedUpload(ctx, client, base)
	if err != nil {
		return fail(err)
	}
	res.Down, res.Up, res.OK = down, up, true
	return res
}

func (t SpeedTester) client(mark uint32, preferIPv6 bool, family *string) (*http.Client, func()) {
	if t.Transport != nil {
		*family = "ip4"
		return &http.Client{Transport: t.Transport}, func() {}
	}
	dialer := HostDialer{Mark: mark, PreferIPv6: preferIPv6, Timeout: ProbeTimeout, FirstFamilyBudget: siteFirstFamilyBudget, Lookup: t.Lookup}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			host, portText, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			port, err := strconv.ParseUint(portText, 10, 16)
			if err != nil {
				return nil, err
			}
			conn, fam, err := dialer.Dial(ctx, host, uint16(port))
			if err == nil {
				*family = fam
			}
			return conn, err
		},
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: ProbeTimeout,
		MaxIdleConnsPerHost: 1,
		ForceAttemptHTTP2:   true,
	}
	return &http.Client{Transport: transport}, transport.CloseIdleConnections
}

// errSpeedHTTP reports an unexpected answer from the speed endpoint.
var errSpeedHTTP = errors.New("routing: speed endpoint answered unexpectedly")

func speedGet(ctx context.Context, client *http.Client, target string, body io.Reader) (*http.Response, error) {
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", siteUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %d", errSpeedHTTP, resp.StatusCode)
	}
	return resp, nil
}

// speedDownload reads until speedDownBytes or the phase ends and reports the
// rate from the first byte on (connection setup is the ping's business).
func speedDownload(ctx context.Context, client *http.Client, base string) (float64, error) {
	pctx, cancel := context.WithTimeout(ctx, speedPhase)
	defer cancel()
	resp, err := speedGet(pctx, client, base+"/__down?bytes="+strconv.Itoa(speedDownBytes), nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	start := time.Now()
	n, err := io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(start)
	if err != nil && pctx.Err() == nil {
		return 0, err
	}
	return mbps(n, elapsed), nil
}

// speedUpload sends up to speedUpBytes of zeros and reports the rate the
// connection accepted them at; a phase that ends mid-body still counts what
// was sent.
func speedUpload(ctx context.Context, client *http.Client, base string) (float64, error) {
	pctx, cancel := context.WithTimeout(ctx, speedPhase)
	defer cancel()
	body := &countingReader{r: io.LimitReader(zeroReader{}, speedUpBytes)}
	start := time.Now()
	resp, err := speedGet(pctx, client, base+"/__up", body)
	elapsed := time.Since(start)
	if err != nil {
		if sent := body.n.Load(); pctx.Err() != nil && ctx.Err() == nil && sent > 0 {
			return mbps(sent, elapsed), nil
		}
		return 0, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return mbps(body.n.Load(), elapsed), nil
}

func mbps(n int64, d time.Duration) float64 {
	if d <= 0 || n <= 0 {
		return 0
	}
	return float64(n) * 8 / d.Seconds() / 1e6
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// countingReader counts the bytes the transport took; the transport may
// still be reading when the request returns, so the count is atomic.
type countingReader struct {
	r io.Reader
	n atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

func classifySpeedError(err error) string {
	if errors.Is(err, errSpeedHTTP) {
		return "http"
	}
	return classifySiteError(err)
}
