#!/usr/bin/env python3
"""Maintain explicitly registered SeeSize artifacts; dry-run unless --apply.
Never removes the live database, daily backups, credentials, or unknown files.
"""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

PROTECTED = {'data', 'backups', 'admin.token', 'agent.token', 'maintenance-artifacts.json', 'storage-status.json'}

def plan(root, entries, now):
    targets = []
    rollback = []
    seen = set()
    for entry in entries:
        name = entry['name']
        if name in seen or name in PROTECTED or Path(name).name != name or name in ('.', '..') or '/' in name or '\\' in name:
            raise ValueError('unsafe or duplicate registered artifact: ' + name)
        seen.add(name)
        if entry['kind'] == 'rollback':
            if not (name.startswith('pre-') or name.startswith('hub-before-')):
                raise ValueError('rollback name not allowed')
        elif entry['kind'] == 'temporary':
            if not (name.startswith('restore-check-') or name.startswith('migrate-') or name == 'migration'):
                raise ValueError('temporary name not allowed')
        else:
            raise ValueError('unknown kind')
        path = root / name
        if not path.exists() and not path.is_symlink():
            continue
        # Reject symlinks and mounts anywhere inside the registered tree.
        if path.is_symlink() or path.resolve().parent != root:
            raise ValueError('unsafe resolved target')
        for current, dirs, files in os.walk(path, followlinks=False):
            for item in [Path(current), *[Path(current)/n for n in dirs+files]]:
                if item.is_symlink() or os.path.ismount(item):
                    raise ValueError('symlink or mount inside artifact: ' + str(item))
        if entry['kind'] == 'rollback':
            rollback.append((entry['created_at'], name))
        elif dt.datetime.fromisoformat(entry['expires_at']) <= now:
            targets.append(name)
    # Retain three newest registered rollback points, independent of backups/.
    targets.extend(name for _, name in sorted(rollback, reverse=True)[3:])
    return sorted(targets)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', default='/home/wsr/seesize')
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    raw = Path(args.root)
    root = raw.resolve(strict=True)
    if raw.is_symlink() or root in (Path('/'), Path.home()) or not (root/'data/seesize.db').is_file():
        raise SystemExit('refusing unrecognized application root')
    entries = json.loads((root/'maintenance-artifacts.json').read_text())
    now = dt.datetime.now(dt.timezone.utc)
    targets = plan(root, entries, now)
    if args.apply:
        # Do not delete artifacts while any SeeSize backup/restore job is live.
        output = subprocess.check_output(['systemctl','--user','list-units','--all','--type=service','--no-legend','--plain','seesize*'], text=True)
        for line in output.splitlines():
            fields = line.split()
            if len(fields)>=4 and ('backup' in fields[0] or 'restore' in fields[0]) and fields[2] in ('active','activating','deactivating','reloading'):
                raise SystemExit('backup/restore service active; retry next maintenance window')
        for name in targets:
            path = root/name
            if path.is_dir():
                shutil.rmtree(path)
            else:
                path.unlink()
        # Backup temp files only: never WAL/SHM or completed .db files.
        backups = root/'backups'
        if backups.is_symlink() or backups.resolve().parent != root:
            raise SystemExit('unsafe backup directory')
        for path in backups.iterdir():
            owned = ((path.name.startswith('.seesize-backup-') or path.name.startswith('.status-')) and path.name.endswith('.partial'))
            if owned and not path.is_symlink() and path.is_file() and now.timestamp()-path.stat().st_mtime>48*3600:
                path.unlink()
                targets.append('backups/'+path.name)
    total = sum(p.stat().st_size for p in root.rglob('*') if not p.is_symlink() and p.is_file())
    free = shutil.disk_usage(root).free
    report = {'checked_at': now.isoformat(), 'applied':args.apply, 'removed' if args.apply else 'planned':targets,
              'application_bytes':total, 'filesystem_free_bytes':free,
              'warning': total>1024**3 or free<2*1024**3,
              'warning_rule':'application > 1 GiB or filesystem free < 2 GiB; not a hard quota'}
    if args.apply:
        fd, temp = tempfile.mkstemp(prefix='.storage-',dir=root)
        try:
            with os.fdopen(fd,'w') as stream:
                json.dump(report,stream,indent=2)
            os.replace(temp,root/'storage-status.json')
        finally:
            if os.path.exists(temp): os.unlink(temp)
    print(json.dumps(report,ensure_ascii=False))
    return 1 if report['warning'] else 0

if __name__ == '__main__':
    raise SystemExit(main())
