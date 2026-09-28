# Third-party components

WG-Guard itself is MIT-licensed. The release bundle includes this inventory, the matching
license texts in `third_party/licenses/`, and an SPDX 2.3 inventory of the Go modules linked into the
Linux/amd64 binary. The standalone binary does **not** include AmneziaWG executables; the
Docker image builds and carries the pinned tools and userspace daemon as separate programs.

| Component | License at the reviewed source | Distribution |
|---|---|---|
| [amneziawg-tools](https://github.com/amnezia-vpn/amneziawg-tools/tree/ee0f0a9aa34ff0a0da4b3433b9512781cfe02843) | GPL-2.0; [COPYING](third_party/licenses/AmneziaWG-tools-COPYING) | Built from this exact commit for managed host tools and the runtime image; invoked as a subprocess |
| [amneziawg-go](https://github.com/amnezia-vpn/amneziawg-go/tree/b5928efb6ca19f0153958460c3d141f04abc5c2e) | MIT; [LICENSE](third_party/licenses/AmneziaWG-go-LICENSE) | Exact-source userspace daemon in the runtime image and managed native setup |
| [amneziawg-linux-kernel-module](https://github.com/amnezia-vpn/amneziawg-linux-kernel-module) | GPL-2.0 | Exact-source host DKMS build; not embedded in the WG-Guard binary or image |
| [htmx 2.0.4](https://github.com/bigskysoftware/htmx/tree/v2.0.4) | 0BSD; [LICENSE](third_party/licenses/htmx-2.0.4-LICENSE) | Embedded prebuilt `web/static/js/htmx.min.js` |
| [Lucide](https://github.com/lucide-icons/lucide) subset | ISC; some Feather-derived icons retain MIT notices in the [upstream license](third_party/licenses/Lucide-LICENSE) | Embedded `web/static/img/icons.svg` sprite |
| [Vazirmatn](https://github.com/rastikerdar/vazirmatn) | SIL OFL 1.1; [license](third_party/licenses/Vazirmatn-OFL.txt) | Embedded Regular/SemiBold WOFF2 files |

The Go production binary links direct and transitive modules. `sbom.spdx.json` records exact
linked module versions for each release build; the bundle includes the license/notice files
from those pinned module sources under `licenses/go/`. Direct dependencies include `modernc.org/sqlite` (pure-Go
SQLite), `filippo.io/age` (encrypted backups), `golang.org/x/crypto` (Argon2id and ACME),
`golang.org/x/sys` and `x/term` (host and terminal integration), `github.com/BurntSushi/toml`
(configuration), and `rsc.io/qr` (QR encoding). `github.com/makiuchi-d/gozxing` is used only by
tests and is absent from the release binary.

The Docker runtime also contains Ubuntu packages. Their package notices remain under
`/usr/share/doc` in the image. The image includes WG-Guard and pinned AmneziaWG notices under
`/usr/share/doc/wg-guard`. Upstream source URLs and exact revisions above identify the
corresponding externally built components; no installer/runtime claim relies on a mutable PPA.
