#!/usr/bin/env bash
# bench-idle.sh — measure wg-guard steady-state RSS and idle CPU
# (docs/archive/ARCHITECTURE_V2_PROPOSAL.md §8: RSS ≤ 50 MB @ 100 devices,
# ≤ 80 MB @ 1000 devices; idle CPU ≤ 0.5 % average on 1 vCPU).
#
# The node runs with `-backend fake` (in-memory tunnels): no root, no AWG
# tooling, no host networking — the measurement covers the control plane
# (HTTP + scheduler + accounting over a synthetic device population), which
# is exactly what the budgets describe.
#
# Usage:
#   scripts/bench-idle.sh [DURATION_SECONDS] [PEERS] [BIN]
#   DURATION  sampling window in seconds          (default 600 = 10 min)
#   PEERS     users+devices seeded via the API    (default 0)
#   BIN       wg-guard binary to measure          (default: built from tree)
#
# Examples:
#   scripts/bench-idle.sh 60 0            # quick idle sanity check
#   scripts/bench-idle.sh 600 1000        # the §8 stress point
set -euo pipefail

DURATION="${1:-600}"
PEERS="${2:-0}"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${3:-$REPO/bench-wgguard}"
PORT="${WGG_BENCH_PORT:-18432}"
WORK="$(mktemp -d)"
cleanup() {
  if [ -n "${SRV_PID:-}" ]; then
    kill "$SRV_PID" 2>/dev/null || true
    wait "$SRV_PID" 2>/dev/null || true
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

if [ ! -x "$BIN" ]; then
  echo "building $BIN (CGO_ENABLED=0)…" >&2
  (cd "$REPO" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BIN" ./cmd/wg-guard)
fi

cat > "$WORK/wg-guard.toml" <<EOF
data_dir = "$WORK/data"
http_listen = "127.0.0.1:$PORT"
[tls]
mode = "dev"
EOF

mkdir -p "$WORK/data"   # the token CLI opens the DB directly; serve creates it later
# stdout carries the plaintext (consumed below); stderr goes to a file — it is
# operator-facing prose ("shown once"), noise in unattended runs.
"$BIN" token create -config "$WORK/wg-guard.toml" -name bench \
  -scopes users.read,users.create,users.bulk,devices.write,interfaces.write \
  > "$WORK/token.txt" 2> "$WORK/token.err" || {
  echo "token create failed:" >&2; cat "$WORK/token.err" >&2; exit 1; }
TOKEN="$(head -1 "$WORK/token.txt")"
if [ -z "$TOKEN" ]; then
  echo "token create produced no plaintext:" >&2; cat "$WORK/token.err" >&2; exit 1
fi

echo "starting node (backend=fake, $PEERS peers)…" >&2
"$BIN" serve -config "$WORK/wg-guard.toml" -backend fake > "$WORK/serve.log" 2>&1 &
SRV_PID=$!

for _ in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:$PORT/readyz" >/dev/null 2>&1; then break; fi
  sleep 0.2
done
if ! curl -fsS "http://127.0.0.1:$PORT/readyz" >/dev/null 2>&1; then
  echo "node did not become ready; log:" >&2; cat "$WORK/serve.log" >&2; exit 1
fi
# api METHOD... — REST helper: prints the body on 2xx, fails loudly with the
# error envelope otherwise (a bench that seeds nothing must not "measure").
api() {
  local out code body
  if ! out=$(curl -sS -w '\n%{http_code}' -H "Authorization: Bearer $TOKEN" \
       -H "Content-Type: application/json" "$@"); then
    echo "curl failed: $*" >&2; exit 1
  fi
  code="${out##*$'\n'}"
  body="${out%$'\n'*}"
  case "$code" in
    2*) printf '%s' "$body" ;;
    *) echo "API $code for: $*" >&2; echo "$body" >&2; exit 1 ;;
  esac
}

