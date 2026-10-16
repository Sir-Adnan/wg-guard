package theme

import (
	"strings"
	"testing"
	"testing/fstest"

	webassets "github.com/Sir-Adnan/wg-guard/web"
)

func TestShippedPresetsLoadAndStayReadable(t *testing.T) {
	r, err := Load(webassets.FS, "themes")
	if err != nil {
		t.Fatal(err)
	}
	if r.All()[0].ID != DefaultID || r.Default() == nil {
		t.Fatal("the built-in preset must lead the list")
	}
	if len(r.All()) < 20 {
		t.Fatalf("expected the reviewed preset set, got %d", len(r.All()))
	}
	for _, p := range r.All() {
		for _, mode := range []string{"light", "dark"} {
			if failures := CheckContrast(p.Values(mode)); len(failures) > 0 {
				t.Errorf("%s %s: %v", p.ID, mode, failures)
			}
		}
		css := string(r.CSS(p))
		if p.ID != DefaultID && !strings.Contains(css, `:root[data-visual-preset="`+p.ID+`"]{`) {
			t.Errorf("%s: missing preset selector", p.ID)
		}
		if !strings.Contains(css, "--background:") || !strings.Contains(css, "--shadow-sm:") || !strings.Contains(css, "--font-sans:") {
			t.Errorf("%s: generated tokens are incomplete", p.ID)
		}
	}
	if css := string(r.CSS(r.Default())); !strings.HasPrefix(strings.SplitN(css, "\n", 3)[2], ":root{") {
		t.Fatal("the default preset must apply to every document")
	}
	// A light-only correction must not leak into dark mode.
	for _, p := range r.All() {
		if p.ID == DefaultID || p.Contrast["light"]["primary-text"] == "" || p.Contrast["dark"]["primary-text"] != "" {
			continue
		}
		dark := strings.SplitN(string(r.CSS(p)), "\n", 5)[3]
		if !strings.Contains(dark, "--primary-text:") {
			t.Errorf("%s: dark rule must restate primary-text", p.ID)
		}
	}
	if r.Resolve("retired") != DefaultID || !r.Known("linear") {
		t.Fatal("resolve must fall back to the default")
	}
	if !strings.Contains(string(r.SwatchCSS()), `[data-swatch="zen"]{--sw-background:`) {
		t.Fatal("swatches are missing")
	}
}

