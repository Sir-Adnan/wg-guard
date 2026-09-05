#!/usr/bin/env bash
# Phase 8 real-host configuration/QR/client verification.
#
# The harness runs the candidate node and two clients in dedicated network
# namespaces. The host installation, WG-Guard database, tunnel interfaces,
# and nftables ruleset are never used. Secret-bearing values live only in a
# mode-0700 temporary directory and are removed by the EXIT trap.
set -Eeuo pipefail
umask 077

readonly SERVER_NS="wgg-p8-srv"
readonly RECOMMENDED_NS="wgg-p8-rec"
readonly RANDOMIZED_NS="wgg-p8-rnd"
readonly BRIDGE="wgp8br0"
readonly SERVER_VETH="p8srvh"
readonly RECOMMENDED_VETH="p8rech"
readonly RANDOMIZED_VETH="p8rndh"
readonly SERVER_ADDRESS="198.18.80.2"
readonly RECOMMENDED_TRANSPORT="198.18.80.3"
readonly RANDOMIZED_TRANSPORT="198.18.80.4"
readonly PANEL_PORT="39088"
readonly PROXY_PORT="39089"
readonly RECOMMENDED_IFACE="awg6"
readonly RANDOMIZED_IFACE="awg7"
readonly USERSPACE_IFACE="p8usrec"
readonly RECOMMENDED_SUBNET="10.246.80.0/24"
readonly RANDOMIZED_SUBNET="10.246.81.0/24"
readonly RECOMMENDED_PORT="48580"
readonly RANDOMIZED_PORT="48581"
readonly EXPECTED_TOOLS="v3.1.20260812"
readonly EXPECTED_TOOLS_PACKAGE="1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1"
readonly EXPECTED_DKMS_PACKAGE="1.0.0-0~202608282205+3c38e16~ubuntu24.04.1"
readonly EXPECTED_USERSPACE="v3.1.20260828"
readonly EXPECTED_USERSPACE_COMMIT="b5928efb6ca19f0153958460c3d141f04abc5c2e"

BIN=""
QRCHECK=""
HOLD=0
WORKDIR=""
SERVICE_PID=""
USERSPACE_PID=""
PROXY_PID=""
PAYLOAD_PID=""
MODULE_WAS_LOADED=0
MODULE_LOADED_BY_HARNESS=0
SERVER_NS_OWNED=0
RECOMMENDED_NS_OWNED=0
RANDOMIZED_NS_OWNED=0
BRIDGE_OWNED=0
USERSPACE_SOCKET_OWNED=0
API_CURL_CONFIG=""
COOKIE_JAR=""

log() { printf 'phase8-vps: %s\n' "$*"; }
die() { log "FAIL: $*" >&2; exit 1; }

usage() {
  cat <<'EOF'
usage: verify-phase8-vps.sh --binary PATH --qrcheck PATH [--hold]

The owner password is read as one line from stdin and is never printed.
--hold leaves the isolated node running for browser QA until SIGINT/SIGTERM.
EOF
}

while (($#)); do
  case "$1" in
    --binary) [[ $# -ge 2 ]] || die "--binary requires a path"; BIN="$2"; shift 2 ;;
    --qrcheck) [[ $# -ge 2 ]] || die "--qrcheck requires a path"; QRCHECK="$2"; shift 2 ;;
    --hold) HOLD=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ ${EUID:-$(id -u)} -eq 0 ]] || die "root is required"
[[ -n "$BIN" && -x "$BIN" ]] || die "candidate binary is missing or not executable"
[[ -n "$QRCHECK" && -x "$QRCHECK" ]] || die "qrcheck helper is missing or not executable"
BIN="$(readlink -f -- "$BIN")"
QRCHECK="$(readlink -f -- "$QRCHECK")"

IFS= read -r OWNER_PASSWORD || die "owner password was not supplied on stdin"
OWNER_PASSWORD="${OWNER_PASSWORD%$'\r'}"
[[ "$OWNER_PASSWORD" =~ ^[A-Za-z0-9_-]{16,128}$ ]] ||
  die "owner password must be 16-128 URL-safe characters"

for command in awk cat chmod cmp curl cut dpkg-query env go grep id ip jq ln mkdir \
  mktemp modinfo modprobe nft ping python3 readlink rm seq sha256sum sleep sysctl tc tr uname; do
  command -v "$command" >/dev/null || die "required command is missing: $command"
done
for command in awg awg-quick amneziawg-go; do
  command -v "$command" >/dev/null || die "pinned AWG command is missing: $command"
done

source /etc/os-release
[[ "${ID:-}" == "ubuntu" && "${VERSION_ID:-}" == "24.04" ]] ||
  die "this evidence gate requires Ubuntu 24.04"
[[ "$(uname -m)" == "x86_64" ]] || die "this Phase 8 gate requires x86_64"
[[ "$(uname -r)" =~ ^6\.8\.0-[0-9]+-generic$ ]] ||
  die "this evidence gate requires the Ubuntu 24.04 generic 6.8 kernel series"
TOOLS_PACKAGE="$(dpkg-query -W -f='${Version}' amneziawg-tools 2>/dev/null)" ||
  die "amneziawg-tools package is not installed"
DKMS_PACKAGE="$(dpkg-query -W -f='${Version}' amneziawg-dkms 2>/dev/null)" ||
  die "amneziawg-dkms package is not installed"
[[ "$TOOLS_PACKAGE" == "$EXPECTED_TOOLS_PACKAGE" ]] ||
  die "amneziawg-tools package does not match the pinned revision"
[[ "$DKMS_PACKAGE" == "$EXPECTED_DKMS_PACKAGE" ]] ||
  die "amneziawg-dkms package does not match the pinned revision"
[[ "$(awg --version | awk '{print $2}')" == "$EXPECTED_TOOLS" ]] ||
  die "amneziawg-tools does not match $EXPECTED_TOOLS"

USERSPACE_BUILD="$(go version -m "$(command -v amneziawg-go)")"
grep -Fq $'mod\tgithub.com/amnezia-vpn/amneziawg-go/v3\t'"$EXPECTED_USERSPACE" <<<"$USERSPACE_BUILD" ||
  die "amneziawg-go module does not match $EXPECTED_USERSPACE"
grep -Fq $'build\tvcs.revision='"$EXPECTED_USERSPACE_COMMIT" <<<"$USERSPACE_BUILD" ||
  die "amneziawg-go source revision does not match the pinned commit"
USERSPACE_SHA256="$(sha256sum "$(command -v amneziawg-go)" | awk '{print $1}')"
MODULE_VERSION="$(modinfo -F version amneziawg 2>/dev/null)" ||
  die "cannot read the installed AmneziaWG module version"
[[ -n "$MODULE_VERSION" ]] || die "installed AmneziaWG module has no version metadata"

for ns in "$SERVER_NS" "$RECOMMENDED_NS" "$RANDOMIZED_NS"; do
  ip netns list | awk '{print $1}' | grep -Fxq "$ns" && die "network namespace already exists: $ns"
done
for host_iface in "$BRIDGE" "$SERVER_VETH" "$RECOMMENDED_VETH" "$RANDOMIZED_VETH"; do
  ip link show dev "$host_iface" >/dev/null 2>&1 &&
    die "host interface already exists: $host_iface"
done
[[ ! -e "/var/run/amneziawg/$USERSPACE_IFACE.sock" ]] ||
  die "userspace socket already exists: /var/run/amneziawg/$USERSPACE_IFACE.sock"
grep -qw amneziawg /proc/modules && MODULE_WAS_LOADED=1

WORKDIR="$(mktemp -d /var/lib/wg-guard-phase8-verify.XXXXXX)"
chmod 0700 "$WORKDIR"

cleanup() {
  local rc=$?
  trap - ERR
  set +e
  if [[ -n "$SERVICE_PID" ]]; then
    kill "$SERVICE_PID" >/dev/null 2>&1
    wait "$SERVICE_PID" >/dev/null 2>&1
  fi
  if [[ -n "$USERSPACE_PID" ]]; then
    kill "$USERSPACE_PID" >/dev/null 2>&1
    wait "$USERSPACE_PID" >/dev/null 2>&1
  fi
  if [[ -n "$PROXY_PID" ]]; then
    kill "$PROXY_PID" >/dev/null 2>&1
    wait "$PROXY_PID" >/dev/null 2>&1
  fi
  if [[ -n "$PAYLOAD_PID" ]]; then
    kill "$PAYLOAD_PID" >/dev/null 2>&1
    wait "$PAYLOAD_PID" >/dev/null 2>&1
  fi
  if [[ "$USERSPACE_SOCKET_OWNED" -eq 1 ]]; then
    rm -f -- "/var/run/amneziawg/$USERSPACE_IFACE.sock"
  fi
  if [[ "$RANDOMIZED_NS_OWNED" -eq 1 ]]; then
    ip netns del "$RANDOMIZED_NS" >/dev/null 2>&1
  fi
  if [[ "$RECOMMENDED_NS_OWNED" -eq 1 ]]; then
    ip netns del "$RECOMMENDED_NS" >/dev/null 2>&1
  fi
  if [[ "$SERVER_NS_OWNED" -eq 1 ]]; then
    ip netns del "$SERVER_NS" >/dev/null 2>&1
  fi
  if [[ "$BRIDGE_OWNED" -eq 1 ]]; then
    ip link del dev "$BRIDGE" >/dev/null 2>&1
  fi
  if [[ "$MODULE_LOADED_BY_HARNESS" -eq 1 ]]; then
    modprobe -r amneziawg >/dev/null 2>&1
  fi
  case "$WORKDIR" in
    /var/lib/wg-guard-phase8-verify.*) rm -rf -- "$WORKDIR" ;;
    "") ;;
    *) log "refused unexpected cleanup path: $WORKDIR" >&2 ;;
  esac
  log "cleanup complete (exit $rc)"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP
