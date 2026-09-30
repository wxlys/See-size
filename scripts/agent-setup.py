#!/usr/bin/env python3
"""SeeSize Ubuntu/Debian amd64 agent installer. No silent upgrades."""
import argparse
from contextlib import contextmanager
import getpass
import hashlib
import json
import os
from pathlib import Path
import platform
import pwd
import re
import shutil
import shlex
import subprocess
import sys
import tempfile
import time
import urllib.parse
import urllib.request
import urllib.error
import http.client
import signal
import ssl

ROOT = Path('/opt/seesize-agent')
DATA = Path('/var/lib/seesize-agent')
UNIT = Path('/etc/systemd/system/seesize-agent.service')
ACCOUNT = 'seesize-agent'
REPO = 'https://github.com/wxlys/See-size/releases/download/'
COMPONENTS = ('seesize-agent-linux-amd64', 'seesize-enroll-linux-amd64')
MAX_BINARY = 64 * 1024 * 1024
DOWNLOAD_SECONDS = 180
DOWNLOAD_ATTEMPTS = 3

class DownloadDeadline(ValueError):
    pass

@contextmanager
def download_deadline(seconds):
    # Installer runs in the main thread on Linux. Interrupt even DNS/slow reads.
    if not hasattr(signal, 'setitimer'):
        yield  # Non-Linux unit tests; Linux is the supported installation target.
        return
    def expired(signum, frame):
        raise DownloadDeadline('Download total time limit reached; retry later or use a trusted --bundle')
    previous = signal.signal(signal.SIGALRM, expired)
    timer = signal.setitimer(signal.ITIMER_REAL, seconds)
    try:
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)
        if timer[0]: signal.setitimer(signal.ITIMER_REAL, *timer)

def version(value):
    if not re.fullmatch(r'v\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?', value):
        raise ValueError('Expected explicit release version such as v0.1.0')
    return value

def hub_url(value):
    u = urllib.parse.urlsplit(value)
    if u.scheme != 'https' or not u.hostname or u.username or u.password or u.query or u.fragment or u.path not in ('', '/'):
        raise ValueError('Hub must be an HTTPS origin without credentials, query or path')
    # Restrict to literal ASCII URL characters safe in systemd ExecStart.
    if not re.fullmatch(r'https://[A-Za-z0-9.\-\[\]:]+/?', value):
        raise ValueError('Invalid Hub URL')
    try:
        if u.port is not None and not 1 <= u.port <= 65535: raise ValueError('port')
    except ValueError:
        raise ValueError('Invalid HTTPS port')
    return value.rstrip('/')

def device_id(value):
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,127}', value):
        raise ValueError('Use 1..128 letters, digits, dot, underscore or hyphen for device ID')
    return value

def manifest(body, requested):
    m = json.loads(body)
    if m.get('schema') != 1 or m.get('version') != version(requested) or m.get('platform') != 'linux-amd64':
        raise ValueError('Release metadata version/platform mismatch')
    if set(m.get('sha256', {})) != set(COMPONENTS):
        raise ValueError('Release must contain agent and enrollment hashes')
    if any(not isinstance(h,str) or not re.fullmatch('[0-9a-f]{64}', h) for h in m['sha256'].values()):
        raise ValueError('Invalid SHA256')
    if not isinstance(m.get('hub_requirement'), str) or not m['hub_requirement']:
        raise ValueError('Missing Hub compatibility statement')
    return m

class HTTPSRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        if urllib.parse.urlsplit(newurl).scheme != 'https':
            raise ValueError('Refusing HTTPS downgrade')
        return super().redirect_request(req, fp, code, msg, headers, newurl)