if [ "$PEERS" -gt 0 ]; then
  echo "seeding $PEERS users+devices…" >&2
  # A /24 holds ~253 devices; scale the pool for the stress point.
  SUBNET="10.8.0.0/24"
  [ "$PEERS" -gt 250 ] && SUBNET="10.8.0.0/21"
  IFACE_ID="$(api -X POST -d '{"name":"awg0","listen_port":39100,"ipv4_subnet":"'"$SUBNET"'"}' \
    "http://127.0.0.1:$PORT/api/v1/interfaces" | grep -o '"id":"[0-9a-f-]\{36\}"' | head -1 | cut -d'"' -f4)"
  # Bulk creation caps at 500 per call — chunk the population.
  REMAINING="$PEERS"
  while [ "$REMAINING" -gt 0 ]; do
    CHUNK=500; [ "$REMAINING" -lt 500 ] && CHUNK="$REMAINING"
    api -X POST -d "{\"count\":$CHUNK,\"prefix\":\"p\",\"start_index\":1,\"width\":5,\"duration_seconds\":31536000}" \
      "http://127.0.0.1:$PORT/api/v1/users/bulk" >/dev/null
    REMAINING=$(( REMAINING - CHUNK ))
  done
  # One device per user: page through users and create a device for each.
  CURSOR=""
  while :; do
    PAGE="$(api "http://127.0.0.1:$PORT/api/v1/users?limit=100${CURSOR:+&cursor=$CURSOR}")"
    echo "$PAGE" | grep -o '"id":"[0-9a-f-]\{36\}"' | cut -d'"' -f4 | while read -r UID_; do
      api -X POST -d '{"name":"dev","interface_id":"'"$IFACE_ID"'"}' \
        "http://127.0.0.1:$PORT/api/v1/users/$UID_/devices" >/dev/null
    done
    CURSOR="$(echo "$PAGE" | sed -n 's/.*"next_cursor":"\([^"]*\)".*/\1/p')"
    [ -z "$CURSOR" ] && break
  done
fi

# Give the scheduler one full accounting cycle over the seeded state before
# measuring (the first ensure can rebuild shaper state; steady state is the
# quantity the budgets talk about).
sleep 2

SAMPLE_EVERY=5
SAMPLES=$(( DURATION / SAMPLE_EVERY ))
: > "$WORK/rss.txt"
: > "$WORK/cpu.txt"
read -r U0 S0 _ < <(awk '{print $14, $15, $1}' "/proc/$SRV_PID/stat")
T0=$(date +%s)
echo "sampling $DURATION s ($SAMPLES samples, every ${SAMPLE_EVERY}s)…" >&2
for _ in $(seq 1 "$SAMPLES"); do
  sleep "$SAMPLE_EVERY"
  # int(): some kernels report fractional kB in VmRSS; budgets are integers.
  RSS=$(awk '/^VmRSS:/{print int($2)}' "/proc/$SRV_PID/status") || true
  [ -z "$RSS" ] && break   # process died
  echo "$RSS" >> "$WORK/rss.txt"
  read -r U1 S1 _ < <(awk '{print $14, $15, $1}' "/proc/$SRV_PID/stat")
  CLK=$(getconf CLK_TCK)
  echo "$(( U1 - U0 + S1 - S0 ))" >> "$WORK/cpu.txt"
  U0=$U1; S0=$S1
done
T1=$(date +%s)

if [ ! -s "$WORK/rss.txt" ]; then
  echo "no samples collected; log:" >&2; cat "$WORK/serve.log" >&2; exit 1
fi

AVG_RSS=$(awk '{s+=$1} END{printf "%d", s/NR}' "$WORK/rss.txt")
MAX_RSS=$(sort -n "$WORK/rss.txt" | tail -1 | cut -d. -f1)
ELAPSED=$(( T1 - T0 ))
CLK=$(getconf CLK_TCK)
CPU_PCT=$(awk -v clk="$CLK" -v el="$ELAPSED" '{s+=$1} END{printf "%.2f", 100*s/clk/el}' "$WORK/cpu.txt")

echo "================ bench-idle ================"
echo "peers:            $PEERS"
echo "window:           ${ELAPSED}s"
echo "RSS avg:          $(( AVG_RSS / 1024 )) MB"
echo "RSS max:          $(( MAX_RSS / 1024 )) MB"
echo "CPU avg:          ${CPU_PCT}%"
echo "============================================"
