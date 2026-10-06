#!/usr/bin/env bash
# Isolated Docker/fake-backend persistence and hardening check; no VPN/client claim.
set -euo pipefail
umask 077
[[ $# == 1 ]] || exit 2
image=$(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["image_id"])' "$1")
[[ $image =~ ^sha256:[0-9a-f]{64}$ ]] || exit 2
archive=${1%/*}/runtime_linux_amd64.tar.gz
# The OCI manifest digest inside the export (absent from a legacy export).
manifest=$(python3 -I -c 'import json,sys,tarfile
with tarfile.open(sys.argv[1], "r:gz") as t:
    try:
        index = json.loads(t.extractfile("index.json").read(65536))
    except KeyError:
        raise SystemExit(0)
assert index["schemaVersion"] == 2 and len(index["manifests"]) == 1
print(index["manifests"][0]["digest"])' "$archive")
[[ -z $manifest || $manifest =~ ^sha256:[0-9a-f]{64}$ ]] || exit 2
# Re-import the just-built untagged candidate, without registry/source access.
# The classic image store names it by config digest, containerd by manifest digest.
local_id=$(docker image inspect --format '{{.Id}}' "$image" 2>/dev/null || docker image inspect --format '{{.Id}}' "$manifest")
docker image rm "$local_id" >/dev/null
docker load --input "$archive" >/dev/null
if ! local_id=$(docker image inspect --format '{{.Id}}' "$image" 2>/dev/null); then
  [[ -n $manifest ]] || exit 1
  local_id=$(docker image inspect --format '{{.Id}}' "$manifest")
fi
[[ $local_id == "$image" || $local_id == "$manifest" ]] || exit 1
image=$local_id
data=$(mktemp -d /tmp/wg-guard-image-smoke.XXXXXXXX)
[[ $data =~ ^/tmp/wg-guard-image-smoke\.[A-Za-z0-9]{8}$ ]] || exit 2
name=wg-guard-image-smoke-$$
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; sudo rm -rf -- "$data"; }
trap cleanup EXIT
# Match production ownership; root without DAC_OVERRIDE cannot bypass another
# user's 0700 directory. Never weaken permissions to make the fixture pass.
sudo chown 0:0 "$data"
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
  docker inspect --format 'status={{.State.Status}}, exit={{.State.ExitCode}}, oom={{.State.OOMKilled}}' "$name" >&2
  docker logs --tail 20 "$name" >&2
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
