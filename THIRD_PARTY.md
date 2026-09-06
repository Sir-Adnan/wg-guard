# Third-party components

WG-Guard is MIT-licensed. This file lists third-party code and assets that are vendored,
embedded, or executed, with their licenses. SPDX identifiers are confirmed against the actual
pinned version when the dependency is introduced.

## External VPN components

| Component | License | How used |
|---|---|---|
| amneziawg-tools (`awg`, `awg-quick`) | GPL-2.0-only | Installed from the pinned upstream PPA package on the host and inside locally built runtime images; executed as a subprocess, not linked into the Go binary. Upstream source: github.com/amnezia-vpn/amneziawg-tools |
| amneziawg-linux-kernel-module | GPL-2.0-only | Installed on the host via DKMS package. Not part of WG-Guard |

The standalone Go candidate does not bundle AWG executables. Phase 8.1 locally built Docker
runtime images do contain the pinned tools package. No public image/release is published by
this work. License notices, corresponding-source obligations and the complete distribution
inventory must be reviewed for the actual release artifacts in Phase 12; this table is an
implementation inventory, not a legal-compliance determination.

## Active Go dependencies (confirmed at pin time)

| Module | Version | License | Purpose |
|---|---|---|---|
| modernc.org/sqlite | v1.57.0 | MIT (bundled SQLite sources: public domain / SQLite blessing — see LICENSE-SQLITE in the module) | Pure-Go SQLite driver (CGO_ENABLED=0) |
| golang.org/x/crypto | v0.55.0 | BSD-3-Clause | argon2id password hashing, age primitives and built-in ACME/autocert |
| filippo.io/age | v1.2.1 | BSD-3-Clause | Optional password-encrypted backup archives |
| golang.org/x/term | v0.45.0 | BSD-3-Clause | Terminal detection, width and hidden secret input |
| golang.org/x/sys | v0.47.0 | BSD-3-Clause | Linux lifecycle locks, bounded terminal input and host inspection |
| BurntSushi/toml | v1.6.0 | MIT | Boot configuration file |
| rsc.io/qr | v0.2.0 | BSD-3-Clause | Server-side QR generation for device configs (chosen over skip2/go-qrcode: zero transitive dependencies, maintained encoding; the planned skip2 entry is superseded) |
| github.com/makiuchi-d/gozxing | v0.1.1 | MIT AND Apache-2.0 (upstream ZXing portions; both notices are in the module LICENSE) | Independent QR decoding in `internal/testutil/qrdecode` only; not imported or linked by the production binary |
| golang.org/x/xerrors | v0.0.0-20200804184101-5ec99f83aff1 | BSD-3-Clause | Test-only transitive dependency of gozxing; not linked by the production binary |

The age, x/crypto, x/term and x/sys entries were checked against `go.mod`, actual imports and
their pinned module `LICENSE` files on 2026-09-06. ACME/autocert is part of x/crypto, not a
separate module. The full transitive/frontend notice inventory remains a Phase 12 release gate.

## Planned embedded frontend assets (committed prebuilt; no Node.js at runtime)

| Asset | License (expected) | Purpose |
|---|---|---|
| htmx | BSD-2-Clause | Server-driven UI partials |
| Lucide icons (subset) | ISC | SVG icon sprite |
| Vazirmatn font | SIL OFL 1.1 | Persian + Latin typography (self-hosted, subset) |

OFL fonts are embedded as-is with their license text; the OFL does not require bundling of font
source in the repo as long as notices accompany distribution — the license file ships alongside
the font in `web/static/fonts/`.
