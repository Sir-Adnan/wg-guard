# WG-Guard official image (docs/operations/deployment.md, ADR-0006).
#
# The VPN data plane — the AmneziaWG kernel module, IPv4 forwarding and the
# nftables table — runs on the HOST. This image carries the WG-Guard binary
# plus the pinned amneziawg tooling and runs with host networking and
# CAP_NET_ADMIN, so links, firewall rules and shaping act on the host's own
# network namespace with zero hot-path overhead.
#
# Build (supported amd64 target):
#   docker build --platform linux/amd64 -t wg-guard:candidate .

FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -mod=readonly \
    -ldflags "-s -w \
      -X github.com/Sir-Adnan/wg-guard/internal/version.Version=${VERSION} \
      -X github.com/Sir-Adnan/wg-guard/internal/version.Commit=${COMMIT} \
      -X github.com/Sir-Adnan/wg-guard/internal/version.Date=${BUILD_DATE}" \
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

FROM golang:1.27.1-alpine AS awg-userspace-build
RUN apk add --no-cache git
RUN git -c advice.detachedHead=false clone --quiet --depth 1 \
        --branch v3.1.20260828 --single-branch \
        https://github.com/amnezia-vpn/amneziawg-go.git /src/amneziawg-go \
    && test "$(git -C /src/amneziawg-go rev-parse HEAD)" = "b5928efb6ca19f0153958460c3d141f04abc5c2e" \
    && git -C /src/amneziawg-go diff --quiet b5928efb6ca19f0153958460c3d141f04abc5c2e --
RUN cd /src/amneziawg-go && CGO_ENABLED=0 go build -trimpath -o /out/amneziawg-go . \
    && go version -m /out/amneziawg-go | grep -F 'vcs.revision=b5928efb6ca19f0153958460c3d141f04abc5c2e' \
    && go version -m /out/amneziawg-go | grep -F 'vcs.modified=false'

FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

# Runtime tooling is built from the exact reviewed upstream tag and commit.
# The host kernel module remains installer-managed and is not loaded here.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates nftables iptables iproute2 procps curl \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/wg-guard /usr/local/bin/wg-guard
COPY --from=awg-tools-build /src/amneziawg-tools/src/wg /usr/local/bin/awg
COPY --from=awg-userspace-build /out/amneziawg-go /usr/local/bin/amneziawg-go
COPY --from=build /src/LICENSE /usr/share/doc/wg-guard/LICENSE
COPY --from=build /src/THIRD_PARTY.md /usr/share/doc/wg-guard/THIRD_PARTY.md
COPY --from=build /src/third_party/licenses /usr/share/doc/wg-guard/third_party/licenses

LABEL org.opencontainers.image.source="https://github.com/Sir-Adnan/wg-guard" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.licenses="MIT"
LABEL io.wg-guard.awg-tools.commit="ee0f0a9aa34ff0a0da4b3433b9512781cfe02843"
LABEL io.wg-guard.awg-userspace.commit="b5928efb6ca19f0153958460c3d141f04abc5c2e"

# Host networking is used at runtime (compose sets network_mode: host), so
# EXPOSE is documentation only.
EXPOSE 80 443 8080

ENTRYPOINT ["/usr/local/bin/wg-guard"]
CMD ["serve", "-config", "/etc/wg-guard/wg-guard.toml"]
