# AmneziaWG integration (pinned upstream)

WG-Guard's tunnel integration is executed against **pinned upstream versions**. Nothing in this
document is assumed from memory; every runtime claim is backed by a check recorded in the
[verification log](#verification-log) with a reproduction script under
[`fixtures/`](fixtures/). The Phase 8 source audit, exact revisions, and reproduction paths are
captured in [`fixtures/phase8-upstream-contract.txt`](fixtures/phase8-upstream-contract.txt). If a
behavior is not listed here as verified, WG-Guard code must treat it as unverified and gate it.

## Pinned versions (re-audited in Phase 8, 2026-09-01)

| Component | Pinned version | Source |
|---|---|---|
| amneziawg-tools (`awg`, `awg-quick`) | **v3.1.20260812** (commit `ee0f0a9aa34ff0a0da4b3433b9512781cfe02843`; Debian package `1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1`) | PPA `ppa:amnezia/ppa` |
| amneziawg-dkms (kernel module source) | source **v3.1.20260828**, commit `3c38e168beb7c60dec41dfe423d41555205a3dac`; package **1.0.0** (`0~202608282205+3c38e16~ubuntu24.04.1`) | PPA `ppa:amnezia/ppa` |
| amneziawg-go (userspace daemon, fallback backend) | **v3.1.20260828**, commit `b5928efb6ca19f0153958460c3d141f04abc5c2e`; binary `amneziawg-go` | built from github.com/amnezia-vpn/amneziawg-go |
| Verification environment (userspace) | WSL2 Ubuntu **26.04 LTS**, kernel `6.18.33.1-microsoft-standard-WSL2` | local |
| Verification environment (kernel) | dedicated VPS, Ubuntu **24.04 LTS** (noble), KVM, kernel `6.8.0-137-generic`, x86_64; PPA packages natively | 2026-08-31 |

Package note: the PPA builds for `noble` (24.04). On Ubuntu 26.04 (`resolute`) the PPA has no
Release file — workaround used here (and by the installer if it ever meets this case): add the
PPA and pin its suite to `noble`. On the supported matrix (22.04/24.04/Debian 12) the PPA works
natively (24.04 verified here; 22.04/Debian 12 verification belongs to the Phase 11 matrix).

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
| 11–14 | H1–H4 | inclusive u32 range string, canonical `N` or `N-M` |
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

## Supported parameter contract

The tools parser accepts the complete key list in
[`fixtures/configc-keys.txt`](fixtures/configc-keys.txt), but parser acceptance is only the first
capability level. WG-Guard distinguishes:

1. **parsed** by `amneziawg-tools`;
2. **transported and observable** through the kernel and/or userspace backend;
3. **compatible** with an actual client and real traffic;
4. **supported** by WG-Guard across validation, persistence, apply/dump, drift, API, client config,
   backup, and restore.

A lower level never implies a higher one. The Phase 8 source fixture records the exact paths and
revisions used for this matrix.

| Field | Parser and section | Runtime representation / backends | Placement and parity | Dump / clearing | WG-Guard contract |
|---|---|---|---|---|---|
| `Jc` | u16 scalar, `[Interface]` | Kernel + userspace | Sender-local; may differ by side. `1..128` when obfuscation is enabled; upstream recommends `4..12`. | Dumped. `Jc=0` disables junk and is accepted on kernel. | Supported. Recommended/randomized profiles generate it independently and validate relationships. |
| `Jmin`, `Jmax` | u16 scalar, `[Interface]` | Kernel + userspace | Sender-local; may differ by side. Require `Jmin < Jmax ≤ 1280` under the documented MTU assumption. | Dumped; omitted values persist. | Supported with local validation even though the pinned kernel accepts invalid ordering. |
| `S1`, `S2` | u16 scalar, `[Interface]` | Kernel + userspace | Packet compatibility values. Require `S1 ≤ 1132`, `S2 ≤ 1188`, and `S1 + 56 ≠ S2`. | Dumped; omitted values persist. | Supported. Both must be at least 12 when HPK is enabled. |
| `S3`, `S4` | u16 scalar, `[Interface]` | Kernel + userspace | Packet compatibility values for cookie and transport messages. | Dumped; zero/removal may require recreation when HPK is active. | Supported but absent from the safe recommended profile unless client compatibility is proven; both must be at least 12 with HPK. |
| `H1`–`H4` | `u32_range_from_string`, `[Interface]`; inclusive `N` or `N-M`, each bound `0..4294967295`, `low ≤ high` | Packed u64; kernel + userspace preserve the full interval | Packet compatibility values. Enabled profiles use nonzero values; upstream recommends each selected value in `5..2147483647`. | All four are dumped canonically. Omission persists; zero clearing is runtime-constrained and mode transitions recreate. | Supported losslessly. The four inclusive intervals must be pairwise **non-overlapping**, not merely unequal. |
| `I1`–`I5` | string, `[Interface]`; backend parses signature tags/bytes | Kernel + userspace | Sender/client-local custom packets; parity is not required. | Dumped as text/hex; omission persists. | Supported as explicit advanced opt-in only. Not generated by the safe recommended profile; client warnings remain because known clients differ. |
| `HeaderProtectionKey` | 32-byte key, `[Interface]` | Kernel + userspace | Packet compatibility secret; both sides require the same key. All `S1`–`S4` values must be at least 12. | Dumped as a key. Omission persists; the pinned kernel cannot clear it through normal `setconf`. | Supported as explicit advanced opt-in. Never logged or returned by status APIs; removal recreates the interface. |
| `ContentPaddingAddition` | `u16_range_from_string`, `[Interface]`; inclusive `N` or `N-M` | Packed u32; kernel + userspace | Sender/client-side padding control. Upstream recommends setting compatible behavior on both sides but does not require equality. | Dumped; omission persists. | Supported as explicit advanced opt-in. Both bounds are capped at 65535 despite the pinned tools parser's wider pre-pack check. |
| `RekeyAfterTime` | same u16 range format, `[Interface]` | Kernel + userspace | Sender/client-side timing control in seconds. | Dumped; omission persists. | Supported as explicit advanced opt-in; zero means backend default. |
| `RekeyTimeout` | same u16 range format, `[Interface]` | Kernel + userspace | Sender/client-side timing control in seconds. | Dumped; omission persists. | Supported as explicit advanced opt-in; zero means backend default. |
| `RejectAfterTime` | same u16 range format, `[Interface]` | Kernel + userspace | Sender/client-side timing control in seconds. | Dumped; omission persists. | Supported as explicit advanced opt-in; zero means backend default. |
| `KeepaliveTimeout` | same u16 range format, `[Interface]` | Kernel + userspace | Sender/client-side timing control in seconds; distinct from peer `PersistentKeepalive`. | Dumped; omission persists. | Supported as explicit advanced opt-in; zero means backend default. |
| `MaxHandshakeAttempts` | same u16 range format, `[Interface]` | Kernel + userspace | Sender/client-side attempt-count control. | Dumped; omission persists. | Supported as explicit advanced opt-in; zero means backend default. |
| `RandomTrailers` | boolean `on/off`, `[Interface]` | Kernel + userspace | Packet-shape behavior must only be enabled for clients verified to tolerate it. | Dumped; omission persists. | Supported as explicit unsafe/client-specific opt-in; always off in generated profiles. |
| `DisableCookies` | boolean `on/off`, `[Interface]` | Kernel + userspace | Disables the under-load cookie path and weakens DoS protection. | Dumped; omission persists. | Supported as explicit unsafe opt-in only; always off in generated profiles. |
| `PersistentKeepalive` | `u16_range_from_string`, `[Peer]`; inclusive `N` or `N-M` | Packed u32; kernel + userspace | Peer/client-side interval in seconds. | Peer dump field 8 preserves the range; `0`/`off` disables it. | Supported losslessly through the global client setting and rendered peer config. Bounds are capped at 65535. |
| `AdvancedSecurity` | boolean `on/off`, **`[Peer]` only** | Kernel tools send a flag, but pinned kernel `set_peer` does not consume/store it; pinned userspace transport returns `EINVAL` | No verified client/server behavioral contract. | Ordinary 8-field peer dump omits it, so set state cannot be observed or reconciled. | **Unsupported and not exposed.** Parser-only/no-op acceptance must not be advertised as capability. Revisit only after upstream storage semantics and real compatible-client traffic are proven. |

### Range representation

Pinned tools pack u32 ranges as `high << 32 | low` and u16 ranges as
`high << 16 | low`. They print a scalar when the bounds are equal and `low-high` otherwise. The
tools u16 parser checks `UINT32_MAX` before narrowing into 16-bit halves; values above `65535`
therefore truncate rather than remain lossless. WG-Guard deliberately applies the real packed
width and rejects either u16 bound above `65535`.

### Constraint enforcement lives at runtime, not in the parser

The tools parser accepts `Jmin > Jmax`, overlapping H intervals, and `S1 + 56 == S2`. The
userspace daemon rejects all three at `setconf`; the pinned kernel rejects overlapping H ranges
but accepts the other two. WG-Guard validates the strict union before invoking `awg`:
`Jc` 1–128 when enabled, `Jmin < Jmax ≤ 1280`, `S1 ≤ 1132`, `S2 ≤ 1188`,
`S1 + 56 ≠ S2`, and H1–H4 pairwise non-overlapping. HPK additionally requires every one of
`S1`–`S4` to be at least the 12-byte nonce size.

Placement guidance changed between upstream generations: the kernel README gives a broad legacy
parity statement, while the pinned userspace README identifies J/I, timing, keepalive, and content
padding as side-local controls. WG-Guard follows the field-specific pinned userspace guidance and
requires exact parity only for packet compatibility values (`S1`–`S4`, `H1`–`H4`, and HPK).

## Generated profile policies

WG-Guard has one server-side generator for service, REST, and panel flows. These are product
policies inside the verified upstream constraints, not claims that one parameter set is optimal
on every network. Real compatible-client traffic remains the Phase 8 promotion gate.

- **Plain:** no AWG parameters.
- **Recommended:** `Jc=4`, `Jmin=40`, `Jmax=70`, `S1=15`, and `S2=64`. H1–H4 are fresh scalars,
  one drawn from each of four disjoint bands covering `5..2147483647`; they are therefore
  non-zero, distinct, non-overlapping, and not a shared installation fingerprint. S3/S4, HPK,
  I1–I5, timers/padding, RandomTrailers, and DisableCookies remain unset/off.
- **Randomized:** `Jc=4..12`, `Jmin=20..60`, `Jmax=80..240`; S1/S2 are `12..256` with
  `S1+56 != S2`; S3/S4 are `12..64` and accompany a fresh 32-byte HPK. H1–H4 are true ranges,
  each constructed inside its own disjoint header band with a positive span no larger than
  100,000,000. Padding/timer intervals are generated within the bounded policy validated in
  `internal/iface/profile.go`, with RejectAfterTime starting after RekeyAfterTime. I1–I5,
  RandomTrailers, and DisableCookies remain unset/off because they are client-specific or unsafe.

All selections use `crypto/rand` with unbiased bounded sampling; dependent values are constructed
together and the completed profile must pass both the general runtime validator and the narrower
policy validator. Entropy failure returns no partial profile. The browser never generates protocol
values: its authenticated, CSRF-protected, `no-store` preview request only populates values returned
by the canonical generator. REST creation rejects `preset` plus explicit `obfuscation`. Stored HPKs
are never returned by REST or embedded into edit-page HTML.

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
apply cleanly via `setconf`. Kernel behavior was subsequently verified in the matrix below;
Phase 8 re-audits lossless client/config parity rather than assuming runtime acceptance is client
compatibility.

## Kernel-module runtime matrix (real VPS, 2026-08-31)

Full evidence: [`fixtures/verify-vps-kernel-matrix.txt`](fixtures/verify-vps-kernel-matrix.txt).
Environment: dedicated Ubuntu 24.04 KVM VPS, kernel 6.8.0-137, `amneziawg` module 3.1.20260812
from the PPA (module name is **amneziawg** — `modprobe amneziawg`; links are
`ip link add … type amneziawg`). Every claim below was executed against that module.

- **Accepted + round-tripped through the kernel**: S3/S4; **H1–H4 as u32 ranges** (dump echoes
  `14600319-413859944` verbatim); all six timer/padding `N`/`N-M` params; RandomTrailers /
  DisableCookies; I1–I5 both template literals (`<r 105>`) and hex blobs; peer
  PersistentKeepalive **ranges** (`25-35`).
- **HeaderProtectionKey is coupled to all four S values**: pinned source requires each of
  S1–S4 to be at least 12. VPS testing also found that S3/S4 must accompany an HPK write in the
  same `setconf` message even when they appear to persist. HPK rotation works when the coherent
  padding block is present; HPK cannot be cleared (`(none)` is parser-rejected, omission persists).
- **AdvancedSecurity is not a kernel capability at this pin.** `setconf` returns success because
  the tools send a recognized netlink attribute, but `set_peer` never consumes or stores it and
  ordinary dump output has no field for it. The userspace tools path rejects it with `EINVAL`.
- **Clearing semantics**: `S3 = 0`/`S4 = 0` and `H1..H4 = 0` are rejected while HPK is set;
  `Jc = 0` alone is accepted (junk disabled). Omitted keys persist (re-verified on kernel).
  A fresh interface dumps `H1..H4 = 1,2,3,4` (stock header values), everything else
  0 / off / `(null)` / `(none)`.
- **Constraint enforcement differs by backend**: duplicate `H1=H2` and the all-zero block are
  rejected on both backends; `Jmin > Jmax` and `S1 + 56 == S2` are **accepted by the kernel
  module** (userspace daemon rejects them). WG-Guard validates the full set locally regardless.

WG-Guard implementation consequences: `render.go` emits a coherent S1–S4 block with
HeaderProtectionKey; interface validation rejects HPK unless all four paddings meet the nonce
minimum; the 2.0/3.x set stays capability-gated off by default where client support varies;
clearing HPK/S3/S4 on a live interface requires recreation. `AdvancedSecurity` is not modeled,
rendered, or advertised.

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
| **2.0/3.x generation runtime behavior (kernel)** | VPS acceptance/round-trip matrix | S3/S4, H ranges, timers, flags, I1–I5, HPK (⇒coherent S block), and peer keepalive ranges round-trip | ✅ **verified (VPS kernel)**; client compatibility still varies — params stay gated |
| `AdvancedSecurity` behavior | Pinned tools/kernel/userspace source plus VPS `setconf` | parser accepts; kernel setter ignores/unobservably succeeds; userspace transport rejects; dump omits | ⛔ unsupported/gated |
| **setconf headerless config on kernel** | VPS: conf without `[Interface]` | rejected (`Line unrecognized`) — explicit headers required | ✅ **verified (VPS kernel)** |
| **Kernel constraint enforcement** | VPS: dup-H rejected; Jmin>Jmax / S1+56==S2 accepted | differs from userspace; WG-Guard validates locally | ✅ **verified (VPS kernel)** |
| PPA on Ubuntu 22.04 / Debian 12 | requires real VPS matrix | — | ⚠️ Phase 11 |

Reproduction: [`fixtures/verify-wsl2.sh`](fixtures/verify-wsl2.sh) and
[`fixtures/verify-wsl2-runtime.sh`](fixtures/verify-wsl2-runtime.sh).

## Known upstream issues respected by design

- arm64 userspace H4 corruption (amneziawg-go #110) — userspace fallback is not the default;
  arm64 verification is part of the Phase 11 matrix.
- iOS rejects I1–I5 configs at startup (#115) — I1–I5 is opt-in per profile with client warnings.
- `RandomTrailers` panic history (#178) — default `off` (verified default in `showconf` fixture).
- IPv6-disabled hosts resetting the listen port (#148) — WG-Guard always sets an explicit
  listen port and reconciles it against the DB.
