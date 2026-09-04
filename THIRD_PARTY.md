# Third-party components

WG-Guard is MIT-licensed. This file lists third-party code and assets that are vendored,
embedded, or executed, with their licenses. SPDX identifiers are confirmed against the actual
pinned version when the dependency is introduced.

## Executed, not distributed

| Component | License | How used |
|---|---|---|
| amneziawg-tools (`awg`, `awg-quick`) | GPL-2.0-only | Installed as a system package (PPA) and executed as a subprocess. Never vendored, linked, or modified. Upstream source: github.com/amnezia-vpn/amneziawg-tools |
| amneziawg-linux-kernel-module | GPL-2.0-only | Installed on the host via DKMS package. Not part of WG-Guard |

Executing a GPL program as a separate process is not covered by the GPL. WG-Guard does not
redistribute AWG binaries inside its own artifacts; the installer installs upstream packages
from the official PPA, whose source availability satisfies the license.

## Active Go dependencies (confirmed at pin time)

| Module | Version | License | Purpose |
|---|---|---|---|
| modernc.org/sqlite | v1.57.0 | MIT (bundled SQLite sources: public domain / SQLite blessing — see LICENSE-SQLITE in the module) | Pure-Go SQLite driver (CGO_ENABLED=0) |
| golang.org/x/crypto | v0.55.0 | BSD-3-Clause | argon2id password hashing |
| BurntSushi/toml | v1.6.0 | MIT | Boot configuration file |
| rsc.io/qr | v0.2.0 | BSD-3-Clause | Server-side QR generation for device configs (chosen over skip2/go-qrcode: zero transitive dependencies, maintained encoding; the planned skip2 entry is superseded) |
| github.com/makiuchi-d/gozxing | v0.1.1 | MIT AND Apache-2.0 (upstream ZXing portions; both notices are in the module LICENSE) | Independent QR decoding in `internal/testutil/qrdecode` only; not imported or linked by the production binary |
| golang.org/x/xerrors | v0.0.0-20200804184101-5ec99f83aff1 | BSD-3-Clause | Test-only transitive dependency of gozxing; not linked by the production binary |

## Planned Go dependencies (SPDX to be confirmed at pin time)

| Module | License (expected) | Purpose |
|---|---|---|
| filippo.io/age | BSD-3-Clause | Standard age encryption for optional backup protection (Phase 6) |
| golang.org/x/crypto/acme/autocert | BSD-3-Clause | Built-in ACME (already a transitive pin of x/crypto; imported with the installer, Phase 7 — ADR-0011) |

## Planned embedded frontend assets (committed prebuilt; no Node.js at runtime)

| Asset | License (expected) | Purpose |
|---|---|---|
| htmx | BSD-2-Clause | Server-driven UI partials |
| Lucide icons (subset) | ISC | SVG icon sprite |
| Vazirmatn font | SIL OFL 1.1 | Persian + Latin typography (self-hosted, subset) |

OFL fonts are embedded as-is with their license text; the OFL does not require bundling of font
source in the repo as long as notices accompany distribution — the license file ships alongside
the font in `web/static/fonts/`.
