package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	webassets "github.com/Sir-Adnan/wg-guard/web"
)

// --- assets ------------------------------------------------------------------

type asset struct {
	data  []byte
	hash  string
	ctype string
}

type assetSet map[string]asset

var contentTypes = map[string]string{
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".svg":   "image/svg+xml",
	".woff2": "font/woff2",
	".png":   "image/png",
	".ico":   "image/x-icon",
}

// initAssets reads every static file into memory once (≈160 KB total —
// fonts dominate) and hashes it. Request serving is then pure memory +
// http.ServeContent, with immutable caching keyed by the ?v= hash.
func (s *Server) initAssets() error {
	sub, err := fs.Sub(webassets.FS, "static")
	if err != nil {
		return fmt.Errorf("web: static fs: %w", err)
	}
	set := assetSet{}
	err = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(sub, p)
		if err != nil {
			return fmt.Errorf("web: read asset %s: %w", p, err)
		}
		sum := sha256.Sum256(data)
		ctype := contentTypes[strings.ToLower(path.Ext(p))]
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		set["/"+p] = asset{data: data, hash: hex.EncodeToString(sum[:8]), ctype: ctype}
		return nil
	})
	if err != nil {
		return fmt.Errorf("web: assets: %w", err)
	}
	s.assets = set
	return nil
}

// handleAssets serves one embedded file with strong caching: URLs carry
// ?v=<content hash> (template helpers), so content changes always change
// the URL and responses may be immutable-cached forever.
func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	a, ok := s.assets["/"+name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", a.ctype)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+a.hash+`"`)
	http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(a.data))
}

// assetURL returns the cache-busted public URL for an embedded asset.
func (s *Server) assetURL(name string) string {
	if a, ok := s.assets[name]; ok {
		return "/assets" + name + "?v=" + a.hash
	}
	return "/assets" + name
}

// --- templates ---------------------------------------------------------------

// pageTemplate is one page's template set: the shared base (both layouts +
// partials) cloned and extended with the page's own {{define}} blocks.
type pageTemplate struct {
	t *template.Template
}

// initTemplates parses the base once and clones it per page so every page
// can own a "content" block without name collisions.
func (s *Server) initTemplates() error {
	base := template.New("base").Funcs(template.FuncMap{
		"list": func(items ...string) []string { return items },
		"dict": func(kvs ...any) map[string]any {
			m := make(map[string]any, len(kvs)/2)
			for i := 0; i+1 < len(kvs); i += 2 {
				if k, ok := kvs[i].(string); ok {
					m[k] = kvs[i+1]
				}
			}
			return m
		},
		// i64u normalizes the integer shapes stored on domain structs so
		// formatting helpers (which take int64) can be used uniformly.
		"i64u": func(v any) int64 {
			switch x := v.(type) {
			case int64:
				return x
			case uint64:
				return int64(x)
			case *int64:
				if x != nil {
					return *x
				}
			case int:
				return int64(x)
			}
			return 0
		},
		"timePtr": func(t time.Time) *time.Time { return &t },
	})
	files, err := fs.Glob(webassets.FS, "templates/*.html")
	if err != nil {
		return fmt.Errorf("web: templates glob: %w", err)
	}
	var baseFiles []string
	pageFiles := map[string]string{} // page name → file
	for _, f := range files {
		switch {
		case strings.HasPrefix(path.Base(f), "layout_"),
			strings.HasPrefix(path.Base(f), "partial_"):
			baseFiles = append(baseFiles, f)
		case strings.HasPrefix(path.Base(f), "page_"):
			pageFiles[strings.TrimSuffix(strings.TrimPrefix(path.Base(f), "page_"), ".html")] = f
		}
	}
	if len(baseFiles) == 0 {
		return fmt.Errorf("web: no base templates found")
	}
	base, err = base.ParseFS(webassets.FS, baseFiles...)
	if err != nil {
		return fmt.Errorf("web: parse base templates: %w", err)
	}
	pages := make(map[string]*pageTemplate, len(pageFiles))
	for name, f := range pageFiles {
		clone, err := base.Clone()
		if err != nil {
			return fmt.Errorf("web: clone %s: %w", name, err)
		}
		t, err := clone.ParseFS(webassets.FS, f)
		if err != nil {
			return fmt.Errorf("web: parse page %s: %w", name, err)
		}
		pages[name] = &pageTemplate{t: t}
	}
	if len(pages) == 0 {
		return fmt.Errorf("web: no page templates found")
	}
	s.pages = pages
	return nil
}

// View is the render context for every template execution. Locale-scoped
// helpers are methods so templates stay i18n-aware without global state.
type View struct {
	Locale    i18n.Locale
	Dir       string
	Theme     string // "light" | "dark" | "system"
	Path      string // request path, for nav highlighting
	PageClass string // content width tier (" content--narrow" on form/settings routes)
	Admin     *auth.Admin
	CSRF      string
	Version   string

	// iconBase is the cache-busted sprite URL, resolved per server.
	iconBase string
	// assetFn resolves cache-busted asset URLs (set per server).
	assetFn func(string) string

	// ToastMsg carries a post-redirect-flash message (already localized).
	ToastMsg string

	// Data carries the page-specific payload (typed per page).
	Data any
}

// A returns the cache-busted URL for an embedded static asset, e.g.
// {{.A "/css/app.css"}}.
func (v *View) A(name string) string { return v.assetFn(name) }

// IsActive reports whether the current path belongs to a nav section.
func (v *View) IsActive(section string) bool {
	if section == "/" {
		return v.Path == "/"
	}
	return v.Path == section || strings.HasPrefix(v.Path, section+"/")
}

// Can reports whether the signed-in admin may use a scope-gated surface
// (owners always pass). The nav renders accordingly; the server enforces
// regardless (security.md: the UI never hides what the server doesn't
// enforce — this is the polite direction, not the security one).
func (v *View) Can(scope string) bool {
	if v.Admin == nil {
		return false
	}
	return auth.Authorized(v.Admin.Role, v.Admin.Permissions, scope)
}

// T translates key with the request's locale.
func (v *View) T(key string, args ...any) string {
	return i18n.T(v.Locale, key, args...)
}

// Icon renders a sprite reference. Icons are recognition aids, never
// decoration; text labels stay in markup next to them.
func (v *View) Icon(name string) template.HTML {
	return template.HTML(`<svg class="icon" aria-hidden="true"><use href="` +
		v.iconBase + `#i-` + name + `"></use></svg>`)
}

