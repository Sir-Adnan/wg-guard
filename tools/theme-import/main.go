// Command theme-import converts a tweakcn export into a WG-Guard preset
// file. It is a development helper; the panel never fetches themes.
//
//	go run ./tools/theme-import -id zen -source https://tweakcn.com/themes/<id> \
//	    -ui default,inset,underline,raised < export.json > web/themes/zen.json
//
// Input is the registry JSON (https://tweakcn.com/r/themes/<id>) or the CSS
// from the theme's Code button. See docs/product/design-system.md.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/web/theme"
)

func main() {
	id := flag.String("id", "", "preset id (lowercase slug, also the file name)")
	name := flag.String("name", "", "display name (defaults to the export's name)")
	group := flag.String("group", "tweakcn", "group: wg-guard, shadcn or tweakcn")
	source := flag.String("source", "", "source page URL (required for tweakcn)")
	ui := flag.String("ui", "default,inset,underline,raised", "density,sidebar,tabs,surface")
	flag.Parse()
	axes := strings.Split(*ui, ",")
	if len(axes) != 4 {
		fail("-ui needs four comma-separated values")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fail(err.Error())
	}
	out, err := theme.Import(data, theme.ImportOptions{
		ID: *id, Name: *name, Group: *group, Source: *source,
		UI: theme.UI{Density: axes[0], Sidebar: axes[1], Tabs: axes[2], Surface: axes[3]},
	})
	if err != nil {
		fail(err.Error())
	}
	os.Stdout.Write(out)
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "theme-import:", msg)
	os.Exit(1)
}
