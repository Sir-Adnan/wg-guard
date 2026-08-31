# Phase 7 — Deployment, installer & production operations

Status: **complete** (2026-08-31). Scope per ROADMAP.md: official multi-arch image, compose,
interactive installer (Docker default, native secondary), host shim, built-in ACME (ADR-0011),
update/rollback, uninstall. This file is the verification record; user-facing behavior is
documented in [../operations/deployment.md](../operations/deployment.md) and
[../operations/runbook.md](../operations/runbook.md).

## What was built

- **Built-in ACME** (`feat(tls)`): `tls.mode=acme` serves TLS from the wg-guard process via
  `autocert` (part of the already-required x/crypto; x/net + x/text join as indirect deps —
  no new module). HTTP-01 challenge on a dedicated plain-HTTP sidecar (`tls.acme_http_port`,
  default 80) bound synchronously so an occupied port fails boot loudly; certificates cached
  under `<data_dir>/acme`; the sidecar's redirect fallback targets the configured domain and
  the REAL TLS port (autocert's built-in fallback hardcodes :443, which would strand
  custom-port deployments) and never bounces arbitrary Host headers.
- **Interactive installer** (`wg-guard install`): mode (Docker default, native secondary),
  domain/TLS mode (ACME default when a domain is given), panel port, challenge port, image;
  `--yes` for non-interactive installs (flags + defaults, never reads stdin). Preflight: root
  check, completed-install refusal, busy port refusal, DNS warning, docker/systemd presence.
  Artifacts: boot config (0600), compose project (docker) or hardened systemd unit (native),
  host CLI at `/usr/local/bin/wg-guard`, install-state contract, module boot persistence.
  AWG ports/subnet/MTU are deliberately NOT collected: the settings registry already owns
  those defaults (`network.mtu`, `network.port_min/max`…) and per-interface values are
  hot-editable in the panel; collecting values the installer cannot persist would be a lie.
- **Host shim**: in docker mode the SAME binary is the shim — mode-aware dispatch reads the
  install state and routes panel/data commands (`backup`, `restore`, `settings`, `token`,
  `secrets`, `reconcile`…) into the container via `docker exec -i`; host commands
  (`install`, `update`, `uninstall`, `status`, `doctor`, `version`) run locally; `serve` is
  refused with compose hints. Same CLI in both modes (ADR-0006).
- **`wg-guard update`**: pre-upgrade backup in the owning environment (container exec in
  docker mode), image pull (best-effort — locally-built images have no registry) or staged
  binary swap (`--binary`), `up -d`/restart, health check with automatic rollback to the
  previous compose image reference / previous binary (`<bin>.pre-update`). Never automatic.
- **`wg-guard uninstall`**: `--dry-run` plan; stops the node; removes ONLY state-recorded
  artifacts (compose, unit, host CLI, boot-persistence file, config, state); data and
  installer-installed packages kept unless `--purge-data` / `--purge-packages`.
- **`wg-guard status`**: install state, image, container/unit status line, health probe.
- **Docker image** (`Dockerfile`): multi-stage golang build (CGO_ENABLED=0) onto
  `ubuntu:24.04` + pinned `amneziawg-tools` from `ppa:amnezia/ppa` + nftables + iproute2;
  amd64/arm64 buildable; reference compose under `deploy/`.
- **Module boot persistence + self-heal**: the installer writes
  `/etc/modules-load.d/wg-guard.conf` and, when the DKMS module is registered for a different
  kernel series than the running one (typical after an unattended kernel upgrade), installs
  the matching headers and runs `dkms autoinstall` + `depmod -a` before modprobe.

## Verification record

All items below are **implemented + unit tested** (`go test ./...` green on Windows/Go 1.27
and WSL2 `-race` across all packages; the install package runs its whole flow against an
in-memory `Host` seam including health-checked rollback paths). VPS drills executed on the
dedicated Ubuntu 24.04 host (`zedutch.zenmode.ir`), recorded per item:

| Drill | Result |
|---|---|
| Docker-mode fresh install (`install --mode docker --domain zedutch.zenmode.ir`) | ✅ preflight → config 0600 → compose → shim → container up → health 302 on the challenge sidecar → state file. Artifacts inspected (config content, compose shape, state JSON, modules-load.d) |
| ACME issuance on a public domain | ✅ real Let's Encrypt certificate issued for `zedutch.zenmode.ir` on first HTTPS visit; `/healthz` over TLS 200; port 80 redirects 302 to the HTTPS URL; cert + account key land in `/var/lib/wg-guard/acme` |
| Host shim routing (docker mode) | ✅ `wg-guard status` host-side; `wg-guard backup list` execs into the container; `serve` refused with compose hint |
| Reboot persistence | ✅ container auto-restarted healthy (restart: unless-stopped), panel answered HTTPS 200 from the cached certificate, kernel module auto-loaded via `/etc/modules-load.d/wg-guard.conf` |
| Kernel-upgrade self-heal | ✅ after apt upgraded the kernel (6.8.0-137 → 6.8.0-138) the DKMS module no longer matched; the installer's recovery ladder installed the matching headers, rebuilt via `dkms autoinstall`, and modprobe succeeded (verified live during reinstall) |
| Uninstall | ✅ `--dry-run` plan matches the real removal; artifacts removed; data kept by default; the CLI removes itself (documented) |
| Update (docker) | see below — executed after this table was written |

### Real-host defects found and fixed during this phase

1. **ETXTBSY on self-install** — the installer copying its own running binary onto
   `/usr/local/bin/wg-guard` failed with "text file busy"; `Host.CopyFile` now writes via a
   sibling temp file + rename (and skips the same-path case).
2. **Shim argv bug** — the docker routing invoked `exec -i …` without the leading `docker`;
   every container-routed command failed. Regression only visible on a real docker host.
3. **`status` showed a raw container hash** — now docker's own status line via a new
   capturing `Host.Output`.
4. **Module not loaded after reboot** — the apt kernel upgrade left the DKMS module built for
   the old series; fixed by the recovery ladder above (the modules-load.d entry alone was
   necessary but not sufficient).

## Honest notes

- The official registry image (`wgguard/wg-guard`) is NOT published yet — publishing versioned
  multi-arch images is the Phase 8 release pipeline. The drills used a locally-built image and
  the documented `--image` override; `update` treats a failed pull as a warning for exactly
  this case.
- ACME renewal is automatic (autocert) but the 60-day renewal itself was not observed — only
  initial issuance; the cache + challenge path is identical code.
- Native systemd mode is unit-tested and was exercised on the VPS in a shorter drill (see
  below); the full Phase 8 VPS matrix (Ubuntu 22.04, Debian 12, arm64) remains open.
- Debian 12 has no AmneziaWG PPA build; the installer's module step warns and the userspace
  fallback applies there (untested — Phase 8 matrix).
