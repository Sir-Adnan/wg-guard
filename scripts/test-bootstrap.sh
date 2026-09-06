#!/usr/bin/env bash
# Executable fixtures: no network, package manager, sudo, or host mutation.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf -- "$fixture"' EXIT
mkdir "$fixture/bin" "$fixture/tmp"
export FIXTURE_ROOT="$fixture" TMPDIR="$fixture/tmp"
python3 - "$fixture" <<'PY'
import hashlib,io,json,pathlib,sys,tarfile
p=pathlib.Path(sys.argv[1])
binary=b'#!/usr/bin/env bash\nif [[ "$1" == installer-contract ]]; then if [[ "${FIXTURE_MODE:-}" == owner-unsafe ]]; then printf \'{"revision":1,"data_contract":"schema7-h-ranges-v1","prerequisites":true,"recovery":true,"local_owner":false}\\n\'; exit 0; fi; [[ "${FIXTURE_MODE:-}" != old-installer ]] || exit 2; printf \'{"revision":1,"data_contract":"schema7-h-ranges-v1","prerequisites":true,"recovery":true,"local_owner":true,"coordinated_restore":false}\\n\'; exit 0; fi\nprintf "%s\\n" "$@" > "$FIXTURE_ROOT/argv"\nif read -r answer; then printf "%s" "$answer" > "$FIXTURE_ROOT/input"; fi\n'
(p/'binary').write_bytes(binary)
(p/'sums').write_text(hashlib.sha256(binary).hexdigest()+'  wg-guard_linux_amd64\n')
go_script='''#!/usr/bin/env python3
import os,pathlib,sys
p=pathlib.Path(%r)
if sys.argv[1]=='version':
 print('go version '+('go1.20.0' if (p/'old-go').exists() else 'go1.99.0')+' linux/amd64');sys.exit(0)
with (p/'build-args').open('a') as f:f.write(' '.join(sys.argv[1:])+'\\n')
pathlib.Path(sys.argv[sys.argv.index('-o')+1]).write_bytes((p/'binary').read_bytes())
''' % str(p)
(p/'bin'/'go').write_text(go_script)
for name,member,body,mode in [('source','wg-guard-0123456789abcdef0123456789abcdef01234567/go.mod',b'module github.com/Sir-Adnan/wg-guard\n\ngo 1.25.0\n',0o600),('toolchain','go/bin/go',go_script.encode(),0o700)]:
 with tarfile.open(p/name,'w:gz') as archive:
  h=tarfile.TarInfo(member);h.size=len(body);h.mode=mode;archive.addfile(h,io.BytesIO(body))
