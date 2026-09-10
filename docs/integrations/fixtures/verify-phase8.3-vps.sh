#!/usr/bin/env bash
# Phase 8.3: create interfaces after service startup and prove that canonical
# client configs can route public IPv4, DNS, and HTTPS through Docker's
# FORWARD=DROP policy. Run only on the dedicated disposable Ubuntu 24.04 VPS.
set -Eeuo pipefail

PUBLIC_IP=""
BASE_URL="http://127.0.0.1:8080"
while (($#)); do
  case "$1" in
    --public-ip) PUBLIC_IP=${2:?}; shift 2 ;;
    --base-url) BASE_URL=${2:?}; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ $EUID -eq 0 ]] || { echo "run as root" >&2; exit 2; }
[[ $PUBLIC_IP =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  echo "--public-ip is required" >&2; exit 2;
}
for command in awk chmod curl docker getent grep head ip iptables mktemp nft python3 seq wc; do
  command -v "$command" >/dev/null || { echo "missing command: $command" >&2; exit 2; }
done

WORK=$(mktemp -d /root/wg-guard-phase83-egress.XXXXXX)
chmod 0700 "$WORK"
TOKEN_ID=""
AWG_BIN=/root/wg-guard-phase83-awg
STAGE=prepare
NAMESPACES=()

cleanup() {
  rc=$?
  trap - EXIT
  set +e
  for ns in "${NAMESPACES[@]}"; do
    ip netns del "$ns" >/dev/null 2>&1
    rm -rf "/etc/netns/$ns"
  done
  if [[ -n $TOKEN_ID ]]; then
    /usr/local/bin/wg-guard token revoke "$TOKEN_ID" >/dev/null 2>&1
  fi
  rm -f "$AWG_BIN"
  rm -rf "$WORK"
  if ((rc != 0)); then
    echo "FAIL stage=$STAGE exit=$rc" >&2
  fi
  exit "$rc"
}
trap cleanup EXIT

api() {
  local method=$1 path=$2 output=$3 input=${4:-}
  local args=(--config "$WORK/api.curl" --request "$method" --output "$output")
  if [[ -n $input ]]; then
    args+=(--header 'Content-Type: application/json' --data-binary "@$input")
  fi
  curl "${args[@]}" "$BASE_URL$path"
}

STAGE=readiness
curl -fsS "$BASE_URL/readyz" >/dev/null
docker inspect wg-guard >/dev/null
docker cp wg-guard:/usr/local/bin/awg "$AWG_BIN" >/dev/null
chmod 0700 "$AWG_BIN"

STAGE=token
/usr/local/bin/wg-guard token create -name phase83-egress -expires-in 2h \
  -scopes stats.read,interfaces.read,interfaces.write,users.read,users.create,users.update,users.delete,devices.read,devices.write,configs.read \
  >"$WORK/token" 2>"$WORK/token.meta"
TOKEN_ID=$(awk '/^id:/{print $2; exit}' "$WORK/token.meta")
[[ -n $TOKEN_ID && $(head -1 "$WORK/token") == wg_* ]]
python3 - "$WORK/token" "$WORK/api.curl" <<'PY'
import pathlib, sys
token = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8").strip()
pathlib.Path(sys.argv[2]).write_text(
    "silent\nshow-error\nfail-with-body\nconnect-timeout = 5\nmax-time = 30\n"
    f'header = "Authorization: Bearer {token}"\n', encoding="utf-8")
PY
chmod 0600 "$WORK/api.curl"

profiles=(plain recommended randomized)
for idx in 0 1 2; do
  profile=${profiles[$idx]}
  iface="awg$idx"
  subnet="10.83.$idx.0/24"
  gateway="10.83.$idx.1"
  port=$((42830 + idx))
  ns="p83-$profile"
  host_veth="p83h$idx"
  transport_host="172.30.$((83 + idx)).1"
  transport_client="172.30.$((83 + idx)).2"
  STAGE="collision-$profile"
  ! ip link show "$iface" >/dev/null 2>&1
  ! ip link show "$host_veth" >/dev/null 2>&1
  ! ip netns list | awk '{print $1}' | grep -Fxq "$ns"

  STAGE="create-interface-$profile"
  python3 - "$WORK/interface.request" "$iface" "$port" "$subnet" "$profile" <<'PY'
import json, sys
json.dump({
    "name": sys.argv[2], "listen_port": int(sys.argv[3]),
    "ipv4_subnet": sys.argv[4], "mtu": 1380,
    "preset": sys.argv[5], "backend_mode": "kernel",
}, open(sys.argv[1], "w", encoding="utf-8"))
PY
  api POST /api/v1/interfaces "$WORK/interface.json" "$WORK/interface.request"
  interface_id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$WORK/interface.json")

  STAGE="create-user-$profile"
  python3 - "$WORK/user.request" "$interface_id" "$profile" <<'PY'
import json, sys
json.dump({
    "username": "phase83-" + sys.argv[3], "interface_id": sys.argv[2],
    "device_limit": 1, "start_policy": "immediate",
    "duration_seconds": 7200, "enabled": True,
}, open(sys.argv[1], "w", encoding="utf-8"))
PY
  api POST /api/v1/users "$WORK/user.json" "$WORK/user.request"
  user_id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$WORK/user.json")

  STAGE="create-device-$profile"
  python3 - "$WORK/device.request" "$interface_id" <<'PY'
import json, sys
json.dump({
    "name": "phase83-client", "interface_id": sys.argv[2],
    "preshared_key": True,
}, open(sys.argv[1], "w", encoding="utf-8"))
PY
  api POST "/api/v1/users/$user_id/devices" "$WORK/device.json" "$WORK/device.request"
  device_id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$WORK/device.json")
  api GET "/api/v1/devices/$device_id/config" "$WORK/client.conf"
  awk '!/^[[:space:]]*(Address|DNS|MTU)[[:space:]]*=/' "$WORK/client.conf" >"$WORK/client.stripped"
  address=$(awk -F ' *= *' '$1=="Address"{print $2; exit}' "$WORK/client.conf")
  mtu=$(awk -F ' *= *' '$1=="MTU"{print $2; exit}' "$WORK/client.conf")
  [[ -n $address && -n $mtu ]]

  STAGE="client-namespace-$profile"
  ip netns add "$ns"
  NAMESPACES+=("$ns")
  mkdir -p "/etc/netns/$ns"
  printf 'nameserver 1.1.1.1\nnameserver 1.0.0.1\n' >"/etc/netns/$ns/resolv.conf"
  ip link add "$host_veth" type veth peer name eth0 netns "$ns"
  ip address add "$transport_host/30" dev "$host_veth"
  ip link set "$host_veth" up
  ip -n "$ns" link set lo up
  ip -n "$ns" address add "$transport_client/30" dev eth0
  ip -n "$ns" link set eth0 up
  ip -n "$ns" route add default via "$transport_host"
  ip -n "$ns" route add "$PUBLIC_IP/32" via "$transport_host" dev eth0
  ip -n "$ns" link add p83awg type amneziawg
  ip netns exec "$ns" "$AWG_BIN" setconf p83awg "$WORK/client.stripped"
  ip -n "$ns" address add "$address" dev p83awg
  ip -n "$ns" link set p83awg mtu "$mtu" up
  ip -n "$ns" route replace default dev p83awg

  STAGE="handshake-$profile"
  handshake=0
  for _ in $(seq 1 25); do
    ip netns exec "$ns" ping -c 1 -W 1 "$gateway" >/dev/null 2>&1 || true
    handshake=$("$AWG_BIN" show "$iface" latest-handshakes |
      awk 'BEGIN{m=0} $2>m{m=$2} END{print m}')
    [[ ${handshake:-0} -gt 0 ]] && break
    sleep 1
  done
  [[ ${handshake:-0} -gt 0 ]]

  STAGE="egress-$profile"
  ip netns exec "$ns" ping -c 2 -W 3 1.1.1.1 >/dev/null
  ip netns exec "$ns" getent ahostsv4 example.com >/dev/null
  ip netns exec "$ns" curl -4 -fsS --max-time 20 https://example.com/ >/dev/null
  egress=$(ip netns exec "$ns" curl -4 -fsS --max-time 20 https://api.ipify.org)
  [[ $egress == "$PUBLIC_IP" ]]
  "$AWG_BIN" show "$iface" dump |
    awk -F '\t' 'NR>1{rx+=$6;tx+=$7} END{exit !(rx>0&&tx>0)}'

  STAGE="rules-$profile"
  expected=$((2 * (idx + 1)))
  actual=$(iptables -S WGGUARD-FORWARD | grep -c '^-A WGGUARD-FORWARD')
  [[ $actual -eq $expected ]]
  nft list table inet wgguard | grep -Fq "$subnet"
  echo "PASS profile=$profile handshake gateway public-ip dns https nat counters"
done

STAGE=manager-policy
jump_count=$(iptables -S DOCKER-USER |
  grep -Ec -- '--comment "?wgguard:managed:docker-forward"? -j WGGUARD-FORWARD')
[[ $jump_count -eq 1 ]]
forward_policy=$(iptables -S FORWARD |
  awk '$1=="-P"&&$2=="FORWARD"{print $3}')
[[ $forward_policy == DROP ]]
docker exec wg-guard /usr/local/bin/wg-guard doctor >"$WORK/doctor" 2>&1 || true
grep -Eq 'forwarding.*pass|pass.*forwarding' "$WORK/doctor"
echo "PASS doctor recognizes scoped Docker forwarding under FORWARD DROP"

STAGE=restart
docker compose -f /etc/wg-guard/compose.yaml restart >/dev/null
for _ in $(seq 1 30); do
  curl -fsS "$BASE_URL/readyz" >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS "$BASE_URL/readyz" >/dev/null
actual=$(iptables -S WGGUARD-FORWARD | grep -c '^-A WGGUARD-FORWARD')
jump_count=$(iptables -S DOCKER-USER |
  grep -Ec -- '--comment "?wgguard:managed:docker-forward"? -j WGGUARD-FORWARD')
[[ $actual -eq 6 && $jump_count -eq 1 ]]
for idx in 0 1 2; do
  ns="p83-${profiles[$idx]}"
  ip netns exec "$ns" curl -4 -fsS --max-time 20 https://example.com/ >/dev/null
done
echo "PASS restart readiness, idempotent rules, and all profiles retain Internet egress"
