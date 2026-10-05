package domaintls

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var challengeToken = regexp.MustCompile(`\A[A-Za-z0-9_-]{1,128}\z`)

// Enrollment permits only a bounded HTTP challenge window. It grants no HTTPS
// certificate/hostname role and is written by the owner-authorized host issuer.
type Enrollment struct {
	Schema    int       `json:"schema"`
	Hosts     []string  `json:"hosts"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (e Enrollment) Validate(now time.Time) error {
	if e.Schema != Schema || len(e.Hosts) < 1 || len(e.Hosts) > 2 || !e.ExpiresAt.After(now) || e.ExpiresAt.After(now.Add(20*time.Minute)) {
		return ErrPolicy
	}
	for _, host := range e.Hosts {
		normalized, err := Hostname(host)
		if err != nil || normalized != host {
			return ErrPolicy
		}
	}
	return nil
}

// Challenge serves fixed root-owned webroot files before the normal challenge
// handler. It never reads a filename, command or domain out of query parameters.
func Challenge(dir string, now func() time.Time, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if dir == "" {
			next.ServeHTTP(w, r)
			return
		}
		const prefix = "/.well-known/acme-challenge/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet || r.URL.RawPath != "" {
			deny(w, 404)
			return
		}
		token := strings.TrimPrefix(r.URL.Path, prefix)
		if !challengeToken.MatchString(token) {
			deny(w, 404)
			return
		}
		raw, err := readRegular(filepath.Join(dir, "enrollment.json"), 2048)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		var enrollment Enrollment
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		at := time.Now()
		if now != nil {
			at = now()
		}
		if decoder.Decode(&enrollment) != nil || decoder.Decode(new(any)) != io.EOF || enrollment.Validate(at) != nil {
			deny(w, 404)
			return
		}
		origin, err := ParseOrigin("https://" + r.Host)
		allowed := false
		for _, host := range enrollment.Hosts {
			allowed = allowed || err == nil && origin.Host == host
		}
		if !allowed {
			next.ServeHTTP(w, r)
			return
		}
		body, err := readRegular(filepath.Join(dir, ".well-known", "acme-challenge", token), 4096)
		if err != nil {
			deny(w, 404)
			return
		}
		if strings.ContainsAny(string(body), "\r\n\x00") || len(body) == 0 {
			deny(w, 404)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	})
}
