// Package wgguard carries the fixed license notices used in runtime artifacts.
// It has no node, configuration or host-operation dependencies.
package wgguard

import "embed"

// Notices is embedded from the same source as the manager, so image builds never
// substitute downloaded or working-directory legal material for that revision.
//
//go:embed LICENSE THIRD_PARTY.md third_party/licenses
var Notices embed.FS
