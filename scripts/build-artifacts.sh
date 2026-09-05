#!/usr/bin/env bash
# Build local candidates from immutable HEAD; never publish tags or releases.
set -euo pipefail
umask 077
root=$(cd "$(dirname "$0")/.." && pwd)
version=
output=
while (($#)); do
  case "$1" in
    --version|--output)
      (($# >= 2)) || { printf 'Missing value\n' >&2; exit 2; }
      if [[ $1 == --version ]]; then version=$2; else output=$2; fi
      shift 2 ;;
    --help|-h) printf 'Usage: bash scripts/build-artifacts.sh --version VERSION --output NEW_DIRECTORY\n'; exit 0 ;;
    *) printf 'Unknown argument: %s\n' "$1" >&2; exit 2 ;;
  esac
done
[[ $version =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ && -n $output && ! -e $output ]] || { printf 'Specify a safe version and a new output directory\n' >&2; exit 2; }
commit=$(git -C "$root" rev-parse --verify HEAD)
[[ $commit =~ ^[0-9a-f]{40}$ ]] || exit 2
stage=$(mktemp -d -t wg-guard-artifacts.XXXXXXXX)
trap 'rm -rf -- "$stage"' EXIT
mkdir "$stage/source" "$stage/assets"
git -C "$root" archive --format=tar "$commit" | tar -x -C "$stage/source"
flags="-s -w -X github.com/Sir-Adnan/wg-guard/internal/version.Version=$version -X github.com/Sir-Adnan/wg-guard/internal/version.Commit=$commit"
for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch GOENV=off GOWORK=off GOFLAGS= GOSUMDB=sum.golang.org \
    GONOSUMDB= GOPRIVATE= GONOPROXY= GOPROXY=https://proxy.golang.org,direct \
    go -C "$stage/source" build -trimpath -buildvcs=false -mod=readonly -ldflags "$flags" \
      -o "$stage/assets/wg-guard_linux_$arch" ./cmd/wg-guard
done
(cd "$stage/assets" && sha256sum wg-guard_linux_amd64 wg-guard_linux_arm64 > checksums.txt)
mkdir -p "$(dirname "$output")"
mkdir "$output"
cp "$stage/assets/"* "$output/"
printf 'Local candidates: %s (%s), %s\n' "$version" "$commit" "$output"