trap 'rc=$?; log "FAIL: unexpected command failure at line $LINENO (exit $rc)" >&2' ERR

# Version, architecture, and collision validation above is deliberately
# complete before the first host mutation outside the private work directory.
modprobe amneziawg
if [[ "$MODULE_WAS_LOADED" -eq 0 ]]; then
  MODULE_LOADED_BY_HARNESS=1
fi
grep -qw amneziawg /proc/modules || die "amneziawg kernel module is not loaded"
IFS= read -r LOADED_MODULE_VERSION </sys/module/amneziawg/version ||
  die "cannot read the loaded AmneziaWG module version"
[[ "$LOADED_MODULE_VERSION" == "$MODULE_VERSION" ]] ||
  die "loaded AmneziaWG module differs from the pinned installed module"

readonly CONFIG="$WORKDIR/wg-guard.toml"
readonly DATA_DIR="$WORKDIR/data"
readonly DB="$DATA_DIR/wg-guard.db"
readonly SERVICE_LOG="$WORKDIR/service.log"
readonly PROXY_LOG="$WORKDIR/proxy.log"
readonly BASE_URL="http://$SERVER_ADDRESS:$PROXY_PORT"
readonly SERVICE_PATH="$WORKDIR/service-bin"
mkdir -p "$DATA_DIR" "$SERVICE_PATH"
: >"$SERVICE_LOG"

# The service gets only the commands needed for tunnel/firewall operation.
# In particular, ufw/firewall-cmd are absent so an isolated verification run
# cannot mutate their host-level persistent configuration files.
for command in awg ip nft sysctl tc; do
  target="$(command -v "$command" 2>/dev/null || true)"
  [[ -n "$target" ]] && ln -s "$target" "$SERVICE_PATH/$command"
done

cat >"$CONFIG" <<EOF
data_dir = "$DATA_DIR"
database_path = "$DB"
master_key_file = "$DATA_DIR/master.key"
http_listen = "127.0.0.1:$PANEL_PORT"

[tls]
mode = "dev"

[log]
level = "info"
format = "json"

[metrics]
enabled = false
EOF

add_namespace() {
  local ns=$1 host_veth=$2 address=$3 owner_flag=$4
  ip netns add "$ns"
  printf -v "$owner_flag" '%s' 1
  ip link add "$host_veth" type veth peer name eth0 netns "$ns"
  ip link set dev "$host_veth" master "$BRIDGE"
  ip link set dev "$host_veth" up
  ip -n "$ns" link set dev lo up
  ip -n "$ns" address add "$address/24" dev eth0
  ip -n "$ns" link set dev eth0 up
  ip -n "$ns" route add default via 198.18.80.1
}

ip link add "$BRIDGE" type bridge
BRIDGE_OWNED=1
ip address add 198.18.80.1/24 dev "$BRIDGE"
ip link set dev "$BRIDGE" up
add_namespace "$SERVER_NS" "$SERVER_VETH" "$SERVER_ADDRESS" SERVER_NS_OWNED
add_namespace "$RECOMMENDED_NS" "$RECOMMENDED_VETH" "$RECOMMENDED_TRANSPORT" RECOMMENDED_NS_OWNED
add_namespace "$RANDOMIZED_NS" "$RANDOMIZED_VETH" "$RANDOMIZED_TRANSPORT" RANDOMIZED_NS_OWNED

