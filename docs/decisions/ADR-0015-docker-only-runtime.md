# ADR-0015 — Docker-only runtime and explicit artifact/host ownership

Status: implemented in Phase 17 and published in v0.1.10; image/source gates (Phase 17)
and real-host acceptance (Phase 20) are distinct. Supersedes the production-mode choice in ADR-0006.

## Decision

One production deployment, Docker, with the AmneziaWG kernel on the host by default.
Keep the explicit reviewed userspace engine inside the image and fake development outside
Docker. Keep one Go node/scheduler and one host lifecycle coordinator, with finite worker/queue
and subprocess bounds. No permanent host agent, new database, generic protocol/plugin layer or
multi-node claim is introduced.

Release assets carry the precompiled manager plus a compressed Docker save archive. One embedded
recipe builds tools/userspace from reviewed commits and consumes that exact manager. Metadata
binds image config digest (not a registry manifest digest), archive checksum, binary/commit,
reviewed core identities, deployment/data/maintenance contracts, legal inventory and Go SBOM.
Production acquisition never falls back to source compilation when an image is missing.
Explicit development builds retain the same recipe. HTTPS repository/tag and release-checksum
trust are separate from content hashes; release attestations remain a publication gate. No
official registry is assumed or published by this change.

Native WG-Guard server execution, flags, service/unit artifacts and lifecycle alternatives are
removed together. Host CLI, kernel/DKMS, broker/renewal/retention systemd tasks and network
diagnostics remain. Schema-4 state and schema-2 journal reject unknown/legacy records; the old
`/etc/wg-guard/install-state.json` is a refusal/collision input only. Existing nodes export with
their original manager and follow the approved fresh-install/restore route. The logical archive
format, account/device API, credentials, addressing, units and entitlement rules are unchanged.

## Layout

Deployment assets live in `/opt/wg-guard`; boot/TLS remain in `/etc/wg-guard`; node data remains
in `/var/lib/wg-guard`. Mutable host authority/journal/recovery artifacts move to private
`/var/lib/wg-guard-host`, outside the node mount. The active command stays in `/usr/local/bin`,
with its independent verified manager cache in `/var/cache/wg-guard` and private logs in
`/var/log/wg-guard`. Paths and managed admission are centralized in `internal/layout`.

This applies the practical service/data separation. It does not claim the full FHS `/opt`
package family, which uses `/etc/opt` and `/var/opt`
([FHS](https://refspecs.linuxfoundation.org/FHS_3.0/fhs/ch03s13.html)). Moving configuration/data
solely to rename directories yields no measured performance/security benefit. Host metadata
separation has a concrete ownership/recovery role and can change at the new deployment boundary.

No required `.env` is added: generated Compose owns immutable image deployment, TOML owns boot
settings and SQLite owns runtime settings. Existing explicit `WGG_*` overrides remain; `.env`
interpolation belongs to Compose and is not an application configuration source
([Docker](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/)).
Secrets remain in private files/stdin or encrypted node storage, never deployment argv/env files.

## Tradeoffs and gates

- Docker reduces supported lifecycle combinations; it does not itself make the kernel faster
  or fully isolate a host-network container. The module/headers still depend on the host.
- Root is read-only, capabilities are limited and temporary memory mounts are bounded. Large
  archive staging and Telegram bodies use disk; physical memory/CPU limits await actual measurement.
- Image import/cache and offline manager remain usable without a running node. Missing/changed
  assets fail before deployment; no mutable latest image or implicit registry fallback exists.
- Normal updates retain a backup, stop before deploying, prove readiness/TLS separately and
  keep current/previous recovery identities. Do not rebuild for routine later updates.
- Old data archives remain supported; old deployment execution does not. Restoring boot settings
  is report-only and target hostname/TLS/core/network acceptance remains an explicit decision.
- New Ubuntu/kernel/client/reboot/TLS/failure/resource cells require Phase 20 evidence. Historical
  Docker/native drills cannot certify this layout/hardening, and source changes authorize no new
  public release, registry publication or mutation of the owner's only host.

Follow [Phase 17](../development/phase17.md), [deployment](../operations/deployment.md) and
[migration preparation](../operations/migration-preparation.md).
