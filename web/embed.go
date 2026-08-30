// Package webassets embeds the panel's prebuilt frontend assets and HTML
// templates. The Go sources live in internal/web; this package exists only
// because go:embed cannot reference parent directories. Assets are
// committed and embedded — no Node runtime, no build step at deploy time
// (docs/product/ui-ux.md).
package webassets

import "embed"

//go:embed templates static
var FS embed.FS
