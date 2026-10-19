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
- Hand-off, re-reviewed at `amnezia-client` `ddde436`:
  - **Android** registers the `vpn` URL scheme (`AndroidManifest.xml`), so **Open in AmneziaVPN**
    redirects to the `vpn://` key and the app opens directly.
  - **iOS and iPadOS** register no URL scheme. The app imports documents instead: `.vpn` (the key
    as text), `.conf` and `.ovpn` (`Info.plist` document types; the scene delegate reads the file
    and runs the same importer). The button fetches `<name>.vpn` and opens the share sheet
    (Web Share with a file), where AmneziaVPN is listed. If Safari drops the tap before the file
    has arrived, the button asks for a second tap, which shares the already fetched file. Without
    Web Share the file downloads and is opened from Files.
  - **Windows, macOS and Linux** register no URL scheme either. The button downloads the `.vpn`
    file, which the app imports with Add → File with connection settings (its picker accepts
    `*.vpn`). **Copy key** remains for Add → Insert key.
  - Without JavaScript, `GET …/amneziavpn` redirects Android to the key and returns the file
    everywhere else; `?format=file` and `?format=text` force one form.
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
- Account type: the API reports `warp_plus: true` for free accounts too, so the panel shows WARP+
  only for the paid `limited` and `unlimited` account types.
- Live registration succeeded on the Ubuntu 24.04 test node (2026-10-16). If it fails on a
  host, generate a profile with wgcf and import it as a WireGuard outbound.
- New identity: the panel repeats `POST /reg` with a fresh key for an existing outbound and,
  when one is stored, re-applies the WARP+ license with `PUT /reg/{id}/account`. No other call is
  added: wgcf has no device removal, so the previous registration is left to expire.
- Health probe: the trace at `https://1.1.1.1/cdn-cgi/trace` reports `warp=on|plus|off` and the
  serving data centre (`colo`); the panel shows both. `warp=off` means the probe did not travel
  through WARP. Not verified on a live host since this change.
