#!/usr/bin/env python3
"""Bounded isolated acceptance test; Linux/Python stdlib, no host network changes."""
import argparse
import datetime as dt
import http.cookiejar
import http.server
import json
import os
import resource
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import threading
import time
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', default='/opt/seesize-dev')
    parser.add_argument('--files', type=int, default=1000)
    parser.add_argument('--out', required=True, help='new JSON report; must not exist')
    args = parser.parse_args()
    if not 100 <= args.files <= 10000:
        parser.error('--files must be 100..10000 (isolated Linux VM only)')
    root = Path(args.root).resolve(strict=True)
    for name in ('hub', 'agent', 'scan'):
        if not os.access(root / ('seesize-' + name + '-linux-amd64'), os.X_OK):
            parser.error('missing Linux executable: ' + name)
    report = {'started_at': dt.datetime.now(dt.timezone.utc).isoformat(),
              'scope': 'isolated test; second server untouched', 'status': 'FAILED'}
    # Reserve before creating processes; never overwrite an existing report.
    with open(args.out, 'x', encoding='utf-8') as report_file:
        try:
            with tempfile.TemporaryDirectory(prefix='acceptance-', dir=root) as temporary:
                work = Path(temporary)
                with socket.socket() as sock:
                    sock.bind(('127.0.0.1', 0))
                    hub_port = sock.getsockname()[1]
                base = 'http://127.0.0.1:' + str(hub_port)
                available = threading.Event()
                available.set()

                class Proxy(http.server.BaseHTTPRequestHandler):
                    def do_POST(self):
                        if not available.is_set():
                            self.close_connection = True
                            self.connection.shutdown(socket.SHUT_RDWR)
                            self.connection.close()
                            return
                        body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
                        request = urllib.request.Request(base + self.path, data=body,
                            headers={'Content-Type': 'application/json',
                                     'Authorization': self.headers.get('Authorization', '')})
                        try:
                            with urllib.request.urlopen(request, timeout=5) as response:
                                data = response.read()
                                self.send_response(response.status)
                                self.send_header('Content-Length', str(len(data)))
                                self.end_headers()
                                self.wfile.write(data)
                        except Exception:
                            self.send_error(502)

                    def log_message(self, *_):
                        pass

                proxy = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Proxy)
                proxy.daemon_threads = True
                thread = threading.Thread(target=proxy.serve_forever, daemon=True)
                thread.start()
                token, admin = secrets.token_hex(24), secrets.token_hex(24)
                credential = work / 'agent.token'
                credential.write_text(token)
                credential.chmod(0o600)
                env = dict(os.environ, SEE_SIZE_ADMIN_TOKEN=admin)
                processes = []
                with open(work / 'hub.log', 'w+') as hub_log, open(work / 'agent.log', 'w+') as agent_log:
                    try:
                        hub = subprocess.Popen([str(root / 'seesize-hub-linux-amd64'),
                            '-listen', '127.0.0.1:' + str(hub_port), '-data', str(work / 'hub.db'),
                            '-agent-token', token, '-offline-after', '3s'], env=env,
                            stdout=hub_log, stderr=hub_log)
                        processes.append(hub)
                        for _ in range(50):
                            try:
                                urllib.request.urlopen(base + '/healthz', timeout=1).close()
                                break
                            except Exception:
                                time.sleep(.1)
                        client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
                        login = urllib.request.Request(base + '/api/v1/login',
                            data=json.dumps({'credential': admin}).encode(),
                            headers={'Content-Type': 'application/json', 'X-SeeSize-Request': '1'})
                        client.open(login, timeout=5).close()

                        def get(path):
                            with client.open(base + path, timeout=5) as response:
                                return json.load(response)

                        def wait_for(predicate, seconds=12):
                            deadline = time.monotonic() + seconds
                            while time.monotonic() < deadline:
                                if predicate():
                                    return
                                time.sleep(.25)
                            raise AssertionError('condition not met before timeout')

                        agent = subprocess.Popen([str(root / 'seesize-agent-linux-amd64'),
                            '-hub', 'http://127.0.0.1:' + str(proxy.server_port), '-id', 'isolated-smoke',
                            '-token-file', str(credential), '-interval', '1s'], stdout=agent_log, stderr=agent_log)
                        processes.append(agent)
                        wait_for(lambda: bool(get('/api/v1/servers')['servers']))
                        wait_for(lambda: len(get('/api/v1/servers/isolated-smoke/metrics')['samples']) >= 3)
                        available.clear()
                        print('Simulating connection closure on test-only proxy...', flush=True)
                        wait_for(lambda: not get('/api/v1/servers')['servers'][0]['online'])
                        before = get('/api/v1/servers')['servers'][0]['last_seen']
                        available.set()
                        wait_for(lambda: get('/api/v1/servers')['servers'][0]['online'] and
                                 get('/api/v1/servers')['servers'][0]['last_seen'] != before)
                        report['connection_recovery'] = 'PASS: test connection closed, offline, resumed'
                        files = work / 'files'
                        files.mkdir()
                        for i in range(args.files):
                            (files / ('item-%04d.bin' % i)).write_bytes(b'x' * 4096)
                        initial = len(get('/api/v1/servers/isolated-smoke/metrics')['samples'])
                        started = time.monotonic()
                        usage_before = resource.getrusage(resource.RUSAGE_CHILDREN)
                        scan = subprocess.run([str(root / 'seesize-scan-linux-amd64'), '-root', str(files),
                            '-max-entries', str(args.files + 10), '-rate', '200', '-timeout', '90s'],
                            capture_output=True, text=True, timeout=100, check=True)
                        usage_after = resource.getrusage(resource.RUSAGE_CHILDREN)
                        snapshot = json.loads(scan.stdout)
                        after = len(get('/api/v1/servers/isolated-smoke/metrics')['samples'])
                        assert snapshot['complete'] and snapshot['nodes'][0]['bytes'] == args.files * 4096
                        assert after > initial, 'heartbeat did not advance during scan'
                        report['directory_scan'] = {'files': args.files, 'logical_bytes': args.files * 4096,
                            'elapsed_seconds': round(time.monotonic() - started, 3),
                            'new_heartbeats': after - initial, 'complete': True,
                            'scan_peak_rss_kib': usage_after.ru_maxrss,
                            'scan_cpu_seconds': usage_after.ru_utime + usage_after.ru_stime - usage_before.ru_utime - usage_before.ru_stime}
                        limited = subprocess.run([str(root / 'seesize-scan-linux-amd64'), '-root', str(files),
                            '-max-entries', '10', '-rate', '200', '-timeout', '5s'],
                            capture_output=True, text=True, timeout=10, check=True)
                        assert not json.loads(limited.stdout)['complete'], 'budget exhaustion not marked incomplete'
                        report['limited_scan'] = 'PASS: incomplete marked; local scan only, no alert upload'
                        report['status'] = 'PASS'
                    finally:
                        available.set()
                        for process in reversed(processes):
                            if process.poll() is None:
                                process.terminate()
                                try:
                                    process.wait(timeout=15)
                                except subprocess.TimeoutExpired:
                                    process.kill()
                                    process.wait(timeout=5)
                        proxy.shutdown()
                        proxy.server_close()
                        thread.join(timeout=3)
                        agent_log.seek(0)
                        report['agent_log_tail'] = agent_log.read()[-4000:]
        except Exception as error:
            report['error'] = str(error)
        finally:
            report['finished_at'] = dt.datetime.now(dt.timezone.utc).isoformat()
            json.dump(report, report_file, ensure_ascii=False, indent=2)
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if report['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