def fetch(url, limit):
    if urllib.parse.urlsplit(url).scheme != 'https': raise ValueError('HTTPS required')
    # Do not print URLs, redirect query strings or raw exceptions (may contain secrets).
    print(f'Downloading HTTPS resource (limit {limit} bytes; total budget {DOWNLOAD_SECONDS}s).', flush=True)
    with download_deadline(DOWNLOAD_SECONDS):
        for attempt in range(1, DOWNLOAD_ATTEMPTS + 1):
            print(f'Connecting: attempt {attempt}/{DOWNLOAD_ATTEMPTS}; Ctrl+C cancels.', flush=True)
            try:
                with urllib.request.build_opener(HTTPSRedirect()).open(url, timeout=15) as response:
                    length = response.headers.get('Content-Length')
                    total = int(length) if length is not None else None
                    if total is not None and (total < 0 or total > limit):
                        raise ValueError('Download exceeds size limit or invalid Content-Length')
                    body = bytearray()
                    last = started = time.monotonic()
                    while True:
                        chunk = response.read1(min(64 * 1024, limit + 1 - len(body)))
                        if not chunk: break
                        body.extend(chunk)
                        if len(body) > limit: raise ValueError('Download exceeds size limit')
                        now = time.monotonic()
                        if now - last >= 2:
                            print(f'Downloaded {len(body)}/{total if total is not None else "unknown"} bytes; {len(body)/max(now-started,0.001):.0f} B/s', flush=True)
                            last = now
                    if total is not None and len(body) != total:
                        raise http.client.IncompleteRead(bytes(body))
                    print(f'Download complete: {len(body)} bytes.', flush=True)
                    return bytes(body)
            except urllib.error.HTTPError as error:
                error.close()
                if error.code not in (408,429,500,502,503,504):
                    raise ValueError(f'Download rejected: HTTP {error.code}; check release availability') from None
                reason = f'HTTP {error.code}'
            except (urllib.error.URLError, OSError, http.client.HTTPException) as error:
                cause = getattr(error, 'reason', error)
                if isinstance(cause, ssl.SSLCertVerificationError):
                    raise ValueError('TLS certificate verification failed; not bypassed') from None
                reason = 'network interruption or read timeout'
            if attempt == DOWNLOAD_ATTEMPTS:
                raise ValueError('Download failed after limited retries; retry later or use a trusted --bundle')
            print(f'{reason}; retrying from zero in 2s.', flush=True)
            time.sleep(2)

def validate_binary(body, expected):
    if len(body) < 20 or body[:6] != b'\x7fELF\x02\x01' or int.from_bytes(body[18:20], 'little') != 62:
        raise ValueError('Not a Linux amd64 ELF binary')
    if hashlib.sha256(body).hexdigest() != expected: raise ValueError('SHA256 mismatch')

def run(*args, **kwargs):
    return subprocess.run(args, check=True, timeout=60, **kwargs)

def preflight():
    if os.geteuid() != 0: raise ValueError('Run installation with sudo; no sudo password is collected by this tool')
    if platform.system() != 'Linux' or platform.machine() != 'x86_64': raise ValueError('Linux x86_64 required')
    osinfo = Path('/etc/os-release').read_text()
    if not re.search(r'^ID="?(ubuntu|debian)"?$', osinfo, re.M): raise ValueError('Only Ubuntu/Debian supported')
    for command in ('systemctl', 'useradd', 'runuser'):
        if not shutil.which(command): raise ValueError('Required system command missing: ' + command)
    if shutil.disk_usage('/opt').free < 512 * 1024**2: raise ValueError('At least 512 MiB free required')

def install(args):
    preflight()
    if not sys.stdin.isatty(): raise ValueError('Use a downloaded script in an interactive terminal, not a pipe')
    os.umask(0o022)
    for path in (ROOT, DATA, UNIT):
        if path.exists() or path.is_symlink(): raise ValueError('Existing installation/path; refusing overwrite: ' + str(path))
    state = run('systemctl', 'show', UNIT.name, '--property=LoadState', '--value', capture_output=True, text=True).stdout.strip()
    if state != 'not-found': raise ValueError('Existing service; refusing takeover')
    try: pwd.getpwnam(ACCOUNT)
    except KeyError: pass
    else: raise ValueError('Service account already exists; refusing reuse')
    release = version(args.version)
    # All downloads and verification precede registration and system changes.
    with tempfile.TemporaryDirectory(prefix='seesize-install-') as scratch:
        staged = Path(scratch)
        if args.bundle:
            source = Path(args.bundle).resolve(strict=True)
            raw = (source/'agent-release.json').read_bytes()
        else:
            raw = fetch(REPO + release + '/agent-release.json', 64 * 1024)
        m = manifest(raw, release)
        for name in COMPONENTS:
            body = (source/name).read_bytes() if args.bundle else fetch(REPO + release + '/' + name, MAX_BINARY)
            if len(body) > MAX_BINARY: raise ValueError('Binary too large')
            validate_binary(body, m['sha256'][name])
            (staged/name).write_bytes(body)
        hub = hub_url(input('Hub HTTPS address: ').strip())
        identity = device_id(input('Device ID: ').strip())
        print('Hub compatibility requirement: ' + ascii(m['hub_requirement']))
        print('This installer cannot automatically determine the running Hub version.')
        if input('Confirm administrator verified compatibility; type install: ').strip() != 'install':
            raise ValueError('Cancelled; no system changes')
        # Successful response is only connectivity, not a compatibility guarantee.
        health = json.loads(fetch(hub + '/healthz', 4096))
        if health.get('status') != 'ok': raise ValueError('Hub health check failed')
        run('useradd', '--system', '--user-group', '--home-dir', str(DATA), '--no-create-home', '--shell', '/usr/sbin/nologin', ACCOUNT)
        account = pwd.getpwnam(ACCOUNT)
        ROOT.mkdir(mode=0o755)
        DATA.mkdir(mode=0o700)
        os.chown(DATA, account.pw_uid, account.pw_gid)
        for name in COMPONENTS:
            shutil.copyfile(staged/name, ROOT/name)
            (ROOT/name).chmod(0o755)
        (ROOT/'agent-release.json').write_bytes(raw)
        (ROOT/'connection.json').write_text(json.dumps({'hub':hub, 'id':identity}))
        shutil.copyfile(Path(__file__).resolve(), ROOT/'agent-setup.py')
    print('Programs installed. Starting registration; on failure use the register action to retry.')
    register()

