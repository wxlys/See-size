"""Explicit live API smoke test; creates and deletes only its own random identity.
Run as wsr on the new host. Does not print admin or device credentials.
"""
import datetime
import http.cookiejar
import json
from pathlib import Path
import secrets
import urllib.error
import urllib.request

base = 'http://127.0.0.1:18081'
device = 'delete-smoke-' + secrets.token_hex(8)
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

def call(method, path, data=None, token=None, expected=200):
    headers = {'Content-Type': 'application/json', 'X-SeeSize-Request': '1'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(base + path, data=None if data is None else json.dumps(data).encode(), headers=headers, method=method)
    try:
        response = client.open(req, timeout=20)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        body = json.load(response)
        if response.code != expected:
            raise RuntimeError(f'{method} {path}: expected {expected}, got {response.code}')
        return body

call('POST', '/api/v1/login', {'credential': Path('/home/wsr/seesize/admin.token').read_text().strip()})
registered = False
try:
    before = {s['agent_id'] for s in call('GET', '/api/v1/servers')['servers']}
    code = call('POST', '/api/v1/enrollments', {'agent_id': device}, expected=201)['code']
    token = call('POST', '/api/v1/agents/register', {'agent_id': device, 'code': code}, expected=201)['token']
    registered = True
    heartbeat = {'protocol_version': 'v1', 'agent_id': device, 'hostname': device,
                 'collected_at': datetime.datetime.now(datetime.timezone.utc).isoformat()}
    call('POST', '/api/v1/agents/heartbeat', heartbeat, token, 202)
    path = '/api/v1/devices/' + device
    call('DELETE', path, {'confirm_id': device}, expected=409)
    call('POST', path + '/revoke', {})
    call('DELETE', path, {'confirm_id': 'wrong'}, expected=400)
    result = call('DELETE', path, {'confirm_id': device})
    registered = False
    call('POST', '/api/v1/agents/heartbeat', heartbeat, token, 401)
    after = {s['agent_id'] for s in call('GET', '/api/v1/servers')['servers']}
    assert before == after, 'server inventory unexpectedly changed'
    assert device not in {d['agent_id'] for d in call('GET', '/api/v1/devices')['devices']}
    print(json.dumps({'status': 'PASS', 'test_id': device, 'deleted': result['deleted'],
                      'inventory_unchanged': True}, ensure_ascii=False))
finally:
    if registered:
        print('Test interrupted; attempting cleanup of owned identity: ' + device)
        call('POST', '/api/v1/devices/' + device + '/revoke', {})
        call('DELETE', '/api/v1/devices/' + device, {'confirm_id': device})
    call('POST', '/api/v1/logout', {})
