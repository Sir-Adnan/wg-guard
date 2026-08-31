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
| Verification environment (userspace) | WSL2 Ubuntu **26.04 LTS**, kernel `6.18.33.1-microsoft-standard-WSL2` | local |
| Verification environment (kernel) | dedicated VPS, Ubuntu **24.04 LTS** (noble), KVM, kernel `6.8.0-137-generic`, x86_64; PPA packages natively | 2026-08-31 |

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
this phase; 2.0/3.x remain capability-gated and off by default. Kernel-module runtime
acceptance of the 2.0/3.x set is now verified (see the kernel matrix below); client-app
compatibility still varies, which is why they stay off by default.

Value formats verified from the pinned `src/config.c` (2026-08-31):

| Key | Parser | Value format |
|---|---|---|
| S3, S4 | `parse_uint16` | plain integer 0–65535 |
| H1–H4 | `u32_range_from_string` | `N` or `N-M` (u32 range; WG-Guard writes plain `N`) |
| HeaderProtectionKey | `parse_key` | base64-encoded 32-byte key (44 chars) |
| ContentPaddingAddition, RekeyAfterTime, RekeyTimeout, RejectAfterTime, KeepaliveTimeout, MaxHandshakeAttempts | `u16_range_from_string` | `N` or `N-M` (u16 bounds) |
| RandomTrailers, DisableCookies | `parse_bool` | `on` / `off` (case-insensitive) |
| AdvancedSecurity | `parse_bool` on `ctx->last_peer` | **peer-section key** — it is rejected in `[Interface]`; WG-Guard defers it (per-device plumbing needed) |

## Constraint enforcement lives at runtime, not in the parser

Verified: the tools parser accepts `Jmin > Jmax`, duplicate `H1 = H2`, and `S1 + 56 == S2` —
the userspace **daemon rejects them at `setconf` time** (`Unable to modify interface: Invalid
argument` for duplicate H). WG-Guard must therefore validate the full kernel-README constraint
set itself before ever invoking `awg`: `Jc` 1–128 (recommended 4–12), `Jmin < Jmax ≤ 1280`,
`S1 ≤ 1132`, `S2 ≤ 1188`, `S1 + 56 ≠ S2`, `H1–H4` pairwise distinct. Client↔server parity rule:
all params must match except `Jc/Jmin/Jmax` and `I1–I5` (client-side).

## setconf semantics for obfuscation params (verified Phase 2, WSL2)

Two facts verified against the pinned userspace daemon (2026-08-29, exercised by the
`internal/tunnel/amneziawg` integration tests) that the parser/kernel-README alone do not
reveal:

0. **`awg setconf` requires explicit section headers (tools v3.1, verified on kernel VPS).**
   A headerless config (interface lines before any `[Interface]` header — tolerated by stock
   wg) is rejected with `Line unrecognized: `PrivateKey=…``. `awg-quick strip` is shell/awk and
   still accepts it — not a counterexample. WG-Guard's renderer always emits explicit
   headers.

1. **Explicit zeros are rejected.** `setconf` with an all-zero obfuscation block
   (`Jc = 0 … H4 = 0`) fails with `Unable to modify interface: Invalid argument` — the runtime
   enforces the constraint set on the *written* values, so the all-plain state is not directly
   settable.
2. **Omitted keys persist.** `setconf` without an obfuscation block leaves previously-set
   params untouched (setconf replaces peers and written fields; obfuscation params persist).
   WG-Guard's verify-after-apply catches the resulting state mismatch.

Consequence (implemented): a profile cannot move between plain and obfuscated states in place.
The all-plain state exists only at link creation, so obfuscation-mode transitions recreate the
link (remove + create + peer re-sync) — owned by the reconcile engine. Same-mode value changes
apply cleanly via `setconf`. Kernel-backend parity of these two facts remains a Phase 8 matrix
item.

## Kernel-module runtime matrix (real VPS, 2026-08-31)

Full evidence: [`fixtures/verify-vps-kernel-matrix.txt`](fixtures/verify-vps-kernel-matrix.txt).
Environment: dedicated Ubuntu 24.04 KVM VPS, kernel 6.8.0-137, `amneziawg` module 3.1.20260812
from the PPA (module name is **amneziawg** — `modprobe amneziawg`; links are
`ip link add … type amneziawg`). Every claim below was executed against that module.

