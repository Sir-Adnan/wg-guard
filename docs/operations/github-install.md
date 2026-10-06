> Current source is Docker-only; v0.1.9 retains its preparation-era layout. Existing
> state/layout is not converted in-place; export and verify off-host with the original
> manager before a fresh install/restore. Phase 20 records the new host acceptance.

# GitHub installation and verified builds

The Bash entry point obtains a Linux executable, atomically persists it as an independent local manager and
opens its English management menu. On a fresh node, **Install** consumes that exact cached build;
canceling or failing setup leaves the manager available through `sudo wg-guard` without another
build. On an installed node it refreshes management independently without implicitly updating the
running service.
The shared distribution/installer engine verifies build identity and builds the Docker runtime
image from the selected binary when needed. No published official image is assumed. Installed
nodes expose [terminal management](terminal-management.md) through `sudo wg-guard manage`.

Fresh interactive setup prompts for the administrator username (`admin` on Enter) and password.
Enter at the password prompt creates a 24-character cryptographic password and shows it once after
the node is healthy; invalid manual credentials stay in the prompt instead of leaving partial
setup. Existing installations retain their current administrator identity.

## Commands

Latest published stable release, with an interactive deployment wizard:

```bash
bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash'
```

On an installed node, open domain and SSL management directly:

```bash
bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash -s -- --https'
```

This opens the independent host manager's HTTPS menu; it does not reinstall or
update the running panel. `sudo wg-guard manage --https` opens the same menu offline.
The shortcut can combine with a build selector such as `--commit main`, but not
installation flags or `--list-releases`. See [domain/SSL operations](terminal-management.md).

The entry script comes from `main`, but its default selection is `--release latest`: it resolves
the newest **published stable** GitHub release, not the development branch. `pipefail` makes a
failed download fail the one-liner. Normal HTTPS certificate validation remains enabled. The
longer `curl --proto '=https' --proto-redir '=https' --tlsv1.2` form is valid if an operator
wants explicit protocol restrictions; it is optional for the everyday command. As with any
remote-script command, inspect the script first if you need to review what will run.

The bootstrap reopens `/dev/tty` for installer input rather than consuming script bytes as
answers. After installation, run
`sudo wg-guard`; it opens the verified local manager immediately without GitHub access. Re-running
the one-line command downloads the small bootstrap and resolves the selected release/commit. If
the immutable identity matches the private receipt, no binary/source/toolchain is downloaded and
no build runs. A changed identity is acquired, contract-checked and atomically promoted to
`/var/cache/wg-guard/manager`; `/usr/local/bin/wg-guard` remains the active service/host shim.
Temporary GitHub or compiler failure falls back only to a valid cached manager and emits a warning;
invalid selections and integrity/contract failures do not. `--refresh` always reacquires and never
falls back. Manager refresh alone does not restart or update the installed service.

For inspect-before-run operation, download the entry point first:

```bash
curl -fsSLo wg-guard-install.sh \
  https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh
less wg-guard-install.sh
bash wg-guard-install.sh
```

Keep the downloaded script to choose a source or explicit setup flags:

| Goal | Command |
|---|---|
| List stable releases | `bash wg-guard-install.sh --list-releases` |
| Latest stable, guided setup | `bash wg-guard-install.sh` |
| Exact published release | `bash wg-guard-install.sh --release v0.1.3` |
| Development branch (explicit) | `bash wg-guard-install.sh --commit main` |

The `--` separates bootstrap selection from installer flags. Supplying setup flags starts installation
directly; with no forwarded flags, the manager menu offers the same choice. `--release latest` is
implicit in the guided installation command. New-format releases supply a verified offline image asset; explicit commit builds use the one
embedded recipe. There is no official registry image to pull. The removed `--mode` flag fails
before acquisition. v0.1.9 and earlier retain the preparation-era manager; on such a node use
its tagged guide to export/verify, not the v0.1.10 manager, then fresh-install/restore.

For an **exact release**, pin both the entry script and the selected asset to the tag:

```bash
curl -fsSLo wg-guard-install.sh \
  https://raw.githubusercontent.com/Sir-Adnan/wg-guard/v0.1.3/install.sh
less wg-guard-install.sh
bash wg-guard-install.sh --release v0.1.3
```

For a **reviewed development commit**, replace `FULL_40_CHARACTER_LOWERCASE_SHA` below with the
same real SHA in both commands. A branch such as `main` may advance; a SHA does not.

```bash
curl -fsSLo wg-guard-install.sh \
  https://raw.githubusercontent.com/Sir-Adnan/wg-guard/FULL_40_CHARACTER_LOWERCASE_SHA/install.sh
less wg-guard-install.sh
bash wg-guard-install.sh --commit FULL_40_CHARACTER_LOWERCASE_SHA
```

For a stricter direct download, keep explicit protocol restrictions and `pipefail`:

```bash
bash -o pipefail -c 'curl --proto "=https" --proto-redir "=https" --tlsv1.2 -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash'
```

