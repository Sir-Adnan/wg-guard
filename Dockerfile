# WG-Guard official image (docs/operations/deployment.md, ADR-0006).
#
# The VPN data plane — the AmneziaWG kernel module, IPv4 forwarding and the
# nftables table — runs on the HOST. This image carries the WG-Guard binary
# plus the pinned amneziawg tooling and runs with host networking and
# CAP_NET_ADMIN, so links, firewall rules and shaping act on the host's own
# network namespace with zero hot-path overhead.
#
# Build (amd64/arm64):
#   docker build -t wgguard/wg-guard:latest .
#   docker buildx build --platform linux/amd64,linux/arm64 -t wgguard/wg-guard:latest --push .

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/Sir-Adnan/wg-guard/internal/version.Version=${VERSION} \
      -X github.com/Sir-Adnan/wg-guard/internal/version.Commit=${COMMIT} \
      -X github.com/Sir-Adnan/wg-guard/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /out/wg-guard ./cmd/wg-guard

FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# amneziawg-tools comes from ppa:amnezia/ppa (pinned upstream; see
# docs/integrations/amneziawg.md). software-properties-common is used to add
# the PPA with its signing key and purged right after to keep the image lean.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates nftables iproute2 procps curl \
        gnupg software-properties-common \
    && add-apt-repository -y ppa:amnezia/ppa \
    && apt-get update \
    && apt-get install -y --no-install-recommends amneziawg-tools=1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1 \
    && apt-get purge -y --auto-remove gnupg software-properties-common \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/wg-guard /usr/local/bin/wg-guard

# Host networking is used at runtime (compose sets network_mode: host), so
# EXPOSE is documentation only.
EXPOSE 80 443 8080

ENTRYPOINT ["/usr/local/bin/wg-guard"]
CMD ["serve", "-config", "/etc/wg-guard/wg-guard.toml"]
