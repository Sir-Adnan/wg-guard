# Third-party components

WG-Guard is MIT-licensed. This file lists third-party code and assets that are vendored,
embedded, or executed, with their licenses. SPDX identifiers are confirmed against the actual
pinned version at the time each dependency is introduced (Phase 1+); entries below marked
"planned" are not yet vendored.

## Executed, not distributed

| Component | License | How used |
|---|---|---|
| amneziawg-tools (`awg`, `awg-quick`) | GPL-2.0-only | Installed as a system package (PPA) and executed as a subprocess. Never vendored, linked, or modified. Upstream source: github.com/amnezia-vpn/amneziawg-tools |
| amneziawg-linux-kernel-module | GPL-2.0-only | Installed on the host via DKMS package. Not part of WG-Guard |

Executing a GPL program as a separate process is not covered by the GPL. WG-Guard does not
redistribute AWG binaries inside its own artifacts; the installer installs upstream packages
from the official PPA, whose source availability satisfies the license.

## Planned Go dependencies (SPDX to be confirmed at pin time, Phase 1+)

| Module | License (expected) | Purpose |
|---|---|---|
| modernc.org/sqlite | MIT (bundled SQLite: public domain / SQLite blessing) | Pure-Go SQLite driver |
| golang.org/x/crypto | BSD-3-Clause | argon2id password hashing |
| golang.org/x/time/rate | BSD-3-Clause | Rate limiting |
| filippo.io/age | BSD-3-Clause | Standard age encryption for optional backup protection |
| BurntSushi/toml | MIT | Boot configuration file |
| skip2/go-qrcode | MIT (to confirm) | Server-side QR generation |

## Planned embedded frontend assets (committed prebuilt; no Node.js at runtime)

| Asset | License (expected) | Purpose |
|---|---|---|
| htmx | BSD-2-Clause | Server-driven UI partials |
| Lucide icons (subset) | ISC | SVG icon sprite |
| Vazirmatn font | SIL OFL 1.1 | Persian + Latin typography (self-hosted, subset) |

OFL fonts are embedded as-is with their license text; the OFL does not require bundling of font
source in the repo as long as notices accompany distribution — the license file ships alongside
the font in `web/static/fonts/`.