# The application remains in its loopback-only development transport so the
# disposable browser session receives a non-Secure cookie over the SSH tunnel.
# This byte-for-byte TCP relay exposes it only on the isolated server namespace;
# it never listens on a host interface.
ip netns exec "$SERVER_NS" python3 - "$SERVER_ADDRESS" "$PROXY_PORT" \
  127.0.0.1 "$PANEL_PORT" >"$PROXY_LOG" 2>&1 <<'PY' &
import select
import socket
import sys
import threading

listen_host, listen_port, target_host, target_port = sys.argv[1:]

def relay(client):
    upstream = None
    try:
        upstream = socket.create_connection((target_host, int(target_port)), timeout=5)
        client.settimeout(None)
        upstream.settimeout(None)
        peers = {client: upstream, upstream: client}
        while True:
            ready, _, _ = select.select(tuple(peers), (), (), 30)
            if not ready:
                continue
            for source in ready:
                data = source.recv(65536)
                if not data:
                    return
                peers[source].sendall(data)
    except OSError:
        pass
    finally:
        client.close()
        if upstream is not None:
            upstream.close()

listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
listener.bind((listen_host, int(listen_port)))
listener.listen(32)
while True:
    client, _ = listener.accept()
    threading.Thread(target=relay, args=(client,), daemon=True).start()
PY
PROXY_PID=$!
proxy_ready=0
for _ in $(seq 1 20); do
  kill -0 "$PROXY_PID" >/dev/null 2>&1 || die "isolated TCP relay exited during startup"
  if ip netns exec "$SERVER_NS" python3 - "$SERVER_ADDRESS" "$PROXY_PORT" <<'PY'
import socket, sys
with socket.create_connection((sys.argv[1], int(sys.argv[2])), timeout=1):
    pass
PY
  then
    proxy_ready=1
    break
  fi
  sleep 0.1
done
[[ "$proxy_ready" -eq 1 ]] || die "isolated TCP relay did not become ready"

# Seed the range-aware client setting and a least-privilege API token while
# the isolated service is stopped. WGG_IN_CONTAINER bypasses any host install
# shim; all paths still point at the disposable database above.
printf '25-35\n' |
  WGG_IN_CONTAINER=1 "$BIN" settings set network.client_persistent_keepalive -stdin \
    -config "$CONFIG" >/dev/null
WGG_IN_CONTAINER=1 "$BIN" token create \
  -name phase8-vps -expires-in 2h \
  -scopes interfaces.read,interfaces.write,users.read,users.create,users.delete,devices.read,devices.write,configs.read \
  -config "$CONFIG" >"$WORKDIR/api-token" 2>"$WORKDIR/token-create.log"

API_TOKEN="$(tr -d '\r\n' <"$WORKDIR/api-token")"
[[ "$API_TOKEN" == wg_* ]] || die "token creation did not return the expected prefix"
API_CURL_CONFIG="$WORKDIR/api.curl"
cat >"$API_CURL_CONFIG" <<EOF
silent
show-error
fail-with-body
connect-timeout = 5
max-time = 30
header = "Authorization: Bearer $API_TOKEN"
EOF
COOKIE_JAR="$WORKDIR/admin.cookies"

start_service() {
  ip netns exec "$SERVER_NS" env WGG_IN_CONTAINER=1 PATH="$SERVICE_PATH" \
    "$BIN" serve -config "$CONFIG" >>"$SERVICE_LOG" 2>&1 &
  SERVICE_PID=$!
  for _ in $(seq 1 80); do
    kill -0 "$SERVICE_PID" >/dev/null 2>&1 || die "isolated service exited during startup"
    if curl --silent --show-error --fail --max-time 2 "$BASE_URL/healthz" >/dev/null 2>&1; then
      return
    fi
    sleep 0.25
  done
  die "isolated service did not become healthy"
}

stop_service() {
  [[ -n "$SERVICE_PID" ]] || return
  kill "$SERVICE_PID"
  wait "$SERVICE_PID" || true
  SERVICE_PID=""
}

start_service

# Bootstrap a disposable owner only to exercise authenticated admin and
# subscription surfaces. The password and resulting cookie never leave the
# private work directory or appear in argv/output.
cat >"$WORKDIR/onboarding.form" <<EOF
username=phase8owner&password=$OWNER_PASSWORD&password_confirm=$OWNER_PASSWORD&endpoint=$SERVER_ADDRESS
EOF
onboard_code="$(curl --silent --show-error --output "$WORKDIR/onboarding.html" \
  --write-out '%{http_code}' --cookie-jar "$COOKIE_JAR" --cookie "$COOKIE_JAR" \
  --header 'Content-Type: application/x-www-form-urlencoded' \
  --data-binary "@$WORKDIR/onboarding.form" "$BASE_URL/onboarding")"
[[ "$onboard_code" == "303" ]] || die "onboarding returned HTTP $onboard_code"

api_json() {
  local method=$1 path=$2 body=$3 output=$4
  curl --config "$API_CURL_CONFIG" --request "$method" \
    --header 'Content-Type: application/json' --data-binary "@$body" \
    --output "$output" "$BASE_URL$path"
}

api_download() {
  local path=$1 output=$2 headers=$3
  curl --config "$API_CURL_CONFIG" --dump-header "$headers" \
    --output "$output" "$BASE_URL$path"
}

admin_download() {
  local path=$1 output=$2 headers=$3
  curl --silent --show-error --fail --max-time 30 --cookie "$COOKIE_JAR" \
    --dump-header "$headers" --output "$output" "$BASE_URL$path"
}

assert_headers() {
  local headers=$1 media=$2 disposition=$3
  tr -d '\r' <"$headers" >"$headers.normalized"
  grep -Eqi '^Cache-Control:.*no-store' "$headers.normalized" || die "no-store header missing"
  grep -Eqi '^X-Content-Type-Options:[[:space:]]*nosniff' "$headers.normalized" ||
    die "nosniff header missing"
  grep -Eqi '^X-Frame-Options:[[:space:]]*DENY' "$headers.normalized" ||
    die "frame-denial header missing"
  grep -Eqi '^Referrer-Policy:[[:space:]]*(no-referrer|same-origin)' "$headers.normalized" ||
    die "referrer-policy header missing"
  grep -Eqi "^Content-Type:[[:space:]]*$media" "$headers.normalized" ||
    die "unexpected content type"
  grep -Eqi "^Content-Disposition:[[:space:]]*$disposition;.*filename=" "$headers.normalized" ||
    die "unexpected content disposition"
}

