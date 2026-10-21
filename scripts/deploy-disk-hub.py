#!/usr/bin/env python3
"""One-time wsrser activation for the tested 174b91a directory explorer."""
import importlib.util
import json
from http.cookies import SimpleCookie
from pathlib import Path

spec = importlib.util.spec_from_file_location('chart_deployment', Path(__file__).with_name('deploy-chart-hub.py'))
deployment = importlib.util.module_from_spec(spec)
spec.loader.exec_module(deployment)
deployment.RELEASE = 'disk-20261021-174b91a'
deployment.STAGE = deployment.ROOT/'restore-check-disk-deploy-174b91a'
deployment.EXPECTED = '9d0f8d971d90ff347f67d66782f81887c027905332fd66b91bf55d43e4f829ff'
original_probe = deployment.probe

def probe():
    report = original_probe()
    credential = (deployment.ROOT/'admin.token').read_text().strip()
    with deployment.request('/api/v1/login', body=json.dumps({'credential':credential}).encode()) as response:
        cookies = SimpleCookie();cookies.load(response.headers['Set-Cookie'])
        cookie = 'seesize_session='+cookies['seesize_session'].value
    try:
        with deployment.request('/', cookie) as response:
            page = response.read()
            assert b'data-disk-sort' in page and b'data-disk-source' in page and b'renderDiskTree' in page
        with deployment.request('/api/v1/servers/wsrser-main/disk', cookie) as response:
            data = json.load(response)
            assert 'reference_snapshot' in data
            if not data['snapshots']:
                assert data['reference_snapshot'] is None and not data['comparable']
            elif not data['snapshots'][0]['complete']:
                assert not data['comparable'] and not data['changes'] and not data['alerts']
            report['disk_snapshots'] = len(data['snapshots'])
    finally:
        with deployment.request('/api/v1/logout', cookie, body=b'{}') as response:
            assert response.status == 200
    return report

deployment.probe = probe

if __name__ == '__main__':
    deployment.main()
