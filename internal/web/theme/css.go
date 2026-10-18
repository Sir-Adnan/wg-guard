package theme

import (
	"fmt"
	"strconv"
	"strings"
)

// emitOrder is the declaration order inside a generated rule. Keys a preset
// does not define are skipped and inherit the built-in default.
var emitOrder = append(append([]string{}, ColorKeys...), OptionalKeys...)

var shadowSizes = []string{"shadow-2xs", "shadow-xs", "shadow-sm", "shadow", "shadow-md", "shadow-lg", "shadow-xl", "shadow-2xl"}

// genericFamilies are CSS generic font keywords. They are dropped from a
// source stack and re-added after the self-hosted fallbacks, so a generic
// system face never wins over the bundled one.
var genericFamilies = map[string]bool{
	"sans-serif": true, "serif": true, "monospace": true, "system-ui": true, "ui-sans-serif": true,
	"ui-serif": true, "ui-monospace": true, "cursive": true, "fantasy": true,
}

func fontStack(source, kind string) string {
	var names []string
	for _, part := range strings.Split(source, ",") {
		name := strings.TrimSpace(part)
		if name == "" || genericFamilies[strings.Trim(name, `"'`)] {
			continue
		}
		names = append(names, `"`+strings.Trim(name, `"'`)+`"`)
	}
	switch kind {
	case "font-mono":
		names = append(names, `"Geist Mono"`, "ui-monospace", "SFMono-Regular", "Menlo", "Consolas", "monospace")
	case "font-serif":
		names = append(names, "ui-serif", "Georgia", "serif")
	default:
		names = append(names, `"Geist"`, "ui-sans-serif", "system-ui", "-apple-system", `"Segoe UI"`, "sans-serif")
	}
	return strings.Join(names, ", ")
}

// shadows derives the tweakcn shadow scale from its ingredients when an
// export carries only the ingredients (hand-written presets).
func shadows(v map[string]string) map[string]string {
	out := map[string]string{}
	color, opacity := v["shadow-color"], v["shadow-opacity"]
	if color == "" || opacity == "" {
		return out
	}
	o, err := strconv.ParseFloat(opacity, 64)
	if err != nil {
		return out
	}
	x, y := orDefault(v["shadow-offset-x"], "0px"), orDefault(v["shadow-offset-y"], "1px")
	blur, spread := orDefault(v["shadow-blur"], "3px"), orDefault(v["shadow-spread"], "0px")
	ink := func(f float64) string {
		return fmt.Sprintf("color-mix(in srgb, %s %s%%, transparent)", color, strconv.FormatFloat(o*f*100, 'f', -1, 64))
	}
	base := func(f float64) string { return fmt.Sprintf("%s %s %s %s %s", x, y, blur, spread, ink(f)) }
	layer := func(y2, blur2 string) string {
		return fmt.Sprintf("%s, %s %s %s -1px %s", base(1), x, y2, blur2, ink(1))
	}
	out["shadow-2xs"], out["shadow-xs"] = base(0.5), base(0.5)
	out["shadow-sm"], out["shadow"] = layer("1px", "2px"), layer("1px", "2px")
	out["shadow-md"], out["shadow-lg"], out["shadow-xl"] = layer("2px", "4px"), layer("4px", "6px"), layer("8px", "10px")
	out["shadow-2xl"] = base(2.5)
	return out
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func writeRule(b *strings.Builder, selector string, values map[string]string) {
	b.WriteString(selector)
	b.WriteString("{")
	derived := shadows(values)
	for _, key := range emitOrder {
		v := values[key]
		if v == "" && strings.HasPrefix(key, "shadow") {
			v = derived[key]
		}
		if v == "" {
			continue
		}
		switch key {
		case "font-sans", "font-serif", "font-mono":
			v = fontStack(v, key)
		case "letter-spacing":
			key = "tracking-normal"
			if values["tracking-normal"] != "" {
				continue
			}
		}
		b.WriteString("--")
		b.WriteString(key)
		b.WriteString(":")
		b.WriteString(v)
		b.WriteString(";")
	}
	b.WriteString("}\n")
}

// selectors returns the light, dark and system-dark selectors for a preset.
// The default applies to every document; others to their own attribute.
func selectors(id string) (light, dark, systemDark string) {
	if id == DefaultID {
		return ":root", `:root[data-theme="dark"]`, ":root:not([data-theme])"
	}
	sel := `:root[data-visual-preset="` + id + `"]`
	return sel, sel + `[data-theme="dark"]`, sel + ":not([data-theme])"
}

// CSS returns the token rules for one preset in the tokens cascade layer.
// A key the preset sets in only one mode is completed in the other from the
// built-in default (or its source token for primary-text/destructive-text):
// the light rule also matches in dark mode, so a one-sided value would leak.
func (r *Registry) CSS(p *Preset) []byte {
	var b strings.Builder
	light, dark, systemDark := selectors(p.ID)
	lv, dv := p.Values("light"), p.Values("dark")
	if p.ID != DefaultID {
		def := r.Default()
		complete := func(mode string, own, other map[string]string) {
			for k := range other {
				if own[k] != "" {
					continue
				}
				switch k {
				case "primary-text":
					own[k] = "var(--primary)"
				case "destructive-text":
					own[k] = "var(--destructive)"
				default:
					own[k] = def.Values(mode)[k]
				}
			}
		}
		complete("light", lv, dv)
		complete("dark", dv, lv)
	}
	fmt.Fprintf(&b, "/* %s preset (generated from web/themes/%s.json) */\n@layer tokens{\n", p.Name, p.ID)
	writeRule(&b, light, lv)
	writeRule(&b, dark, dv)
	b.WriteString("@media (prefers-color-scheme: dark){")
	writeRule(&b, systemDark, dv)
	b.WriteString("}\n}\n")
	return []byte(b.String())
}

var swatchKeys = []string{"background", "foreground", "card", "border", "primary", "primary-foreground",
	"muted", "muted-foreground", "accent", "sidebar", "chart-1", "chart-2", "chart-3", "radius", "font-sans"}

func writeSwatch(b *strings.Builder, selector string, values map[string]string) {
	b.WriteString(selector)
	b.WriteString("{")
	for _, key := range swatchKeys {
		v := values[key]
		if v == "" {
			continue
		}
		if key == "font-sans" {
			v = fontStack(v, key)
		}
		fmt.Fprintf(b, "--sw-%s:%s;", key, v)
	}
	b.WriteString("}\n")
}

// SwatchCSS returns preview variables for every preset, so the Appearance
// screen can draw a card per preset without inline styles (strict CSP).
func (r *Registry) SwatchCSS() []byte {
	var b strings.Builder
	b.WriteString("/* Preset preview swatches (generated from web/themes) */\n@layer components{\n")
	for _, p := range r.presets {
		sel := `[data-swatch="` + p.ID + `"]`
		writeSwatch(&b, sel, p.Values("light"))
		writeSwatch(&b, `:root[data-theme="dark"] `+sel+`,[data-swatch-mode="dark"]`+sel, p.Values("dark"))
		b.WriteString("@media (prefers-color-scheme: dark){")
		writeSwatch(&b, `:root:not([data-theme]) `+sel, p.Values("dark"))
		b.WriteString("}\n")
	}
	b.WriteString("}\n")
	return []byte(b.String())
}