def register():
    preflight()
    # Refuse unowned installer paths and symlink substitutions.
    for path in (ROOT, ROOT/'connection.json', ROOT/'agent-release.json', *[ROOT/n for n in COMPONENTS]):
        if path.is_symlink() or path.stat().st_uid != 0 or path.stat().st_mode & 0o022:
            raise ValueError('Unsafe installer path: ' + str(path))
    connection = json.loads((ROOT/'connection.json').read_text())
    hub, identity = hub_url(connection['hub']), device_id(connection['id'])
    saved = json.loads((ROOT/'agent-release.json').read_text())
    m = manifest(json.dumps(saved), saved['version'])
    for name in COMPONENTS: validate_binary((ROOT/name).read_bytes(), m['sha256'][name])
    account = pwd.getpwnam(ACCOUNT)
    if DATA.is_symlink() or DATA.stat().st_uid != account.pw_uid or DATA.stat().st_mode & 0o077:
        raise ValueError('Unsafe credential directory')
    if UNIT.exists() or UNIT.is_symlink(): raise ValueError('Service already configured; use status, not register')
    token = DATA/'agent.token'
    if token.is_symlink(): raise ValueError('Credential symlink refused')
    if token.exists() and token.stat().st_size == 0:
        # Only the enrollment tool's empty reservation, never a valid credential.
        token.unlink()
    if not token.exists():
        if not sys.stdin.isatty(): raise ValueError('Interactive terminal required for hidden registration code')
        code = getpass.getpass('One-time registration code (hidden): ').strip()
        if not code: raise ValueError('Empty registration code')
        env = {k:v for k,v in os.environ.items() if not k.startswith('SEE_SIZE_')}
        env['SEE_SIZE_ENROLL_CODE'] = code
        result = subprocess.run(['runuser','-u',ACCOUNT,'--',str(ROOT/COMPONENTS[1]),'-hub',hub,'-id',identity,'-out',str(token)],env=env,capture_output=True,timeout=30)
        env.pop('SEE_SIZE_ENROLL_CODE', None)
        code = ''
        if result.returncode:
            raise ValueError('Registration failed. Verify URL/ID, generate a fresh code and retry register. Installed files retained; credential/code not printed.')
    if not token.is_file() or not token.stat().st_size or token.stat().st_uid != account.pw_uid or token.stat().st_mode & 0o077:
        raise ValueError('Invalid credential file')
    unit = ('[Unit]\nDescription=SeeSize managed agent\nWants=network-online.target\nAfter=network-online.target\n'
            '[Service]\nType=simple\nUser='+ACCOUNT+'\nGroup='+ACCOUNT+'\n'
            'ExecStart='+str(ROOT/COMPONENTS[0])+' -hub '+hub+' -id '+identity+' -token-file '+str(token)+' -interval 10s\n'
            'Restart=on-failure\nRestartSec=5\nUMask=0077\nNoNewPrivileges=yes\nProtectSystem=strict\nProtectHome=yes\n'
            '[Install]\nWantedBy=multi-user.target\n')
    with UNIT.open('x') as stream: stream.write(unit)
    run('systemctl','daemon-reload')
    run('systemctl','enable','--now',UNIT.name)
    print('Registered; service enabled and start requested. Confirm device online in Hub; service start alone is not acceptance.')

