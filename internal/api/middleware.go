package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/token"
)

// ctxKey is the middleware context namespace.
type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyToken
)

// RequestID returns the request's correlation id ("" before the middleware).
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRequestID).(string)
	return v
}

// TokenFrom returns the verified API token for the request.
func TokenFrom(ctx context.Context) *token.Verified {
	v, _ := ctx.Value(ctxKeyToken).(*token.Verified)
	return v
}

// statusRecorder captures the response status for logging and metrics. The
// optional buf captures the body (idempotency snapshots).
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
	buf    *bytes.Buffer
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	if s.buf != nil {
		s.buf.Write(b)
	}
	return s.ResponseWriter.Write(b)
}

// Flush supports streaming responses (SSE, future dashboard refresh).
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// requestIDMiddleware assigns a correlation id to every request.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 || !isPrintableASCII(id) {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, id)))
	})
}

func newRequestID() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return "req_" + hex.EncodeToString(buf)
}

func isPrintableASCII(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

// loggingMiddleware logs one structured line per request. Bodies, query
// strings, and authorization headers are never logged (security.md).
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		if s.Log == nil {
			return
		}
		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		} else if rec.status >= 400 {
			level = slog.LevelWarn
		}
		s.Log.Log(r.Context(), level, "http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start).Round(time.Microsecond).String(),
			"request_id", RequestID(r.Context()),
		)
	})
}

// recoverMiddleware converts handler panics into the error envelope.
func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if s.Log != nil {
					s.Log.Error("panic recovered",
						"path", r.URL.Path, "request_id", RequestID(r.Context()), "panic", v)
				}
				writeErr(w, r, http.StatusInternalServerError, domain.CodeInternal, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets the baseline headers for API responses. Sensitive
// endpoints (config/qr) additionally send no-store.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware allows token-authenticated cross-origin API access (tokens
// are not cookies, so CSRF does not apply). Scoped to /api/v1.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1") {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", "*")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-Id")
			h.Set("Access-Control-Expose-Headers", "X-Request-Id, X-RateLimit-Limit, X-RateLimit-Remaining, Retry-After")
			h.Set("Access-Control-Max-Age", "86400")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// maxBodyMiddleware rejects oversized request bodies (1 MiB — bulk creation
// of 500 users with metadata fits well within it).
func maxBodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		next.ServeHTTP(w, r)
	})
}

// authMiddleware authenticates the bearer token and enforces the route's
// required scope. Public routes are registered without it.
func (s *Server) authMiddleware(required string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			writeErr(w, r, http.StatusUnauthorized, domain.CodeUnauthorized, "missing bearer token")
			return
		}
		v, err := s.Tokens.Verify(r.Context(), strings.TrimSpace(header[len(prefix):]), clientIP(r))
		if err != nil {
			// Any verification failure (malformed, unknown, revoked, expired,
			// CIDR-blocked) is an authentication failure: 401, not 400.
			writeErr(w, r, http.StatusUnauthorized, domain.CodeUnauthorized, "invalid token")
			return
		}
		if !v.Authorize(required) {
			writeErr(w, r, http.StatusForbidden, domain.CodeForbidden,
				"token lacks the required scope: "+required)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyToken, v)))
	})
}

// rateLimitEntry is one per-token fixed-window counter.
type rateLimitEntry struct {
	window int64
	count  int
}

// rateLimiter is the per-token fixed-window limiter. A map guarded by a
// mutex with 60s windows: O(1) memory per active token, cleaned by
// housekeeping so revoked tokens cannot accumulate.
type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateLimitEntry
	limit   int // requests per 60 s window; 0 = unlimited
}

func newRateLimiter(limit int) *rateLimiter {
	return &rateLimiter{entries: map[string]*rateLimitEntry{}, limit: limit}
}

// SetLimit live-applies a new per-token limit (api.rate_limit_per_minute
// setting change): current windows keep counting against the new ceiling,
// in-flight windows are not reset (an operator tightening the limit does not
// grant a fresh window).
func (l *rateLimiter) SetLimit(limit int) {
	l.mu.Lock()
	l.limit = limit
	l.mu.Unlock()
}

// allow reports whether the token may proceed; it also returns the window
// remaining count for the response headers.
func (l *rateLimiter) allow(tokenID string, now int64) (bool, int, int) {
	if l.limit <= 0 {
		return true, l.limit, l.limit
	}
	window := now / 60
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[tokenID]
	if !ok || e.window != window {
		if !ok {
			e = &rateLimitEntry{}
			l.entries[tokenID] = e
		}
		e.window = window
		e.count = 0
	}
	if e.count >= l.limit {
		return false, l.limit - e.count, l.limit
	}
	e.count++
	return true, l.limit - e.count, l.limit
}

// enforce (housekeeping) drops windows older than the current one.
func (l *rateLimiter) enforce(now int64) {
	window := now / 60
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, e := range l.entries {
		if e.window < window {
			delete(l.entries, id)
		}
	}
}

// rateLimitMiddleware applies the per-token fixed window (api.md:
// RATE_LIMITED with Retry-After). It must run INSIDE auth (needs the token).
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := TokenFrom(r.Context())
		if v != nil {
			ok, remaining, limit := s.limiter.allow(v.Token.ID, time.Now().Unix())
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			if !ok {
				w.Header().Set("Retry-After", retryAfter(60))
				writeErr(w, r, http.StatusTooManyRequests, domain.CodeRateLimited,
					"rate limit exceeded; retry later")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func retryAfter(windowSeconds int) string {
	return strconv.Itoa(windowSeconds - int(time.Now().Unix()%60) + 1)
}
