#!/usr/bin/env bash
# Acquisition entry. The acquired Go binary owns installation/management/lifecycle.
set -euo pipefail
umask 077

ui_cyan= ui_green= ui_yellow= ui_red= ui_dim= ui_reset=
ui_width=${COLUMNS:-72}
if [[ -t 2 ]] && ui_size=$(stty size </dev/tty 2>/dev/null); then
  ui_width=${ui_size##* }
fi
[[ $ui_width =~ ^[0-9]+$ ]] || ui_width=72
((ui_width < 20)) && ui_width=20
((ui_width > 72)) && ui_width=72
if [[ -t 2 && ${TERM:-dumb} != dumb && -z ${NO_COLOR:-} ]]; then
  ui_cyan=$'\033[36;1m'; ui_green=$'\033[32;1m'; ui_yellow=$'\033[33;1m'; ui_red=$'\033[31;1m'; ui_dim=$'\033[2m'; ui_reset=$'\033[0m'
fi
ui_header() {
  local rule='------------------------------------------------------------------------'
  printf '\n%sWG-GUARD%s\n%sSecure AmneziaWG node setup%s\n%s\n' "$ui_cyan" "$ui_reset" "$ui_dim" "$ui_reset" "${rule:0:ui_width}" >&2
}
ui_step() { printf '\n%s[%s]%s %s\n' "$ui_cyan" "$1" "$ui_reset" "$2" >&2; }
ui_ok() { printf '%sOK%s  %s\n' "$ui_green" "$ui_reset" "$1" >&2; }
ui_warn() { printf '%sWARN%s  %s\n' "$ui_yellow" "$ui_reset" "$1" >&2; }
ui_error() { printf '%sERROR%s  %s\n' "$ui_red" "$ui_reset" "$1" >&2; }
ui_note() { printf '%s%s%s\n' "$ui_dim" "$1" "$ui_reset" >&2; }
die() { ui_error "$1"; exit "${2:-2}"; }

channel=release
ref=latest
list=0
refresh=0
args=()
while (($#)); do
  case "$1" in
    --help|-h)
      printf '%s\n' 'WG-Guard GitHub bootstrap' \
        'Usage: bash install.sh [--release latest|TAG | --commit main|FULL_SHA | --refresh | --list-releases] [-- INSTALL_FLAGS]' \
        'Platform: Ubuntu 24.04 or newer on amd64/x86_64.' \
        'Default: latest published stable release. Development source is never selected implicitly.' \
        'Terminal UI: English only.' \
        'Everyday command after the first download: sudo wg-guard' \
        '--refresh strictly reacquires the selected GitHub build; ordinary runs reuse a verified current manager.' \
        'Advanced install flags (for example --yes --mode native) are forwarded unchanged.'
      exit 0 ;;
    --release|--commit)
      (($# >= 2)) || die 'Missing selection value'
      channel=${1#--}; ref=$2; shift 2 ;;
    --list-releases) list=1; shift ;;
    --refresh) refresh=1; shift ;;
    --) shift; args+=("$@"); break ;;
    *) args+=("$1"); shift ;;
  esac
done
((list)) || { ui_header; ui_step '1/4' 'Checking system compatibility'; }
[[ $(uname -s) == Linux ]] || die 'Only Linux is supported'
os_id=
os_version=
[[ -r /etc/os-release ]] || die 'WG-Guard requires Ubuntu 24.04 or newer on amd64/x86_64'
while IFS='=' read -r key value; do
  value=${value%$'\r'}
  value=${value#\"}; value=${value%\"}
  value=${value#\'}; value=${value%\'}
  case "$key" in ID) os_id=$value;; VERSION_ID) os_version=$value;; esac
done < /etc/os-release
if [[ $os_id != ubuntu || ! $os_version =~ ^([0-9]+)\.([0-9]+)$ ]]; then
  die 'WG-Guard requires Ubuntu 24.04 or newer on amd64/x86_64'
fi
os_year=$((10#${BASH_REMATCH[1]})); os_month=$((10#${BASH_REMATCH[2]}))
((os_year > 24 || os_year == 24 && os_month >= 4)) || die 'WG-Guard requires Ubuntu 24.04 or newer on amd64/x86_64'
case $(uname -m) in x86_64|amd64) arch=amd64;; *) die 'WG-Guard requires Ubuntu 24.04 or newer on amd64/x86_64';; esac
((list)) || ui_ok "Ubuntu $os_version · $arch"
sudo_cmd=()
if [[ $(id -u) != 0 ]]; then
  command -v sudo >/dev/null || die 'Run as root or install sudo'
  sudo_cmd=(sudo)