(p/'bin'/'curl').write_text('''#!/usr/bin/env python3
import json,os,pathlib,sys
p=pathlib.Path(os.environ['FIXTURE_ROOT']);a=sys.argv[1:];url=a[-1]
with (p/'requests').open('a') as f:f.write(url+'\\n')
out=pathlib.Path(a[a.index('--output')+1]);header=pathlib.Path(a[a.index('--dump-header')+1]);header.write_text('HTTP/1.1 200 OK\\r\\n\\r\\n')
mode=os.environ.get('FIXTURE_MODE','valid')
if url=='https://go.dev/dl/?mode=json':
 import hashlib
 data=json.dumps([{'version':'go1.99.0','stable':True,'files':[{'filename':'go1.99.0.linux-amd64.tar.gz','os':'linux','arch':'amd64','kind':'archive','size':(p/'toolchain').stat().st_size,'sha256':hashlib.sha256((p/'toolchain').read_bytes()).hexdigest()}]}]).encode()
elif url=='https://go.dev/dl/go1.99.0.linux-amd64.tar.gz':data=b'x'*(p/'toolchain').stat().st_size if mode=='bad-toolchain' else (p/'toolchain').read_bytes()
elif '/tar.gz/' in url:data=(p/'source').read_bytes()
elif '/commits/' in url:data=json.dumps({'sha':'0123456789abcdef0123456789abcdef01234567'}).encode()
elif '/releases?' in url:data=b'[]' if mode=='empty' else json.dumps([{'tag_name':'v1','published_at':'2026-01-01','draft':False,'prerelease':False}]).encode()
elif '/releases/tags/' in url:
 base='https://github.com/Sir-Adnan/wg-guard/releases/download/v1/'
 data=json.dumps({'tag_name':'v1','published_at':'2026-01-01','assets':[{'name':n,'browser_download_url':base+n,'size':(p/f).stat().st_size} for n,f in [('wg-guard_linux_amd64','binary'),('checksums.txt','sums')]]}).encode()
elif url.endswith('checksums.txt'):data=(p/'sums').read_bytes()
elif url.endswith('wg-guard_linux_amd64'):data=b'x'*(p/'binary').stat().st_size if mode=='corrupt' else (p/'binary').read_bytes()
else:sys.exit(22)
out.write_bytes(data);sys.stdout.write('200')
''')
(p/'bin'/'uname').write_text('#!/bin/sh\ncase "$1" in -s) echo Linux;; -m) echo x86_64;; esac\n')
(p/'bin'/'id').write_text('#!/bin/sh\nif test -e "$FIXTURE_ROOT/nonroot"; then echo 1000; else echo 0; fi\n')
(p/'bin'/'sudo').write_text('#!/bin/sh\nprintf "used\\n" > "$FIXTURE_ROOT/sudo-used"\nexec "$@"\n')
(p/'bin'/'git').write_text('''#!/usr/bin/env python3
import io,sys,tarfile
if 'rev-parse' in sys.argv:print('0123456789abcdef0123456789abcdef01234567')
elif 'archive' in sys.argv:
 with tarfile.open(fileobj=sys.stdout.buffer,mode='w|') as archive:
  data=b'module fixture.test\\n\\ngo 1.25.0\\n';h=tarfile.TarInfo('go.mod');h.size=len(data);archive.addfile(h,io.BytesIO(data))
else:sys.exit(1)
''')
for f in (p/'bin').iterdir():f.chmod(0o755)
PY
export PATH="$fixture/bin:$PATH"
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
bash "$root/install.sh" --help </dev/null >/dev/null
test ! -e "$fixture/requests" || fail 'help performed acquisition'
bash "$root/install.sh" --release v1 -- --yes --mode native </dev/null
test "$(head -n 1 "$fixture/argv")" = install || fail 'install dispatch'
test "$(sed -n '2p' "$fixture/argv")" = --build-metadata || fail 'build identity forwarding'
test "$(sed -n '4p' "$fixture/argv")" = --yes || fail 'argument forwarding'
test "$(tail -n 1 "$fixture/argv")" = native || fail 'argument value forwarding'
test -z "$(ls -A "$fixture/tmp")" || fail 'success cleanup'
rm "$fixture/argv"
if FIXTURE_MODE=old-installer bash "$root/install.sh" --release v1 --yes </dev/null; then fail 'old installer accepted'; fi
test ! -e "$fixture/argv" || fail 'old installer deployment ran'
if FIXTURE_MODE=owner-unsafe bash "$root/install.sh" --release v1 --yes </dev/null; then fail 'owner-unsafe installer accepted'; fi
test ! -e "$fixture/argv" || fail 'owner-unsafe deployment ran'
if FIXTURE_MODE=corrupt bash "$root/install.sh" --release v1 --yes </dev/null; then fail 'corrupt binary accepted'; fi
test ! -e "$fixture/argv" || fail 'corrupt executable ran'
test -z "$(ls -A "$fixture/tmp")" || fail 'failure cleanup'
if FIXTURE_MODE=empty bash "$root/install.sh" --release latest --yes </dev/null; then fail 'empty release accepted'; fi
if bash "$root/install.sh" --commit abc --yes </dev/null; then fail 'short commit accepted'; fi
bash "$root/install.sh" --list-releases </dev/null > "$fixture/list"
test "$(cat "$fixture/list")" = v1 || fail 'list dispatch'
cat "$root/install.sh" | bash -s -- --release v1 --yes
test ! -e "$fixture/input" || fail 'piped script consumed as answers'
test -z "$(ls -A "$fixture/tmp")" || fail 'piped cleanup'
bash "$root/install.sh" --commit main --yes </dev/null
test -s "$fixture/build-args" || fail 'source build did not execute'
rm "$fixture/build-args"
touch "$fixture/old-go"
bash "$root/install.sh" --commit 0123456789abcdef0123456789abcdef01234567 --yes </dev/null
test -s "$fixture/build-args" || fail 'toolchain build did not execute'
test -z "$(ls -A "$fixture/tmp")" || fail 'toolchain cleanup'
rm "$fixture/argv"
if FIXTURE_MODE=bad-toolchain bash "$root/install.sh" --commit main --yes </dev/null; then fail 'corrupt toolchain accepted'; fi
test ! -e "$fixture/argv" || fail 'corrupt toolchain produced executable'
test -z "$(ls -A "$fixture/tmp")" || fail 'corrupt toolchain cleanup'
touch "$fixture/nonroot"
bash "$root/install.sh" --release v1 --yes </dev/null
test -s "$fixture/sudo-used" || fail 'non-root installation was not elevated'
bash "$root/scripts/build-artifacts.sh" --version v1 --output "$fixture/artifacts"
(cd "$fixture/artifacts" && sha256sum --check checksums.txt)
test -f "$fixture/artifacts/wg-guard_linux_arm64" || fail 'arm64 asset missing'
printf 'bootstrap fixtures passed\n'
