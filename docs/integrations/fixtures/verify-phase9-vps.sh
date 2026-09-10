#!/usr/bin/env bash
# Phase 9 real-host observability gate. Run only on the dedicated disposable
# Ubuntu 24.04 amd64 VPS after installing the exact candidate in --mode.
set -Eeuo pipefail
umask 077

MODE=""
BASE_URL="http://127.0.0.1:8080"
ADMIN_USER=""
ADMIN_PASSWORD_FILE=""
CANDIDATE=""
RUN_FAILURES=0

log() { printf 'phase9-vps: %s\n' "$*"; }
die() { log "FAIL: $*" >&2; exit 1; }

while (($#)); do
  case "$1" in
    --mode) [[ $# -ge 2 ]] || die "--mode requires native or docker"; MODE="$2"; shift 2 ;;
    --base-url) [[ $# -ge 2 ]] || die "--base-url requires a loopback URL"; BASE_URL="$2"; shift 2 ;;
    --admin-user) [[ $# -ge 2 ]] || die "--admin-user requires a value"; ADMIN_USER="$2"; shift 2 ;;
    --admin-password-file) [[ $# -ge 2 ]] || die "--admin-password-file requires a path"; ADMIN_PASSWORD_FILE="$2"; shift 2 ;;
    --candidate) [[ $# -ge 2 ]] || die "--candidate requires a path"; CANDIDATE="$2"; shift 2 ;;
    --failure-drills) RUN_FAILURES=1; shift ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ "$MODE" == native || "$MODE" == docker ]] || die "--mode must be native or docker"
[[ ${EUID:-$(id -u)} -eq 0 ]] || die "root is required"
[[ "$BASE_URL" =~ ^http://127\.0\.0\.1:[0-9]{1,5}$ ]] || die "--base-url must be loopback HTTP"
if [[ -n "$ADMIN_PASSWORD_FILE" ]]; then
  [[ -n "$ADMIN_USER" && -f "$ADMIN_PASSWORD_FILE" ]] || die "admin user/private password file must be paired"
  [[ "$(stat -c %a "$ADMIN_PASSWORD_FILE")" == 600 ]] || die "admin password file must be 0600"
fi
if [[ -n "$CANDIDATE" ]]; then
  [[ -x "$CANDIDATE" ]] || die "candidate is not executable"
  CANDIDATE="$(readlink -f -- "$CANDIDATE")"
fi

for command in awk chmod cp curl date getconf grep head ip mktemp mv python3 readlink rm sed seq sort stat systemctl systemd-tmpfiles timeout touch tr wc awg; do
  command -v "$command" >/dev/null || die "missing required command: $command"
done
if [[ "$MODE" == native ]]; then
  command -v systemd-analyze >/dev/null || die "missing required command: systemd-analyze"
else
  command -v docker >/dev/null || die "missing required command: docker"
fi
source /etc/os-release
[[ "${ID:-}" == ubuntu && "${VERSION_ID:-}" == 24.04 ]] || die "Ubuntu 24.04 is required"
[[ "$(uname -m)" == x86_64 ]] || die "amd64/x86_64 is required"

INSTALLED_MODE="$(python3 - /etc/wg-guard/install-state.json <<'PY'
import json, sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["mode"])
PY
)"
[[ "$INSTALLED_MODE" == "$MODE" ]] || die "installed mode is $INSTALLED_MODE, expected $MODE"
curl --silent --show-error --fail "$BASE_URL/readyz" >/dev/null || die "node is not ready"

readonly NS="wgg-p9-client"
readonly HOST_VETH="p9vethh"
readonly CLIENT_IFACE="p9awg"
readonly SERVER_IFACE="awg6"
readonly TRANSPORT_HOST="198.18.90.1"
readonly TRANSPORT_CLIENT="198.18.90.2"
readonly VPN_GATEWAY="10.246.91.1"
readonly LISTEN_PORT="48991"

WORKDIR="$(mktemp -d /var/lib/wg-guard-phase9-verify.XXXXXX)"
chmod 0700 "$WORKDIR"
readonly TEST_USERNAME="phase9-${WORKDIR##*.}"
TOKEN_ID=""
INTERFACE_ID=""
USER_ID=""
DEVICE_ID=""
NS_OWNED=0
VETH_OWNED=0
SERVER_OWNED=0
AWG_HIDDEN=0
TC_HIDDEN=0
DOCKER_WRAPPER=0
LOAD_PID=""
TRAFFIC_PID=""

restore_tool() {
  local tool=$1
  if [[ "$MODE" == native ]]; then
    [[ -e "/usr/local/bin/$tool.phase9-hold" ]] && mv "/usr/local/bin/$tool.phase9-hold" "/usr/local/bin/$tool"
    [[ -e "/usr/sbin/$tool.phase9-hold" ]] && mv "/usr/sbin/$tool.phase9-hold" "/usr/sbin/$tool"
  else
    docker exec wg-guard sh -c "test ! -e /usr/local/bin/$tool.phase9-hold || mv /usr/local/bin/$tool.phase9-hold /usr/local/bin/$tool" >/dev/null 2>&1 || true
    docker exec wg-guard sh -c "test ! -e /usr/sbin/$tool.phase9-hold || mv /usr/sbin/$tool.phase9-hold /usr/sbin/$tool" >/dev/null 2>&1 || true
  fi
  return 0
}

api() {
  local method=$1 path=$2 output=$3 body=${4:-}
  local args=(--config "$WORKDIR/api.curl" --request "$method" --output "$output")
  if [[ -n "$body" ]]; then
    args+=(--header 'Content-Type: application/json' --data-binary "@$body")
  fi
  local rc=0
  curl "${args[@]}" "$BASE_URL$path" || rc=$?
  if ((rc)); then
    local code="unknown" message=""
    if [[ -s "$output" ]]; then
      readarray -t error_fields < <(python3 - "$output" <<'PY'
import json, sys
try:
    error=json.load(open(sys.argv[1], encoding="utf-8")).get("error", {})
    print(error.get("code", "unknown"))
    print(str(error.get("message", "")).replace("\n", " ")[:160])
except Exception:
    print("unreadable")
    print("")
PY
)
      code="${error_fields[0]:-unknown}"
      message="${error_fields[1]:-}"
    fi
    log "API $method $path failed code=$code message=$message curl_exit=$rc" >&2
    return "$rc"
  fi
}

cleanup() {
  local rc=$?
  trap - ERR
  set +e
  [[ -n "$LOAD_PID" ]] && kill "$LOAD_PID" >/dev/null 2>&1
  [[ -n "$TRAFFIC_PID" ]] && kill "$TRAFFIC_PID" >/dev/null 2>&1
  ((AWG_HIDDEN)) && restore_tool awg
  ((TC_HIDDEN)) && restore_tool tc
  if ((DOCKER_WRAPPER)); then
    rm -f -- /usr/local/bin/docker /run/wg-guard-phase9-docker-fail-once
  fi
  if [[ -s "$WORKDIR/api.curl" ]]; then
    [[ -n "$DEVICE_ID" ]] && api DELETE "/api/v1/devices/$DEVICE_ID" "$WORKDIR/cleanup-device.json" >/dev/null 2>&1
    [[ -n "$USER_ID" ]] && api DELETE "/api/v1/users/$USER_ID" "$WORKDIR/cleanup-user.json" >/dev/null 2>&1
    [[ -n "$INTERFACE_ID" ]] && api DELETE "/api/v1/interfaces/$INTERFACE_ID" "$WORKDIR/cleanup-interface.json" >/dev/null 2>&1
  fi
  if ((SERVER_OWNED)); then ip link del "$SERVER_IFACE" >/dev/null 2>&1; fi
  if ((NS_OWNED)); then ip netns del "$NS" >/dev/null 2>&1; fi
  if ((VETH_OWNED)); then ip link del "$HOST_VETH" >/dev/null 2>&1; fi
  [[ -n "$TOKEN_ID" ]] && /usr/local/bin/wg-guard token revoke "$TOKEN_ID" >/dev/null 2>&1
  case "$WORKDIR" in
    /var/lib/wg-guard-phase9-verify.*) rm -rf -- "$WORKDIR" ;;
    *) log "refused unexpected cleanup path: $WORKDIR" ;;
  esac
  log "cleanup complete (exit $rc)"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP
trap 'rc=$?; log "FAIL at line $LINENO (exit $rc)" >&2' ERR

log "environment mode=$MODE os=$ID-$VERSION_ID arch=$(uname -m) kernel=$(uname -r)"

if [[ "$MODE" == native ]]; then
  grep -Fxq 'LogNamespace=wg-guard' /etc/systemd/system/wg-guard.service || die "unit has no log namespace"
  for setting in MaxRetentionSec=7day MaxFileSec=1day SystemMaxUse=128M RuntimeMaxUse=64M; do
    systemd-analyze cat-config systemd/journald@wg-guard.conf | grep -Fxq "$setting" || die "missing journal setting $setting"
  done
  systemctl is-active --quiet systemd-journald@wg-guard.service || die "journal namespace is inactive"
else
  [[ "$(docker inspect --format '{{.HostConfig.LogConfig.Type}}' wg-guard)" == local ]] || die "Docker local log driver is not active"
  LOG_CONFIG="$(docker inspect --format '{{json .HostConfig.LogConfig.Config}}' wg-guard)"
  for setting in '"max-size":"16m"' '"max-file":"8"' '"compress":"true"'; do
    grep -Fq "$setting" <<<"$LOG_CONFIG" || die "Docker log option is missing: $setting"
  done
fi

grep -Fxq 'd /var/lib/wg-guard/operations 0700 root root m:7d -' /etc/tmpfiles.d/wg-guard-operations.conf || die "tmpfiles policy differs"
[[ "$(stat -c %a /var/lib/wg-guard/operations)" == 700 ]] || die "operation directory mode differs"
OLD_DATE="$(date -u -d '8 days ago' +%F)"
OLD_FILE="/var/lib/wg-guard/operations/operations-$OLD_DATE.jsonl"
printf '%s\n' 'expired synthetic record' >"$OLD_FILE"
chmod 0600 "$OLD_FILE"
touch -d '8 days ago' "$OLD_FILE"
systemd-tmpfiles --clean /etc/tmpfiles.d/wg-guard-operations.conf
[[ ! -e "$OLD_FILE" ]] || die "tmpfiles did not remove an expired operation file"

/usr/local/bin/wg-guard logs --tail 5 --since 1h >"$WORKDIR/service.log"
[[ -s "$WORKDIR/service.log" ]] || die "service log command returned no records"
/usr/local/bin/wg-guard logs --source operations --since 7d >"$WORKDIR/operations.log"
grep -q '"source":"operations"' "$WORKDIR/operations.log" || die "operation source returned no canonical record"
for invalid in '--tail 10001' '--since 8d' '--component database'; do
  # shellcheck disable=SC2086 -- fixed local corpus, never user input
  if /usr/local/bin/wg-guard logs $invalid >/dev/null 2>&1; then die "invalid logs input accepted: $invalid"; fi
done
timeout --preserve-status --signal=INT 3s /usr/local/bin/wg-guard logs --tail 1 --since 1h --follow >"$WORKDIR/follow.log"
log "CLI logs verified follow_exit=0 service_lines=$(wc -l <"$WORKDIR/service.log") operation_lines=$(wc -l <"$WORKDIR/operations.log")"

if [[ "$MODE" == native ]]; then
  BEFORE_RESTARTS="$(systemctl show wg-guard.service -p NRestarts --value)"
  systemctl kill --signal=SIGKILL --kill-whom=main wg-guard.service
else
  BEFORE_RESTARTS="$(docker inspect --format '{{.RestartCount}}' wg-guard)"
  CONTAINER_PID="$(docker inspect --format '{{.State.Pid}}' wg-guard)"
  [[ "$CONTAINER_PID" =~ ^[1-9][0-9]*$ ]] || die "container PID is unavailable"
  kill -KILL "$CONTAINER_PID"
fi
for _ in $(seq 1 40); do
  sleep 1
  curl --silent --fail "$BASE_URL/readyz" >/dev/null 2>&1 && break
done
curl --silent --show-error --fail "$BASE_URL/readyz" >/dev/null || die "service did not recover from a forced crash"
if [[ "$MODE" == native ]]; then
  AFTER_RESTARTS="$(systemctl show wg-guard.service -p NRestarts --value)"
else
  AFTER_RESTARTS="$(docker inspect --format '{{.RestartCount}}' wg-guard)"
fi
[[ "$AFTER_RESTARTS" -gt "$BEFORE_RESTARTS" ]] || die "restart counter did not advance"
log "forced crash recovered restart_count=$BEFORE_RESTARTS->$AFTER_RESTARTS"

/usr/local/bin/wg-guard token create -name phase9-vps -expires-in 2h \
  -scopes stats.read,interfaces.read,interfaces.write,users.read,users.create,users.update,users.delete,devices.read,devices.write,configs.read \
  >"$WORKDIR/token" 2>"$WORKDIR/token.meta"
TOKEN_ID="$(awk '/^id:/{print $2; exit}' "$WORKDIR/token.meta")"
[[ -n "$TOKEN_ID" && "$(head -1 "$WORKDIR/token")" == wg_* ]] || die "API token creation failed"
python3 - "$WORKDIR/token" "$WORKDIR/api.curl" <<'PY'
import pathlib, sys
token = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8").strip()
pathlib.Path(sys.argv[2]).write_text(
    'silent\nshow-error\nfail-with-body\nconnect-timeout = 5\nmax-time = 30\n'
    f'header = "Authorization: Bearer {token}"\n', encoding="utf-8")
PY
chmod 0600 "$WORKDIR/api.curl"

ip netns list | awk '{print $1}' | grep -Fxq "$NS" && die "test namespace already exists"
ip link show "$HOST_VETH" >/dev/null 2>&1 && die "test veth already exists"
ip link show "$SERVER_IFACE" >/dev/null 2>&1 && die "test AWG interface already exists"
ip netns add "$NS"; NS_OWNED=1
ip link add "$HOST_VETH" type veth peer name eth0 netns "$NS"; VETH_OWNED=1
ip address add "$TRANSPORT_HOST/30" dev "$HOST_VETH"
ip link set "$HOST_VETH" up
ip -n "$NS" link set lo up
ip -n "$NS" address add "$TRANSPORT_CLIENT/30" dev eth0
ip -n "$NS" link set eth0 up
ip -n "$NS" route add default via "$TRANSPORT_HOST"

python3 - "$WORKDIR/interface.request.json" <<PY
import json, sys
json.dump({"name":"$SERVER_IFACE","listen_port":$LISTEN_PORT,"ipv4_subnet":"10.246.91.0/24","mtu":1380,"preset":"recommended","backend_mode":"kernel","endpoint_override":"$TRANSPORT_HOST"}, open(sys.argv[1], "w"))
PY
api POST /api/v1/interfaces "$WORKDIR/interface.json" "$WORKDIR/interface.request.json"
SERVER_OWNED=1
INTERFACE_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$WORKDIR/interface.json")"
python3 - "$INTERFACE_ID" "$TEST_USERNAME" "$WORKDIR/user.request.json" <<'PY'
import json, sys
json.dump({"username":sys.argv[2],"interface_id":sys.argv[1],"device_limit":1,"start_policy":"immediate","duration_seconds":7200,"speed_limit_down_kbps":2048,"speed_limit_up_kbps":2048,"enabled":True}, open(sys.argv[3], "w"))
PY
api POST /api/v1/users "$WORKDIR/user.json" "$WORKDIR/user.request.json"
USER_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$WORKDIR/user.json")"
python3 - "$INTERFACE_ID" "$WORKDIR/device.request.json" <<'PY'
import json, sys
json.dump({"name":"phase9-client","interface_id":sys.argv[1],"preshared_key":True}, open(sys.argv[2], "w"))
PY
api POST "/api/v1/users/$USER_ID/devices" "$WORKDIR/device.json" "$WORKDIR/device.request.json"
DEVICE_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$WORKDIR/device.json")"
curl --config "$WORKDIR/api.curl" --output "$WORKDIR/client.conf" "$BASE_URL/api/v1/devices/$DEVICE_ID/config"

awk '!/^[[:space:]]*(Address|DNS|MTU)[[:space:]]*=/' "$WORKDIR/client.conf" >"$WORKDIR/client.stripped.conf"
ip -n "$NS" link add "$CLIENT_IFACE" type amneziawg
ip netns exec "$NS" awg setconf "$CLIENT_IFACE" "$WORKDIR/client.stripped.conf"
CLIENT_ADDRESS="$(awk -F ' *= *' '$1=="Address"{print $2; exit}' "$WORKDIR/client.conf")"
ip -n "$NS" address add "$CLIENT_ADDRESS" dev "$CLIENT_IFACE"
ip -n "$NS" link set "$CLIENT_IFACE" mtu 1380 up
ip -n "$NS" route add 10.246.91.0/24 dev "$CLIENT_IFACE"
for _ in $(seq 1 25); do
  ip netns exec "$NS" ping -c 1 -W 1 "$VPN_GATEWAY" >/dev/null 2>&1 || true
  HANDSHAKE="$(awg show "$SERVER_IFACE" latest-handshakes | awk 'BEGIN{m=0} $2>m{m=$2} END{print m}')"
  [[ "${HANDSHAKE:-0}" -gt 0 ]] && break
  sleep 1
done
[[ "${HANDSHAKE:-0}" -gt 0 ]] || die "real AmneziaWG handshake did not establish"

ip netns exec "$NS" ping -i 0.05 -s 1200 -w 45 "$VPN_GATEWAY" >/dev/null & TRAFFIC_PID=$!
# A rate needs two consecutive samples from the same counter generation. Waiting
# for slightly more than two cadences avoids coupling the gate to scheduler phase.
sleep 22
api GET '/api/v1/node/telemetry?points=180' "$WORKDIR/telemetry-traffic.json"
MAIN_PID="$(systemctl show wg-guard.service -p MainPID --value)"
if [[ "$MODE" == docker ]]; then MAIN_PID="$(docker inspect --format '{{.State.Pid}}' wg-guard)"; fi
python3 - "$WORKDIR/telemetry-traffic.json" "/proc/$MAIN_PID/status" <<'PY'
import json, pathlib, sys
d=json.load(open(sys.argv[1])); p=d["latest"]
assert d["cadence_seconds"] == 10 and len(d["points"]) >= 2
assert p["health"] == "healthy", p
assert p["vpn_rx_bytes_per_second"] is not None and p["vpn_tx_bytes_per_second"] is not None
assert p["vpn_rx_bytes_per_second"] + p["vpn_tx_bytes_per_second"] > 0
mem={}
for line in pathlib.Path('/proc/meminfo').read_text().splitlines():
    if ':' in line: mem[line.split(':',1)[0]]=int(line.split()[1])*1024
host_pct=100*(mem['MemTotal']-mem['MemAvailable'])/mem['MemTotal']
assert abs(host_pct-p["memory_percent"]) < 5
rss=int(next(x.split()[1] for x in pathlib.Path(sys.argv[2]).read_text().splitlines() if x.startswith('VmRSS:')))*1024
assert abs(rss-p["process_rss_bytes"]) < 32*1024*1024
print(f'TELEMETRY_TRAFFIC health={p["health"]} points={len(d["points"])} vpn_rate={p["vpn_rx_bytes_per_second"]+p["vpn_tx_bytes_per_second"]:.1f} memory_delta={abs(host_pct-p["memory_percent"]):.2f}')
PY
wait "$TRAFFIC_PID"; TRAFFIC_PID=""
sleep 12
api GET '/api/v1/node/telemetry?points=180' "$WORKDIR/telemetry-activity.json"
python3 - "$WORKDIR/telemetry-activity.json" <<'PY'
import datetime, json, sys
d=json.load(open(sys.argv[1])); p=d['latest']; points=d['points']
assert p['online_users'] >= 1 and p['active_peers'] >= 1
times=[datetime.datetime.fromisoformat(x['timestamp'].replace('Z','+00:00')) for x in points]
gaps=[(b-a).total_seconds() for a,b in zip(times,times[1:])]
assert not gaps or max(gaps) < 16, gaps
print(f'TELEMETRY_ACTIVITY online={p["online_users"]} peers={p["active_peers"]} max_gap={max(gaps) if gaps else 0:.2f}')
PY

python3 - <<'PY' &
import hashlib, time
blob=bytearray(64*1024*1024); end=time.time()+25
while time.time()<end: hashlib.sha256(blob).digest()
PY
LOAD_PID=$!
sleep 22
api GET '/api/v1/node/telemetry?points=5' "$WORKDIR/telemetry-load.json"
wait "$LOAD_PID"; LOAD_PID=""
python3 - "$WORKDIR/telemetry-load.json" <<'PY'
import json, sys
p=json.load(open(sys.argv[1]))['latest']
assert p['cpu_percent'] is not None and p['cpu_percent'] > 1
assert p['memory_used_bytes'] and p['load_1'] is not None
print(f'TELEMETRY_LOAD cpu={p["cpu_percent"]:.2f} memory={p["memory_used_bytes"]} load1={p["load_1"]:.2f}')
PY

START_TICKS="$(awk '{print $14+$15}' "/proc/$MAIN_PID/stat")"; START_TIME="$(date +%s%N)"
sleep 30
END_TICKS="$(awk '{print $14+$15}' "/proc/$MAIN_PID/stat")"; END_TIME="$(date +%s%N)"
CPU_PCT="$(awk -v ticks="$((END_TICKS-START_TICKS))" -v ns="$((END_TIME-START_TIME))" -v hz="$(getconf CLK_TCK)" 'BEGIN{printf "%.3f",100*ticks/hz/(ns/1000000000)}')"
RSS_KIB="$(awk '/^VmRSS:/{print $2}' "/proc/$MAIN_PID/status")"
for _ in $(seq 1 20); do curl --config "$WORKDIR/api.curl" --output /dev/null --write-out '%{time_total}\n' "$BASE_URL/api/v1/node/telemetry?points=1"; done >"$WORKDIR/latency"
P95="$(sort -n "$WORKDIR/latency" | sed -n '19p')"
log "resources idle_cpu=${CPU_PCT}% rss_kib=$RSS_KIB api_p95=${P95}s"

if [[ -n "$ADMIN_PASSWORD_FILE" ]]; then
  python3 - "$ADMIN_USER" "$ADMIN_PASSWORD_FILE" "$WORKDIR/login.form" <<'PY'
import pathlib, sys, urllib.parse
password=pathlib.Path(sys.argv[2]).read_text().strip()
pathlib.Path(sys.argv[3]).write_text(urllib.parse.urlencode({'username':sys.argv[1],'password':password,'next':'/dashboard'}))
PY
  LOGIN_CODE="$(curl --silent --show-error --output "$WORKDIR/login.html" --write-out '%{http_code}' --cookie-jar "$WORKDIR/cookies" --header 'Content-Type: application/x-www-form-urlencoded' --data-binary "@$WORKDIR/login.form" "$BASE_URL/login")"
  [[ "$LOGIN_CODE" == 303 ]] || die "dashboard login returned $LOGIN_CODE"
  curl --silent --show-error --fail --cookie "$WORKDIR/cookies" --output "$WORKDIR/dashboard-live.html" "$BASE_URL/dashboard/live"
  grep -q 'id="telemetry-card"' "$WORKDIR/dashboard-live.html" || die "live dashboard has no telemetry card"
  [[ "$(grep -c 'class="ops-metric"' "$WORKDIR/dashboard-live.html")" -eq 4 ]] || die "live dashboard card count differs"
  grep -q 'data-health="healthy"' "$WORKDIR/dashboard-live.html" || die "live dashboard health differs from API"
  log "dashboard verified bytes=$(wc -c <"$WORKDIR/dashboard-live.html") cards=4"
fi

ip link del "$SERVER_IFACE"
sleep 12
api GET '/api/v1/node/telemetry?points=3' "$WORKDIR/telemetry-missing.json"
python3 - "$WORKDIR/telemetry-missing.json" <<'PY'
import json, sys
p=json.load(open(sys.argv[1]))['latest']
assert p['health']=='degraded' and 'awg_interface_missing' in p['issues'], p
print('TELEMETRY_MISSING health=degraded issue=awg_interface_missing')
PY
/usr/local/bin/wg-guard restart --yes >/dev/null
ip link show "$SERVER_IFACE" >/dev/null || die "restart did not reconcile the missing interface"

if ((RUN_FAILURES)); then
  if [[ "$MODE" == native ]]; then
    mv /usr/local/bin/awg /usr/local/bin/awg.phase9-hold
  else
    docker exec wg-guard mv /usr/local/bin/awg /usr/local/bin/awg.phase9-hold
  fi
  AWG_HIDDEN=1
  sleep 35
  restore_tool awg; AWG_HIDDEN=0
  /usr/local/bin/wg-guard logs --since 2m --component awg >"$WORKDIR/awg-errors.log"
  grep -q 'accounting cycle interface error' "$WORKDIR/awg-errors.log" || die "AWG failure was not classified"

  python3 - "$WORKDIR/user-speed.request.json" <<'PY'
import json, sys
json.dump({"speed_limit_down_kbps":3072,"speed_limit_up_kbps":3072}, open(sys.argv[1], "w"))
PY
  if [[ "$MODE" == native ]]; then
    mv /usr/sbin/tc /usr/sbin/tc.phase9-hold
  else
    docker exec wg-guard mv /usr/sbin/tc /usr/sbin/tc.phase9-hold
  fi
  TC_HIDDEN=1
  api PATCH "/api/v1/users/$USER_ID" "$WORKDIR/user-speed.json" "$WORKDIR/user-speed.request.json"
  sleep 35
  restore_tool tc; TC_HIDDEN=0
  /usr/local/bin/wg-guard logs --since 2m --component network >"$WORKDIR/network-errors.log"
  grep -q 'shaper refresh failed' "$WORKDIR/network-errors.log" || die "network failure was not classified"
  log "AWG and network failure classification verified"
fi

CAPABILITY="phase9SyntheticCapability0123456789_-"
# Unknown API routes are access-logged; the public subscription surface
# intentionally does not log expected invalid-token 404s.
curl --silent --output /dev/null "$BASE_URL/api/v1/phase9/sub/$CAPABILITY" || true
sleep 1
/usr/local/bin/wg-guard logs --tail 1000 --since 2h >"$WORKDIR/disclosure.log"
if grep -Fq "$CAPABILITY" "$WORKDIR/disclosure.log" || grep -Fqf "$WORKDIR/token" "$WORKDIR/disclosure.log"; then
  die "service diagnostics disclosed a test secret"
fi
if [[ -n "$ADMIN_PASSWORD_FILE" ]] && grep -Fqf "$ADMIN_PASSWORD_FILE" "$WORKDIR/disclosure.log"; then
  die "service diagnostics disclosed the administrator password"
fi
grep -Fq '/sub/[REDACTED]' "$WORKDIR/disclosure.log" || die "capability redaction marker was not observed"
log "actual-secret disclosure scan passed"

if [[ "$MODE" == docker && -n "$CANDIDATE" && "$RUN_FAILURES" -eq 1 ]]; then
  [[ ! -e /usr/local/bin/docker ]] || die "cannot install the scoped Docker failure shim"
  IMAGE_ID="$(docker inspect --format '{{.Image}}' wg-guard)"
  STAGED_IMAGE_BINARY="$WORKDIR/image-binary"
  cp /usr/local/bin/wg-guard "$STAGED_IMAGE_BINARY"
  chmod 0700 "$STAGED_IMAGE_BINARY"
  cat >/usr/local/bin/docker <<'SH'
#!/bin/sh
marker=/run/wg-guard-phase9-docker-fail-once
case " $* " in
  *" compose "*" up -d --pull never "*)
    if [ ! -e "$marker" ]; then : >"$marker"; exit 42; fi ;;
esac
exec /usr/bin/docker "$@"
SH
  chmod 0755 /usr/local/bin/docker; DOCKER_WRAPPER=1
  if /usr/local/bin/wg-guard update --binary "$STAGED_IMAGE_BINARY" --image "$IMAGE_ID" --local-image --skip-backup >/dev/null 2>&1; then
    die "injected Docker update failure was accepted"
  fi
  curl --silent --fail "$BASE_URL/readyz" >/dev/null || die "failed Docker update did not recover"
  rm -f -- /usr/local/bin/docker /run/wg-guard-phase9-docker-fail-once; DOCKER_WRAPPER=0
  /usr/local/bin/wg-guard update --binary "$STAGED_IMAGE_BINARY" --image "$IMAGE_ID" --local-image --skip-backup >/dev/null

  cat >/usr/local/bin/docker <<'SH'
#!/bin/sh
marker=/run/wg-guard-phase9-docker-fail-once
case " $* " in
  *" compose "*" up -d --pull never "*)
    if [ ! -e "$marker" ]; then : >"$marker"; exit 42; fi ;;
esac
exec /usr/bin/docker "$@"
SH
  chmod 0755 /usr/local/bin/docker; DOCKER_WRAPPER=1
  if /usr/local/bin/wg-guard update --rollback >/dev/null 2>&1; then
    die "injected Docker rollback failure was accepted"
  fi
  curl --silent --fail "$BASE_URL/readyz" >/dev/null || die "failed Docker rollback did not recover"
  rm -f -- /usr/local/bin/docker /run/wg-guard-phase9-docker-fail-once; DOCKER_WRAPPER=0
  /usr/local/bin/wg-guard logs --source operations --since 1h >"$WORKDIR/docker-lifecycle.log"
  grep -q '"operation":"update","outcome":"failed"' "$WORKDIR/docker-lifecycle.log" || die "failed update outcome missing"
  grep -q '"operation":"rollback","outcome":"failed"' "$WORKDIR/docker-lifecycle.log" || die "failed rollback outcome missing"
  log "Docker update/rollback failure recovery verified"
fi

log "PASS mode=$MODE"
