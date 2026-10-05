package domaintls

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

type Snapshot func() (Policy, error)

func peerIsLoopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Router surrounds the complete API/web mux, so public hostname requests cannot
// reach login, administration, health, metrics or APIs through another adapter.
func Router(snapshot Snapshot, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := snapshot()
		if err != nil || p.Validate() != nil {
			deny(w, http.StatusServiceUnavailable)
			return
		}
		if r.Method == http.MethodGet && (r.URL.Path == "/healthz" || r.URL.Path == "/readyz") && peerIsLoopback(r.RemoteAddr) {
			// Fixed host health checks can use the listener's local address; a
			// forwarded header never establishes locality or public access.
			if ip := net.ParseIP(strings.Trim(r.URL.Hostname(), "[]")); ip != nil && ip.IsLoopback() {
				next.ServeHTTP(w, r)
				return
			}
			if u, e := url.Parse("https://" + r.Host); e == nil {
				if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsLoopback() {
					next.ServeHTTP(w, r)
					return
				}
				panel, _ := ParseOrigin(p.PanelOrigin())
				if u.Hostname() == panel.Host && r.TLS != nil && sameHostname(r.TLS.ServerName, panel.Host) {
					next.ServeHTTP(w, r)
					return
				}
			}
		}
		origin, err := ParseOrigin("https://" + r.Host)
		if err != nil || r.TLS != nil && r.TLS.ServerName != "" && !sameHostname(r.TLS.ServerName, origin.Host) {
			deny(w, http.StatusNotFound)
			return
		}
		panel, _ := ParseOrigin(p.PanelOrigin())
		if origin == panel {
			next.ServeHTTP(w, r)
			return
		}
		sub, _ := ParseOrigin(p.SubscriptionOrigin())
		if origin != sub || r.Method != http.MethodGet && r.Method != http.MethodHead || r.URL.RawPath != "" || !PublicPath(r.URL.Path) {
			deny(w, http.StatusNotFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameHostname(a, b string) bool { host, err := Hostname(a); return err == nil && host == b }

func deny(w http.ResponseWriter, status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.Error(w, http.StatusText(status), status)
}