// IconS is the small variant (inline in dense rows).
func (v *View) IconS(name string) template.HTML {
	return template.HTML(`<svg class="icon icon--sm" aria-hidden="true"><use href="` +
		v.iconBase + `#i-` + name + `"></use></svg>`)
}

// B formats a byte count with locale units.
func (v *View) B(n int64) string { return i18n.FormatBytes(v.Locale, n) }

// N formats an integer with grouping.
func (v *View) N(n int64) string { return i18n.FormatInt(n) }

// D renders a date ("never" when nil).
func (v *View) D(t *time.Time) string {
	if t == nil {
		return v.T("common.never")
	}
	return i18n.FormatDate(v.Locale, *t, nil)
}

// DT renders a date with time.
func (v *View) DT(t *time.Time) string {
	if t == nil {
		return v.T("common.never")
	}
	return i18n.FormatDateTime(v.Locale, *t, nil)
}

// Rel renders relative time ("never" when nil).
func (v *View) Rel(t *time.Time) string {
	if t == nil {
		return v.T("common.never")
	}
	return i18n.FormatRelative(v.Locale, time.Now(), *t)
}

// RelIn renders a future-relative hint ("in 3 days") for expiry columns;
// past dates fall back to the past-relative form.
func (v *View) RelIn(t *time.Time) string {
	if t == nil {
		return v.T("common.never")
	}
	return i18n.FormatRelativeUntil(v.Locale, time.Now(), *t)
}

// Dur renders a duration in seconds.
func (v *View) Dur(sec int64) string { return i18n.FormatDuration(v.Locale, sec) }

// St renders a localized lifecycle status label.
func (v *View) St(status string) string { return v.T("status." + status) }

// StClass maps a status to its badge tone class.
func (v *View) StClass(status string) string {
	switch status {
	case "active":
		return "badge--ok"
	case "waiting_first_connection":
		return "badge--info"
	case "expired":
		return "badge--warn"
	case "suspended", "disabled":
		return "badge"
	case "traffic_exceeded":
		return "badge--danger"
	default:
		return "badge"
	}
}

// KD renders a kbps limit for form inputs ("" when nil).
func (v *View) KD(n *int) string {
	if n == nil {
		return ""
	}
	return i18n.FormatInt(int64(*n))
}

