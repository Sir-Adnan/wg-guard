# Phase 12 — Release candidate

Status: **planned; not implemented**. Starts only after Phase 11 certification closes.

Phase 8.1 establishes the GitHub acquisition contract and local checksummed candidate builder
needed by the installer. This phase consumes those foundations, repeats exact-candidate tests,
and owns the final multi-arch publication workflow, provenance and freeze.

## Objective

Produce a clean, reproducible, installable, fully documented release candidate and stop before
public publication pending explicit project-owner approval.

## Scope and deliverables

- Close or explicitly defer every audit finding and release blocker with residual risk.
- Freeze versioning, CHANGELOG/release notes, README, installation/upgrade/recovery guidance,
  compatibility matrix, API/OpenAPI/examples, screenshots, licensing and third-party notices.
- Final repository hygiene, history/secret/artifact checks, `.gitignore`, generated-file policy,
  toolchain consistency and concise `AGENTS.md` review.
- Versioned linux/amd64 and linux/arm64 binaries, checksums, release metadata, multi-arch
  container build, provenance/SBOM and reproducibility evidence where practical.
- Manual approval-gated publication workflow; no signing or publication secrets in the repo.
- Install and upgrade both Docker and native modes from the exact candidate artifacts.
- Final browser/API/config/QR/metrics/logging/recovery/deployment regression and CI verification.
- Final professional readiness report with supported, unsupported, unverified and deferred work.

## Milestones

1. Freeze API, data migration, documentation and compatibility claims.
2. Complete repository, license, secret, dependency and generated-artifact hygiene.
3. Build/checksum/inspect multi-architecture candidate artifacts from a clean revision.
4. Exercise candidate install, upgrade, rollback and smoke workflows.
5. Run the final complete verification and CI suite; review the final diff/history.
6. Push candidate commits, prepare the publication action, issue the readiness report, and stop.

## Verification

- Clean-clone deterministic builds where practical; checksum and archive-content verification.
- linux/amd64 and linux/arm64 binary execution/package checks; multi-arch manifest inspection via
  a local/test registry or CI artifact without final public publication.
- Docker/native install, health, upgrade, rollback and data-preservation smoke on real Ubuntu
  24.04 using the exact candidate artifacts.
- Full formatting, vet, test, race, integration, vulnerability, asset, benchmark, API/OpenAPI,
  browser, config/QR and repository-hygiene gates.

## Documentation

All living documentation must agree with the candidate. Historical archives remain frozen.
`AGENTS.md` points to authoritative current docs without duplicating them.

## Completion criteria

RB-008 and every remaining release blocker close; CI is green; candidate artifacts install and
upgrade successfully; checksums/metadata and support claims are verified; the readiness report
concludes whether the product is genuinely ready.

## Explicit publication boundary

Do not create the final public tag/release or publish the official registry image without explicit
project-owner approval. Phase 12 ends by waiting for that approval.