def check_update(args):
    current = json.loads((ROOT/'agent-release.json').read_text())
    target = version(args.version)
    raw = (Path(args.bundle)/'agent-release.json').read_bytes() if args.bundle else fetch(REPO+target+'/agent-release.json',64*1024)
    m = manifest(raw,target)
    print('Installed package: '+version(current['version']))
    print('Requested package: '+m['version'])
    print('Hub compatibility: '+ascii(m['hub_requirement']))
    print('Same package version.' if target==current['version'] else 'Different package version (not necessarily newer).')
    print('Read-only check. Use update with an explicit target and type update to confirm. No automatic/forced update.')

def atomic_copy(source, destination):
    fd, name = tempfile.mkstemp(prefix='.replace-', dir=destination.parent)
    os.close(fd)
    temporary = Path(name)
    try:
        shutil.copyfile(source, temporary)
        temporary.chmod(0o755 if destination.name in COMPONENTS else 0o644)
        with temporary.open('r+b') as stream: os.fsync(stream.fileno())
        temporary.replace(destination)
    finally:
        temporary.unlink(missing_ok=True)

def validate_rollback(path, names):
    if not path.exists() and not path.is_symlink(): return
    if path.is_symlink() or not path.is_dir() or path.stat().st_uid != 0:
        raise ValueError('Unsafe rollback directory')
    if {p.name for p in path.iterdir()} != set(names): raise ValueError('Unexpected rollback contents; inspect manually')
    for p in path.iterdir():
        if p.is_symlink() or not p.is_file() or p.stat().st_uid != 0:
            raise ValueError('Unsafe rollback file')

@contextmanager
def update_workspace():
    if any(ROOT.glob('.update-*')):
        raise ValueError('Interrupted update workspace exists; inspect recovery files before retrying')
    work = Path(tempfile.mkdtemp(prefix='.update-', dir=ROOT))
    try:
        yield work
    finally:
        if (work/'restore-required').exists():
            print('Recovery files retained for inspection: '+str(work), file=sys.stderr)
        else:
            shutil.rmtree(work)