fi

# The GitHub entry checks the selected release/branch before opening the local
# manager. The active service binary is never replaced by this bootstrap.
management_entry=1
for ((i=0; i<${#args[@]}; i++)); do
  case "${args[i]}" in
    --lang|-lang) i=$((i+1)) ;;
    --lang=*|-lang=*) ;;
    *) management_entry=0 ;;
  esac
done
installed_bin=/usr/local/bin/wg-guard
installed_state=/etc/wg-guard/install-state.json
manager_receipt=/var/cache/wg-guard/manager-build.json
manager_bin=/var/cache/wg-guard/manager
((list)) || ui_step '2/4' 'Preparing prerequisites'
missing=()
for pair in curl:curl python3:python3 tar:tar sha256sum:coreutils; do
  command -v "${pair%%:*}" >/dev/null || missing+=("${pair#*:}")
done
[[ -s /etc/ssl/certs/ca-certificates.crt ]] || missing+=(ca-certificates)
if ((${#missing[@]})); then
  command -v apt-get >/dev/null || die "Install prerequisites: ${missing[*]}"
  "${sudo_cmd[@]}" apt-get -o DPkg::Lock::Timeout=300 update
  "${sudo_cmd[@]}" apt-get -o DPkg::Lock::Timeout=300 install -y --no-install-recommends "${missing[@]}"
fi
((list)) || ui_ok 'Prerequisites ready'
stage=$(mktemp -d -t wg-guard-bootstrap.XXXXXXXX)
cleanup() { rm -rf -- "$stage"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# A manager receipt is trusted only when the complete root-owned chain, digest
# and installer contract agree. Legacy Phase 8.2 receipts may point at the
# active host binary; a successful check migrates them to manager_bin later.
cache_trusted=0
cache_channel= cache_ref= cache_commit= cache_version= cache_digest= cache_bin=
if ((list == 0)) &&
   "${sudo_cmd[@]}" test -f "$manager_receipt" && "${sudo_cmd[@]}" test ! -L "$manager_receipt"; then
  cache_record=$("${sudo_cmd[@]}" python3 -I - "$manager_receipt" "$manager_bin" "$installed_bin" <<'PY' 2>/dev/null || true
import json,pathlib,re,sys
receipt=pathlib.Path(sys.argv[1])
manager,installed=sys.argv[2:]
raw=receipt.read_bytes()
if len(raw)>8192:raise ValueError('oversized receipt')
b=json.loads(raw)
required=('Channel','Ref','Commit','Version','SHA256','BinaryPath')
if any(not isinstance(b.get(k),str) for k in required):raise ValueError('invalid receipt')
sha=re.compile(r'[0-9a-f]{40}\Z');digest=re.compile(r'[0-9a-f]{64}\Z');tag=re.compile(r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}\Z')
if b['Channel'] not in ('release','commit') or not sha.fullmatch(b['Commit']) or not digest.fullmatch(b['SHA256']) or not (0<len(b['Version'])<=160) or any(c in b['Version'] for c in '\r\n\t'):
    raise ValueError('invalid receipt')
if b['Channel']=='commit' and b['Ref']!=b['Commit'] or b['Channel']=='release' and not tag.fullmatch(b['Ref']):
    raise ValueError('invalid receipt')
if b['BinaryPath'] not in (manager,installed):raise ValueError('invalid manager path')
print('\n'.join(b[k] for k in required))
PY
)
  mapfile -t cache_fields <<<"$cache_record"
  if ((${#cache_fields[@]} == 6)); then
    cache_channel=${cache_fields[0]}; cache_ref=${cache_fields[1]}; cache_commit=${cache_fields[2]}
    cache_version=${cache_fields[3]}; cache_digest=${cache_fields[4]}; cache_bin=${cache_fields[5]}
    cache_dir=${manager_receipt%/*}
    if "${sudo_cmd[@]}" test -d "$cache_dir" && "${sudo_cmd[@]}" test ! -L "$cache_dir" &&
       "${sudo_cmd[@]}" test -f "$cache_bin" && "${sudo_cmd[@]}" test -x "$cache_bin" && "${sudo_cmd[@]}" test ! -L "$cache_bin"; then
      receipt_owner=$("${sudo_cmd[@]}" stat -c '%u' "$manager_receipt")
      receipt_mode=$("${sudo_cmd[@]}" stat -c '%a' "$manager_receipt")
      dir_owner=$("${sudo_cmd[@]}" stat -c '%u' "$cache_dir")
      dir_mode=$("${sudo_cmd[@]}" stat -c '%a' "$cache_dir")
      bin_owner=$("${sudo_cmd[@]}" stat -c '%u' "$cache_bin")
      bin_mode=$("${sudo_cmd[@]}" stat -c '%a' "$cache_bin")
      observed_digest=$("${sudo_cmd[@]}" sha256sum "$cache_bin" 2>/dev/null || true)
      observed_digest=${observed_digest%% *}
      cache_contract=$(timeout 5 "${sudo_cmd[@]}" "$cache_bin" installer-contract </dev/null 2>/dev/null || true)
      if [[ $receipt_owner == 0 && $dir_owner == 0 && $bin_owner == 0 &&
            $receipt_mode =~ ^[0-7]{3}$ && $dir_mode =~ ^[0-7]{3}$ && $bin_mode =~ ^[0-7]{3}$ ]] &&
         (( (8#$receipt_mode & 0022) == 0 && (8#$dir_mode & 0022) == 0 && (8#$bin_mode & 0022) == 0 )) &&
         [[ $observed_digest == "$cache_digest" ]] && (( ${#cache_contract} <= 4096 )) &&
         CACHE_CONTRACT=$cache_contract python3 -I - <<'PY' >/dev/null 2>&1
import json,os
c=json.loads(os.environ['CACHE_CONTRACT'])
assert c.get('revision')==2 and c.get('prerequisites') is True and c.get('recovery') is True
assert c.get('local_owner') is True and c.get('coordinated_restore') is True and c.get('data_lease') is True
assert c.get('persistent_manager') is True and c.get('secure_exposure') is True
assert isinstance(c.get('data_contract'),str) and c['data_contract']
PY
      then
        cache_trusted=1
      fi
    fi
  fi
fi
cache_reuse=$((cache_trusted && management_entry))

open_cached_manager() {
  ui_step 'LOCAL' 'Opening verified local manager'
  cleanup
  trap - EXIT
  if { true </dev/tty; } 2>/dev/null; then
    exec "${sudo_cmd[@]}" "$cache_bin" manage --build-metadata "$manager_receipt" --lang en </dev/tty
  fi
  exec "${sudo_cmd[@]}" "$cache_bin" manage --build-metadata "$manager_receipt" --lang en </dev/null
}

if ((list == 0)); then
  ui_step '3/4' 'Checking for manager updates'
  if [[ $channel == commit ]]; then
    ui_warn 'Development source selected. A changed revision may take several minutes to build.'
  fi
fi
if ! python3 -I - "$channel" "$ref" "$arch" "$stage" "$list" "$refresh" "$cache_reuse" "$cache_channel" "$cache_ref" "$cache_commit" "$cache_version" "$cache_digest" "$cache_bin" <<'PY'
import gzip,hashlib,json,os,pathlib,re,shutil,subprocess,sys,tarfile,threading,time,urllib.parse
channel,ref,arch,stage,list_only,refresh,cache_trusted,cache_channel,cache_ref,cache_commit,cache_version,cache_digest,cache_bin=sys.argv[1:]
stage=pathlib.Path(stage)
API='https://api.github.com/repos/Sir-Adnan/wg-guard'
REPO='https://github.com/Sir-Adnan/wg-guard'
SHA=re.compile(r'[0-9a-f]{40}\Z')
TAG=re.compile(r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}\Z')
HEX=re.compile(r'[0-9a-f]{64}\Z')
HEARTBEAT=15.0

heartbeat_stop=threading.Event()
heartbeat_thread=None
def heartbeat():
    started=time.monotonic()
    color=sys.stderr.isatty() and os.environ.get('NO_COLOR','')=='' and os.environ.get('TERM','dumb')!='dumb'
    prefix='\x1b[36;1mINFO\x1b[0m' if color else 'INFO'
    while not heartbeat_stop.wait(HEARTBEAT):
        elapsed=int(time.monotonic()-started)
        print(f'{prefix}  Still working · {elapsed}s elapsed',file=sys.stderr,flush=True)

if list_only!='1':
    heartbeat_thread=threading.Thread(target=heartbeat,daemon=True)
    heartbeat_thread.start()

def require(value,message):
    if not value: raise ValueError(message)

def download(url,dest,limit,expected=None):
    initial=urllib.parse.urlsplit(url).hostname
    for attempt in range(6):
        parsed=urllib.parse.urlsplit(url)
        require(parsed.scheme=='https' and not parsed.username and not parsed.password and not parsed.fragment and parsed.port in (None,443),'Unsafe download URL')
        require(parsed.hostname in {initial,'release-assets.githubusercontent.com','codeload.github.com','dl.google.com'},'Untrusted redirect')
        header=stage/'http-headers'
        # Redirects are validated here, never followed implicitly by curl.
        command=['curl','--disable','--proto','=https','--fail','--silent','--show-error','--connect-timeout','15','--max-time','300',
                 '--max-filesize',str(limit),'--dump-header',str(header),'--output',str(dest),'--write-out','%{http_code}',url]
        status=subprocess.run(command,check=True,stdout=subprocess.PIPE,timeout=310).stdout.decode().strip()
        require(dest.stat().st_size<=limit,'Download exceeds size limit')
        if status in ('301','302','303','307','308'):
            require(header.stat().st_size<=64<<10,'Redirect headers exceed limit')
            locations=[line.partition(':')[2].strip() for line in header.read_text().splitlines() if line.lower().startswith('location:')]
            require(len(locations)==1,'Ambiguous redirect')
            url=urllib.parse.urljoin(url,locations[0]);continue
        require(status=='200','HTTP acquisition failed')
        size=dest.stat().st_size
        require(size>0 and (expected is None or size==expected),'Download size mismatch')
        h=hashlib.sha256()
        with dest.open('rb') as f:
            for block in iter(lambda:f.read(65536),b''):h.update(block)
        return h.hexdigest()
    raise ValueError('Too many redirects')

def metadata(url):
    file=stage/'metadata.json';download(url,file,1<<20)
    return json.loads(file.read_bytes())

def stable(r):
    return isinstance(r,dict) and not r.get('draft',False) and not r.get('prerelease',False) and bool(r.get('published_at')) and TAG.fullmatch(r.get('tag_name',''))

def releases():
    rows=metadata(API+'/releases?per_page=30&page=1')
    require(isinstance(rows,list) and len(rows)<=30,'Invalid or oversized release catalog')
    return [r for r in rows if stable(r)]

def immutable(value):
    sha=metadata(API+'/commits/'+value).get('sha','')
    require(SHA.fullmatch(sha) and (not SHA.fullmatch(value) or value==sha),'Invalid immutable commit')
    return sha

def extract(archive,dest,root,limit):
    dest.mkdir(mode=0o700);seen=set();total=0;count=0
    with gzip.open(archive,'rb') as compressed,tarfile.open(fileobj=compressed,mode='r|') as tf:
        for member in tf:
            count+=1;require(count<=50000,'Archive has too many entries')
            name=member.name.rstrip('/')
            parts=name.split('/')
            require(parts[0]==root and all(p not in ('','.','..') for p in parts) and not any(c in name for c in '\\:\x00'),'Unsafe archive path')
            require(name not in seen and (member.isfile() or member.isdir()),'Unsafe or duplicate archive member')
            seen.add(name);total+=member.size
            require(0<=member.size and total<=limit,'Expanded archive exceeds limit')
            target=dest.joinpath(*parts[1:])
            if len(parts)==1:require(member.isdir(),'Invalid root');continue
            if member.isdir():target.mkdir(mode=0o700,parents=True,exist_ok=True);continue
            target.parent.mkdir(mode=0o700,parents=True,exist_ok=True)
            with target.open('xb') as out,tf.extractfile(member) as data:
                shutil.copyfileobj(data,out,65536)
            target.chmod(0o700 if member.mode&0o111 else 0o600)
        require(len(compressed.read((1<<20)+1))<=1<<20,'Archive trailing data exceeds limit')

def go_version(value):
    match=re.fullmatch(r'go(1)\.([0-9]+)(?:\.([0-9]+))?',value)
    return tuple(int(v or 0) for v in match.groups()) if match else (0,0,0)

def compiler(minimum,env):
    existing=shutil.which('go')
    if existing:
        try:
            out=subprocess.run([existing,'version'],env=env,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=15,check=True).stdout.decode().split()
            if len(out)>=3 and go_version(out[2])>=minimum:return existing
        except (subprocess.SubprocessError,OSError):pass
    rows=metadata('https://go.dev/dl/?mode=json')
    require(isinstance(rows,list),'Invalid Go metadata')
    for release in rows:
        version=release.get('version','')
        if not release.get('stable') or go_version(version)<minimum:continue
        files=[f for f in release.get('files',[]) if f.get('os')=='linux' and f.get('arch')==arch and f.get('kind')=='archive']
        require(len(files)==1,'Ambiguous Go toolchain')
        f=files[0];name=version+'.linux-'+arch+'.tar.gz'
        require(f.get('filename')==name and HEX.fullmatch(f.get('sha256','')) and 0<f.get('size',0)<=256<<20,'Invalid Go toolchain metadata')
        archive=stage/'go.tar.gz'
        require(download('https://go.dev/dl/'+name,archive,256<<20,f['size'])==f['sha256'],'Go toolchain checksum mismatch')
        extract(archive,stage/'toolchain','go',1<<30)
        return str(stage/'toolchain'/'bin'/'go')
    raise ValueError('No compatible official Go compiler')

try:
    if list_only=='1':
        for r in releases():print(r['tag_name'])
        sys.exit(0)
    release=None
    if channel=='release':
        if ref=='latest':
            rows=releases();require(rows,'No published stable release exists; use --commit main explicitly for development')
            ref=rows[0]['tag_name']
        require(TAG.fullmatch(ref),'Invalid release tag')
        release=metadata(API+'/releases/tags/'+ref)
        require(stable(release) and release['tag_name']==ref,'Release is not published stable')
        sha=immutable(ref);version=ref
        selected_ref=ref
    else:
        require(ref=='main' or SHA.fullmatch(ref),'Commit must be main or a full lowercase 40-character SHA')
        sha=immutable(ref);version='0.0.0-dev.'+sha[:12]
        selected_ref=sha

    if refresh!='1' and cache_trusted=='1' and cache_channel==channel and cache_ref==selected_ref and cache_commit==sha and cache_version==version:
        (stage/'build.json').write_text(json.dumps(dict(Channel=cache_channel,Ref=cache_ref,Commit=cache_commit,Version=cache_version,SHA256=cache_digest,BinaryPath=cache_bin)))
        (stage/'build.json').chmod(0o600)
        (stage/'cache-hit').touch(mode=0o600)
        sys.exit(0)

    candidate=stage/'candidate.part'
    if channel=='release':
        name='wg-guard_linux_'+arch
        def asset(name,limit):
            values=[a for a in release.get('assets',[]) if a.get('name')==name]
            require(len(values)==1,'Missing or duplicate release asset')
            a=values[0]
            require(a.get('browser_download_url')==REPO+'/releases/download/'+ref+'/'+name and 0<a.get('size',0)<=limit,'Unsafe release asset')
            return a
        checks=asset('checksums.txt',64<<10);binary=asset(name,256<<20)
        sums=stage/'checksums.txt';download(checks['browser_download_url'],sums,64<<10,checks['size'])
        seen={}
        for line in sums.read_text().splitlines():
            if not line:continue
            fields=line.split();require(len(fields)==2 and HEX.fullmatch(fields[0]),'Malformed checksum manifest')
            filename=fields[1].removeprefix('*');require(TAG.fullmatch(filename) and filename not in seen,'Ambiguous checksum manifest')
            seen[filename]=fields[0]
        require(name in seen,'Missing binary checksum')
        digest=download(binary['browser_download_url'],candidate,256<<20,binary['size'])
        require(digest==seen[name],'Binary SHA-256 mismatch')
    else:
        archive=stage/'source.tar.gz';download('https://codeload.github.com/Sir-Adnan/wg-guard/tar.gz/'+sha,archive,128<<20)
        source=stage/'source';extract(archive,source,'wg-guard-'+sha,512<<20)
        mod=(source/'go.mod').read_text();match=re.search(r'^go (1\.[0-9]+(?:\.[0-9]+)?)\s*$',mod,re.M)
        require(match,'Source has no valid Go requirement')
        allowed=('PATH','HTTPS_PROXY','HTTP_PROXY','NO_PROXY','https_proxy','http_proxy','no_proxy','SSL_CERT_FILE','SSL_CERT_DIR')
        env={k:os.environ[k] for k in allowed if k in os.environ}
        env.update(HOME=str(stage),TMPDIR=str(stage),GOCACHE=str(stage/'cache'),GOMODCACHE=str(stage/'modules'),GOPATH=str(stage/'gopath'),GOENV='off',GOWORK='off',GOTOOLCHAIN='local',CGO_ENABLED='0',GOOS='linux',GOARCH=arch,GOPROXY='https://proxy.golang.org,direct',GOSUMDB='sum.golang.org')
        go=compiler(go_version('go'+match[1]),env)
        flags='-s -w -X github.com/Sir-Adnan/wg-guard/internal/version.Version='+version+' -X github.com/Sir-Adnan/wg-guard/internal/version.Commit='+sha
        with (stage/'build.log').open('ab') as build_log:
            subprocess.run([go,'build','-trimpath','-buildvcs=false','-mod=readonly','-modcacherw','-ldflags',flags,'-o',str(candidate),'./cmd/wg-guard'],cwd=source,env=env,stdout=build_log,stderr=build_log,check=True,timeout=900)
        require(candidate.is_file() and 0<candidate.stat().st_size<=256<<20,'Invalid compiler output')
        h=hashlib.sha256()
        with candidate.open('rb') as f:
            for block in iter(lambda:f.read(65536),b''):h.update(block)
        digest=h.hexdigest()
    candidate.chmod(0o700);candidate.rename(stage/'wg-guard')
    # Probe without privilege or node-data access; cap output on disk and time.
    import resource
    def contract_limits():resource.setrlimit(resource.RLIMIT_FSIZE,(4096,4096))
    contract_path=stage/'installer-contract.json'
    with contract_path.open('wb') as contract_output:
        subprocess.run([str(stage/'wg-guard'),'installer-contract'],stdin=subprocess.DEVNULL,stdout=contract_output,stderr=subprocess.DEVNULL,timeout=15,check=True,preexec_fn=contract_limits)
    contract=json.loads(contract_path.read_bytes())
    require(contract.get('revision')==2 and contract.get('prerequisites') is True and contract.get('recovery') is True and contract.get('local_owner') is True and contract.get('coordinated_restore') is True and contract.get('data_lease') is True and contract.get('persistent_manager') is True and contract.get('secure_exposure') is True and isinstance(contract.get('data_contract'),str) and contract['data_contract'],'Selected build lacks the Phase 8.2 persistent-manager/secure-exposure installer contract; choose a compatible build')
    (stage/'build.json').write_text(json.dumps(dict(Channel=channel,Ref=selected_ref,Commit=sha,Version=version,SHA256=digest,BinaryPath=str(stage/'wg-guard'))))
    (stage/'build.json').chmod(0o600)
except subprocess.SubprocessError:
    # CalledProcessError includes argv; redirect URLs may contain temporary tokens.
    (stage/'fallback-ok').touch(mode=0o600)
    print('WG-Guard acquisition failed: download or compiler command failed/timed out',file=sys.stderr)
    sys.exit(1)
except (ValueError,KeyError,TypeError,OSError,tarfile.TarError) as error:
    print('WG-Guard acquisition failed: '+str(error),file=sys.stderr)
    sys.exit(1)
finally:
    heartbeat_stop.set()
    if heartbeat_thread is not None:heartbeat_thread.join(timeout=1)
PY
then
  if ((management_entry && refresh == 0 && cache_trusted)) && [[ -f $stage/fallback-ok ]]; then
    ui_warn 'GitHub update check unavailable; opening the verified local manager.'
    open_cached_manager
  fi
  ui_error 'Verified build acquisition did not complete. Review the message above and retry.'
  exit 1
fi
((list)) && exit 0
read -r selected_version selected_commit run_bin < <(python3 -I - "$stage/build.json" <<'PY'
import json,sys
build=json.load(open(sys.argv[1],'rb'))
print(build['Version'],build['Commit'][:12],build['BinaryPath'])
PY
)
ui_ok "Build ready: $selected_version · $selected_commit"
# Every accepted build lives in a separate manager cache. This keeps bootstrap
# refreshes from changing the binary used by an active native/Docker service.
manager_dir=${manager_receipt%/*}
if "${sudo_cmd[@]}" test -L "$manager_dir"; then
  die "Refusing unsafe manager cache path: $manager_dir"
fi
"${sudo_cmd[@]}" install -d -m 0700 "$manager_dir"
if ! "${sudo_cmd[@]}" test -f "$installed_state" && "${sudo_cmd[@]}" test -e "$installed_bin" && ((cache_trusted == 0)); then
  die "Refusing to replace an unmanaged $installed_bin; move it explicitly and retry"
fi
cache_hit=0
[[ -f $stage/cache-hit ]] && cache_hit=1
if ((cache_hit == 0)) || [[ $run_bin != "$manager_bin" ]]; then
  manager_tmp=$("${sudo_cmd[@]}" mktemp "${manager_dir}/.manager.new.XXXXXXXX")
  receipt_tmp=$("${sudo_cmd[@]}" mktemp "${manager_dir}/.manager-build.new.XXXXXXXX")
  persist_cleanup() { "${sudo_cmd[@]}" rm -f -- "$manager_tmp" "$receipt_tmp"; }
  trap 'persist_cleanup; cleanup' EXIT
  "${sudo_cmd[@]}" install -m 0755 "$run_bin" "$manager_tmp"
  python3 -I - "$stage/build.json" "$stage/manager-build.json" "$manager_bin" <<'PY'
import json,pathlib,sys
source,target,binary=map(pathlib.Path,sys.argv[1:])
build=json.loads(source.read_bytes())
build['BinaryPath']=str(binary)
target.write_text(json.dumps(build,separators=(',',':'))+'\n')
target.chmod(0o600)
PY
  "${sudo_cmd[@]}" install -m 0600 "$stage/manager-build.json" "$receipt_tmp"
  "${sudo_cmd[@]}" mv -f -- "$manager_tmp" "$manager_bin"
  "${sudo_cmd[@]}" mv -f -- "$receipt_tmp" "$manager_receipt"
  trap cleanup EXIT
fi
run_bin=$manager_bin
run_metadata=$manager_receipt
if ! "${sudo_cmd[@]}" test -f "$installed_state"; then
  bin_tmp=$("${sudo_cmd[@]}" mktemp "${installed_bin}.new.XXXXXXXX")
  persist_cleanup() { "${sudo_cmd[@]}" rm -f -- "$bin_tmp"; }
  trap 'persist_cleanup; cleanup' EXIT
  "${sudo_cmd[@]}" install -m 0755 "$manager_bin" "$bin_tmp"
  "${sudo_cmd[@]}" mv -f -- "$bin_tmp" "$installed_bin"
  trap cleanup EXIT
fi
if ((cache_hit)); then
  ui_ok 'Local manager is current'
else
  ui_ok 'Local manager updated · service version unchanged'
fi
ui_step '4/4' 'Opening WG-Guard manager'
# A piped script is never an answer stream. Reopen the controlling terminal
# only for interactive entry; noninteractive flags remain usable without it.
interactive=1
for arg in "${args[@]}"; do [[ "$arg" == --yes || "$arg" == -yes || "$arg" == --yes=true ]] && interactive=0; done
entry=manage
# Explicit setup arguments retain the install-only contract. Locale alone does
# not force a reinstall when the operator reruns the one-command entry.
for ((i=0; i<${#args[@]}; i++)); do
  case "${args[i]}" in
    --lang|-lang) i=$((i+1)) ;;
    --lang=*|-lang=*) ;;
    *) entry=install ;;
  esac
done
((interactive)) || entry=install
dispatch_args=("${args[@]}")
[[ $entry == manage ]] && dispatch_args=(--lang en)
if ((interactive)) && { true </dev/tty; } 2>/dev/null; then
  "${sudo_cmd[@]}" "$run_bin" "$entry" --build-metadata "$run_metadata" "${dispatch_args[@]}" </dev/tty
else
  "${sudo_cmd[@]}" "$run_bin" "$entry" --build-metadata "$run_metadata" "${dispatch_args[@]}" </dev/null
fi
