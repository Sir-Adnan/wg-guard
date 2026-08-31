package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/config"
)

const (
	sessionCookie = "wg_session"
	themeCookie   = "wg_theme"
	localeCookie  = "wg_locale"

	csrfHeader = "X-CSRF-Token"
	csrfField  = "_csrf"
	csrfInfo   = "wg-guard:csrf:v1" // HMAC info string, versioned for rotation
)

type ctxKey int

const (
	ctxAdmin ctxKey = iota
	ctxSession
	ctxCSRF
)

// adminFrom returns the authenticated panel account, or nil when anonymous.
func adminFrom(r *http.Request) *auth.Admin {
	a, _ := r.Context().Value(ctxAdmin).(*auth.Admin)
	return a
}

// csrfFrom returns the request's CSRF token (header first, then form field).
func csrfFrom(r *http.Request) string {
	if v := r.Header.Get(csrfHeader); v != "" {
		return v
	}
	return r.PostFormValue(csrfField)
}

// deriveCSRF maps a session token to its CSRF token. The session token never
// leaves the HttpOnly cookie, so a cross-site attacker can compute neither
// value; SameSite=Lax already withholds the cookie on cross-site POSTs.
func deriveCSRF(sessionToken string) string {
	mac := hmac.New(sha256.New, []byte(sessionToken))
	mac.Write([]byte(csrfInfo))
	return hex.EncodeToString(mac.Sum(nil))
}

// csrfValid compares in constant time; length mismatch is not fatal to the
// comparison (subtle.ConstantTimeCompare returns 0).
func csrfValid(sessionToken, presented string) bool {
	want := deriveCSRF(sessionToken)
	return subtle.ConstantTimeCompare([]byte(want), []byte(presented)) == 1
}

// sessionMiddleware resolves the session cookie to an admin. Failures are
// NOT errors — anonymous rendering (login page) is a normal state; protected
// routes enforce presence themselves via requireAuth.
func (s *Server) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			a, err := s.Sessions.Validate(r.Context(), c.Value)
			if err == nil {
				r = r.WithContext(context.WithValue(r.Context(), ctxAdmin, &a))
				r = r.WithContext(context.WithValue(r.Context(), ctxSession, c.Value))
				r = r.WithContext(context.WithValue(r.Context(), ctxCSRF, deriveCSRF(c.Value)))
				w.Header().Add("Vary", "Cookie")
			} else {
				// Stale or revoked cookie: drop it so templates don't loop.
				s.clearSessionCookie(w)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requireAuth gates a handler behind a valid session. Full-page requests
// redirect to /login; htmx requests answer 401 with HX-Redirect so the swap
// replaces the whole document.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminFrom(r) == nil {
			if s.needsOnboarding(r) {
				http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
				return
			}
			if isHX(r) {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// requirePermission gates a handler behind a scope check on the session
// admin (owners always pass). The redirect target shows a denial toast —
// the server enforces, the UI never pretends (security.md).
func (s *Server) requirePermission(scope string, next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		a := adminFrom(r)
		if !auth.Authorized(a.Role, a.Permissions, scope) {
			if isHX(r) {
				w.Header().Set("X-WG-Error", "forbidden")
				w.WriteHeader(http.StatusForbidden)
				return
			}
			s.redirectToast(w, r, "/", "common.denied")
			return
		}
		next(w, r)
	})
}

// needsOnboarding reports whether the node has no owner yet (first run).
// One indexed COUNT on a table with a handful of rows — cheap per request.
func (s *Server) needsOnboarding(r *http.Request) bool {
	has, err := s.Admins.HasOwner(r.Context())
	return err == nil && !has
}

// requireCSRF rejects state-changing requests without the per-session token.
// Login and onboarding are exempt: no session exists yet, and SameSite=Lax
// already withholds the cookie from cross-site posts.
func (s *Server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		tok, _ := r.Context().Value(ctxSession).(string)
		if tok == "" {
			// Anonymous POSTs (login/onboarding) skip CSRF — they mint the
			// session; they are rate-limited instead.
			next.ServeHTTP(w, r)
			return
		}
		if !csrfValid(tok, csrfFrom(r)) {
			if isHX(r) {
				w.Header().Set("X-WG-Error", "csrf")
				w.WriteHeader(http.StatusForbidden)
				return
			}
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders applies the panel CSP and hardening headers. No inline
// script/style is used anywhere, so 'self' suffices; img-src keeps data: for
// the in-CSS select chevron.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self'; "+
				"img-src 'self' data:; font-src 'self'; connect-src 'self'; "+
				"object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// bodyCap bounds panel request bodies (forms are tiny; bulk payloads are
// well under this).
func bodyCap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		next.ServeHTTP(w, r)
	})
}

func isHX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// --- cookies -----------------------------------------------------------------

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(),
	})
}

// cookieSecure is true unless the node terminates TLS in dev mode behind
// loopback (config guarantees dev = loopback-only).
func (s *Server) cookieSecure() bool {
	return s.TLSMode != config.TLSModeDev
}

// --- login rate limiter ------------------------------------------------------

// ipLimiter is a fixed-window failure counter per source IP for the login
// and onboarding forms. Successful logins do not count. The map is bounded:
// window resets drop counters, and a hard cap resets the whole table rather
// than growing without limit under a spoofed-source flood (proxies forward
// real IPs; direct deployments see kernel source addresses).
type ipLimiter struct {
	m      map[string]*ipWindow
	window time.Duration
	max    int
}

type ipWindow struct {
	failures int
	resetAt  time.Time
}

func newIPLimiter() *ipLimiter {
	return &ipLimiter{m: make(map[string]*ipWindow), window: time.Minute, max: 8}
}

// newRateLimiter is the generic fixed-window limiter (public surfaces).
func newRateLimiter(window time.Duration, max int) *ipLimiter {
	return &ipLimiter{m: make(map[string]*ipWindow), window: window, max: max}
}

// blocked reports whether ip has exceeded the failure budget in the current
// window; a failed attempt registers via fail().
func (l *ipLimiter) blocked(ip string, now time.Time) bool {
	w, ok := l.m[ip]
	if !ok || now.After(w.resetAt) {
		return false
	}
	return w.failures >= l.max
}

func (l *ipLimiter) fail(ip string, now time.Time) {
	if len(l.m) > 10_000 {
		l.m = make(map[string]*ipWindow)
	}
	w, ok := l.m[ip]
	if !ok || now.After(w.resetAt) {
		l.m[ip] = &ipWindow{failures: 1, resetAt: now.Add(l.window)}
		return
	}
	w.failures++
}

// trim drops expired windows (housekeeping; called opportunistically).
func (l *ipLimiter) trim(now time.Time) {
	for ip, w := range l.m {
		if now.After(w.resetAt) {
			delete(l.m, ip)
		}
	}
}

// --- misc helpers ------------------------------------------------------------

// safeNext validates a post-login redirect target: same-origin paths only.
func safeNext(n string) string {
	if n == "" || !strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") ||
		strings.Contains(n, "://") || strings.ContainsAny(n, "\r\n") {
		return ""
	}
	return n
}
