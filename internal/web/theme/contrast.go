package theme

import "fmt"

// MinContrast is the WCAG AA ratio for normal text; every pair below is
// text on its own surface somewhere in the panel.
const MinContrast = 4.5

// pair is one foreground token read on one background token. derive names
// the source token a project-only token (primary-text, destructive-text)
// starts from when the preset does not set it.
type pair struct{ fg, bg, derive string }

var textPairs = []pair{
	{"card-foreground", "card", ""},
	{"popover-foreground", "popover", ""},
	{"primary-foreground", "primary", ""},
	{"secondary-foreground", "secondary", ""},
	{"accent-foreground", "accent", ""},
	{"muted-foreground", "background", ""},
	{"muted-foreground", "card", ""},
	{"sidebar-foreground", "sidebar", ""},
	{"sidebar-accent-foreground", "sidebar-accent", ""},
	{"sidebar-primary-foreground", "sidebar-primary", ""},
	{"primary-text", "background", "primary"},
	{"primary-text", "card", "primary"},
	{"destructive-text", "background", "destructive"},
	{"destructive-text", "card", "destructive"},
	{"destructive-foreground", "destructive", ""},
}

func lookup(values map[string]string, key, derive string) string {
	if v := values[key]; v != "" {
		return v
	}
	if derive != "" {
		return values[derive]
	}
	if key == "destructive-foreground" {
		return "#ffffff"
	}
	return ""
}

// CheckContrast returns every text pair below MinContrast for one mode, plus
// the base foreground/background pair, which no correction may paper over.
func CheckContrast(values map[string]string) []string {
	var failures []string
	check := func(fgKey, bgKey, derive string) {
		fg, err1 := ParseColor(lookup(values, fgKey, derive))
		bg, err2 := ParseColor(lookup(values, bgKey, ""))
		if err1 != nil || err2 != nil {
			failures = append(failures, fmt.Sprintf("%s on %s: unreadable color", fgKey, bgKey))
			return
		}
		if r := Contrast(fg, bg); r < MinContrast {
			failures = append(failures, fmt.Sprintf("%s on %s: %.2f", fgKey, bgKey, r))
		}
	}
	check("foreground", "background", "")
	for _, p := range textPairs {
		check(p.fg, p.bg, p.derive)
	}
	return failures
}

// FixContrast computes project corrections for one mode so that every pair
// passes, moving a failing foreground toward black or white. Fills keep
// their source color so a preset still looks like itself. The source values
// are never edited: corrections land in the returned map.
func FixContrast(values map[string]string) (map[string]string, error) {
	merged := map[string]string{}
	for k, v := range values {
		merged[k] = v
	}
	fixes := map[string]string{}
	for _, p := range textPairs {
		fgRaw, bgRaw := lookup(merged, p.fg, p.derive), lookup(merged, p.bg, "")
		fg, err1 := ParseColor(fgRaw)
		bg, err2 := ParseColor(bgRaw)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("%s on %s: unreadable color", p.fg, p.bg)
		}
		if Contrast(fg, bg) >= MinContrast {
			continue
		}
		// Either black or white always reaches 4.5:1 on an opaque fill, so a
		// foreground moved far enough toward the better one always passes.
		fixed := towardReadable(fg, bg)
		fixes[p.fg], merged[p.fg] = fixed, fixed
	}
	if left := CheckContrast(merged); len(left) > 0 {
		return nil, fmt.Errorf("unresolved contrast: %v", left)
	}
	return fixes, nil
}

// towardReadable mixes fg toward whichever of black and white reads better
// on bg, keeping as much of its hue as the ratio allows.
func towardReadable(fg, bg RGB) string {
	black, white := RGB{}, RGB{1, 1, 1}
	target := white
	if Contrast(black, bg) > Contrast(white, bg) {
		target = black
	}
	for step := 0.05; step < 1; step += 0.05 {
		if mixed := fg.Mix(target, step); Contrast(mixed, bg) >= MinContrast+0.1 { // margin survives hex rounding
			return mixed.Hex()
		}
	}
	return target.Hex()
}
