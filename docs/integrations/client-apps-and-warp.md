# Client apps, AmneziaVPN keys and WARP

This document records the external formats WG-Guard emits or consumes outside the pinned
AmneziaWG runtime ([amneziawg.md](amneziawg.md)). Each one was source-reviewed at the version
below and has not been run against a live service. Re-review before relying on a changed
upstream.

## Client apps on the subscription page

`internal/web/sub_apps.go` is the single catalog. Links point only to official stores, releases
or project sites.

| Platform | Apps, in order |
|---|---|
| Android | AmneziaWG (Google Play, GitHub APK), WG Tunnel (Google Play, website), AmneziaVPN |
| iPhone / iPad | AmneziaWG (official Amnezia guide with the store link), AmneziaVPN |
| Windows | AmneziaWG (GitHub release), WG Tunnel (website), AmneziaVPN |
| macOS | AmneziaWG (official Amnezia guide), WG Tunnel (website), AmneziaVPN |
| Linux | WG Tunnel (website), AmneziaVPN, amneziawg-tools (`awg-quick`, source) |

AmneziaWG and WG Tunnel import the canonical `.conf` or its QR code directly. AmneziaVPN comes
after them on every platform except Linux, where command-line tools come last; it is offered
as the one-app option for every OS.

## AmneziaVPN `vpn://` key

- Reviewed: `amnezia-vpn/amnezia-client` at `30ff345`, `ImportController::extractConfigFromData`
  and its `vpn://` handling.
- Encoding: `vpn://` + base64url without padding of Qt `qCompress(data)`. `qCompress` is a
  4-byte big-endian uncompressed length followed by a zlib stream.
- WG-Guard encodes the canonical client config text unchanged. The importer recognizes a native
  AmneziaWG/WireGuard config by its `[Interface]` and `[Peer]` sections. Implementation:
  `internal/clientconf/amneziavpn.go`; a round-trip test decodes the key back to the exact bytes.
- Android registers the `vpn` URL scheme, so **Open in AmneziaVPN** opens the app directly.
  Desktop and iOS users paste the copied key using Add → Insert key.
- Not verified: an import on a physical device.

## Cloudflare WARP registration

- Reviewed: `ViRb3/wgcf` at `de6bba7`, API version `v0a5641`, base
  `https://api.cloudflareclient.com`.
- Calls used, and nothing else:
  - `POST /{ver}/reg` with a fresh X25519 public key, install ID, `tos` timestamp, model and
    locale; it returns the account ID, token, license and the peer/interface configuration;
  - `PUT /{ver}/reg/{id}/account` with `{license}` to apply a WARP+ key;
  - `GET /{ver}/reg/{id}/account` to read the account type.
- Request format: Android-style `CF-Client-Version` and `User-Agent` headers, pinned to TLS 1.2,
  as wgcf does. There is no uTLS dependency.
- Storage: the token, private key and license are sealed in the outbound's secret.
- Not verified: registration against the live API (the CI proxy blocks it). If it fails on a
  host, generate a profile with wgcf and import it as a WireGuard outbound.
