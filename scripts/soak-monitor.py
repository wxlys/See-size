#!/usr/bin/env python3
"""Observe existing SeeSize systemd services. No restart, load generation or configuration changes."""
import argparse
import csv
import datetime as dt
import os
from pathlib import Path
import signal
import subprocess
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', default='/opt/seesize-dev')
    parser.add_argument('--hours', type=float, default=24)
    parser.add_argument('--interval', type=int, default=60)
    parser.add_argument('--out', required=True, help='new CSV report')
    args = parser.parse_args()
    if not 0 < args.hours <= 168 or not 10 <= args.interval <= 3600:
        parser.error('hours must be (0,168], interval 10..3600 seconds')
    root = Path(args.root).resolve(strict=True)
    stopping = False

    def stop(*_):
        nonlocal stopping
        stopping = True

    signal.signal(signal.SIGINT, stop)
    signal.signal(signal.SIGTERM, stop)
    columns = ['utc', 'service', 'state', 'pid', 'restarts', 'rss_kib', 'cpu_seconds',
               'db_bytes', 'wal_bytes', 'backup_bytes', 'error']
    with open(args.out, 'x', newline='', encoding='utf-8') as file:
        writer = csv.DictWriter(file, fieldnames=columns)
        writer.writeheader()
        deadline = time.monotonic() + args.hours * 3600
        while not stopping and time.monotonic() < deadline:
            for service in ('seesize-hub', 'seesize-agent'):
                row = {'utc': dt.datetime.now(dt.timezone.utc).isoformat(), 'service': service}
                try:
                    result = subprocess.run(['systemctl', 'show', service, '-p', 'ActiveState', '-p', 'MainPID', '-p', 'NRestarts'],
                                            capture_output=True, text=True, check=True, timeout=5)
                    info = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
                    row.update(state=info.get('ActiveState', ''), pid=info.get('MainPID', ''), restarts=info.get('NRestarts', ''))
                    pid = int(info.get('MainPID', '0'))
                    if pid:
                        stat = Path('/proc/%d/stat' % pid).read_text().rsplit(')', 1)[1].split()
                        row['cpu_seconds'] = (int(stat[11]) + int(stat[12])) / os.sysconf('SC_CLK_TCK')
                        status = Path('/proc/%d/status' % pid).read_text().splitlines()
                        row['rss_kib'] = next(int(s.split()[1]) for s in status if s.startswith('VmRSS:'))
                    for field, name in (('db_bytes', 'seesize.db'), ('wal_bytes', 'seesize.db-wal')):
                        path = root / 'data' / name
                        row[field] = path.stat().st_size if path.exists() else 0
                    row['backup_bytes'] = sum(p.stat().st_size for p in (root / 'backups').glob('seesize-*.db') if p.is_file())
                except Exception as error:
                    row['error'] = str(error)
                writer.writerow(row)
            file.flush()
            print('Recorded', dt.datetime.now(dt.timezone.utc).isoformat(), flush=True)
            until = min(deadline, time.monotonic() + args.interval)
            while not stopping and time.monotonic() < until:
                time.sleep(min(1, until - time.monotonic()))
    print('Observation stopped; report:', args.out, flush=True)


if __name__ == '__main__':
    main()
