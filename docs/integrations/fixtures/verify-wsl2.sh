#!/usr/bin/env bash
# WG-Guard Phase 0 — AmneziaWG upstream verification script (WSL2 Ubuntu / Debian).
# Reproduces every check recorded in docs/integrations/amneziawg.md.
# Usage (as root): bash docs/integrations/fixtures/verify-wsl2.sh
set -uo pipefail

WORK=/root/awgfix
mkdir -p "$WORK"
cd "$WORK"

step() { printf '\n=== %s ===\n' "$*"; }

step "system"
lsb_release -ds 2>/dev/null || cat /etc/os-release | head -2
uname -r

step "package versions"
dpkg -s amneziawg-tools 2>/dev/null | grep -E '^(Package|Version)'
dpkg -s amneziawg 2>/dev/null | grep -E '^(Package|Version)'
dpkg -s amneziawg-dkms 2>/dev/null | grep -E '^(Package|Version)'

step "awg version"
awg --version 2>&1

step "genkey / pubkey / genpsk"
awg genkey > "$WORK/priv" 2> "$WORK/genkey.err"
echo "genkey_exit=$? bytes=$(wc -c < "$WORK/priv")"
cat "$WORK/genkey.err"
awg pubkey < "$WORK/priv" > "$WORK/pub" 2>&1 && echo "pubkey_ok: $(cat "$WORK/pub")"
awg genpsk > "$WORK/psk" 2>&1 && echo "genpsk_ok"

PRIV=$(cat "$WORK/priv")
PUB=$(cat "$WORK/pub")
PSK=$(cat "$WORK/psk")

step "strip: legacy 1.0 obfuscation params (parser acceptance)"
cat > "$WORK/striplegacy.conf" <<EOF
[Interface]
PrivateKey = $PRIV
Address = 10.8.0.1/24
ListenPort = 39411
MTU = 1420
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
PublicKey = $PUB
PresharedKey = $PSK
AllowedIPs = 10.8.0.2/32
PersistentKeepalive = 25
EOF
chmod 600 "$WORK"/*.conf
awg-quick strip "$WORK/striplegacy.conf" 2>&1
echo "strip_legacy_exit=$?"

step "strip: 1.5 signature packets (I1-I5, parser acceptance)"
cat > "$WORK/strip15.conf" <<EOF
[Interface]
PrivateKey = $PRIV
Address = 10.8.0.1/24
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
I1 = aabbccdd
I2 = 11223344
I3 = 55667788
I4 = 99aabbcc
I5 = ddeeff00
[Peer]
PublicKey = $PUB
AllowedIPs = 10.8.0.3/32
EOF
chmod 600 "$WORK/strip15.conf"
awg-quick strip "$WORK/strip15.conf" 2>&1
echo "strip_15_exit=$?"

step "parser-level negative tests (constraints enforced where?)"
sed 's/^Jmin = 40/Jmin = 70/; s/^Jmax = 70/Jmax = 40/' "$WORK/striplegacy.conf" > "$WORK/neg1.conf"
awg-quick strip "$WORK/neg1.conf" >/dev/null 2>&1 && echo "PARSER_ACCEPTS Jmin>Jmax" || echo "PARSER_REJECTS Jmin>Jmax"
sed 's/^H2 = 2345678/H2 = 1234567/' "$WORK/striplegacy.conf" > "$WORK/neg2.conf"
awg-quick strip "$WORK/neg2.conf" >/dev/null 2>&1 && echo "PARSER_ACCEPTS duplicate H1=H2" || echo "PARSER_REJECTS duplicate H1=H2"
sed 's/^S2 = 61/S2 = 142/' "$WORK/striplegacy.conf" > "$WORK/neg3.conf"
awg-quick strip "$WORK/neg3.conf" >/dev/null 2>&1 && echo "PARSER_ACCEPTS S1+56==S2" || echo "PARSER_REJECTS S1+56==S2"

step "config.c accepted key list (pinned tools source)"
if [ -f /opt/awg-src/amneziawg-tools/src/config.c ]; then
  grep -oE '"[A-Za-z0-9]+"' /opt/awg-src/amneziawg-tools/src/config.c | tr -d '"' | sort -u | grep -vE '^(calloc|endpoint|fopen|fwmark|getc|h1|h2|h3|h4|stderr|stdout|wg)$'
else
  echo "tools source not present — git clone https://github.com/amnezia-vpn/amneziawg-tools /opt/awg-src/amneziawg-tools"
fi

step "kernel module availability"
modinfo amnezia 2>&1 | head -4
modprobe amnezia 2>&1; echo "modprobe_exit=$?"

step "userspace daemon build (pinned tag)"
if [ -d /opt/awg-src/amneziawg-go ]; then
  cd /opt/awg-src/amneziawg-go
  git fetch --tags --force 2>/dev/null
  git checkout -q v3.1.20260828
  echo "checkout: $(git log --oneline -1)"
  make 2>&1 | tail -12
  if [ -x ./awg ]; then echo "DAEMON_BUILT"; else echo "DAEMON_BUILD_FAILED"; fi
else
  echo "/opt/awg-src/amneziawg-go missing"
fi
