package web

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// The reviewed TweakCN exports are local data. No remote CSS or executable
// theme code is loaded by a running node. Project metrics are explicitly
// separate from values provided by the source themes.
//
//go:embed visual_presets.json
var visualPresetJSON []byte

type visualPreset struct {
	ID              string                       `json:"id"`
	Name            string                       `json:"name"`
	Source          string                       `json:"source"`
	SourceThemeID   string                       `json:"source_theme_id"`
	Styles          map[string]map[string]string `json:"styles"`
	ProjectMetrics  map[string]string            `json:"project_metrics"`
	ProjectContrast map[string]map[string]string `json:"project_contrast,omitempty"`
}

type visualPresetCatalog struct {
	Schema  int `json:"schema"`
	BuiltIn struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"built_in"`
	Presets []visualPreset `json:"presets"`
}

var sourceStyleKeys = []string{
	"background", "foreground", "card", "card-foreground", "popover", "popover-foreground",
	"primary", "primary-foreground", "secondary", "secondary-foreground", "muted", "muted-foreground",
	"accent", "accent-foreground", "destructive", "destructive-foreground", "border", "input", "ring",
	"chart-1", "chart-2", "chart-3", "chart-4", "chart-5", "sidebar", "sidebar-foreground",
	"sidebar-primary", "sidebar-primary-foreground", "sidebar-accent", "sidebar-accent-foreground",
	"sidebar-border", "sidebar-ring", "font-sans", "font-serif", "font-mono", "radius",
	"shadow-color", "shadow-opacity", "shadow-blur", "shadow-spread", "shadow-offset-x",
	"shadow-offset-y", "letter-spacing", "spacing",
}

var projectMetricKeys = []string{
	"icon_size", "icon_small_size", "icon_stroke_width", "icon_container_size", "control_height",
}

func safeCSSValue(value string) bool {
	if value == "" || strings.ContainsAny(value, ";{}<>\r\n") {
		return false
	}
	lower := strings.ToLower(value)
	return !strings.Contains(lower, "url(") && !strings.Contains(lower, "@import")
}

func validPresetID(id string) bool {
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

func loadVisualPresets() (visualPresetCatalog, []byte, error) {
	var catalog visualPresetCatalog
	if err := json.Unmarshal(visualPresetJSON, &catalog); err != nil {
		return catalog, nil, fmt.Errorf("web: visual presets: %w", err)
	}
	if catalog.Schema != 1 || catalog.BuiltIn.ID != "wg-guard-neutral" || len(catalog.Presets) == 0 {
		return catalog, nil, fmt.Errorf("web: invalid visual preset registry header")
	}
	seen := map[string]bool{catalog.BuiltIn.ID: true}
	for _, p := range catalog.Presets {
		if !validPresetID(p.ID) || seen[p.ID] || p.Name == "" || !validPresetID(p.SourceThemeID) || p.Source != "https://tweakcn.com/themes/"+p.SourceThemeID {
			return catalog, nil, fmt.Errorf("web: invalid visual preset %q", p.ID)
		}
		seen[p.ID] = true
		if len(p.Styles) != 2 {
			return catalog, nil, fmt.Errorf("web: incomplete modes for %s", p.ID)
		}
		for _, mode := range []string{"light", "dark"} {
			values := p.Styles[mode]
			if len(values) != len(sourceStyleKeys) {
				return catalog, nil, fmt.Errorf("web: incomplete %s export for %s", mode, p.ID)
			}
			for _, key := range sourceStyleKeys {
				if !safeCSSValue(values[key]) {
					return catalog, nil, fmt.Errorf("web: invalid %s %s value for %s", mode, key, p.ID)
				}
			}
			opacity, err := strconv.ParseFloat(values["shadow-opacity"], 64)
			if err != nil || opacity < 0 || opacity > 1 {
				return catalog, nil, fmt.Errorf("web: invalid %s shadow opacity for %s", mode, p.ID)
			}
		}
		if len(p.ProjectMetrics) != len(projectMetricKeys) {
			return catalog, nil, fmt.Errorf("web: incomplete project metrics for %s", p.ID)
		}
		for _, key := range projectMetricKeys {
			if !safeCSSValue(p.ProjectMetrics[key]) {
				return catalog, nil, fmt.Errorf("web: invalid project metric %s for %s", key, p.ID)
			}
		}
		for mode, values := range p.ProjectContrast {
			if mode != "light" && mode != "dark" {
				return catalog, nil, fmt.Errorf("web: invalid contrast mode for %s", p.ID)
			}
			for key, value := range values {
				if key != "text_accent" && key != "action_background" && key != "action_foreground" && key != "sidebar_foreground" && key != "secondary_foreground" && key != "muted_foreground" || !safeCSSValue(value) {
					return catalog, nil, fmt.Errorf("web: invalid project contrast %s for %s", key, p.ID)
				}
			}
		}
	}
	return catalog, catalog.stylesheet(), nil
}

func (c visualPresetCatalog) known(id string) bool {
	if id == c.BuiltIn.ID {
		return true
	}
	for _, p := range c.Presets {
		if p.ID == id {
			return true
		}
	}
	return false
}

func (c visualPresetCatalog) resolve(id string) string {
	if c.known(id) {
		return id
	}
	return c.BuiltIn.ID
}

func (c visualPresetCatalog) name(id string) string {
	if id == c.BuiltIn.ID {
		return c.BuiltIn.Name
	}
	for _, p := range c.Presets {
		if p.ID == id {
			return p.Name
		}
	}
	return c.BuiltIn.Name
}

func writePresetValues(out *strings.Builder, selector string, values, contrast map[string]string) {
	out.WriteString(selector)
	out.WriteString(" {\n")
	for _, key := range sourceStyleKeys {
		fmt.Fprintf(out, "  --vp-%s: %s;\n", key, values[key])
	}
	textAccent := contrast["text_accent"]
	if textAccent == "" {
		textAccent = values["primary"]
	}
	actionBackground := contrast["action_background"]
	if actionBackground == "" {
		actionBackground = values["primary"]
	}
	actionForeground := contrast["action_foreground"]
	if actionForeground == "" {
		actionForeground = values["primary-foreground"]
	}
	sidebarForeground := contrast["sidebar_foreground"]
	if sidebarForeground == "" {
		sidebarForeground = values["sidebar-primary-foreground"]
	}
	secondaryForeground := contrast["secondary_foreground"]
	if secondaryForeground == "" {
		secondaryForeground = values["secondary-foreground"]
	}
	mutedForeground := contrast["muted_foreground"]
	if mutedForeground == "" {
		mutedForeground = values["muted-foreground"]
	}
	fmt.Fprintf(out, "  --vp-readable-text-accent: %s;\n  --vp-readable-action-background: %s;\n  --vp-readable-action-foreground: %s;\n  --vp-readable-sidebar-foreground: %s;\n  --vp-readable-secondary-foreground: %s;\n  --vp-readable-muted-foreground: %s;\n", textAccent, actionBackground, actionForeground, sidebarForeground, secondaryForeground, mutedForeground)
	opacity, _ := strconv.ParseFloat(values["shadow-opacity"], 64) // validated when the catalog loads
	fmt.Fprintf(out, "  --vp-shadow-ink: color-mix(in srgb, var(--vp-shadow-color) %g%%, transparent);\n", opacity*100)
	out.WriteString("}\n")
}

func writePresetPreview(out *strings.Builder, selector string, values map[string]string) {
	fmt.Fprintf(out, "%s { --preview-bg: %s; --preview-card: %s; --preview-primary: %s; --preview-foreground: %s; }\n",
		selector, values["background"], values["card"], values["primary"], values["foreground"])
}

func (c visualPresetCatalog) stylesheet() []byte {
	var out strings.Builder
	out.WriteString("/* Reviewed local TweakCN values; WG-Guard semantic bridge below. */\n")
	for _, p := range c.Presets {
		selector := `:root[data-visual-preset="` + p.ID + `"]`
		writePresetValues(&out, selector, p.Styles["light"], p.ProjectContrast["light"])
		out.WriteString(selector + " {\n")
		for _, key := range projectMetricKeys {
			fmt.Fprintf(&out, "  --vp-%s: %s;\n", strings.ReplaceAll(key, "_", "-"), p.ProjectMetrics[key])
		}
		out.WriteString("}\n")
		writePresetValues(&out, `:root[data-theme="dark"][data-visual-preset="`+p.ID+`"]`, p.Styles["dark"], p.ProjectContrast["dark"])
		out.WriteString("@media (prefers-color-scheme: dark) {\n")
		writePresetValues(&out, `:root:not([data-theme])[data-visual-preset="`+p.ID+`"]`, p.Styles["dark"], p.ProjectContrast["dark"])
		out.WriteString("}\n")
		writePresetPreview(&out, `.visual-preset-choice[data-choice="`+p.ID+`"]`, p.Styles["light"])
		writePresetPreview(&out, `:root[data-theme="dark"] .visual-preset-choice[data-choice="`+p.ID+`"]`, p.Styles["dark"])
		out.WriteString("@media (prefers-color-scheme: dark) {\n")
		writePresetPreview(&out, `:root:not([data-theme]) .visual-preset-choice[data-choice="`+p.ID+`"]`, p.Styles["dark"])
		out.WriteString("}\n")
	}
	out.WriteString(visualPresetBridge)
	return []byte(out.String())
}

const visualPresetBridge = `
:root[data-visual-preset]:not([data-visual-preset="wg-guard-neutral"]) {
  --bg: var(--vp-background);
  --bg-elev: var(--vp-card);
  --card-fg: var(--vp-card-foreground);
  --bg-subtle: var(--vp-muted);
  --bg-hover: color-mix(in srgb, var(--vp-accent) 25%, var(--vp-card));
  --fg: var(--vp-foreground);
  --fg-muted: var(--vp-readable-muted-foreground);
  --fg-subtle: var(--vp-readable-muted-foreground);
  --border: var(--vp-border);
  --border-strong: color-mix(in srgb, var(--vp-border) 72%, var(--vp-foreground));
  --control-border: var(--vp-input);
  --accent: var(--vp-readable-action-background);
  --accent-fg: var(--vp-readable-action-foreground, var(--vp-primary-foreground));
  --accent-hover: color-mix(in srgb, var(--vp-primary) 84%, var(--vp-foreground));
  --accent-soft: color-mix(in srgb, var(--vp-primary) 12%, transparent);
  --primary-button-bg: var(--vp-readable-action-background);
  --primary-button-hover-bg: var(--vp-readable-action-background);
  --brand: var(--vp-readable-text-accent, var(--vp-primary));
  --brand-2: var(--vp-chart-2);
  --brand-hover: color-mix(in srgb, var(--vp-primary) 78%, var(--vp-foreground));
  --brand-soft: color-mix(in srgb, var(--vp-primary) 12%, transparent);
  --violet-soft: color-mix(in srgb, var(--vp-chart-2) 12%, transparent);
  --danger: var(--vp-destructive);
  --danger-solid: var(--vp-destructive);
  --danger-solid-hover: color-mix(in srgb, var(--vp-destructive) 82%, var(--vp-foreground));
  --danger-soft: color-mix(in srgb, var(--vp-destructive) 12%, transparent);
  --ring: color-mix(in srgb, var(--vp-ring) 30%, transparent);
  --grid-line: color-mix(in srgb, var(--vp-border) 32%, transparent);
  --glass: color-mix(in srgb, var(--vp-card) 84%, transparent);
  --r-sm: calc(var(--vp-radius) * .55);
  --r-md: calc(var(--vp-radius) * .8);
  --r-lg: var(--vp-radius);
  --r-xl: calc(var(--vp-radius) * 1.4);
  --sp-1: var(--vp-spacing);
  --sp-2: calc(var(--vp-spacing) * 2);
  --sp-3: calc(var(--vp-spacing) * 3);
  --sp-4: calc(var(--vp-spacing) * 4);
  --sp-5: calc(var(--vp-spacing) * 6);
  --sp-6: calc(var(--vp-spacing) * 8);
  --sp-7: calc(var(--vp-spacing) * 12);
  --sp-8: calc(var(--vp-spacing) * 16);
  --control-h: var(--vp-control-height);
  --icon-size: var(--vp-icon-size);
  --icon-small-size: var(--vp-icon-small-size);
  --icon-stroke: var(--vp-icon-stroke-width);
  --icon-container-size: var(--vp-icon-container-size);
  --shadow-1: var(--vp-shadow-offset-x) var(--vp-shadow-offset-y) var(--vp-shadow-blur) var(--vp-shadow-spread) var(--vp-shadow-ink);
  --shadow-2: var(--vp-shadow-offset-x) var(--vp-shadow-offset-y) calc(var(--vp-shadow-blur) * 2) var(--vp-shadow-spread) var(--vp-shadow-ink);
  --shadow-3: var(--vp-shadow-offset-x) var(--vp-shadow-offset-y) calc(var(--vp-shadow-blur) * 4) var(--vp-shadow-spread) var(--vp-shadow-ink);
  --sidebar-bg: var(--vp-sidebar);
  --sidebar-fg: var(--vp-sidebar-foreground);
  --sidebar-hover: var(--vp-sidebar-accent);
  --sidebar-hover-fg: var(--vp-sidebar-accent-foreground);
  --sidebar-active: var(--vp-sidebar-primary);
  --sidebar-active-fg: var(--vp-readable-sidebar-foreground);
  --sidebar-border: var(--vp-sidebar-border);
  --popover-bg: var(--vp-popover);
  --popover-fg: var(--vp-popover-foreground);
  --secondary-bg: var(--vp-secondary);
  --secondary-fg: var(--vp-readable-secondary-foreground);
  --chart-first: var(--vp-chart-1);
  --chart-second: var(--vp-chart-2);
  --chart-third: var(--vp-chart-3);
  --chart-fourth: var(--vp-chart-4);
  --chart-fifth: var(--vp-chart-5);
  --font-mono: var(--vp-font-mono), ui-monospace, monospace;
}
:root[lang="en"][data-visual-preset]:not([data-visual-preset="wg-guard-neutral"]) { --font-ui: var(--vp-font-sans), ui-sans-serif, system-ui, sans-serif; --ui-tracking: var(--vp-letter-spacing); }
:root[lang="fa"][data-visual-preset]:not([data-visual-preset="wg-guard-neutral"]) { --font-ui: "Vazirmatn", var(--vp-font-sans), ui-sans-serif, system-ui, sans-serif; }
`