normalize_api_state() {
  jq -cS '{
    jc:(.obfuscation.jc|tostring), jmin:(.obfuscation.jmin|tostring),
    jmax:(.obfuscation.jmax|tostring), s1:(.obfuscation.s1|tostring),
    s2:(.obfuscation.s2|tostring), s3:(.obfuscation.s3|tostring),
    s4:(.obfuscation.s4|tostring), h1:(.obfuscation.h1|tostring),
    h2:(.obfuscation.h2|tostring), h3:(.obfuscation.h3|tostring),
    h4:(.obfuscation.h4|tostring),
    i1:(.obfuscation.i1 // ""), i2:(.obfuscation.i2 // ""),
    i3:(.obfuscation.i3 // ""), i4:(.obfuscation.i4 // ""),
    i5:(.obfuscation.i5 // ""),
    header_protection_key_set:.obfuscation.header_protection_key_set,
    content_padding_addition:(.obfuscation.content_padding_addition|tostring),
    rekey_after_time:(.obfuscation.rekey_after_time|tostring),
    rekey_timeout:(.obfuscation.rekey_timeout|tostring),
    reject_after_time:(.obfuscation.reject_after_time|tostring),
    keepalive_timeout:(.obfuscation.keepalive_timeout|tostring),
    max_handshake_attempts:(.obfuscation.max_handshake_attempts|tostring),
    random_trailers:.obfuscation.random_trailers,
    disable_cookies:.obfuscation.disable_cookies
  }' "$1" >"$2"
}

read_db_state() {
  local iface_id=$1 output=$2
  python3 - "$DB" "$iface_id" "$output" <<'PY'
import json, sqlite3, sys
db_path, iface_id, output = sys.argv[1:]
con = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
row = con.execute("""
SELECT jc,jmin,jmax,s1,s2,s3,s4,h1_range,h2_range,h3_range,h4_range,
       h1,h2,h3,h4,i1,i2,i3,i4,i5,header_protection_key <> '',content_padding_addition,
       rekey_after_time,rekey_timeout,reject_after_time,keepalive_timeout,
       max_handshake_attempts,random_trailers,disable_cookies
FROM tunnel_interfaces WHERE id=?
""", (iface_id,)).fetchone()
if row is None:
    raise SystemExit("interface missing from staged database")
def text(value):
    return "0" if value is None or value == "" else str(value)
for canonical, legacy in zip(row[7:11], row[11:15]):
    if int(str(canonical).split("-", 1)[0]) != legacy:
        raise SystemExit("rollback low-bound mirror differs")
state = {
    "jc": text(row[0]), "jmin": text(row[1]), "jmax": text(row[2]),
    "s1": text(row[3]), "s2": text(row[4]), "s3": text(row[5]),
    "s4": text(row[6]), "h1": text(row[7]), "h2": text(row[8]),
    "h3": text(row[9]), "h4": text(row[10]),
    "i1": row[15] or "", "i2": row[16] or "", "i3": row[17] or "",
    "i4": row[18] or "", "i5": row[19] or "",
    "header_protection_key_set": bool(row[20]),
    "content_padding_addition": text(row[21]),
    "rekey_after_time": text(row[22]), "rekey_timeout": text(row[23]),
    "reject_after_time": text(row[24]), "keepalive_timeout": text(row[25]),
    "max_handshake_attempts": text(row[26]),
    "random_trailers": bool(row[27]), "disable_cookies": bool(row[28]),
}
with open(output, "w", encoding="utf-8") as f:
    json.dump(state, f, sort_keys=True, separators=(",", ":"))
    f.write("\n")
PY
}

read_runtime_state() {
  local iface_name=$1 output=$2
  # The raw dump contains private/preshared keys. It is piped directly into
  # the redactor and is never written or emitted.
  ip netns exec "$SERVER_NS" awg show "$iface_name" dump |
    python3 -c '
import json, sys
lines = [line.rstrip("\n").split("\t") for line in sys.stdin]
if not lines or len(lines[0]) != 29:
    raise SystemExit("unexpected AWG dump shape")
f = lines[0]
state = {
    "jc": f[3], "jmin": f[4], "jmax": f[5], "s1": f[6], "s2": f[7],
    "s3": f[8], "s4": f[9], "h1": f[10], "h2": f[11],
    "h3": f[12], "h4": f[13],
    "i1": "" if f[14] == "(null)" else f[14],
    "i2": "" if f[15] == "(null)" else f[15],
    "i3": "" if f[16] == "(null)" else f[16],
    "i4": "" if f[17] == "(null)" else f[17],
    "i5": "" if f[18] == "(null)" else f[18],
    "header_protection_key_set": f[19] not in ("", "(none)"),
    "content_padding_addition": f[20], "rekey_after_time": f[21],
    "rekey_timeout": f[22], "reject_after_time": f[23],
    "keepalive_timeout": f[24], "max_handshake_attempts": f[25],
    "random_trailers": f[26] == "on", "disable_cookies": f[27] == "on",
}
with open(sys.argv[1], "w", encoding="utf-8") as out:
    json.dump(state, out, sort_keys=True, separators=(",", ":"))
    out.write("\n")
' "$output"
}

read_config_state() {
  local config=$1 output=$2
  python3 - "$config" "$output" <<'PY'
import json, pathlib, sys
config_path, output = sys.argv[1:]
values = {}
section = None
for raw in pathlib.Path(config_path).read_text(encoding="utf-8").splitlines():
    line = raw.strip()
    if not line or line.startswith("#"):
        continue
    if line.startswith("[") and line.endswith("]"):
        section = line[1:-1]
        continue
    if section != "Interface" or "=" not in line:
        continue
    key, value = (part.strip() for part in line.split("=", 1))
    if key in values:
        raise SystemExit(f"duplicate client configuration key: {key}")
    values[key] = value
def value(key, default="0"):
    return values.get(key, default)
state = {
    "jc": value("Jc"), "jmin": value("Jmin"), "jmax": value("Jmax"),
    "s1": value("S1"), "s2": value("S2"), "s3": value("S3"), "s4": value("S4"),
    "h1": value("H1"), "h2": value("H2"), "h3": value("H3"), "h4": value("H4"),
    "i1": value("I1", ""), "i2": value("I2", ""), "i3": value("I3", ""),
    "i4": value("I4", ""), "i5": value("I5", ""),
    "header_protection_key_set": "HeaderProtectionKey" in values,
    "content_padding_addition": value("ContentPaddingAddition"),
    "rekey_after_time": value("RekeyAfterTime"), "rekey_timeout": value("RekeyTimeout"),
    "reject_after_time": value("RejectAfterTime"), "keepalive_timeout": value("KeepaliveTimeout"),
    "max_handshake_attempts": value("MaxHandshakeAttempts"),
    "random_trailers": value("RandomTrailers", "off") == "on",
    "disable_cookies": value("DisableCookies", "off") == "on",
}
with open(output, "w", encoding="utf-8") as f:
    json.dump(state, f, sort_keys=True, separators=(",", ":"))
    f.write("\n")
PY
}

extract_panel_tokens() {
  local user_id=$1 prefix=$2
  local detail="$WORKDIR/$prefix.user.html"
  curl --silent --show-error --fail --cookie "$COOKIE_JAR" \
    --output "$detail" "$BASE_URL/users/$user_id"
  python3 - "$detail" "$WORKDIR/$prefix.csrf" <<'PY'
import html, re, sys
body = open(sys.argv[1], encoding="utf-8").read()
match = re.search(r'name="_csrf" value="([^"]+)"', body)
if not match:
    raise SystemExit("CSRF token missing from user detail")
open(sys.argv[2], "w", encoding="utf-8").write(html.unescape(match.group(1)))
PY
  csrf="$(cat "$WORKDIR/$prefix.csrf")"
  # --data-binary preserves every byte; unlike a browser form encoder, a
  # trailing newline would become part of the token and fail CSRF validation.
  printf '_csrf=%s' "$csrf" >"$WORKDIR/$prefix.sub.form"
  code="$(curl --silent --show-error --output "$WORKDIR/$prefix.sub-create.html" \
    --write-out '%{http_code}' --cookie "$COOKIE_JAR" \
    --header 'Content-Type: application/x-www-form-urlencoded' \
    --data-binary "@$WORKDIR/$prefix.sub.form" "$BASE_URL/users/$user_id/sub/create")"
  [[ "$code" == "303" ]] || die "subscription create returned HTTP $code"
  curl --silent --show-error --fail --cookie "$COOKIE_JAR" \
    --output "$detail" "$BASE_URL/users/$user_id"
  python3 - "$detail" "$WORKDIR/$prefix.sub-url" <<'PY'
import html, re, sys
body = open(sys.argv[1], encoding="utf-8").read()
match = re.search(r'id="sub-url"[^>]*value="([^"]+)"', body)
if not match:
    raise SystemExit("subscription URL missing from user detail")
open(sys.argv[2], "w", encoding="utf-8").write(html.unescape(match.group(1)))
PY
}

public_download() {
  local url=$1 output=$2 headers=$3 config=$4
  cat >"$config" <<EOF
silent
show-error
fail-with-body
connect-timeout = 5
max-time = 30
url = "$url"
output = "$output"
dump-header = "$headers"
EOF
  curl --config "$config"
}

assert_config_shape() {
  local policy=$1 config=$2
  grep -Eq '^PrivateKey = [A-Za-z0-9+/]{43}=$' "$config" || die "private-key line missing"
  grep -Eq '^PersistentKeepalive = 25-35$' "$config" || die "keepalive range missing"
  awk '/^\[Peer\]$/{peer=NR} /^H1 = /{h=NR} END{exit !(h && peer && h < peer)}' "$config" ||
    die "AWG interface fields do not precede [Peer]"
  if [[ "$policy" == "recommended" ]]; then
    [[ "$(grep -Ec '^H[1-4] = [0-9]+$' "$config")" -eq 4 ]] ||
      die "recommended scalar headers missing"
    grep -q '^HeaderProtectionKey = ' "$config" && die "recommended profile unexpectedly enables HPK"
  else
    [[ "$(grep -Ec '^H[1-4] = [0-9]+-[0-9]+$' "$config")" -eq 4 ]] ||
      die "randomized H ranges missing"
    grep -q '^HeaderProtectionKey = ' "$config" || die "randomized HPK missing"
    for field in ContentPaddingAddition RekeyAfterTime RekeyTimeout RejectAfterTime KeepaliveTimeout MaxHandshakeAttempts; do
      grep -Eq "^$field = [0-9]+-[0-9]+$" "$config" || die "randomized $field range missing"
    done
  fi
  return 0
}

assert_config_network_state() {
  local config=$1 iface_json=$2 device_json=$3 listen_port=$4
  local address mtu server_key endpoint allowed_ips
  address="$(awk -F ' *= *' '$1=="Address"{print $2; exit}' "$config")"
  mtu="$(awk -F ' *= *' '$1=="MTU"{print $2; exit}' "$config")"
  # Split only the directive's first assignment. Base64 public keys end in
  # padding '=' characters, which a generic equals-sign field separator
  # would silently discard.
  server_key="$(awk '/^PublicKey[[:space:]]*=/{sub(/^[^=]*=[[:space:]]*/, ""); print; exit}' "$config")"
  endpoint="$(awk -F ' *= *' '$1=="Endpoint"{print $2; exit}' "$config")"
  allowed_ips="$(awk -F ' *= *' '$1=="AllowedIPs"{print $2; exit}' "$config")"
  [[ "$address" == "$(jq -er '.ipv4_address' "$device_json")" ]] || die "client Address differs from device API state"
  [[ "$mtu" == "$(jq -er '.mtu|tostring' "$iface_json")" ]] || die "client MTU differs from interface API state"
  [[ "$server_key" == "$(jq -er '.public_key' "$iface_json")" ]] || die "client server key differs from interface API state"
  [[ "$endpoint" == "$SERVER_ADDRESS:$listen_port" ]] || die "client Endpoint differs from interface API state"
  [[ "$allowed_ips" == "0.0.0.0/0" ]] || die "client AllowedIPs differs from node setting"
}

assert_config_crypto_state() {
  local config=$1 iface_json=$2 device_json=$3 iface_name=$4
  local expected_server_key runtime_server_key expected_device_key derived_device_key
  local runtime_peer_key runtime_psk config_psk
  expected_server_key="$(jq -er '.public_key' "$iface_json")"
  runtime_server_key="$(ip netns exec "$SERVER_NS" awg show "$iface_name" public-key)"
  [[ "$runtime_server_key" == "$expected_server_key" ]] ||
    die "server runtime public key differs from interface API state"

  expected_device_key="$(jq -er '.public_key' "$device_json")"
  derived_device_key="$(awk '/^PrivateKey[[:space:]]*=/{sub(/^[^=]*=[[:space:]]*/, ""); print; exit}' "$config" |
    awg pubkey)"
  [[ "$derived_device_key" == "$expected_device_key" ]] ||
    die "client private key does not derive the device API public key"

  read -r runtime_peer_key runtime_psk < <(
    ip netns exec "$SERVER_NS" awg show "$iface_name" dump |
      awk -F '\t' 'NR==2{print $1, $2; exit}'
  )
  [[ "$runtime_peer_key" == "$expected_device_key" ]] ||
    die "server runtime peer differs from the device API public key"
  config_psk="$(awk '/^PresharedKey[[:space:]]*=/{sub(/^[^=]*=[[:space:]]*/, ""); print; exit}' "$config")"
  [[ -n "$config_psk" && "$runtime_psk" == "$config_psk" ]] ||
    die "server runtime peer and client configuration preshared keys differ"
  unset runtime_psk config_psk
}

configure_client() {
  local ns=$1 config=$2 subnet=$3
  local stripped="$config.stripped"
  local address mtu ka
  awg-quick strip "$config" >"$stripped"
  ip -n "$ns" link add p8awg type amneziawg
  ip netns exec "$ns" awg setconf p8awg "$stripped"
  address="$(awk -F ' *= *' '$1=="Address"{print $2; exit}' "$config")"
  mtu="$(awk -F ' *= *' '$1=="MTU"{print $2; exit}' "$config")"
  [[ -n "$address" && -n "$mtu" ]] || die "client Address/MTU missing"
  ip -n "$ns" address add "$address" dev p8awg
  ip -n "$ns" link set dev p8awg mtu "$mtu" up
  ip -n "$ns" route replace "$subnet" dev p8awg
  ka="$(ip netns exec "$ns" awg show p8awg dump |
    awk -F '\t' 'NR==2{print $8}')"
  [[ "$ka" == "25-35" ]] || die "client runtime lost PersistentKeepalive range"
}