func TestParseRejectsUnsafeOrIncompletePresets(t *testing.T) {
	good, err := Import([]byte(sampleCSS), ImportOptions{ID: "sample", Name: "Sample", Group: "tweakcn", Source: "https://tweakcn.com/themes/x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(good); err != nil {
		t.Fatalf("imported preset must parse: %v", err)
	}
	bad := map[string]string{
		"injection": strings.Replace(string(good), `"#ffffff"`, `"red;}body{display:none"`, 1),
		"url":       strings.Replace(string(good), `"#ffffff"`, `"url(https://x)"`, 1),
		"import":    strings.Replace(string(good), `"#ffffff"`, `"@import x"`, 1),
		"unknown":   strings.Replace(string(good), `"background":`, `"bogus": "1", "background":`, 1),
		"ui":        strings.Replace(string(good), `"density": "default"`, `"density": "huge"`, 1),
		"schema":    strings.Replace(string(good), `"schema": 2`, `"schema": 1`, 1),
		"missing":   strings.Replace(string(good), `"ring": "#999999",`, ``, 1),
	}
	for name, data := range bad {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
	fsys := fstest.MapFS{"t/other.json": {Data: good}}
	if _, err := Load(fsys, "t"); err == nil {
		t.Fatal("file name must match id and the default must exist")
	}
}

func TestImportFixesContrastWithoutEditingSource(t *testing.T) {
	css := strings.Replace(sampleCSS, "--primary-foreground: #ffffff;", "--primary-foreground: #fdfdfd;", 1)
	css = strings.Replace(css, "--primary: #171717;", "--primary: #f5d90a;", 1)
	out, err := Import([]byte(css), ImportOptions{ID: "yellow", Name: "Yellow", Group: "wg-guard"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if p.CSSVars.Light["primary-foreground"] != "#fdfdfd" || p.Contrast["light"]["primary-foreground"] == "" {
		t.Fatalf("expected a correction beside the untouched source: %+v", p.Contrast)
	}
	if f := CheckContrast(p.Values("light")); len(f) > 0 {
		t.Fatalf("corrected preset still fails: %v", f)
	}
}

func TestFontStackPutsBundledFacesBeforeGenerics(t *testing.T) {
	if got := fontStack(`'Inter', ui-sans-serif, sans-serif, system-ui`, "font-sans"); !strings.HasPrefix(got, `"Inter", "Geist", ui-sans-serif`) {
		t.Fatalf("sans stack: %s", got)
	}
	if got := fontStack(`monospace`, "font-mono"); !strings.HasPrefix(got, `"Geist Mono", ui-monospace`) {
		t.Fatalf("mono stack: %s", got)
	}
}

func TestParseColorForms(t *testing.T) {
	for in, want := range map[string]string{
		"#fff": "#ffffff", "oklch(1 0 0)": "#ffffff", "oklch(0% 0 0)": "#000000",
		"hsl(0 0% 0% / 0.1)": "#000000", "rgb(255, 0, 0)": "#ff0000", "oklch(0.6280 0.2577 29.2339)": "#ff0000",
	} {
		c, err := ParseColor(in)
		if err != nil || c.Hex() != want {
			t.Errorf("%s: got %s %v want %s", in, c.Hex(), err, want)
		}
	}
}

const sampleCSS = `:root {
  --background: #ffffff; --foreground: #171717; --card: #ffffff; --card-foreground: #171717;
  --popover: #ffffff; --popover-foreground: #171717; --primary: #171717; --primary-foreground: #ffffff;
  --secondary: #f2f2f2; --secondary-foreground: #171717; --muted: #fafafa; --muted-foreground: #666666;
  --accent: #f2f2f2; --accent-foreground: #171717; --destructive: #cb2a2f; --border: #ebebeb;
  --input: #e0e0e0; --ring: #999999; --chart-1: #0070f3; --chart-2: #7928ca; --chart-3: #f5a623;
  --chart-4: #0d9488; --chart-5: #e5186f; --sidebar: #fafafa; --sidebar-foreground: #4d4d4d;
  --sidebar-primary: #171717; --sidebar-primary-foreground: #ffffff; --sidebar-accent: #ebebeb;
  --sidebar-accent-foreground: #171717; --sidebar-border: #ebebeb; --sidebar-ring: #999999;
  --font-sans: Inter, sans-serif; --radius: 0.5rem; --unknown-token: 1px;
}
.dark {
  --background: #0a0a0a; --foreground: #ededed; --card: #0a0a0a; --card-foreground: #ededed;
  --popover: #111111; --popover-foreground: #ededed; --primary: #ededed; --primary-foreground: #0a0a0a;
  --secondary: #1a1a1a; --secondary-foreground: #ededed; --muted: #111111; --muted-foreground: #a1a1a1;
  --accent: #1f1f1f; --accent-foreground: #ededed; --destructive: #d93036; --border: #242424;
  --input: #2e2e2e; --ring: #707070; --chart-1: #3291ff; --chart-2: #a26ee8; --chart-3: #f5a623;
  --chart-4: #2dd4bf; --chart-5: #ff4d8d; --sidebar: #0a0a0a; --sidebar-foreground: #a1a1a1;
  --sidebar-primary: #ededed; --sidebar-primary-foreground: #0a0a0a; --sidebar-accent: #1f1f1f;
  --sidebar-accent-foreground: #ededed; --sidebar-border: #1f1f1f; --sidebar-ring: #707070;
  --font-sans: Inter, sans-serif; --radius: 0.5rem;
}`
