package theme

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ImportOptions describes the preset written from a tweakcn export.
type ImportOptions struct {
	ID, Name, Group, Source string
	UI                      UI
}

// Import converts a tweakcn export into a reviewed preset file. It accepts
// the registry JSON (https://tweakcn.com/r/themes/<id>) or the CSS shown by
// the theme's Code button (":root { … } .dark { … }"). Keys WG-Guard does
// not consume are dropped, and contrast corrections are computed so the
// result passes CheckContrast in both modes.
func Import(data []byte, opt ImportOptions) ([]byte, error) {
	var vars struct {
		Theme, Light, Dark map[string]string
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var reg struct {
			Name    string `json:"name"`
			CSSVars struct {
				Theme map[string]string `json:"theme"`
				Light map[string]string `json:"light"`
				Dark  map[string]string `json:"dark"`
			} `json:"cssVars"`
		}
		if err := json.Unmarshal(trimmed, &reg); err != nil {
			return nil, fmt.Errorf("registry json: %w", err)
		}
		vars.Theme, vars.Light, vars.Dark = reg.CSSVars.Theme, reg.CSSVars.Light, reg.CSSVars.Dark
		if opt.Name == "" {
			opt.Name = reg.Name
		}
	} else {
		var err error
		if vars.Light, err = cssBlock(string(data), `:root`); err != nil {
			return nil, err
		}
		if vars.Dark, err = cssBlock(string(data), `.dark`); err != nil {
			return nil, err
		}
	}
	keep := map[string]bool{}
	for _, k := range append(append([]string{}, ColorKeys...), OptionalKeys...) {
		keep[k] = true
	}
	filter := func(in map[string]string, shared bool) map[string]string {
		out := map[string]string{}
		for k, v := range in {
			v = strings.TrimSpace(v)
			if !keep[k] || !SafeValue(v) {
				continue
			}
			if shared && contains(ColorKeys, k) {
				continue
			}
			out[k] = v
		}
		return out
	}
	p := Preset{Schema: 2, ID: opt.ID, Name: opt.Name, Group: opt.Group, Source: opt.Source, UI: opt.UI}
	p.CSSVars.Theme, p.CSSVars.Light, p.CSSVars.Dark = filter(vars.Theme, true), filter(vars.Light, false), filter(vars.Dark, false)
	// Values repeated identically in both modes belong to the shared section.
	for k, v := range p.CSSVars.Light {
		if contains(ColorKeys, k) || p.CSSVars.Dark[k] != v {
			continue
		}
		if cur, ok := p.CSSVars.Theme[k]; ok && cur != v {
			continue
		}
		p.CSSVars.Theme[k] = v
		delete(p.CSSVars.Light, k)
		delete(p.CSSVars.Dark, k)
	}
	if err := p.UI.validate(); err != nil {
		return nil, err
	}
	p.Contrast = map[string]map[string]string{}
	for _, mode := range []string{"light", "dark"} {
		fixes, err := FixContrast(p.Values(mode))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", mode, err)
		}
		if len(fixes) > 0 {
			p.Contrast[mode] = fixes
		}
	}
	if len(p.Contrast) == 0 {
		p.Contrast = nil
	}
	out, err := marshalPreset(&p)
	if err != nil {
		return nil, err
	}
	if _, err := Parse(out); err != nil {
		return nil, fmt.Errorf("imported preset does not validate: %w", err)
	}
	return out, nil
}

var declRE = regexp.MustCompile(`--([a-z0-9-]+)\s*:\s*([^;]+);`)

func cssBlock(css, selector string) (map[string]string, error) {
	i := strings.Index(css, selector+" {")
	if i < 0 {
		i = strings.Index(css, selector+"{")
	}
	if i < 0 {
		return nil, fmt.Errorf("css export: %s block not found", selector)
	}
	body := css[i:]
	j := strings.Index(body, "}")
	if j < 0 {
		return nil, fmt.Errorf("css export: %s block not closed", selector)
	}
	out := map[string]string{}
	for _, m := range declRE.FindAllStringSubmatch(body[:j], -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// marshalPreset writes a preset with stable key order so diffs stay small.
func marshalPreset(p *Preset) ([]byte, error) {
	var b bytes.Buffer
	enc := func(v any) string { out, _ := json.Marshal(v); return string(out) }
	section := func(m map[string]string, indent string) string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		order := map[string]int{}
		for i, k := range emitOrder {
			order[k] = i
		}
		sort.Slice(keys, func(i, j int) bool { return order[keys[i]] < order[keys[j]] })
		var s strings.Builder
		s.WriteString("{")
		for i, k := range keys {
			if i > 0 {
				s.WriteString(",")
			}
			s.WriteString("\n" + indent + "  " + enc(k) + ": " + enc(m[k]))
		}
		if len(keys) > 0 {
			s.WriteString("\n" + indent)
		}
		s.WriteString("}")
		return s.String()
	}
	fmt.Fprintf(&b, "{\n  \"schema\": 2,\n  \"id\": %s,\n  \"name\": %s,\n  \"group\": %s,\n", enc(p.ID), enc(p.Name), enc(p.Group))
	if p.Source != "" {
		fmt.Fprintf(&b, "  \"source\": %s,\n", enc(p.Source))
	}
	fmt.Fprintf(&b, "  \"ui\": {\"density\": %s, \"sidebar\": %s, \"tabs\": %s, \"surface\": %s},\n",
		enc(p.UI.Density), enc(p.UI.Sidebar), enc(p.UI.Tabs), enc(p.UI.Surface))
	fmt.Fprintf(&b, "  \"cssVars\": {\n    \"theme\": %s,\n    \"light\": %s,\n    \"dark\": %s\n  }",
		section(p.CSSVars.Theme, "    "), section(p.CSSVars.Light, "    "), section(p.CSSVars.Dark, "    "))
	if len(p.Contrast) > 0 {
		b.WriteString(",\n  \"contrast\": {")
		first := true
		for _, mode := range []string{"light", "dark"} {
			if p.Contrast[mode] == nil {
				continue
			}
			if !first {
				b.WriteString(",")
			}
			first = false
			fmt.Fprintf(&b, "\n    %s: %s", enc(mode), section(p.Contrast[mode], "    "))
		}
		b.WriteString("\n  }")
	}
	b.WriteString("\n}\n")
	return b.Bytes(), nil
}