Use `--commit main` only for a deliberately selected development head; it resolves to an immutable
SHA before the build. A local unpushed commit cannot be acquired remotely. For unattended setup,
use a private regular owner-password file (0600), then forward the flags; never put the password
in an argument or shell history:

```bash
bash wg-guard-install.sh --release latest -- \
  --yes --owner-username admin \
  --owner-password-file /root/wg-guard-owner-password
```

Read [owner setup and terminal constraints](terminal-management.md)
for exposure, domain and secret handling before automating a deployment.

`--release latest` is the default. The catalog is one bounded page of 30 GitHub releases; drafts,
prereleases and unpublished entries are excluded. Latest chooses the first stable entry on
that page. Exact tags use GitHub's tag-specific release endpoint, including tags outside the
page. If there are no stable releases on the page, the request fails; it never switches to
development source. Release acquisition requires both platform assets and their checksum
manifest. The list command is read-only except for missing local acquisition prerequisites.

`--commit main` and exact SHA selections resolve through GitHub before fetching the immutable
source tarball. The development version is `0.0.0-dev.<first-12-SHA-characters>`, and the full
commit is stamped into the binary. No branch name or unvalidated external text enters build
arguments. The compact bootstrap shows the selected version and short commit; full build identity
is retained for lifecycle verification and diagnostics.

With no forwarded setup flags (or only a legacy language flag), the bootstrap calls
`wg-guard manage`.
Explicit setup flags such as `--domain panel.example.com` select `wg-guard install`; all arguments after `--`
and unrecognized bootstrap flags are forwarded unchanged. `--yes` always selects install with
noninteractive flags/defaults, including the existing installed-node refusal. Interactive
input is reopened from `/dev/tty` when available, otherwise `/dev/null`. A piped script is never
read as installer answers. `--help` works without a terminal or any acquisition prerequisite.
The bootstrap, setup wizard, manager and command diagnostics are English-only. Legacy `--lang`
values remain accepted so existing automation does not break, but they no longer change output.
Boolean prompts display `[Y/n]` or `[y/N]`, accept `y/yes/n/no` case-insensitively, and use the
displayed default on Enter. Capable TTYs use cyan information, green success, yellow warning and
red failure; redirected output, `TERM=dumb` and `NO_COLOR` remain plain.

## One-line selection examples

```bash
# Latest published stable release (default)
bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash'

# Latest development source; opens its manager directly
bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash -s -- --commit main'

# Exact official release: replace vX.Y.Z with a published tag
bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash -s -- --release vX.Y.Z'

# Exact development source: replace the placeholder with its full immutable SHA
bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash -s -- --commit FULL_40_CHARACTER_LOWERCASE_SHA'
```

On a fresh development-test server, choose `--commit main` at bootstrap instead of
opening preparation v0.1.9 and selecting new source inside that old manager. The old
manager's candidate contract gate can reject the new installer **before installation**,
even with no active node. That is separate from new source failing on the host. Existing
pre-refactor state remains a fresh-install/export boundary; no compatibility bypass or
in-place converter is added. A development manager's header uses `0.0.0-dev.<short SHA>`;
a default stable header uses the published release version.

## Build prerequisites and cost

The installer accepts Ubuntu 24.04 or newer on amd64/x86_64 only. Ubuntu 24.04 is the currently
verified target; later Ubuntu releases must expose the exact catalogued packages or installation
fails before deployment. Root or `sudo` is required to perform installation. Acquisition/build
run as the invoking user; privilege elevation occurs for missing packages and the acquired
management/install command. The script checks curl, CA certificates, Python 3,
tar and sha256sum. On apt systems it installs only missing packages (`curl`, `ca-certificates`,
`python3`, `tar`, `coreutils`) after refreshing indexes; it never performs a blanket upgrade.
Python uses only its standard library and is an acquisition/build prerequisite, not a panel
runtime dependency. Installed missing packages are retained; downloaded sources, caches and
temporary compiler are removed on exit.

Ubuntu package commands wait up to five minutes for the standard dpkg lock, allowing
`unattended-upgrades` to finish without creating a false recovery case. Acquisition shows real
stages: resolving the build, downloading/checking assets or pinned source, preparing a verified
toolchain when needed, compiling, and checking installer compatibility. Host installation and
updates show the active package, AWG, Docker or systemd task. Capable terminals redraw a single
elapsed-time status line; redirected runs emit a contextual line at most once a minute instead of
repeating a generic heartbeat. No estimated percentage or completion time is claimed. Detailed
host package/build/Docker command output is bounded in root-only
`/var/log/wg-guard/installer.log` (mode 0600, one rotated generation); command arguments are not
logged. Open **Logs** in the local manager or run `sudo wg-guard logs --source installer --tail 200
--follow` to see recent output and follow a running host operation. The bootstrap's temporary
download/compiler staging is removed on exit and is represented by its terminal stage results,
not retained in that host command log.

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
adequate build memory; constrained nodes should use verified release binaries. Managed Go
acquisition and runtime assembly use fresh private directories below
`/var/cache/wg-guard/staging`, rather than inheriting `/tmp` or `TMPDIR`. Each build
still isolates and removes its source/toolchain/module caches on ordinary exit;
this is scratch space, not a shared mutable Go cache. A host that mounts this cache
on tmpfs must provision it appropriately. Each HTTP request
has a five-minute deadline; source compilation has a fifteen-minute deadline. The Go acquisition
operation has a twenty-minute overall context deadline. Shell transfers have at most six
validated redirect requests, each bounded separately. Cancellation stops acquisition before
promotion. SIGKILL/power loss can leave an owned temporary directory for manual removal.