def update(args):
    preflight()
    if not sys.stdin.isatty(): raise ValueError('Interactive confirmation required')
    os.umask(0o022)
    names = (*COMPONENTS, 'agent-release.json', 'agent-setup.py')
    for path in (ROOT, UNIT, ROOT/'connection.json', *[ROOT/n for n in names]):
        if path.is_symlink() or path.stat().st_uid != 0 or path.stat().st_mode & 0o022:
            raise ValueError('Unsafe installed path: ' + str(path))
    current = json.loads((ROOT/'agent-release.json').read_text())
    current = manifest(json.dumps(current), current['version'])
    for name in COMPONENTS: validate_binary((ROOT/name).read_bytes(), current['sha256'][name])
    connection = json.loads((ROOT/'connection.json').read_text())
    hub, identity = hub_url(connection['hub']), device_id(connection['id'])
    token = DATA/'agent.token'
    account = pwd.getpwnam(ACCOUNT)
    for path in (DATA, token):
        if path.is_symlink() or path.stat().st_uid != account.pw_uid or path.stat().st_mode & 0o077:
            raise ValueError('Unsafe credential path')
    if not token.is_file() or not token.stat().st_size: raise ValueError('Missing credential')
    expected_args = [str(ROOT/COMPONENTS[0]), '-hub', hub, '-id', identity, '-token-file', str(token), '-interval', '10s']
    starts = [line[len('ExecStart='):] for line in UNIT.read_text().splitlines() if line.startswith('ExecStart=')]
    if len(starts) != 1 or shlex.split(starts[0]) != expected_args:
        raise ValueError('Custom service arguments require manual upgrade')
    if run('systemctl','show',UNIT.name,'-p','DropInPaths','--value',capture_output=True,text=True).stdout.strip():
        raise ValueError('Service overrides require manual upgrade')
    state = run('systemctl','show',UNIT.name,'-p','ActiveState','--value',capture_output=True,text=True).stdout.strip()
    if state != 'active': raise ValueError('Start and verify existing service before updating')
    rollback = ROOT/'rollback'
    validate_rollback(rollback, names)
    target = version(args.version)
    if target == current['version']: raise ValueError('Already this package version; no replacement')
    with update_workspace() as work:
        candidate = work/'candidate'; candidate.mkdir()
        previous = work/'previous'; previous.mkdir()
        raw = (Path(args.bundle)/'agent-release.json').read_bytes() if args.bundle else fetch(REPO+target+'/agent-release.json',64*1024)
        desired = manifest(raw,target)
        for name in COMPONENTS:
            body = (Path(args.bundle)/name).read_bytes() if args.bundle else fetch(REPO+target+'/'+name,MAX_BINARY)
            if len(body)>MAX_BINARY: raise ValueError('Binary too large')
            validate_binary(body,desired['sha256'][name])
            (candidate/name).write_bytes(body)
        (candidate/'agent-release.json').write_bytes(raw)
        # The reviewed updater used for this invocation becomes the installed tool.
        shutil.copyfile(Path(__file__).resolve(),candidate/'agent-setup.py')
        print('Agent package: '+current['version']+' -> '+target)
        print('Hub requirement: '+ascii(desired['hub_requirement']))
        print('Verify this Hub requirement with your administrator; automatic compatibility detection is not available.')
        print('This may be a downgrade. Hub is NOT updated. Device configuration/credential and boot enablement are preserved.')
        if input('Type update to accept the requirement and replace this Agent; Enter cancels: ').strip()!='update':
            print('Cancelled; running service unchanged.'); return
        for name in names: shutil.copyfile(ROOT/name,previous/name)
        unchanged = {p:hashlib.sha256(p.read_bytes()).digest() for p in (UNIT,ROOT/'connection.json',token)}
        def verify_running():
            # The exact installed agent sends one real heartbeat with existing credentials.
            # No -k, no re-enrollment, no management credential on the monitored host.
            probe = subprocess.run(['runuser','-u',ACCOUNT,'--',*expected_args,'-once'],capture_output=True,timeout=30)
            if probe.returncode: raise ValueError('Agent heartbeat probe failed')
            time.sleep(2)
            if run('systemctl','show',UNIT.name,'-p','ActiveState','--value',capture_output=True,text=True).stdout.strip()!='active':
                raise ValueError('Agent service not active')
            if any(hashlib.sha256(p.read_bytes()).digest()!=h for p,h in unchanged.items()):
                raise ValueError('Protected configuration changed')
        # Stop before any replacement. Rollback restores the complete old file set.
        recovery_marker = work/'restore-required'
        recovery_marker.write_text('Keep previous/ until successful update or verified rollback.\n')
        run('systemctl','stop',UNIT.name)
        try:
            for name in names: atomic_copy(candidate/name,ROOT/name)
            run('systemctl','start',UNIT.name)
            verify_running()
        except (Exception,KeyboardInterrupt):
            run('systemctl','stop',UNIT.name)
            for name in names: atomic_copy(previous/name,ROOT/name)
            run('systemctl','start',UNIT.name)
            try: verify_running()
            except Exception:
                raise ValueError('Old files restored, but heartbeat verification failed; check network and service. No database or credential was restored.')
            recovery_marker.unlink()
            raise ValueError('Update failed; old package restored and heartbeat verified')
        # Bounded retention: one prior package only, no credentials in rollback.
        validate_rollback(rollback,names)
        if rollback.exists(): shutil.rmtree(rollback)
        previous.replace(rollback)
        recovery_marker.unlink()
        print('Update passed: heartbeat accepted; previous package retained in '+str(rollback))
        print('Confirm continuous sampling in Hub. Short probe is not long-running acceptance.')

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('install','register','check-update','update'))
    parser.add_argument('--version', help='explicit GitHub release tag')
    parser.add_argument('--bundle', help='trusted local release directory for offline validation')
    args = parser.parse_args()
    if args.action in ('install','check-update','update') and not args.version: parser.error('--version required')
    if args.action=='check-update': check_update(args); return
    preflight()
    import fcntl
    fd = os.open('/run/lock/seesize-agent-setup.lock',os.O_CREAT|os.O_RDWR|os.O_NOFOLLOW,0o600)
    with os.fdopen(fd,'w') as lock:
        if os.fstat(lock.fileno()).st_uid!=0: raise ValueError('Unsafe lock owner')
        fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
        if args.action=='install': install(args)
        elif args.action=='register': register()
        else: update(args)

if __name__=='__main__':
    try: main()
    except KeyboardInterrupt:
        raise SystemExit('Cancelled. Download preparation does not stop the existing Agent. If replacement had started, inspect service status and any retained recovery directory.')
    except (ValueError,OSError,subprocess.SubprocessError) as error:
        raise SystemExit(str(error))
