#!/usr/bin/env bash
# WG-Guard Phase 0 — userspace AmneziaWG daemon runtime verification (WSL2).
# Builds the pinned amneziawg-go, brings up a tunnel interface, and captures
# the real UAPI behaviors WG-Guard will rely on (setconf, show dump, showconf).
# Usage (as root): bash docs/integrations/fixtures/verify-wsl2-runtime.sh
set -uo pipefail

WORK=/root/awgfix
SRC=/opt/awg-src/amneziawg-go
mkdir -p "$WORK"
cd "$SRC"

step() { printf '\n=== %s ===\n' "$*"; }

step "install build prerequisites + build pinned daemon"
export DEBIAN_FRONTEND=noninteractive
apt-get install -y -qq build-essential >/dev/null 2>&1 || { apt-get update -qq; apt-get install -y build-essential; }
git checkout -q v3.1.20260828
make 2>&1 | tail -3
[ -x ./amneziawg-go ] && echo "DAEMON_BUILT: $(./amneziawg-go --version 2>&1 | head -1)" || { echo "DAEMON_BUILD_FAILED"; exit 1; }

step "prepare TUN device (WSL2)"
mkdir -p /dev/net
[ -e /dev/net/tun ] || mknod /dev/net/tun c 10 200
ls -la /dev/net/tun

PRIV=$(awg genkey)
PUB=$(echo "$PRIV" | awg pubkey)
PEERPRIV=$(awg genkey)
PEERPUB=$(echo "$PEERPRIV" | awg pubkey)

step "start userspace daemon for interface awg0"
./amneziawg-go awg0 > "$WORK/awg0-daemon.log" 2>&1 &
sleep 1
pgrep -a amneziawg-go | head -3 || true

step "UAPI socket location"
ls -la /var/run/amnezia/ 2>/dev/null || ls -la /var/run/ | grep -i -E "amnezia|awg"

step "awg setconf with legacy 1.0 obfuscation params (runtime acceptance)"
cat > "$WORK/daemon0.conf" <<EOF
[Interface]
PrivateKey = $PRIV
ListenPort = 39411
Jc = 5
Jmin = 40
Jmax = 70
S1 = 86
S2 = 61
H1 = 1234567
H2 = 2345678
H3 = 3456789
H4 = 4567890
[Peer]
PublicKey = $PEERPUB
AllowedIPs = 10.8.0.2/32
EOF
chmod 600 "$WORK/daemon0.conf"
awg setconf awg0 "$WORK/daemon0.conf" && echo "SETCONF_OK"

step "awg show awg0 dump (authoritative dump format fixture)"
awg show awg0 dump | sed "s/$PRIV/<private-key-redacted>/; s/$PEERPRIV/<peer-priv>/; s|$(echo "$PEERPUB" | sed 's/[\/&]/\\&/g')|<peer-public-key>|" | cat -A | head -8
echo "--- readable dump ---"
awg show awg0 dump | head -4

step "awg showconf awg0 (params persisted by daemon)"
awg showconf awg0 | sed "s/$PRIV/<private-key-redacted>/"

step "awg set incremental peer op"
awg set awg0 peer "$PEERPUB" persistent-keepalive 25 && echo "SET_PEER_OK"

step "ip link view of userspace tunnel"
ip link show awg0 2>&1 | head -2

step "stop daemon"
pkill -f "amneziawg-go awg0" 2>/dev/null; sleep 1
pgrep -a amneziawg-go || echo "daemon stopped"

step "runtime negative: setconf with duplicate H1=H2 (runtime-level check?)"
./amneziawg-go awg0 > "$WORK/awg0-daemon2.log" 2>&1 &
sleep 1
sed 's/^H2 = 2345678/H2 = 1234567/' "$WORK/daemon0.conf" > "$WORK/daemon0-neg.conf"
awg setconf awg0 "$WORK/daemon0-neg.conf" 2>&1 && echo "DAEMON_ACCEPTS_duplicate_H" || echo "DAEMON_REJECTS_duplicate_H"
pkill -f "amneziawg-go awg0" 2>/dev/null
echo "runtime verification done"
