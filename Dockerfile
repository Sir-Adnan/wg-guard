# WG-Guard official image (docs/operations/deployment.md, ADR-0006).
#
# The VPN data plane — the AmneziaWG kernel module, IPv4 forwarding and the
# nftables table — runs on the HOST. This image carries the WG-Guard binary
# plus the pinned amneziawg tooling and runs with host networking and
# CAP_NET_ADMIN, so links, firewall rules and shaping act on the host's own
# network namespace with zero hot-path overhead.
#
# Build (supported amd64 target):
#   docker build -t wgguard/wg-guard:latest .
#   docker buildx build --platform linux/amd64 -t wgguard/wg-guard:latest --push .

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

FROM ubuntu:24.04 AS awg-tools-build

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git build-essential \
    && rm -rf /var/lib/apt/lists/*
RUN git -c advice.detachedHead=false clone --quiet --depth 1 \
        --branch v3.1.20260812 --single-branch \
        https://github.com/amnezia-vpn/amneziawg-tools.git /src/amneziawg-tools \
    && test "$(git -C /src/amneziawg-tools rev-parse HEAD)" = "ee0f0a9aa34ff0a0da4b3433b9512781cfe02843" \
    && git -C /src/amneziawg-tools diff --quiet ee0f0a9aa34ff0a0da4b3433b9512781cfe02843 -- \
    && make -C /src/amneziawg-tools/src

FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Runtime tooling is built from the exact reviewed upstream tag and commit.
# The host kernel module remains installer-managed and is not loaded here.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates nftables iptables iproute2 procps curl \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/wg-guard /usr/local/bin/wg-guard
COPY --from=awg-tools-build /src/amneziawg-tools/src/wg /usr/local/bin/awg

LABEL io.wg-guard.awg-tools.commit="ee0f0a9aa34ff0a0da4b3433b9512781cfe02843"

# Host networking is used at runtime (compose sets network_mode: host), so
# EXPOSE is documentation only.
EXPOSE 80 443 8080

ENTRYPOINT ["/usr/local/bin/wg-guard"]
CMD ["serve", "-config", "/etc/wg-guard/wg-guard.toml"]
