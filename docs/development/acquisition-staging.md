# Acquisition staging correction — 2026-10-05

An owner-run development update failed during Go module extraction, before panel
replacement. The displayed compiler error cut off the final filesystem reason.
The host report shows `/tmp` is tmpfs with a 1.9 GiB capacity, 832 MiB available and
user quotas enabled, while the root disk has 68 GiB free and inodes are not exhausted.
The exact errno was not retained; these observations establish a constrained temporary
filesystem, not a verified ENOSPC-versus-quota distinction. The supplied installer log
contained only earlier Docker installation output.

Current source puts lifecycle acquisition and assembly beneath the managed private
cache staging directory, checks private permissions/owner and refuses symlink roots.
It retains per-attempt source/toolchain/module isolation and ordinary-exit cleanup;
no shared mutable compiler cache, root mount change or foreign `/tmp` deletion is added.
Source failure messages keep a sanitized bounded suffix and preserve typed exit causes.
Acquisition failures also reach the existing private bounded installer log without
stdout, arguments or source content. REST/OpenAPI, install-state and data contracts
are unchanged. The [operator guide](../operations/github-install.md) records the old
manager's explicit disk-backed temporary-root workaround.

Focused Windows CLI/distribution/install/logsafe/layout checks and vet passed.
Linux Go 1.26 race checks passed for those packages, including staging environment,
mode/symlink/owner refusal, final error reason/redaction/UTF-8 and bounded log output.
This is local/source evidence; the corrected default path has not been run on the
owner's server here, and exact main CI and Phase 20 remain separate gates.
