# Phase 12 — v0.1.0 release

Status: **complete** (2026-09-28) for the documented Ubuntu 24.04 amd64 support boundary.
The owner-approved [v0.1.0 GitHub Release](https://github.com/Sir-Adnan/wg-guard/releases/tag/v0.1.0)
is public. Its tag resolves to `fb38c1fca0a3689b209aff6c814780cb31d1cb11`.

## Delivered

- The English/Persian READMEs, installation and recovery guidance, compatibility claims,
  CHANGELOG, third-party notices and living status docs were aligned. Obsolete transient
  planning files were removed; authoritative phase records and historical evidence remain.
- An immutable Linux/amd64 binary, SHA-256 manifest, release metadata, SPDX linked-Go-module
  inventory and notices bundle are published. The Docker runtime image is built locally from
  the selected binary; no official registry image was published.
- The manual exact-commit workflow runs source gates, compares the image and release binary,
  creates binary/bundle and SBOM attestations, stages assets as a draft, downloads and verifies
  them, then publishes. No repository publication secret is used.

## Final gate

- At publication, clean `main` and `origin/main` matched the release commit. The [main CI run](https://github.com/Sir-Adnan/wg-guard/actions/runs/36372371606)
  and [release run](https://github.com/Sir-Adnan/wg-guard/actions/runs/36372832174) passed
  formatting, vet, race, bootstrap, synthetic backup, reachable-vulnerability and artifact
  checks. Repeated candidate builds produced identical manifests; the public manifest matches
  the locally built final revision. The published binary's SHA-256 is
  `a5843e8dc077737c08cd832b1f203bcaee2436e43f826df33d6b215c5a586bde`.
- On the dedicated real Ubuntu 24.04 amd64 VPS, candidate Docker/native fresh installs,
  updates, rollback and data-preserving lifecycle smoke passed. The exact final binary updated
  a native node and stayed healthy. After publication, the documented unpinned bootstrap
  selected `v0.1.0`, freshly installed Docker, and served healthy panel, OpenAPI and CSS responses;
  host and container binary hashes matched the published asset. The test node and staging files
  were purged afterward; the expected data-lock tombstone remains.
- Phase 10's full bilingual browser/viewport/state matrix and Phase 11's kernel/client,
  recovery and resource certification remain the applicable feature-frozen evidence. Phase 12
  changed delivery, documentation and test infrastructure, so those unaffected drills were not
  repeated. API/OpenAPI public contract did not change.

## Support boundary

Production verification covers the recorded Ubuntu 24.04 amd64 Docker/native paths only.
Later Ubuntu releases, active firewalld, real-host UFW coexistence, physical-device testing,
1000 simultaneous handshakes, a multi-day traffic soak and long-interval ACME renewal remain
uncertified. See [status.md](status.md) and [release-readiness.md](release-readiness.md).
Future public versions and an official registry image require separate owner approval.