wait_for_handshake() {
  local client_ns=$1 server_iface=$2 gateway=$3 client_ip=$4
  local latest
  for _ in $(seq 1 20); do
    ip netns exec "$client_ns" ping -c 1 -W 1 "$gateway" >/dev/null 2>&1 || true
    latest="$(ip netns exec "$SERVER_NS" awg show "$server_iface" latest-handshakes |
      awk 'BEGIN{m=0} $2>m{m=$2} END{print m}')"
    if [[ "${latest:-0}" -gt 0 ]]; then
      ip netns exec "$SERVER_NS" ping -c 2 -W 2 \
        "$client_ip" >/dev/null
      return
    fi
    sleep 1
  done
  local transport endpoint allowed server_port client_keepalive
  if ip netns exec "$client_ns" ping -c 1 -W 1 "$SERVER_ADDRESS" >/dev/null 2>&1; then
    transport="reachable"
  else
    transport="unreachable"
  fi
  endpoint="$(ip netns exec "$client_ns" awg show p8awg endpoints | awk 'NR==1{print $2}')"
  allowed="$(ip netns exec "$SERVER_NS" awg show "$server_iface" allowed-ips | awk 'NR==1{print $2}')"
  server_port="$(ip netns exec "$SERVER_NS" awg show "$server_iface" listen-port)"
  client_keepalive="$(ip netns exec "$client_ns" awg show p8awg persistent-keepalive | awk 'NR==1{print $2, $3}')"
  log "handshake diagnostics: transport=$transport endpoint=$endpoint server_port=$server_port server_allowed=$allowed client_keepalive=$client_keepalive"
  die "handshake did not establish on $server_iface"
}

