package web

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"sort"

	"github.com/Sir-Adnan/wg-guard/internal/web/theme"
	webassets "github.com/Sir-Adnan/wg-guard/web"
)

// loadThemes reads the reviewed preset files. Presets are local data; no
// remote CSS or executable theme code is ever loaded by a running node.
func loadThemes() (*theme.Registry, error) {
	return theme.Load(webassets.FS, "themes")
}

// buildStyles concatenates web/styles in path order into the single
// app.css bundle, with the built-in preset's tokens right after the layer
// declaration. Modular sources, one cached request, no build tool.
func buildStyles(themes *theme.Registry) ([]byte, error) {
	var files []string
	err := fs.WalkDir(webassets.FS, "styles", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && path.Ext(p) == ".css" {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("web: styles: %w", err)
	}
	sort.Strings(files)
	if len(files) == 0 || path.Base(files[0]) != "00-layers.css" {
		return nil, fmt.Errorf("web: styles must start with 00-layers.css")
	}
	var b bytes.Buffer
	for i, f := range files {
		data, err := fs.ReadFile(webassets.FS, f)
		if err != nil {
			return nil, fmt.Errorf("web: read %s: %w", f, err)
		}
		fmt.Fprintf(&b, "/* --- %s --- */\n", f)
		b.Write(data)
		b.WriteByte('\n')
		if i == 0 {
			b.Write(themes.CSS(themes.Default()))
		}
	}
	return b.Bytes(), nil
}

// themeAssetName is the per-preset stylesheet path, loaded only when that
// preset is active (the default ships inside app.css).
func themeAssetName(id string) string { return "/css/themes/" + id + ".css" }

// presetChoice is one entry of a preset picker.
type presetChoice struct {
	ID, Name, Group string
	UI              theme.UI
}

func (s *Server) presetChoices() []presetChoice {
	all := s.themes.All()
	out := make([]presetChoice, 0, len(all))
	for _, p := range all {
		out = append(out, presetChoice{ID: p.ID, Name: p.Name, Group: p.Group, UI: p.UI})
	}
	return out
}

func (s *Server) presetName(id string) string {
	if p := s.themes.Get(s.themes.Resolve(id)); p != nil {
		return p.Name
	}
	return ""
}
