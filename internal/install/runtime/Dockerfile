# Canonical runtime recipe; rendered with reviewed core identities.
FROM ubuntu:24.04 AS awg-tools-build
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates git build-essential \
 && rm -rf /var/lib/apt/lists/*
RUN git -c advice.detachedHead=false clone --quiet --depth 1 --branch {{TOOLS_VERSION}} --single-branch {{TOOLS_REPOSITORY}} /src/amneziawg-tools \
 && test "$(git -C /src/amneziawg-tools rev-parse HEAD)" = "{{TOOLS_COMMIT}}" \
 && git -C /src/amneziawg-tools diff --quiet {{TOOLS_COMMIT}} -- \
 && make -C /src/amneziawg-tools/src

FROM golang:1.27.1-alpine AS awg-userspace-build
RUN apk add --no-cache git
RUN git -c advice.detachedHead=false clone --quiet --depth 1 --branch {{USERSPACE_VERSION}} --single-branch https://github.com/amnezia-vpn/amneziawg-go.git /src/amneziawg-go \
 && test "$(git -C /src/amneziawg-go rev-parse HEAD)" = "{{USERSPACE_COMMIT}}" \
 && git -C /src/amneziawg-go diff --quiet {{USERSPACE_COMMIT}} --
RUN cd /src/amneziawg-go && CGO_ENABLED=0 go build -trimpath -o /out/amneziawg-go . \
 && go version -m /out/amneziawg-go | grep -F 'vcs.revision={{USERSPACE_COMMIT}}' \
 && go version -m /out/amneziawg-go | grep -F 'vcs.modified=false'

FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates nftables iptables iproute2 procps curl \
 && rm -rf /var/lib/apt/lists/*
COPY --from=awg-tools-build /src/amneziawg-tools/src/wg /usr/local/bin/awg
COPY --from=awg-userspace-build /out/amneziawg-go /usr/local/bin/amneziawg-go
COPY wg-guard /usr/local/bin/wg-guard
COPY notices /usr/share/doc/wg-guard
ENV WGG_IN_CONTAINER=1
ENTRYPOINT ["/usr/local/bin/wg-guard"]
CMD ["serve", "-config", "/etc/wg-guard/wg-guard.toml"]
