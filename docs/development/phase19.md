# Phase 19 — Installer and operational journeys

The later [system health follow-up](system-health.md) exposes shared read-only
runtime/readiness/accounting/telemetry evidence to the panel and authenticated REST,
with an additive OpenAPI contract. Its verification is separate from the original
Phase 19 source/artifact identities below.

The later [backup workbench follow-up](backup-workbench.md) adds panel verification,
saved reports/conditional cancellation, enabled-owner access checks and native section
navigation. Its changed source has a separate verification record; the initial Phase 19
CI/artifact identities below do not certify that subsequent implementation.

Implemented on 2026-10-05 after Phase 18. Source remains unreleased; latest stable
is preparation v0.1.9. No live-server rebuild, CA operation, destructive owner-data
change, registry image or public release was performed.

## Final change

Fresh installation can consume a verified portable `.wgg` archive before starting
its managed listener. The manager offers **Install from verified backup**; CLI
uses `install --from-backup ARCHIVE`, with optional hidden `--backup-password` or
a regular 0600 `--backup-password-file`. Password values never belong in argv.
Encrypted archives are detected for interactive hidden input. Noninteractive
installation must explicitly supply private password input when required.

The archive is streamed/decrypted once into private offline staging and checked
with the existing migration/integrity/domain/encrypted-carrier gate. An enabled
non-reseller source owner is required. Missing/disabled source access is repaired
on the original node; the installer does not create replacement credentials.
Archive verification occurs before package/network/deployment mutation. The
source inventory/endpoint and separately chosen target listener/TLS are reviewed
before the existing install confirmation. Runtime default/Telegram/network seeding
and owner bootstrap are skipped for restored data. Existing target DB/key/WAL,
pending restore or recovery files cause refusal before installation changes and
again under exclusive data ownership before replacement.

The narrow `install.InitialData` seam supplies a safe review and an empty-target
paired apply, without host command authority. `backup.PreparedInstall` rechecks
immutable staging, copies bounded members with cancellation, and reuses approved
paired replacement/recovery. Source boot TOML is saved as `.restored` for review;
it never replaces target paths/TLS. Accounts/access, DB/master key, customer link,
config credentials/IPs, quota/expiry/usage and settings are preserved. For owned
Nginx, challenge-only preparation may occur during certificate issuance, but proxy
promotion follows successful archive application. Failure keeps the managed node
stopped behind the existing install journal and recovery view.

Operational states now share `internal/operation`: queued, running, scheduled,
completed, canceled, review, awaiting restart and recovery required. This is a
presentation vocabulary, not a generic executor or replacement queue. Update and
domain queues retain closed requests and their existing schema/ownership. Their
pages and Backups reuse one semantic receipt component with localized next actions.
CLI setup/restore use the same states. A reviewed backup is not applied; approval
is explicitly awaiting managed restart, with the supported host command shown.
CLI restore prints completion only after service start/readiness/journal success.
Prompt cancellation is neutral rather than a false failure/recovery warning.

The English terminal's access group now has independent domain status/configure/
renew/remove/recovery. Its form normalizes/reviews origin, role and certificate
ownership, accepts controlled file paths and never transports PEM values. Domain,
backup and installation actions reuse their existing services and locks. Quiet
actual-stage progress, recent/live component logs, independent manager acquisition
and recovery without a healthy panel remain the verified Phase 15–18 contracts;
no shell implementation or new scheduler/archive worker is added.

## Verification

Fresh checks cover initial-data-before-listener ordering, absence of default owner/
settings writes, failed apply, existing data/owner overrides/missing required kernel,
source owner authentication, unchanged master key/device ciphertext/public keys/
IPs/customer token/quota/usage/expiry/endpoint and inactive target boot. Missing/
disabled owner, modified staging, busy target and repeat apply are refused.
CLI flags, archive path/selected manager identity, EOF/back/cancel, normalized
manual domain intent and secret-free arguments are checked. Existing ordinary
host/backup/CLI/queue/catalog tests passed; applicable unchanged packages reuse Go
cache. The full ordinary suite and vet/build passed before final bounded-copy/
progress additions; those additions have fresh focused checks and exact CI will
cover the delivered revision. Linux race passed for backup/install/domainqueue/
updatequeue/operation/terminal/CLI and separately web. Bootstrap fixtures and
asset measurement passed; no asset-size ceiling or universal resource claim is made.

Chromium and WebKit each passed 48 fa/en Light/Dark responsive cells across Update
Center, Backups and Domains (320/390/768/1440px), keyboard, truthful operation
states, localized next actions and 6 no-JavaScript cells. Browser viewport checks
are not physical-device verification. No new axe scan was run.

Exact CI/artifact acceptance passed for the frozen implementation SHA as recorded below.
Physical new-host install-from-backup, real kernel/userspace, CA/forwarding/reboot,
client traffic and resource acceptance stay in Phase 20. The owner's off-host
backup verification and explicit rebuild/publication approvals still gate that work.
No account REST API, archive format, data contract or runtime dependency changed.

Contracts: [operator journey](../operations/terminal-management.md),
[backup/restore](../operations/backup-restore.md), [security](../operations/security.md),
[refactor program](refactor-program.md).


## Exact source and candidate evidence

Implementation `e589b2a5fc1ad4d92afb689c1c5dfea45e91d862` was committed/pushed to
main and passed [CI](https://github.com/Sir-Adnan/wg-guard/actions/runs/37298179907):
both Go 1.25.x/stable full race and isolated encrypted-archive load jobs, build,
vulnerability scan and runtime-image provenance/offline-load/fake persistence/
hardening checks succeeded. Source verification is separate from physical Phase 20.

The unpublished seven-day Actions artifact `11340587799` (67,526,564 B) was
independently downloaded. ZIP SHA-256
`e3094198433e4713d0e5c04dbb2c1c1193cb5dcda6ff13b9d45bf416942130be`
matched GitHub's digest; all six contained checksums passed. Manager/runtime
source and binary bindings matched; binary version ran in WSL and named the
same SHA. Docker archive config digest/source/domain protocol labels were read
independently and matched metadata. Image ID:
`sha256:b943a3240518df98e20ac2bdf1e6552e0f457cd610117b49699215a619f94732`.
Runtime metadata SHA-256:
`1b223ec7da2578c1c2281ddf0ca43d685bf56a8eb4553a300b71c04a916794b0`.
No registry image or new public release was published; latest stable remains v0.1.9.
