// Package migrations embeds the numbered SQL migration files so the single
// binary is self-contained (docs/architecture/database.md).
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed *.sql
var FS embed.FS

// Read returns one embedded migration by name (migration tests).
func Read(name string) ([]byte, error) {
	return fs.ReadFile(FS, name)
}