udp_round_trip() {
  local listener_ns=$1 listener_ip=$2 sender_ns=$3 port=$4 marker=$5
  ip netns exec "$listener_ns" python3 - "$listener_ip" "$port" "$marker" <<'PY' &
import socket, sys
host, port, marker = sys.argv[1], int(sys.argv[2]), sys.argv[3].encode()
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.settimeout(10)
s.bind((host, port))
payload, peer = s.recvfrom(1024)
if payload != marker:
    raise SystemExit("unexpected UDP payload")
s.sendto(b"ack:" + marker, peer)
PY
  PAYLOAD_PID=$!
  sleep 0.2
  ip netns exec "$sender_ns" python3 - "$listener_ip" "$port" "$marker" <<'PY'
import socket, sys
host, port, marker = sys.argv[1], int(sys.argv[2]), sys.argv[3].encode()
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.settimeout(10)
s.sendto(marker, (host, port))
payload, _ = s.recvfrom(1024)
if payload != b"ack:" + marker:
    raise SystemExit("UDP acknowledgement mismatch")
PY
  wait "$PAYLOAD_PID"
  PAYLOAD_PID=""
}

tcp_round_trip() {
  local listener_ns=$1 listener_ip=$2 sender_ns=$3 port=$4 marker=$5
  ip netns exec "$listener_ns" python3 - "$listener_ip" "$port" "$marker" <<'PY' &
import socket, sys
host, port, marker = sys.argv[1], int(sys.argv[2]), sys.argv[3].encode()
listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
listener.bind((host, port))
listener.listen(1)
listener.settimeout(10)
conn, _ = listener.accept()
with conn:
    conn.settimeout(10)
    payload = conn.recv(1024)
    if payload != marker:
        raise SystemExit("unexpected TCP payload")
    conn.sendall(b"ack:" + marker)
PY
  PAYLOAD_PID=$!
  sleep 0.2
  ip netns exec "$sender_ns" python3 - "$listener_ip" "$port" "$marker" <<'PY'
import socket, sys
host, port, marker = sys.argv[1], int(sys.argv[2]), sys.argv[3].encode()
with socket.create_connection((host, port), timeout=10) as connection:
    connection.sendall(marker)
    payload = connection.recv(1024)
if payload != b"ack:" + marker:
    raise SystemExit("TCP acknowledgement mismatch")
PY
  wait "$PAYLOAD_PID"
  PAYLOAD_PID=""
}

