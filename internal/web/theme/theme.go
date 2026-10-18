// Package theme loads the panel's visual presets and turns them into CSS.
//
// A preset is one reviewed JSON file in web/themes. Its colors use the
// shadcn/ui token names (background, primary, sidebar-accent…) in the same
// cssVars shape that tweakcn exports, so a theme is data, never code: the
// design-system stylesheet consumes the tokens and a new preset needs no
// change to any page. Each preset also chooses layout axes (density,
// sidebar, tabs and surfaces) so a theme can change composition as well as
// color. Values are validated before they are written into CSS; nothing is
// fetched at runtime. See docs/product/design-system.md.
package theme

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// DefaultID is the built-in preset. Saved choices that are unknown or
// retired resolve to it.
const DefaultID = "wg-guard-neutral"

// ColorKeys are the shadcn tokens every preset must define for both modes.
var ColorKeys = []string{
	"background", "foreground", "card", "card-foreground", "popover", "popover-foreground",
	"primary", "primary-foreground", "secondary", "secondary-foreground", "muted", "muted-foreground",
	"accent", "accent-foreground", "destructive", "border", "input", "ring",
	"chart-1", "chart-2", "chart-3", "chart-4", "chart-5",
	"sidebar", "sidebar-foreground", "sidebar-primary", "sidebar-primary-foreground",
	"sidebar-accent", "sidebar-accent-foreground", "sidebar-border", "sidebar-ring",
}

// OptionalKeys may appear in "theme", "light" or "dark". Status colors are a
// WG-Guard extension; the rest follow tweakcn's export.
var OptionalKeys = []string{
	"destructive-foreground", "success", "success-foreground", "warning", "warning-foreground",
	"info", "info-foreground", "font-sans", "font-serif", "font-mono", "radius",
	"shadow-color", "shadow-opacity", "shadow-blur", "shadow-spread", "shadow-offset-x",
	"shadow-offset-y", "shadow-2xs", "shadow-xs", "shadow-sm", "shadow", "shadow-md",
	"shadow-lg", "shadow-xl", "shadow-2xl", "letter-spacing", "tracking-normal",
	"tracking-tighter", "tracking-tight", "tracking-wide", "tracking-wider", "tracking-widest",
	"spacing", "primary-text", "destructive-text",
}

// ContrastKeys are project-authored readability corrections. They override
// a source value without editing it, so the original export stays reviewable.
var ContrastKeys = []string{
	"card-foreground", "popover-foreground", "primary-foreground", "secondary-foreground",
	"accent-foreground", "muted-foreground", "sidebar-foreground", "sidebar-accent-foreground",
	"sidebar-primary-foreground", "primary-text", "destructive-text", "destructive-foreground",
}

// UI holds the layout axes a preset selects. Every axis has a closed set of
// values that the stylesheet implements.
type UI struct {
	Density string `json:"density"` // compact | default | comfortable
	Sidebar string `json:"sidebar"` // plain | inset | floating
	Tabs    string `json:"tabs"`    // underline | segmented
	Surface string `json:"surface"` // flat | raised
}

var uiValues = map[string][]string{
	"density": {"compact", "default", "comfortable"},
	"sidebar": {"plain", "inset", "floating"},
	"tabs":    {"underline", "segmented"},
	"surface": {"flat", "raised"},
}