Compiler failures preserve a bounded, redacted diagnostic suffix so download chatter
cannot hide a final disk/quota/permission error. Acquisition failures are also recorded
in the private installer log before any panel stop. The generic subprocess error
policy and stdout confidentiality remain unchanged.

On older development managers that still stage under `/tmp`, a RAM-backed `/tmp`
can run out independently of the root disk. Check it with `findmnt -T /tmp`,
`df -h /tmp /var/cache` and `df -i /tmp /var/cache`; quota exhaustion is a separate
possibility even when `df` shows free space. For an already-installed node with a
verified manager cache, an explicit disk-backed temporary root can be used for one
pinned panel update:

```bash
sudo env TMPDIR=/var/cache/wg-guard wg-guard update panel --commit FULL_40_CHARACTER_LOWERCASE_SHA
```

This retains pre-update backup and readiness/rollback gates. It does not request a
core transition or delete temporary data belonging to other processes. After moving
to the corrected manager, the managed staging path is selected without this override.

## Integrity and ownership

Production endpoints are limited to `Sir-Adnan/wg-guard`; explicit Go client endpoint overrides
are trust configuration for tests or deliberately chosen HTTPS mirrors. GitHub responses, JSON,
asset names, exact download URLs, commit SHAs, checksum lines and archive paths are validated.
Release assets are `wg-guard_linux_amd64` and `checksums.txt`, with one
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
this boundary. Release tag identity is resolved through GitHub; the asset manifest alone is not
cryptographically bound to that commit. The v0.1.0 workflow adds exact-commit metadata, an
SPDX inventory and GitHub Actions build attestations; consumers can inspect them against the
tag and checksum before use.

`Client.Acquire(ctx, Selection, absoluteDir)` returns
`Build{Channel, Ref, Commit, Version, SHA256, BinaryPath}`. Release `Ref` is the exact tag; source
`Ref` is the resolved full SHA. The caller owns the returned candidate's private parent directory
and removes it after consuming the binary. Acquisition never changes an active installation.

## Local artifacts and verification

Before dispatching management/install, the bootstrap runs the candidate's `installer-contract` command
without elevation, with a 15-second deadline and a 4096-byte output cap. Missing/older contracts
are refused. Revision 2 requires prerequisite, recoverable-lifecycle, local-owner,
coordinated-restore, `data_lease`, persistent-manager and secure-exposure capabilities plus an
explicit data contract. Revision 1 remains known for retained artifacts, but cannot become the
current bootstrap manager. Candidates lacking shared-volume lifetime data ownership are refused. Retained recovery artifacts
remain governed separately by their recorded identity and data contract.
The bootstrap stores the mode-0755 manager at `/var/cache/wg-guard/manager` and a bounded
mode-0600 receipt at `/var/cache/wg-guard/manager-build.json`, both below a root-only directory, and
passes that private identity to the manager/installer, which validates and deploys the same binary
in Docker and on the host. A fresh host also receives a convenience copy at
`/usr/local/bin/wg-guard`; after installation that path belongs exclusively to the transactional
service lifecycle. Manager and receipt are replaced only after identity, digest and contract
verification.

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

The builder archives immutable local `HEAD`, builds the Linux amd64 target, stamps the full
commit/version, and writes the binary, checksums, metadata, SPDX Go-module inventory and a
notices bundle. Uncommitted work is excluded. The output directory must not already exist.
Local builds create no public tag, release, registry image or upload.

`go test ./internal/distribution ./internal/subprocess` exercises HTTPS fixtures, malformed
selections, integrity/size/cancellation failures, unsafe archives, toolchain checksums and an
actual minimal source compilation. `bash scripts/test-bootstrap.sh` runs fake external utilities
and real script logic for release/list/source/toolchain paths, integrity refusal, piped input,
cleanup, candidate checksums, archive content and SBOM. Linux CI runs those fixtures. Fixtures
and cross-compilation do not prove clean-host source/DKMS provisioning on every later Ubuntu
release; the exact published-artifact drill is recorded in [Phase 12](../development/phase12.md).
Separate real Docker/native evidence is linked from [Phase 8.1](../development/phase8.1.md).

The source extractor accepts only codeload's first-entry PAX global commit comment when it exactly
matches the selected full SHA. Other global metadata and all links, devices, traversal, duplicate
or cross-root members remain rejected before compilation.