verify_profile() {
  local policy=$1 iface_name=$2 listen_port=$3 subnet=$4 client_ns=$5 prefix=$6
  cat >"$WORKDIR/$prefix.iface.request.json" <<EOF
{"name":"$iface_name","listen_port":$listen_port,"ipv4_subnet":"$subnet","mtu":1380,"preset":"$policy","backend_mode":"kernel","endpoint_override":"$SERVER_ADDRESS"}
EOF
  api_json POST /api/v1/interfaces "$WORKDIR/$prefix.iface.request.json" "$WORKDIR/$prefix.iface.json"
  iface_id="$(jq -er '.id' "$WORKDIR/$prefix.iface.json")"
  [[ "$(jq -er '.preset' "$WORKDIR/$prefix.iface.json")" == "$policy" ]] || die "profile policy mismatch"

  cat >"$WORKDIR/$prefix.user.request.json" <<EOF
{"username":"phase8-$prefix","interface_id":"$iface_id","device_limit":1,"start_policy":"immediate","duration_seconds":7200,"enabled":true}
EOF
  api_json POST /api/v1/users "$WORKDIR/$prefix.user.request.json" "$WORKDIR/$prefix.user.json"
  user_id="$(jq -er '.id' "$WORKDIR/$prefix.user.json")"
  cat >"$WORKDIR/$prefix.device.request.json" <<EOF
{"name":"phase8-$prefix-client","interface_id":"$iface_id","preshared_key":true}
EOF
  api_json POST "/api/v1/users/$user_id/devices" "$WORKDIR/$prefix.device.request.json" "$WORKDIR/$prefix.device.json"
  device_id="$(jq -er '.id' "$WORKDIR/$prefix.device.json")"

  normalize_api_state "$WORKDIR/$prefix.iface.json" "$WORKDIR/$prefix.api-state.json"
  read_db_state "$iface_id" "$WORKDIR/$prefix.db-state.json"
  read_runtime_state "$iface_name" "$WORKDIR/$prefix.runtime-state.json"
  cmp -s "$WORKDIR/$prefix.api-state.json" "$WORKDIR/$prefix.db-state.json" ||
    die "$policy API/DB profile state differs"
  cmp -s "$WORKDIR/$prefix.api-state.json" "$WORKDIR/$prefix.runtime-state.json" ||
    die "$policy API/runtime profile state differs"

  api_download "/api/v1/devices/$device_id/config" "$WORKDIR/$prefix.api.conf" "$WORKDIR/$prefix.api-conf.headers"
  api_download "/api/v1/devices/$device_id/qr" "$WORKDIR/$prefix.api.png" "$WORKDIR/$prefix.api-qr.headers"
  assert_headers "$WORKDIR/$prefix.api-conf.headers" 'text/plain' 'attachment'
  assert_headers "$WORKDIR/$prefix.api-qr.headers" 'image/png' 'inline'
  assert_config_shape "$policy" "$WORKDIR/$prefix.api.conf"
  read_config_state "$WORKDIR/$prefix.api.conf" "$WORKDIR/$prefix.config-state.json"
  cmp -s "$WORKDIR/$prefix.api-state.json" "$WORKDIR/$prefix.config-state.json" ||
    die "$policy API/client-config profile state differs"
  assert_config_network_state "$WORKDIR/$prefix.api.conf" "$WORKDIR/$prefix.iface.json" \
    "$WORKDIR/$prefix.device.json" "$listen_port"
  assert_config_crypto_state "$WORKDIR/$prefix.api.conf" "$WORKDIR/$prefix.iface.json" \
    "$WORKDIR/$prefix.device.json" "$iface_name"
  "$QRCHECK" "$WORKDIR/$prefix.api.png" "$WORKDIR/$prefix.api.conf"

  extract_panel_tokens "$user_id" "$prefix"
  admin_download "/devices/$device_id/config" "$WORKDIR/$prefix.admin.conf" "$WORKDIR/$prefix.admin-conf.headers"
  admin_download "/devices/$device_id/qr" "$WORKDIR/$prefix.admin.png" "$WORKDIR/$prefix.admin-qr.headers"
  assert_headers "$WORKDIR/$prefix.admin-conf.headers" 'text/plain' 'attachment'
  assert_headers "$WORKDIR/$prefix.admin-qr.headers" 'image/png' 'inline'
  cmp -s "$WORKDIR/$prefix.api.conf" "$WORKDIR/$prefix.admin.conf" || die "$policy API/admin configs differ"
  "$QRCHECK" "$WORKDIR/$prefix.admin.png" "$WORKDIR/$prefix.api.conf"

  sub_url="$(cat "$WORKDIR/$prefix.sub-url")"
  public_download "$sub_url/devices/$device_id/config" "$WORKDIR/$prefix.sub.conf" \
    "$WORKDIR/$prefix.sub-conf.headers" "$WORKDIR/$prefix.sub-conf.curl"
  public_download "$sub_url/devices/$device_id/qr" "$WORKDIR/$prefix.sub.png" \
    "$WORKDIR/$prefix.sub-qr.headers" "$WORKDIR/$prefix.sub-qr.curl"
  assert_headers "$WORKDIR/$prefix.sub-conf.headers" 'text/plain' 'attachment'
  assert_headers "$WORKDIR/$prefix.sub-qr.headers" 'image/png' 'inline'
  cmp -s "$WORKDIR/$prefix.api.conf" "$WORKDIR/$prefix.sub.conf" || die "$policy API/subscription configs differ"
  "$QRCHECK" "$WORKDIR/$prefix.sub.png" "$WORKDIR/$prefix.api.conf"

  configure_client "$client_ns" "$WORKDIR/$prefix.api.conf" "$subnet"
  gateway="${subnet%0/24}1"
  client_ip="$(jq -er '.ipv4_address' "$WORKDIR/$prefix.device.json" | cut -d/ -f1)"
  wait_for_handshake "$client_ns" "$iface_name" "$gateway" "$client_ip"
  ip netns exec "$SERVER_NS" ping -c 2 -W 2 "$client_ip" >/dev/null
  udp_round_trip "$SERVER_NS" "$gateway" "$client_ns" "$((listen_port + 200))" "phase8-$prefix-client-to-server"
  udp_round_trip "$client_ns" "$client_ip" "$SERVER_NS" "$((listen_port + 201))" "phase8-$prefix-server-to-client"
  tcp_round_trip "$SERVER_NS" "$gateway" "$client_ns" "$((listen_port + 300))" "phase8-$prefix-client-to-server"
  tcp_round_trip "$client_ns" "$client_ip" "$SERVER_NS" "$((listen_port + 301))" "phase8-$prefix-server-to-client"

  state_hash="$(sha256sum "$WORKDIR/$prefix.api-state.json" | awk '{print $1}')"
  config_hash="$(sha256sum "$WORKDIR/$prefix.api.conf" | awk '{print $1}')"
  log "$policy verified: profile_sha256=$state_hash config_sha256=$config_hash"
  printf '%s\n' "$user_id" >"$WORKDIR/$prefix.user-id"
  printf '%s\n' "$device_id" >"$WORKDIR/$prefix.device-id"
}

