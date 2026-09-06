# GitHub acquisition and local candidate artifacts

The Bash entry point obtains a Linux executable and opens its bilingual management entry.
On a fresh node that entry starts installation with the exact acquired build identity; on an
installed node it opens management without reinstalling or implicitly applying an update.
The shared distribution/installer engine verifies build identity and builds the Docker runtime
image from the selected binary when needed. No published official image is assumed. Installed
nodes expose [terminal management](terminal-management.md) through `sudo wg-guard manage`.

## Commands

Download the bootstrap from the intended reviewed ref, inspect it, then run it:

```bash
curl --proto '=https' --tlsv1.2 -fsSLo install.sh \
  https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh
bash install.sh --help
bash install.sh --list-releases
bash install.sh --release latest -- --mode native
bash install.sh --release v0.1.0 -- --mode native
bash install.sh --commit main -- --mode native
bash install.sh --commit FULL_40_CHARACTER_LOWERCASE_SHA -- --mode native --lang fa
```

The tag above is an example, not a claim that a release exists. Before this feature is merged,
replace the bootstrap URL's `main` with the reviewed `codex/installer-lifecycle` branch or its
full commit SHA. Selecting `main` for the binary always means the repository's actual main
branch; downloading a bootstrap from another ref does not change that selection.

For a convenient single command, supply a reviewed full SHA containing the local-owner and
coordinated-restore installer capabilities. This downloads into a uniquely created private temporary file, propagates
download/install failure and cleans up on exit; it does not pipe a failed download into a shell:

```bash
bash -c 'set -euo pipefail; umask 077; ref="$1"; script=$(mktemp /tmp/wg-guard-bootstrap.XXXXXXXX); trap '\''rm -f -- "$script"'\'' EXIT; curl --proto "=https" --tlsv1.2 -fsS --connect-timeout 15 --max-time 120 -o "$script" "https://raw.githubusercontent.com/Sir-Adnan/wg-guard/$ref/install.sh"; bash "$script" --commit "$ref" -- --lang en' -- REVIEWED_FULL_40_CHARACTER_SHA
```

Before a compatible public release exists, explicitly select the reviewed development SHA;
the SHA must have been pushed to GitHub. A local unpushed commit cannot be acquired remotely.
`--commit main` is usable only after main contains this capability. Fresh noninteractive setup
also needs `--owner-password-file /root/private-file` (0600), optionally `--owner-username`;
see [owner setup and terminal constraints](terminal-management.md).

`--release latest` is the default. The catalog is one bounded page of 30 GitHub releases; drafts,
prereleases and unpublished entries are excluded. Latest chooses the first stable entry on
that page. Exact tags use GitHub's tag-specific release endpoint, including tags outside the
page. If there are no stable releases on the page, the request fails; it never switches to
development source. Release acquisition requires both platform assets and their checksum
manifest. The list command is read-only except for missing local acquisition prerequisites.

`--commit main` and exact SHA selections resolve through GitHub before fetching the immutable
source tarball. The development version is `0.0.0-dev.<first-12-SHA-characters>`, and the full
commit is stamped into the binary. No branch name or unvalidated external text enters build
arguments. The bootstrap displays the version, full commit and binary SHA-256.

With no forwarded flags (or only `--lang fa|en`), the bootstrap calls `wg-guard manage`.
Explicit setup flags such as `--mode native` select `wg-guard install`; all arguments after `--`
and unrecognized bootstrap flags are forwarded unchanged. `--yes` always selects install with
noninteractive flags/defaults, including the existing installed-node refusal. Interactive
input is reopened from `/dev/tty` when available, otherwise `/dev/null`. A piped script is never
read as installer answers. `--help` works without a terminal or any acquisition prerequisite.
Initial acquisition diagnostics are English; the Go wizard accepts `--lang fa|en`.

## Build prerequisites and cost

Supported acquisition targets are Linux amd64 and arm64. Root or `sudo` is required to perform
installation. Acquisition/build run as the invoking user; privilege elevation occurs for missing
packages and the acquired management/install command. The script checks curl, CA certificates, Python 3,
tar and sha256sum. On apt systems it installs only missing packages (`curl`, `ca-certificates`,
`python3`, `tar`, `coreutils`) after refreshing indexes; it never performs a blanket upgrade.
Other systems must provision missing tools manually. Python uses only its standard library and
is an acquisition/build prerequisite, not a panel runtime dependency. Installed missing packages
are retained; downloaded sources, caches and temporary compiler are removed on exit.

