"""Enroll the new host only, using the migrated local admin credential.

Run as wsr on the migration target; never prints credentials.
"""
import http.cookiejar
import json
import os
from pathlib import Path
import urllib.request

root = Path('/home/wsr/seesize')
base = 'http://127.0.0.1:18081'
target = root / 'agent.token'
if target.exists():
    raise SystemExit('agent.token already exists; refusing to replace identity')
opener = urllib.request.build_opener(
    urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

def post(path, data):
    request = urllib.request.Request(base + path, json.dumps(data).encode(),
        headers={'Content-Type': 'application/json', 'X-SeeSize-Request': '1'})
    with opener.open(request, timeout=10) as response:
        return json.load(response)

post('/api/v1/login', {'credential': (root / 'admin.token').read_text().strip()})
try:
    code = post('/api/v1/enrollments', {'agent_id': 'wsrser-main'})['code']
    token = post('/api/v1/agents/register', {'agent_id': 'wsrser-main', 'code': code})['token']
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as output:
        output.write(token)
    print('New host enrolled; token saved with mode 0600.')
finally:
    post('/api/v1/logout', {})
