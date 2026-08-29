# Changelog

All notable changes to WG-Guard are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning is
[SemVer](https://semver.org/). The REST API (`/api/v1`) is a compatibility contract from its
first release — see [docs/architecture/api.md](docs/architecture/api.md).

## [Unreleased]

### Added
- Documentation system under `docs/` (product, architecture, integrations, operations,
  development, ADRs) with the original specification archived under `docs/archive/`.
- Project scaffold: Go module (`github.com/Sir-Adnan/wg-guard`), `wg-guard` CLI skeleton
  (`version`), Makefile, lint configuration, GitHub Actions CI (fmt/vet/test/race, amd64+arm64
  builds).
- Verified AmneziaWG upstream pinning results (WSL2 Ubuntu 24.04, `ppa:amnezia/ppa`) recorded in
  `docs/integrations/amneziawg.md`.