verify_profile recommended "$RECOMMENDED_IFACE" "$RECOMMENDED_PORT" \
  "$RECOMMENDED_SUBNET" "$RECOMMENDED_NS" recommended
verify_profile randomized "$RANDOMIZED_IFACE" "$RANDOMIZED_PORT" \
  "$RANDOMIZED_SUBNET" "$RANDOMIZED_NS" randomized

# Repeat the safe recommended profile through the pinned userspace daemon.
# Capture is direct-to-0600 file; raw private/PSK material is never emitted.
ip netns exec "$SERVER_NS" awg showconf "$RECOMMENDED_IFACE" >"$WORKDIR/recommended.server.conf"
ip netns exec "$SERVER_NS" awg showconf "$RANDOMIZED_IFACE" >"$WORKDIR/randomized.server.conf"
# The pinned kernel synthesizes AdvancedSecurity for every peer in showconf
# even though its setter does not consume/store the flag. The pinned userspace
# UAPI rejects that unsupported field. Remove only this known phantom so the
# fallback gate exercises the complete intersection of supported parameters.
awk '!/^[[:space:]]*AdvancedSecurity[[:space:]]*=/' \
  "$WORKDIR/recommended.server.conf" >"$WORKDIR/recommended.userspace.conf"
stop_service
ip -n "$SERVER_NS" link del "$RECOMMENDED_IFACE"
ip netns exec "$SERVER_NS" env WG_PROCESS_FOREGROUND=1 \
  "$(command -v amneziawg-go)" "$USERSPACE_IFACE" >"$WORKDIR/userspace.log" 2>&1 &
USERSPACE_PID=$!
for _ in $(seq 1 40); do
  kill -0 "$USERSPACE_PID" >/dev/null 2>&1 || die "userspace daemon exited during startup"
  if [[ -S "/var/run/amneziawg/$USERSPACE_IFACE.sock" ]]; then
    USERSPACE_SOCKET_OWNED=1
    break
  fi
  sleep 0.25
done
[[ -S "/var/run/amneziawg/$USERSPACE_IFACE.sock" ]] || die "userspace UAPI socket did not appear"
ip netns exec "$SERVER_NS" awg setconf "$USERSPACE_IFACE" "$WORKDIR/recommended.userspace.conf"
ip -n "$SERVER_NS" address add 10.246.80.1/24 dev "$USERSPACE_IFACE"
ip -n "$SERVER_NS" link set dev "$USERSPACE_IFACE" mtu 1380 up
read_runtime_state "$USERSPACE_IFACE" "$WORKDIR/recommended.userspace-state.json"
cmp -s "$WORKDIR/recommended.api-state.json" "$WORKDIR/recommended.userspace-state.json" ||
  die "recommended kernel/userspace profile state differs"
wait_for_handshake "$RECOMMENDED_NS" "$USERSPACE_IFACE" 10.246.80.1 10.246.80.2
udp_round_trip "$SERVER_NS" 10.246.80.1 "$RECOMMENDED_NS" 48980 "phase8-userspace-client-to-server"
udp_round_trip "$RECOMMENDED_NS" 10.246.80.2 "$SERVER_NS" 48981 "phase8-userspace-server-to-client"
tcp_round_trip "$SERVER_NS" 10.246.80.1 "$RECOMMENDED_NS" 48982 "phase8-userspace-client-to-server"
tcp_round_trip "$RECOMMENDED_NS" 10.246.80.2 "$SERVER_NS" 48983 "phase8-userspace-server-to-client"
kill "$USERSPACE_PID"
wait "$USERSPACE_PID" || true
USERSPACE_PID=""
rm -f -- "/var/run/amneziawg/$USERSPACE_IFACE.sock"
USERSPACE_SOCKET_OWNED=0
log "recommended userspace fallback traffic verified"

# Restore the isolated kernel-backed node for browser inspection.
start_service
for iface_name in "$RECOMMENDED_IFACE" "$RANDOMIZED_IFACE"; do
  ip netns exec "$SERVER_NS" awg show "$iface_name" >/dev/null ||
    die "kernel interface was not restored after userspace verification"
done

# The exact secret patterns must not appear in service diagnostics. grep reads
# them from a file so they never become process arguments.
python3 - "$WORKDIR/api-token" "$WORKDIR/recommended.sub-url" \
  "$WORKDIR/randomized.sub-url" "$WORKDIR/recommended.api.conf" \
  "$WORKDIR/randomized.api.conf" "$WORKDIR/recommended.server.conf" \
  "$WORKDIR/randomized.server.conf" >"$WORKDIR/secret-patterns" <<'PY'
import pathlib, re, sys, urllib.parse
print(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8").strip())
for path in sys.argv[2:4]:
    url = pathlib.Path(path).read_text(encoding="utf-8").strip()
    print(url)
    print(urllib.parse.urlsplit(url).path.rsplit("/", 1)[-1])
for path in sys.argv[4:]:
    for line in pathlib.Path(path).read_text(encoding="utf-8").splitlines():
        match = re.match(r"^(?:PrivateKey|PresharedKey|HeaderProtectionKey) = (.+)$", line)
        if match:
            print(match.group(1))
PY
printf '%s\n' "$OWNER_PASSWORD" >>"$WORKDIR/secret-patterns"
for diagnostic in "$SERVICE_LOG" "$WORKDIR/token-create.log" \
  "$WORKDIR/userspace.log" "$PROXY_LOG"; do
  if grep -Fq -f "$WORKDIR/secret-patterns" "$diagnostic"; then
    die "a credential appeared in a verification diagnostic log"
  fi
done

log "environment: Ubuntu $VERSION_ID $(uname -m), kernel $(uname -r), module $MODULE_VERSION"
log "packages: tools $TOOLS_PACKAGE, dkms $DKMS_PACKAGE"
log "candidate: $($BIN version)"
log "candidate_sha256=$(sha256sum "$BIN" | awk '{print $1}')"
log "userspace: $EXPECTED_USERSPACE commit ${EXPECTED_USERSPACE_COMMIT:0:8}"
log "userspace_sha256=$USERSPACE_SHA256"
log "browser admin paths: /users/$(cat "$WORKDIR/recommended.user-id") and /users/$(cat "$WORKDIR/randomized.user-id")"
log "READY: isolated panel $BASE_URL (no subscription capability printed)"

if [[ "$HOLD" -eq 1 ]]; then
  log "holding for browser QA; send SIGINT/SIGTERM to clean up"
  while :; do sleep 30; done
fi
