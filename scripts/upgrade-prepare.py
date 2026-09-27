#!/usr/bin/env python3
"""Validate an upgrade and optionally prepare a private rollback bundle.
Does NOT replace live binaries, restart services, or restore a database.
"""
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

COMPONENTS = ('hub', 'agent', 'backup')

def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024*1024), b''):
            h.update(block)
    return h.hexdigest()

def mapping(items):
    result = {}
    for item in items:
        component, sep, value = item.partition('=')
        if not sep or component not in COMPONENTS or not value or component in result:
            raise ValueError('use unique hub=..., agent=... or backup=... entries')
        result[component] = value
    return result

def validate_candidate(path, expected):
    if not re.fullmatch('[0-9a-fA-F]{64}', expected):
        raise ValueError('expected SHA256 must contain 64 hexadecimal characters')
    path = Path(path).absolute()
    if path != path.resolve(strict=True) or not path.is_file():
        raise ValueError('candidate must be a regular file without symlink path components')
    with path.open('rb') as stream:
        header = stream.read(20)
    if len(header)<20 or header[:6]!=b'\x7fELF\x02\x01' or int.from_bytes(header[18:20],'little')!=62:
        raise ValueError('candidate is not an x86_64 ELF binary')
    actual = digest(path)
    if actual != expected.lower():
        raise ValueError('candidate SHA256 mismatch')
    return path

def atomic_json(path, body):
    fd, name = tempfile.mkstemp(prefix='.upgrade-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as stream:
            json.dump(body, stream, indent=2)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name): os.unlink(name)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', default='/home/wsr/seesize')
    parser.add_argument('--release', required=True, help='unique label, e.g. 20260927-network2')
    parser.add_argument('--candidate', action='append', required=True, help='component=/absolute/path')
    parser.add_argument('--sha256', action='append', required=True, help='component=trusted-build-hash')
    parser.add_argument('--prepare', action='store_true', help='create rollback files; still no activation')
    args = parser.parse_args()
    if not re.fullmatch('[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}', args.release):
        parser.error('invalid release label')
    root = Path(args.root).absolute()
    if root != root.resolve(strict=True) or root in (Path('/'), Path.home()):
        parser.error('dedicated real application root required')
    if root.stat().st_mode & 0o077: parser.error('application root must deny group/other access')
    if not (root/'data/seesize.db').is_file() or (root/'data').is_symlink() or (root/'data/seesize.db').is_symlink():
        parser.error('live database missing or symlinked')
    supplied, hashes = mapping(args.candidate), mapping(args.sha256)
    if supplied.keys() != hashes.keys(): parser.error('candidate/hash component sets must match')
    candidates = {c: validate_candidate(p, hashes[c]) for c,p in supplied.items()}
    for c in COMPONENTS:
        path = root/f'seesize-{c}-linux-amd64'
        if not path.is_file() or path.is_symlink(): parser.error('missing regular current program: '+c)
    for c,p in candidates.items():
        if p in [root/f'seesize-{name}-linux-amd64' for name in COMPONENTS]: parser.error('candidate must not be any live binary')
    target = root/('pre-'+args.release)
    if target.exists() or target.is_symlink(): parser.error('rollback target already exists')
    manifest = root/'maintenance-artifacts.json'
    if manifest.is_symlink(): parser.error('manifest must not be a symlink')
    entries = json.loads(manifest.read_text())
    if any(e['name']==target.name for e in entries): parser.error('release already registered')
    # Conservative estimate includes all old binaries and candidate copies.
    estimate = (root/'data/seesize.db').stat().st_size*2 + sum((root/f'seesize-{c}-linux-amd64').stat().st_size for c in COMPONENTS) + sum(p.stat().st_size for p in candidates.values())
    free = shutil.disk_usage(root).free
    if free < estimate+2*1024**3: parser.error('insufficient headroom: bundle estimate plus 2 GiB required')
    report = {'release':args.release, 'activate':False, 'prepared':False,
              'directory':str(target), 'candidate_hashes':hashes, 'estimated_bytes':estimate}
    if not args.prepare:
        print(json.dumps(report,indent=2)); return
    import fcntl
    lockpath=root/'.maintenance.lock'
    if lockpath.is_symlink(): parser.error('lock must not be a symlink')
    with lockpath.open('a') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
        # Re-read after locking against other prepare and maintenance tasks.
        entries=json.loads(manifest.read_text())
        if any(e['name']==target.name for e in entries): parser.error('release already registered')
        os.mkdir(target,0o700)
        committed=False
        try:
            for c in COMPONENTS:
                shutil.copy2(root/f'seesize-{c}-linux-amd64',target/f'seesize-{c}-linux-amd64')
            stage=target/'candidates';stage.mkdir(mode=0o700)
            for c,p in candidates.items():
                destination=stage/f'seesize-{c}-linux-amd64'
                shutil.copyfile(p,destination);destination.chmod(0o700)
                validate_candidate(destination,hashes[c])
            configs={}
            for unit in ('seesize-hub.service','seesize-agent.service','seesize-backup.service','seesize-backup.timer'):
                configs[unit]=subprocess.check_output(['systemctl','--user','cat',unit],text=True,timeout=10)
            atomic_json(target/'service-configs.json',configs)
            # Existing trusted backup program, never the candidate executable.
            subprocess.run([str(root/'seesize-backup-linux-amd64'),'-source',str(root/'data/seesize.db'),'-dir',str(target),'-keep','1'],check=True,timeout=360)
            report['prepared']=True
            report['old_hashes']={c:digest(target/f'seesize-{c}-linux-amd64') for c in COMPONENTS}
            report['backup_files']=[p.name for p in target.glob('seesize-*.db')]
            if len(report['backup_files'])!=1: raise RuntimeError('expected one verified snapshot')
            report['created_at']=dt.datetime.now(dt.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
            atomic_json(target/'release.json',report)
            entries.append({'name':target.name,'kind':'rollback','created_at':report['created_at']})
            atomic_json(manifest,entries)
            committed=True
        finally:
            if not committed:
                # Only this invocation's newly created, private directory.
                shutil.rmtree(target)
    print(json.dumps(report,indent=2))

if __name__=='__main__':
    try: main()
    except (ValueError,OSError,RuntimeError,subprocess.SubprocessError) as error:
        raise SystemExit(str(error))