// Preset is one validated theme.
type Preset struct {
	Schema  int    `json:"schema"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Group   string `json:"group"` // wg-guard | shadcn | tweakcn
	Source  string `json:"source,omitempty"`
	UI      UI     `json:"ui"`
	CSSVars struct {
		Theme map[string]string `json:"theme"`
		Light map[string]string `json:"light"`
		Dark  map[string]string `json:"dark"`
	} `json:"cssVars"`
	Contrast map[string]map[string]string `json:"contrast,omitempty"`
}

// Values returns the merged tokens for one mode ("light" or "dark"):
// shared theme values, then the mode's own values, then contrast fixes.
func (p *Preset) Values(mode string) map[string]string {
	out := map[string]string{}
	for k, v := range p.CSSVars.Theme {
		out[k] = v
	}
	src := p.CSSVars.Light
	if mode == "dark" {
		src = p.CSSVars.Dark
	}
	for k, v := range src {
		out[k] = v
	}
	for k, v := range p.Contrast[mode] {
		out[k] = v
	}
	return out
}

// Registry is the loaded, ordered preset set.
type Registry struct {
	presets []*Preset
	byID    map[string]*Preset
}

// Load reads every *.json file in dir of fsys. Files load in name order and
// the built-in default must be present.
func Load(fsys fs.FS, dir string) (*Registry, error) {
	files, err := fs.Glob(fsys, path.Join(dir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("theme: glob: %w", err)
	}
	sort.Strings(files)
	r := &Registry{byID: map[string]*Preset{}}
	for _, f := range files {
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, fmt.Errorf("theme: read %s: %w", f, err)
		}
		p, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("theme: %s: %w", path.Base(f), err)
		}
		if strings.TrimSuffix(path.Base(f), ".json") != p.ID {
			return nil, fmt.Errorf("theme: %s: file name must match id %q", path.Base(f), p.ID)
		}
		if r.byID[p.ID] != nil {
			return nil, fmt.Errorf("theme: duplicate id %q", p.ID)
		}
		r.byID[p.ID] = p
		r.presets = append(r.presets, p)
	}
	def := r.byID[DefaultID]
	if def == nil {
		return nil, fmt.Errorf("theme: built-in %q is missing", DefaultID)
	}
	// The default leads the list; the rest keep their group, then name order.
	sort.SliceStable(r.presets, func(i, j int) bool {
		a, b := r.presets[i], r.presets[j]
		if (a.ID == DefaultID) != (b.ID == DefaultID) {
			return a.ID == DefaultID
		}
		if groupRank(a.Group) != groupRank(b.Group) {
			return groupRank(a.Group) < groupRank(b.Group)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return r, nil
}

func groupRank(g string) int {
	switch g {
	case "wg-guard":
		return 0
	case "shadcn":
		return 1
	}
	return 2
}

// Parse decodes and validates one preset file.
func Parse(data []byte) (*Preset, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var p Preset
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}
	if p.Schema != 2 {
		return nil, fmt.Errorf("unsupported schema %d", p.Schema)
	}
	if !ValidID(p.ID) || strings.TrimSpace(p.Name) == "" || len(p.Name) > 48 {
		return nil, fmt.Errorf("invalid id or name")
	}
	switch p.Group {
	case "wg-guard", "shadcn", "tweakcn":
	default:
		return nil, fmt.Errorf("invalid group %q", p.Group)
	}
	if p.Group == "tweakcn" && !strings.HasPrefix(p.Source, "https://tweakcn.com/themes/") {
		return nil, fmt.Errorf("tweakcn preset needs its source URL")
	}
	if err := p.UI.validate(); err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, k := range OptionalKeys {
		allowed[k] = true
	}
	color := map[string]bool{}
	for _, k := range ColorKeys {
		allowed[k], color[k] = true, true
	}
	for k, v := range p.CSSVars.Theme {
		if !allowed[k] || color[k] {
			return nil, fmt.Errorf("theme section: key %q is not shared", k)
		}
		if !SafeValue(v) {
			return nil, fmt.Errorf("theme section: unsafe %s", k)
		}
	}
	for mode, values := range map[string]map[string]string{"light": p.CSSVars.Light, "dark": p.CSSVars.Dark} {
		for k, v := range values {
			if !allowed[k] {
				return nil, fmt.Errorf("%s: unknown key %q", mode, k)
			}
			if !SafeValue(v) {
				return nil, fmt.Errorf("%s: unsafe %s", mode, k)
			}
		}
		for _, k := range ColorKeys {
			if values[k] == "" {
				return nil, fmt.Errorf("%s: missing %s", mode, k)
			}
		}
		merged := p.Values(mode)
		if o := merged["shadow-opacity"]; o != "" {
			f, err := strconv.ParseFloat(o, 64)
			if err != nil || f < 0 || f > 1 {
				return nil, fmt.Errorf("%s: shadow-opacity must be 0..1", mode)
			}
		}
	}
	contrast := map[string]bool{}
	for _, k := range ContrastKeys {
		contrast[k] = true
	}
	for mode, values := range p.Contrast {
		if mode != "light" && mode != "dark" {
			return nil, fmt.Errorf("contrast: invalid mode %q", mode)
		}
		for k, v := range values {
			if !contrast[k] || !SafeValue(v) {
				return nil, fmt.Errorf("contrast: invalid %s %s", mode, k)
			}
		}
	}
	return &p, nil
}

func (u *UI) validate() error {
	fields := map[string]*string{"density": &u.Density, "sidebar": &u.Sidebar, "tabs": &u.Tabs, "surface": &u.Surface}
	defaults := map[string]string{"density": "default", "sidebar": "inset", "tabs": "underline", "surface": "raised"}
	for name, field := range fields {
		if *field == "" {
			*field = defaults[name]
		}
		ok := false
		for _, v := range uiValues[name] {
			ok = ok || v == *field
		}
		if !ok {
			return fmt.Errorf("ui.%s: invalid value %q", name, *field)
		}
	}
	return nil
}

// ValidID reports whether id is a short lowercase slug.
func ValidID(id string) bool {
	if id == "" || len(id) > 48 {
		return false
	}
	for _, r := range id {
		if r != '-' && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// SafeValue rejects anything that could end a declaration, open a block or
// load a resource. Theme values are written into CSS verbatim after this.
func SafeValue(v string) bool {
	if v == "" || len(v) > 200 || strings.ContainsAny(v, ";{}<>\\\r\n@") {
		return false
	}
	lower := strings.ToLower(v)
	return !strings.Contains(lower, "url(") && !strings.Contains(lower, "expression(") && !strings.Contains(lower, "/*")
}

// All returns the presets in display order (default first).
func (r *Registry) All() []*Preset { return r.presets }

// Get returns the preset with id, or nil.
func (r *Registry) Get(id string) *Preset { return r.byID[id] }

// Known reports whether id names a loaded preset.
func (r *Registry) Known(id string) bool { return r.byID[id] != nil }

// Resolve returns id when known, else the built-in default.
func (r *Registry) Resolve(id string) string {
	if r.Known(id) {
		return id
	}
	return DefaultID
}

// Default returns the built-in preset.
func (r *Registry) Default() *Preset { return r.byID[DefaultID] }