- **Accepted + round-tripped through the kernel**: S3/S4; **H1–H4 as u32 ranges** (dump echoes
  `14600319-413859944` verbatim); all six timer/padding `N`/`N-M` params; RandomTrailers /
  DisableCookies; I1–I5 both template literals (`<r 105>`) and hex blobs; peer
  PersistentKeepalive **ranges** (`25-35`); peer-section `AdvancedSecurity = on`.
- **HeaderProtectionKey is kernel-coupled to S3/S4**: writing HPK fails with
  `Invalid argument` unless S3 AND S4 are non-zero **in the same setconf message** — even when
  S3/S4 already persist from a previous setconf. HPK rotation works (new key + S3/S4 in one
  message); HPK cannot be cleared (`(none)` is parser-rejected, omission persists).
- **Clearing semantics**: `S3 = 0`/`S4 = 0` and `H1..H4 = 0` are rejected while HPK is set;
  `Jc = 0` alone is accepted (junk disabled). Omitted keys persist (re-verified on kernel).
  A fresh interface dumps `H1..H4 = 1,2,3,4` (stock header values), everything else
  0 / off / `(null)` / `(none)`.
- **Constraint enforcement differs by backend**: duplicate `H1=H2` and the all-zero block are
  rejected on both backends; `Jmin > Jmax` and `S1 + 56 == S2` are **accepted by the kernel
  module** (userspace daemon rejects them). WG-Guard validates the full set locally regardless.

WG-Guard implementation consequences: `render.go` always emits S3/S4 together with
HeaderProtectionKey; interface validation rejects HPK without S3/S4; the 2.0/3.x set stays
capability-gated off-by-default with report-only drift (kernel acceptance ≠ client support);
clearing HPK/S3/S4 on a live interface is not appliable via setconf (recreate required —
documented in the UI).

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
| setconf explicit-zero obfuscation block | `awg setconf` with `Jc=0…H4=0` | rejected (`Invalid argument`) | ✅ verified (userspace, Phase 2 integration test) |
| setconf omitted obfuscation keys persist | apply obf config, then plain config without block | old params kept (verify detects) | ✅ verified (userspace, Phase 2 integration test) |
| setconf/syncconf/dump round-trip via WG-Guard backend | `go test -tags integration ./internal/tunnel/amneziawg` (WSL2, root) | pass | ✅ verified (userspace) |
| genkey/genpsk/pubkey | direct invocation | work as wireguard-tools | ✅ verified |
| Userspace daemon on WSL2 | built + ran `amneziawg-go awg0` | works (TUN, setconf, dump) | ✅ verified |
| DKMS module build | `apt install amneziawg-dkms` | module compiled (for host kernel series) | ✅ verified (build only) |
| **Kernel-module load + netlink dump** | VPS: `modprobe amneziawg` + `ip link add … type amneziawg` + `awg show dump` | module loads (name `amneziawg`), 29-field dump | ✅ **verified (VPS kernel, 2026-08-31)** |
| **Dump format emitted against kernel module** | VPS: full-combo setconf + dump | same 29-field format; H1–H4 echo ranges verbatim; fresh iface defaults H=1..4 | ✅ **verified (VPS kernel)** |
| **2.0/3.x generation runtime behavior (kernel)** | VPS acceptance/round-trip matrix | S3/S4, H ranges, timers, flags, I1–I5, HPK (⇒S3/S4 same-message), peer keepalive ranges, peer AdvancedSecurity — all accepted | ✅ **verified (VPS kernel)**; client compatibility still varies — params stay gated |
| **setconf headerless config on kernel** | VPS: conf without `[Interface]` | rejected (`Line unrecognized`) — explicit headers required | ✅ **verified (VPS kernel)** |
| **Kernel constraint enforcement** | VPS: dup-H rejected; Jmin>Jmax / S1+56==S2 accepted | differs from userspace; WG-Guard validates locally | ✅ **verified (VPS kernel)** |
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