// U renders a kbps limit as text ("unlimited" when nil).
func (v *View) U(n *int) string {
	if n == nil {
		return v.T("common.unlimited")
	}
	return i18n.FormatKbps(v.Locale, *n)
}

// BLim renders a byte limit ("unlimited" when nil).
func (v *View) BLim(limit *int64) string {
	if limit == nil {
		return v.T("common.unlimited")
	}
	return v.B(*limit)
}

// ExpiryClass colors imminent (warn) and passed (danger) expiries.
func (v *View) ExpiryClass(t *time.Time) string {
	if t == nil {
		return ""
	}
	d := time.Until(*t)
	if d < 0 {
		return "text-danger"
	}
	if d < 7*24*time.Hour {
		return "text-warn"
	}
	return ""
}

// MeterClass picks the nearest width step (CSP forbids inline styles) and
// the tone class for a used/limit pair; limit == nil renders full bar.
func (v *View) MeterClass(used int64, limit *int64) string {
	if limit == nil || *limit <= 0 {
		return "w0"
	}
	return meterClass(used, *limit)
}

// render executes a page inside the given layout ("app" or "auth").
func (s *Server) render(w http.ResponseWriter, r *http.Request, page, layout string, data any) error {
	pt, ok := s.pages[page]
	if !ok {
		return fmt.Errorf("web: unknown page %q", page)
	}
	v := s.newView(r)
	v.Data = data
	s.applyFlash(r, v)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := pt.t.ExecuteTemplate(w, layout, v); err != nil {
		if s.Log != nil {
			s.Log.Error("web: render", "page", page, "err", err)
		}
		return err
	}
	return nil
}

// applyFlash resolves the ?toast= PRG flash into a localized message; the
// ?rawmsg= variant carries an already-rendered safe string.
func (s *Server) applyFlash(r *http.Request, v *View) {
	if raw := r.URL.Query().Get("rawmsg"); raw != "" {
		v.ToastMsg = raw
		return
	}
	key := r.URL.Query().Get("toast")
	if key == "" {
		return
	}
	if targ := r.URL.Query().Get("targ"); targ != "" {
		v.ToastMsg = s.t(r, key, targ)
	} else {
		v.ToastMsg = s.t(r, key)
	}
}

// partial executes one {{define}} block (htmx fragment swap target).
func (s *Server) partial(w http.ResponseWriter, r *http.Request, page, block string, data any) error {
	pt, ok := s.pages[page]
	if !ok {
		return fmt.Errorf("web: unknown page %q", page)
	}
	v := s.newView(r)
	v.Data = data
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	return pt.t.ExecuteTemplate(w, block, v)
}

// newView builds the per-request render context from middleware state.
func (s *Server) newView(r *http.Request) *View {
	v := &View{
		Dir:       "rtl",
		Theme:     themeFrom(r),
		Path:      r.URL.Path,
		PageClass: pageClass(r.URL.Path),
		Version:   s.Version,
		iconBase:  s.assetURL("/img/icons.svg"),
		assetFn:   s.assetURL,
	}
	if a := adminFrom(r); a != nil {
		v.Admin = a
		v.CSRF, _ = r.Context().Value(ctxCSRF).(string)
	}
	v.Locale = s.localeFor(r)
	v.Dir = v.Locale.Dir()
	return v
}

// localeFor resolves the effective locale: the signed-in admin's stored
// preference, else the wg_locale cookie (pre-login choice), else fa.
func (s *Server) localeFor(r *http.Request) i18n.Locale {
	if a := adminFrom(r); a != nil && a.Locale != "" {
		return i18n.Normalize(a.Locale)
	}
	return i18n.Normalize(localeFrom(r))
}

// pageClass picks the content width tier for the route: settings and
// create/edit forms read best on a centered narrow column, while tables and
// dashboards keep the full fluid width (see .content--narrow in app.css).
func pageClass(path string) string {
	if strings.HasPrefix(path, "/settings") ||
		strings.HasSuffix(path, "/new") || strings.HasSuffix(path, "/edit") {
		return " content--narrow"
	}
	return ""
}

func themeFrom(r *http.Request) string {
	if c, err := r.Cookie(themeCookie); err == nil {
		switch c.Value {
		case "light", "dark", "system":
			return c.Value
		}
	}
	return "system"
}

func localeFrom(r *http.Request) string {
	if c, err := r.Cookie(localeCookie); err == nil {
		return c.Value
	}
	return ""
}
