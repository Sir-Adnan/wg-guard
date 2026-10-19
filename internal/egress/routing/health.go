package routing

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

// ProbeURL is fetched through each outbound. Cloudflare's trace endpoint on a
// literal address needs no DNS lookup, answers in a few hundred bytes and
// reports the exit address and its country.
const ProbeURL = "https://1.1.1.1/cdn-cgi/trace"

// ProbeTimeout bounds one probe.
const ProbeTimeout = 6 * time.Second

// ProbeResult is one health observation.
type ProbeResult struct {
	OK      bool
	Latency time.Duration
	ExitIP  string
	// ExitIP6 is the IPv6 exit, probed only for outbounds with the IPv6
	// site bridge on ("" when not probed or unreachable over IPv6).
	ExitIP6 string
	Country string
	Colo    string // Cloudflare data centre (IATA code) that served the probe
	WARP    string // trace "warp" field: on | plus | off ("" when absent)
	Error   string // fixed category: timeout | unreachable | tls | http | unsupported | wrong_exit
	At      time.Time
}

// Prober fetches ProbeURL with a socket mark so the request follows the
// outbound's policy route (or SNAT rule).
type Prober interface {
	Probe(ctx context.Context, mark uint32) ProbeResult
}

// HTTPProber is the production prober. Setting SO_MARK requires
// CAP_NET_ADMIN, which the node already holds for nftables and links.
type HTTPProber struct {
	URL string
}

func (p HTTPProber) Probe(ctx context.Context, mark uint32) ProbeResult {
	url := p.URL
	if url == "" {
		url = ProbeURL
	}
	return fetchTrace(ctx, mark, url)
}

// ProbeURL6 is the trace endpoint on Cloudflare's IPv6 resolver address.
const ProbeURL6 = "https://[2606:4700:4700::1111]/cdn-cgi/trace"

// ProbeIPv6 returns the outbound's IPv6 exit address, or "" when IPv6 does
// not leave through it.
func (p HTTPProber) ProbeIPv6(ctx context.Context, mark uint32) string {
	res := fetchTrace(ctx, mark, ProbeURL6)
	if !res.OK {
		return ""
	}
	if a, err := netip.ParseAddr(res.ExitIP); err == nil && a.Is6() && !a.Is4In6() {
		return a.String()
	}
	return ""
}

// IPv6Prober is implemented by probers that can observe the IPv6 exit.
type IPv6Prober interface {
	ProbeIPv6(ctx context.Context, mark uint32) string
}

func fetchTrace(ctx context.Context, mark uint32, url string) ProbeResult {
	start := time.Now()
	res := ProbeResult{At: start.UTC()}
	dialer := MarkedDialer(mark, ProbeTimeout)
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   ProbeTimeout,
		ResponseHeaderTimeout: ProbeTimeout,
		DisableKeepAlives:     true,
		Proxy:                 nil, // never through an environment proxy
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: ProbeTimeout}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		res.Error = "unsupported"
		return res
	}
	resp, err := client.Do(req)
	if err != nil {
		res.Error = classifyProbeError(err)
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		res.Error = "http"
		return res
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 4096))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		switch k {
		case "ip":
			if ip := net.ParseIP(v); ip != nil {
				res.ExitIP = ip.String()
			}
		case "loc":
			if len(v) == 2 {
				res.Country = strings.ToUpper(v)
			}
		case "colo":
			if len(v) >= 3 && len(v) <= 4 && isAlpha(v) {
				res.Colo = strings.ToUpper(v)
			}
		case "warp":
			if v == "on" || v == "plus" || v == "off" {
				res.WARP = v
			}
		}
	}
	res.OK = true
	res.Latency = time.Since(start)
	return res
}

func classifyProbeError(err error) string {
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "timeout"
	case strings.Contains(err.Error(), "tls"), strings.Contains(err.Error(), "x509"):
		return "tls"
	case errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EACCES):
		return "unsupported"
	}
	return "unreachable"
}

func isAlpha(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}
