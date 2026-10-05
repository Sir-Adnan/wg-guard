#!/usr/bin/env bash
# Isolated Docker/fake-backend persistence and hardening check; no VPN/client claim.
set -euo pipefail
umask 077
[[ $# == 1 ]] || exit 2
image=$(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["image_id"])' "$1")
[[ $image =~ ^sha256:[0-9a-f]{64}$ ]] || exit 2
archive=${1%/*}/runtime_linux_amd64.tar.gz
# Re-import the just-built untagged candidate, without registry/source access.
docker image rm "$image" >/dev/null
docker load --input "$archive" >/dev/null
[[ $(docker image inspect --format '{{.Id}}' "$image") == "$image" ]] || exit 1
data=$(mktemp -d -t wg-guard-image-smoke.XXXXXXXX)
name=wg-guard-image-smoke-$$
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; rm -rf -- "$data"; }
trap cleanup EXIT
docker run -d --name "$name" --network none --cap-drop ALL \
  --cap-add NET_ADMIN --cap-add NET_BIND_SERVICE --security-opt no-new-privileges \
  --read-only --tmpfs /run:rw,nosuid,nodev,size=32m --tmpfs /tmp:rw,nosuid,nodev,size=64m \
  --mount "type=bind,source=$data,target=/var/lib/wg-guard" "$image" serve --backend fake >/dev/null
ready() {
  for ((i=0;i<40;i++)); do
    if docker exec "$name" curl -fsS http://127.0.0.1:8080/readyz 2>/dev/null | grep -q '"status":"ready"'; then return; fi
    sleep .5
  done
  printf 'Runtime did not become ready\n' >&2
  return 1
}
ready
key_before=$(docker exec "$name" sha256sum /var/lib/wg-guard/master.key)
docker restart "$name" >/dev/null
ready
key_after=$(docker exec "$name" sha256sum /var/lib/wg-guard/master.key)
[[ $key_before == "$key_after" ]] || { printf 'Node key changed across restart\n' >&2; exit 1; }
docker exec "$name" test -s /var/lib/wg-guard/wg-guard.db
if docker exec "$name" touch /usr/local/bin/wg-guard-probe 2>/dev/null; then
  printf 'Runtime root filesystem was writable\n' >&2; exit 1
fi
printf 'Runtime image passed: offline archive load, fake readiness, persistent DB/key, restart and read-only root\n'
