#!/usr/bin/env python3
"""Explicit opt-in setup for a disposable Ubuntu VM, never the business host."""
import argparse
import json
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import hashlib
import os
import tempfile

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--disposable-vm', action='store_true', required=True)
p.add_argument('--source', type=Path, required=True)
a = p.parse_args()
# Require a manifest produced by the fresh-build packager, never arbitrary bin/.
manifest = json.loads((a.source / 'build-manifest.json').read_text())
required = {'hub': ['backup-dir', 'admin-token-file'], 'agent': ['token-file', 'interval'], 'backup': ['source', 'verify', 'keep']}
with tempfile.TemporaryDirectory(prefix='seesize-capabilities-') as temporary:
    for component, flags in required.items():
        source = a.source / f'seesize-{component}-linux-amd64'
        if hashlib.sha256(source.read_bytes()).hexdigest() != manifest['sha256'][source.name]:
            raise SystemExit('Package hash mismatch: ' + component)
        executable = Path(temporary) / source.name
        shutil.copyfile(source, executable)
        executable.chmod(0o700)
        environment = {k:v for k,v in os.environ.items() if not k.startswith('SEE_SIZE_')}
        result = subprocess.run([str(executable), '-h'], capture_output=True, text=True, env=environment, timeout=10)
        if result.returncode != 0 or any('\n  -'+flag+' ' not in result.stdout + result.stderr and '\n  -'+flag+'\n' not in result.stdout + result.stderr for flag in flags):
            raise SystemExit('Package capability mismatch: ' + component)
root = Path.home() / 'seesize-upgrade-lab'
units = Path.home() / '.config/systemd/user'
names = ['seesize-hub.service', 'seesize-agent.service', 'seesize-backup.service', 'seesize-backup.timer']
if root.exists() or root.is_symlink():
    raise SystemExit('Lab directory already exists; refusing overwrite')
for name in names:
    result = subprocess.run(['systemctl', '--user', 'show', name, '--property=LoadState', '--value'], capture_output=True, text=True)
    if result.returncode or result.stdout.strip() != 'not-found' or (units / name).exists() or (units / name).is_symlink():
        raise SystemExit('Existing unit or unavailable user manager; refusing setup: ' + name)
with socket.socket() as probe:
    probe.bind(('127.0.0.1', 28081))
for component in ('hub', 'agent', 'backup'):
    if not (a.source / f'seesize-{component}-linux-amd64').is_file():
        raise SystemExit('Missing binary: ' + component)
root.mkdir(mode=0o700)
for folder in ('data', 'backups', 'candidates'):
    (root / folder).mkdir(mode=0o700)
for component in ('hub', 'agent', 'backup'):
    target = root / f'seesize-{component}-linux-amd64'
    shutil.copyfile(a.source / target.name, target)
    target.chmod(0o700)
shutil.copyfile(root / 'seesize-hub-linux-amd64', root / 'candidates/seesize-hub-linux-amd64')
for name in ('admin.token', 'agent.token'):
    with (root / name).open('x') as stream:
        (root / name).chmod(0o600)
        stream.write(secrets.token_hex(32))
(root / 'maintenance-artifacts.json').write_text(json.dumps([]))
# %h avoids embedding the local home path into systemd syntax.
base = '%h/seesize-upgrade-lab'
configs = {
    'seesize-hub.service': f'[Service]\nEnvironmentFile={base}/lab.env\nExecStart={base}/seesize-hub-linux-amd64 -listen 127.0.0.1:28081 -data {base}/data/seesize.db -admin-token-file {base}/admin.token -backup-dir {base}/backups\n',
    'seesize-agent.service': f'[Service]\nExecStart={base}/seesize-agent-linux-amd64 -hub http://127.0.0.1:28081 -token-file {base}/agent.token -id upgrade-lab-only -interval 2s\n',
    'seesize-backup.service': f'[Service]\nType=oneshot\nExecStart={base}/seesize-backup-linux-amd64 -source {base}/data/seesize.db -dir {base}/backups -keep 7\n',
    'seesize-backup.timer': '[Timer]\nOnCalendar=daily\nUnit=seesize-backup.service\n',
}
with (root / 'lab.env').open('x') as stream:
    (root / 'lab.env').chmod(0o600)
    stream.write('SEE_SIZE_AGENT_TOKEN=' + (root / 'agent.token').read_text() + '\n')
units.mkdir(parents=True, exist_ok=True)
for name, content in configs.items():
    with (units / name).open('x') as stream:
        stream.write('[Unit]\nDescription=SeeSize disposable upgrade lab\n' + content)
subprocess.run(['systemctl', '--user', 'daemon-reload'], check=True)
print('Prepared:', root)
print('Nothing started or enabled. Start hub, check health, then start agent.')
