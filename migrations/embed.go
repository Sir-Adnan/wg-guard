// Package migrations embeds the numbered SQL migration files so the single
// binary is self-contained (docs/architecture/database.md).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