An existing Go compiler is accepted only when its version meets the selected source's `go`
directive. Otherwise the bootstrap/package select a compatible stable Linux compiler from
[official Go download metadata](https://go.dev/dl/?mode=json), validate its platform filename,
size and SHA-256, and extract it privately. No global Go installation is changed. This handles
Ubuntu 24.04 hosts without Go or with the distribution's older compiler. The compiler uses
`GOTOOLCHAIN=local`, `GOENV=off`, `GOWORK=off`, CGO disabled, trimpath, readonly module resolution,
and the public Go module checksum database. Builds do not inherit `GOFLAGS`, workspace settings,
private module bypasses or custom checksum databases. Host PATH and explicit proxy/CA settings
are preserved for executable lookup and transport. Module/cache directories are private and
writable for reliable cleanup. No repository hooks or `go generate` run.

Source builds can take several minutes and require substantially more RAM/disk than the running
panel, especially pure-Go SQLite compilation. Allow roughly 2 GiB of free temporary space and
adequate build memory; constrained nodes should use verified release binaries. Each HTTP request
has a five-minute deadline; source compilation has a fifteen-minute deadline. The Go acquisition
operation has a twenty-minute overall context deadline. Shell transfers have at most six
validated redirect requests, each bounded separately. Cancellation stops acquisition before
promotion. SIGKILL/power loss can leave an owned temporary directory for manual removal.

## Integrity and ownership

Production endpoints are limited to `Sir-Adnan/wg-guard`; explicit Go client endpoint overrides
are trust configuration for tests or deliberately chosen HTTPS mirrors. GitHub responses, JSON,
asset names, exact download URLs, commit SHAs, checksum lines and archive paths are validated.
Release assets are `wg-guard_linux_amd64`, `wg-guard_linux_arm64`, and `checksums.txt`, with one
unambiguous SHA-256 entry for the selected platform. Tags containing paths, whitespace or shell
syntax are unsupported. The bootstrap disables curl's personal configuration file.

HTTP catalog data is capped at 1 MiB; checksums at 64 KiB; binaries/toolchain archives at 256 MiB;
compressed source at 128 MiB. Source expansion is capped at 512 MiB, toolchain expansion at
1 GiB, and each archive at 50,000 entries. Only regular files and directories under the exact
expected root are extracted. Traversal, duplicate members, symlinks, hardlinks, devices and
special modes are rejected. Downloads begin as private nonexecutable files and become the
candidate only after verification. A failed Go acquisition removes its owned staging child;
the bootstrap removes its entire staging directory on success/failure.

Checksums provide integrity over trusted GitHub/TLS, not independent publisher authentication.
A compromised publisher account or a malicious explicitly selected source commit is outside
this boundary. Release tag identity is resolved through GitHub; checksum manifests are not
cryptographically bound to that commit. Signing/provenance and final publication remain Phase 12.

`Client.Acquire(ctx, Selection, absoluteDir)` returns
`Build{Channel, Ref, Commit, Version, SHA256, BinaryPath}`. Release `Ref` is the exact tag; source
`Ref` is the resolved full SHA. The caller owns the returned candidate's private parent directory
and removes it after consuming the binary. Acquisition never changes an active installation.

## Local artifacts and verification

Before dispatching management/install, the bootstrap runs the candidate's `installer-contract` command
without elevation, with a 15-second deadline and a 4096-byte output cap. Missing/older contracts
are refused. Revision1 requires prerequisite, recoverable-lifecycle, local-owner,
coordinated-restore and `data_lease` capabilities plus an explicit data contract. Candidates
lacking shared-volume lifetime data ownership are refused. Retained recovery artifacts
remain governed separately by their recorded identity and data contract.
The bootstrap passes private build-identity metadata to the installer,
which validates the candidate and deploys the same binary in Docker and on the host.

The Go install/update flags share `internal/distribution`: `--release TAG|latest` and
`--commit main|FULL_SHA` are mutually exclusive. Docker uses a runtime image built from the
selected binary and verified core bundle, or an explicit image override with a matching
binary checksum. Remote image failure is fatal; only `--local-image` permits an already local
image without pulling. No source or stale-local fallback is implicit. Transaction and legacy
recovery semantics are documented in [lifecycle-recovery.md](lifecycle-recovery.md).

```bash
bash scripts/build-artifacts.sh --version v0.1.0-candidate --output /tmp/wg-guard-candidate
cd /tmp/wg-guard-candidate
sha256sum --check checksums.txt
```

The builder archives immutable local `HEAD`, cross-compiles both Linux targets, stamps the full
commit/version, and writes the exact asset names above. Uncommitted work is excluded. The output
directory must not already exist. This creates no public tag, release, registry image or upload.

`go test ./internal/distribution ./internal/subprocess` exercises HTTPS fixtures, malformed
selections, integrity/size/cancellation failures, unsafe archives, toolchain checksums and an
actual minimal source compilation. `bash scripts/test-bootstrap.sh` runs fake external utilities
and real script logic for release/list/source/toolchain paths, integrity refusal, piped input,
cleanup and candidate checksums. Linux CI runs those fixtures. Neither fixtures nor successful
cross-compilation claim a clean-VPS install, arm64 execution, a published-release install, or
complete Docker/native lifecycle verification; those remain later milestone evidence.
