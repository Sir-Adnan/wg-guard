#!/usr/bin/env bash
# Acquisition entry. The acquired Go binary owns installation/management/lifecycle.
set -euo pipefail
umask 077
channel=release
ref=latest
list=0
args=()
while (($#)); do
  case "$1" in
    --help|-h)
      printf '%s\n' 'WG-Guard GitHub bootstrap' \
        'Usage: bash install.sh [--release latest|TAG | --commit main|FULL_SHA | --list-releases] [-- INSTALL_FLAGS]' \
        'Default: latest published stable release. Development source is never selected implicitly.' \
        'Interactive default opens management (fresh nodes enter setup); --lang fa|en is supported.' \
        'Install flags (for example --yes --mode native) are forwarded unchanged.'
      exit 0 ;;
    --release|--commit)
      (($# >= 2)) || { printf 'Missing selection value\n' >&2; exit 2; }
      channel=${1#--}; ref=$2; shift 2 ;;
    --list-releases) list=1; shift ;;
    --) shift; args+=("$@"); break ;;
    *) args+=("$1"); shift ;;
  esac
done
[[ $(uname -s) == Linux ]] || { printf 'Only Linux is supported\n' >&2; exit 2; }
case $(uname -m) in x86_64|amd64) arch=amd64;; aarch64|arm64) arch=arm64;; *) printf 'Unsupported architecture\n' >&2; exit 2;; esac
sudo_cmd=()
if [[ $(id -u) != 0 ]]; then
  command -v sudo >/dev/null || { printf 'Run as root or install sudo\n' >&2; exit 2; }
  sudo_cmd=(sudo)
fi
missing=()
for pair in curl:curl python3:python3 tar:tar sha256sum:coreutils; do
  command -v "${pair%%:*}" >/dev/null || missing+=("${pair#*:}")
done
[[ -s /etc/ssl/certs/ca-certificates.crt ]] || missing+=(ca-certificates)
if ((${#missing[@]})); then
  command -v apt-get >/dev/null || { printf 'Install prerequisites: %s\n' "${missing[*]}" >&2; exit 2; }
  "${sudo_cmd[@]}" apt-get update
  "${sudo_cmd[@]}" apt-get install -y --no-install-recommends "${missing[@]}"
fi
stage=$(mktemp -d -t wg-guard-bootstrap.XXXXXXXX)
cleanup() { rm -rf -- "$stage"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
python3 -I - "$channel" "$ref" "$arch" "$stage" "$list" <<'PY'
import gzip,hashlib,json,os,pathlib,re,shutil,subprocess,sys,tarfile,urllib.parse
channel,ref,arch,stage,list_only=sys.argv[1:]
stage=pathlib.Path(stage)
API='https://api.github.com/repos/Sir-Adnan/wg-guard'
REPO='https://github.com/Sir-Adnan/wg-guard'
SHA=re.compile(r'[0-9a-f]{40}\Z')
TAG=re.compile(r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}\Z')
HEX=re.compile(r'[0-9a-f]{64}\Z')

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
    candidate=stage/'candidate.part'
    if channel=='release':
        if ref=='latest':
            rows=releases();require(rows,'No published stable release exists; use --commit main explicitly for development')
            ref=rows[0]['tag_name']
        require(TAG.fullmatch(ref),'Invalid release tag')
        r=metadata(API+'/releases/tags/'+ref)
        require(stable(r) and r['tag_name']==ref,'Release is not published stable')
        sha=immutable(ref);version=ref
        name='wg-guard_linux_'+arch
        def asset(name,limit):
            values=[a for a in r.get('assets',[]) if a.get('name')==name]
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
        require(ref=='main' or SHA.fullmatch(ref),'Commit must be main or a full lowercase 40-character SHA')
        sha=immutable(ref);version='0.0.0-dev.'+sha[:12]
        print('Development source: '+sha,file=sys.stderr)
        archive=stage/'source.tar.gz';download('https://codeload.github.com/Sir-Adnan/wg-guard/tar.gz/'+sha,archive,128<<20)
        source=stage/'source';extract(archive,source,'wg-guard-'+sha,512<<20)
        mod=(source/'go.mod').read_text();match=re.search(r'^go (1\.[0-9]+(?:\.[0-9]+)?)\s*$',mod,re.M)
        require(match,'Source has no valid Go requirement')
        allowed=('PATH','HTTPS_PROXY','HTTP_PROXY','NO_PROXY','https_proxy','http_proxy','no_proxy','SSL_CERT_FILE','SSL_CERT_DIR')
        env={k:os.environ[k] for k in allowed if k in os.environ}
        env.update(HOME=str(stage),TMPDIR=str(stage),GOCACHE=str(stage/'cache'),GOMODCACHE=str(stage/'modules'),GOPATH=str(stage/'gopath'),GOENV='off',GOWORK='off',GOTOOLCHAIN='local',CGO_ENABLED='0',GOOS='linux',GOARCH=arch,GOPROXY='https://proxy.golang.org,direct',GOSUMDB='sum.golang.org')
        go=compiler(go_version('go'+match[1]),env)
        flags='-s -w -X github.com/Sir-Adnan/wg-guard/internal/version.Version='+version+' -X github.com/Sir-Adnan/wg-guard/internal/version.Commit='+sha
        subprocess.run([go,'build','-trimpath','-buildvcs=false','-mod=readonly','-modcacherw','-ldflags',flags,'-o',str(candidate),'./cmd/wg-guard'],cwd=source,env=env,check=True,timeout=900)
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
    require(contract.get('revision')==1 and contract.get('prerequisites') is True and contract.get('recovery') is True and contract.get('local_owner') is True and contract.get('coordinated_restore') is True and isinstance(contract.get('data_contract'),str) and contract['data_contract'],'Selected build lacks the Phase 8.1 owner/restore installer contract; choose a compatible build')
    (stage/'build.json').write_text(json.dumps(dict(Channel=channel,Ref=ref if channel=='release' else sha,Commit=sha,Version=version,SHA256=digest,BinaryPath=str(stage/'wg-guard'))))
    (stage/'build.json').chmod(0o600)
    print('Selected '+version+' ('+sha+'), SHA-256 '+digest,file=sys.stderr)
except subprocess.SubprocessError:
    # CalledProcessError includes argv; redirect URLs may contain temporary tokens.
    print('WG-Guard acquisition failed: download or compiler command failed/timed out',file=sys.stderr)
    sys.exit(1)
except (ValueError,KeyError,TypeError,OSError,tarfile.TarError) as error:
    print('WG-Guard acquisition failed: '+str(error),file=sys.stderr)
    sys.exit(1)
PY
((list)) && exit 0
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
if ((interactive)) && { true </dev/tty; } 2>/dev/null; then
  "${sudo_cmd[@]}" "$stage/wg-guard" "$entry" --build-metadata "$stage/build.json" "${args[@]}" </dev/tty
else
  "${sudo_cmd[@]}" "$stage/wg-guard" "$entry" --build-metadata "$stage/build.json" "${args[@]}" </dev/null
fi
