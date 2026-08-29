# AmneziaWG integration (pinned upstream)

WG-Guard's tunnel integration is executed against **pinned upstream versions**. Nothing in this
document is assumed from memory; every runtime claim is backed by a check recorded in the
[verification log](#verification-log) with a reproduction script under
[`fixtures/`](fixtures/). If a behavior is not listed here as verified, WG-Guard code must treat
it as unverified and gate accordingly.

## Pinned versions (Phase 0, 2026-08-29)

| Component | Pinned version | Source |
|---|---|---|
| amneziawg-tools (`awg`, `awg-quick`) | **v3.1.20260812** (commit `ee0f0a9`, tag `v3.1.20260812`; Debian package `1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1`) | PPA `ppa:amnezia/ppa` |
| amneziawg-dkms (kernel module source) | **1.0.0** (`0~202608282205+3c38e16~ubuntu24.04.1`) | PPA `ppa:amnezia/ppa` |
| amneziawg-go (userspace daemon, fallback backend) | **v3.1.20260828** (tag; binary `amneziawg-go`) | built from github.com/amnezia-vpn/amneziawg-go |
| Verification environment | WSL2 Ubuntu **26.04 LTS**, kernel `6.18.33.1-microsoft-standard-WSL2` | local |

Package note: the PPA builds for `noble` (24.04). On Ubuntu 26.04 (`resolute`) the PPA has no
Release file — workaround used here (and by the installer if it ever meets this case): add the
PPA and pin its suite to `noble`. On the supported matrix (22.04/24.04/Debian 12) the PPA works
natively (24.04 verified here; 22.04/Debian 12 verification belongs to the Phase 8 VPS matrix).

Install: `apt install amneziawg-tools amneziawg-dkms` (avoid the `amneziawg` meta-package;
it is a dependency-only package, and the tools+dkms pair is what we need). DKMS builds require
`build-essential` and kernel headers.

## CLI surface (verified, tools v3.1)

- `awg` subcommands: `show`, `showconf`, `set`, `setconf`, `addconf`, `syncconf`, `genkey`,
  `genpsk`, `pubkey` — plus per-parameter show selectors beyond stock wg:
  `jc, jmin, jmax, s1, s2, s3, s4, h1..h4, i1..i5, header-protection-key,
  content-padding-addition, rekey-after-time, rekey-timeout, reject-after-time,
  keepalive-timeout, max_handshake_attempts, random-trailers, disable-cookies`.
- `awg-quick` config dir: `/etc/amnezia/amneziawg/<iface>.conf`. WG-Guard does **not** use
  awg-quick for interface bring-up (it mutates global firewall state); it manages links and
  addresses itself and applies config via `awg setconf`/`awg syncconf`.
- Backends are transparent to the CLI: kernel module via netlink, userspace daemon via UAPI
  unix socket at **`/var/run/amneziawg/<iface>.sock`** (verified in daemon source
  `ipc/uapi_unix.go`; socket exists while the daemon runs, removed on exit). Kernel-module
  UAPI does not use that socket (generic netlink family), so doctor must distinguish
  "interface exists via kernel" from "socket present via userspace".

## `awg show <iface> dump` format — tools v3.1 (CRITICAL for parsing)

The AWG v3.1 dump format **extends stock WireGuard**. Stock wg dump has a 4-field interface
line; AWG v3.1 has **29**. WG-Guard's parser must field-count to detect the format and reject
unknown formats loudly.

Interface line, tab-separated, in order (source: `amneziawg-tools/src/show.c`, `dump_print`):

| # | Field | Notes |
|---|---|---|
| 1 | private_key | base64 |
| 2 | public_key | base64 |
| 3 | listen_port | uint |
| 4 | Jc | uint |
| 5 | Jmin | uint |
| 6 | Jmax | uint |
| 7 | S1 | uint |
| 8 | S2 | uint |
| 9 | S3 | uint (2.0 gen) |
| 10 | S4 | uint (2.0 gen) |
| 11–14 | H1–H4 | u32-range string — plain `N` verified; tolerate `N-M` |
| 15–19 | I1–I5 | hex blob or literal `(null)` |
| 20 | header_protection_key | base64 or `(none)` |
| 21–26 | content_padding_addition, rekey_after_time, rekey_timeout, reject_after_time, keepalive_timeout, max_handshake_attempts | u16-range strings (`0` when unset) |
| 27 | random_trailers | `on`/`off` |
| 28 | disable_cookies | `on`/`off` |
| 29 | fwmark | `0x…` or `off` |

Peer line, tab-separated (8 fields, superset-compatible with stock wg):

| # | Field | Notes |
|---|---|---|
| 1 | public_key | base64 |
| 2 | preshared_key | base64 or `(none)` |
| 3 | endpoint | `ip:port` or `(none)` |
| 4 | allowed_ips | comma-joined `ip/cidr`, or `(none)` |
| 5 | latest_handshake | unix seconds, `0` = never |
| 6 | rx_bytes | uint |
| 7 | tx_bytes | uint |
| 8 | persistent_keepalive | u16-range string or `off` |

Live fixture: [`fixtures/dump-awg0-userspace.txt`](fixtures/dump-awg0-userspace.txt);
`showconf`: [`fixtures/showconf-awg0-userspace.txt`](fixtures/showconf-awg0-userspace.txt).
`awg show <iface>` with no interfaces exits 0 with empty output (do not treat as an error).

## Configuration keys (parser-level, tools v3.1 `src/config.c`)

Full accepted list (see [`fixtures/configc-keys.txt`](fixtures/configc-keys.txt)):
`PrivateKey, PublicKey, PresharedKey, ListenPort, FwMark, PersistentKeepalive (alias
KeepaliveTimeout), AllowedIPs, Endpoint, Jc, Jmin, Jmax, S1, S2, S3, S4, H1–H4, I1–I5,
HeaderProtectionKey, AdvancedSecurity, ContentPaddingAddition, DisableCookies, RandomTrailers,
RejectAfterTime, RekeyAfterTime, RekeyTimeout, MaxHandshakeAttempts`.

The parser accepts the 2.0/3.x-generation keys (S3/S4, header protection, timers, flags) —
**parser acceptance ≠ runtime support**. Legacy 1.0 params are the only set verified end-to-end
this phase; 2.0/3.x remain capability-gated and off by default.

## Constraint enforcement lives at runtime, not in the parser

Verified: the tools parser accepts `Jmin > Jmax`, duplicate `H1 = H2`, and `S1 + 56 == S2` —
the userspace **daemon rejects them at `setconf` time** (`Unable to modify interface: Invalid
argument` for duplicate H). WG-Guard must therefore validate the full kernel-README constraint
set itself before ever invoking `awg`: `Jc` 1–128 (recommended 4–12), `Jmin < Jmax ≤ 1280`,
`S1 ≤ 1132`, `S2 ≤ 1188`, `S1 + 56 ≠ S2`, `H1–H4` pairwise distinct. Client↔server parity rule:
all params must match except `Jc/Jmin/Jmax` and `I1–I5` (client-side).

## Interface naming

AWG interface names follow the same 15-char kernel limit as WireGuard (an `awg-…` filename over
15 chars is rejected by awg-quick). WG-Guard uses `awg0…awg7`.

## Verification log

| Check | Method | Result | Status |
|---|---|---|---|
| Tools version on PPA | `awg --version` in WSL2 | `amneziawg-tools v3.1.20260812` | ✅ verified (WSL2) |
| Legacy 1.0 params accepted by parser | `awg-quick strip` fixture | accepted, canonicalized | ✅ verified (WSL2) |
| I1–I5 accepted by parser | `awg-quick strip` fixture | accepted | ✅ verified (WSL2) |
| 2.0/3.x keys present in parser | `config.c` key list | S3/S4/HPK/AdvancedSecurity/… accepted | ✅ verified (source) |
| Dump format + field order | userspace daemon + `dump_print` source | 29-field interface line, 8-field peer line | ✅ verified (WSL2 + source) |
| UAPI socket path | daemon source + runtime | `/var/run/amneziawg/<iface>.sock` | ✅ verified |
| Runtime constraint rejection | `awg setconf` with duplicate H | rejected (`Invalid argument`) | ✅ verified (userspace) |
| genkey/genpsk/pubkey | direct invocation | work as wireguard-tools | ✅ verified |
| Userspace daemon on WSL2 | built + ran `amneziawg-go awg0` | works (TUN, setconf, dump) | ✅ verified |
| DKMS module build | `apt install amneziawg-dkms` | module compiled (for host kernel series) | ✅ verified (build only) |
| **Kernel-module load + netlink dump** | requires real KVM VPS | — | ⚠️ **requires real VPS (Phase 2/8)** |
| **Dump format emitted against kernel module** | requires real KVM VPS (fields may default-fill) | — | ⚠️ **requires real VPS** |
| 2.0/3.x generation runtime behavior | requires real VPS + compatible clients | — | ⚠️ deferred (capability-gated) |
| PPA on Ubuntu 22.04 / Debian 12 | requires real VPS matrix | — | ⚠️ Phase 8 |

Reproduction: [`fixtures/verify-wsl2.sh`](fixtures/verify-wsl2.sh) and
[`fixtures/verify-wsl2-runtime.sh`](fixtures/verify-wsl2-runtime.sh).

## Known upstream issues respected by design

- arm64 userspace H4 corruption (amneziawg-go #110) — userspace fallback is not the default;
  arm64 verification is part of the Phase 8 matrix.
- iOS rejects I1–I5 configs at startup (#115) — I1–I5 is opt-in per profile with client warnings.
- `RandomTrailers` panic history (#178) — default `off` (verified default in `showconf` fixture).
- IPv6-disabled hosts resetting the listen port (#148) — WG-Guard always sets an explicit
  listen port and reconciles it against the DB.
