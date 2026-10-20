#!/usr/bin/env python3
"""Activate the already-tested chart Hub on the wsrser user deployment only."""
import datetime as dt
import fcntl
import hashlib
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
import urllib.request

ROOT = Path('/home/wsr/seesize')
RELEASE = 'charts-20261020-e0f1030'
STAGE = ROOT/'restore-check-chart-deploy-e0f1030'
EXPECTED = 'aa208982928dfaef26b78f65dc8969b7e0129a0833b99ca09d0c678711275e5d'
TIMERS = ('seesize-maintenance.timer', 'seesize-backup.timer')

def run(*args, **kw):
    return subprocess.run(args, check=True, timeout=360, **kw)

def state(unit):
    return subprocess.check_output(['systemctl','--user','show',unit,'-p','ActiveState','--value'],text=True).strip()

def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def request(path, cookie=None, body=None):
    headers = {'X-SeeSize-Request':'1'}
    if cookie: headers['Cookie']=cookie
    if body is not None: headers['Content-Type']='application/json'
    req=urllib.request.Request('http://127.0.0.1:18081'+path, data=body, headers=headers)
    return urllib.request.urlopen(req, timeout=15)

def probe():
    with request('/healthz') as response:
        assert json.load(response)['status']=='ok'
    credential=(ROOT/'admin.token').read_text().strip()
    with request('/api/v1/login',body=json.dumps({'credential':credential}).encode()) as response:
        cookies=SimpleCookie();cookies.load(response.headers['Set-Cookie'])
        assert cookies['seesize_session']['secure']
        cookie='seesize_session='+cookies['seesize_session'].value
    try:
        with request('/',cookie) as response:
            assert b'chart-dialog' in response.read()
        with request('/api/v1/servers',cookie) as response:
            servers=json.load(response)['servers']
        main=next(s for s in servers if s['agent_id']=='wsrser-main')
        assert main['online']
        points={}
        for span in ('30m','1h','24h'):
            with request('/api/v1/servers/wsrser-main/trend?range='+span,cookie) as response:
                data=json.load(response);assert data['points'];points[span]=len(data['points'])
        now=dt.datetime.now(dt.timezone.utc)
        end=now.replace(minute=(now.minute//30)*30,second=0,microsecond=0).isoformat().replace('+00:00','Z')
        with request('/api/v1/servers/wsrser-main/trend?range=30m&to='+end,cookie) as response:
            assert json.load(response)['points']
        with request('/api/v1/backup-status',cookie) as response:
            assert response.status==200
        return {'last_seen':main['last_seen'],'points':points}
    finally:
        with request('/api/v1/logout',cookie,body=b'{}') as response: assert response.status==200

def main():
    assert ROOT.resolve()==ROOT and ROOT.stat().st_mode & 0o077==0
    assert STAGE.resolve()==STAGE and STAGE.is_dir()
    candidate=STAGE/'seesize-hub-linux-amd64'
    assert digest(candidate)==EXPECTED
    live=ROOT/'seesize-hub-linux-amd64'
    unchanged={p:digest(p) for p in (ROOT/'seesize-agent-linux-amd64',ROOT/'admin.token',ROOT/'agent.token')}
    configs=subprocess.check_output(['systemctl','--user','cat','seesize-hub'])
    agent_pid=subprocess.check_output(['systemctl','--user','show','seesize-agent','-p','MainPID','--value']).strip()
    original={timer:state(timer) for timer in TIMERS}
    assert state('seesize-hub')=='active' and state('seesize-agent')=='active'
    activated=False
    started=dt.datetime.now(dt.timezone.utc).isoformat()
    try:
        for timer in TIMERS: run('systemctl','--user','stop',timer)
        deadline=time.monotonic()+120
        for service in ('seesize-maintenance.service','seesize-backup.service'):
            while state(service) in ('active','activating','deactivating'):
                if time.monotonic()>deadline: raise RuntimeError('Maintenance/backup still running')
                time.sleep(1)
        run('python3',str(STAGE/'upgrade-prepare.py'),'--root',str(ROOT),'--release',RELEASE,
            '--candidate','hub='+str(candidate),'--sha256','hub='+EXPECTED,'--prepare')
        rollback=ROOT/('pre-'+RELEASE)
        with (ROOT/'.maintenance.lock').open('a') as lock:
            fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
            try:
                run('systemctl','--user','stop','seesize-hub')
                activated=True
                replacement=STAGE/'activate-hub'
                shutil.copyfile(candidate,replacement);replacement.chmod(0o700)
                os.replace(replacement,live)
                run('systemctl','--user','start','seesize-hub')
                for attempt in range(20):
                    try:
                        with request('/healthz') as response: assert response.status==200
                        break
                    except Exception:
                        if attempt==19: raise
                        time.sleep(1)
                before=probe()
                time.sleep(12)
                after=probe()
                assert after['last_seen']>before['last_seen'], 'main heartbeat did not advance'
                assert all(digest(p)==h for p,h in unchanged.items())
                assert subprocess.check_output(['systemctl','--user','cat','seesize-hub'])==configs
                assert subprocess.check_output(['systemctl','--user','show','seesize-agent','-p','MainPID','--value']).strip()==agent_pid
                assert digest(live)==EXPECTED
            except BaseException:
                if activated:
                    run('systemctl','--user','stop','seesize-hub')
                    replacement=STAGE/'rollback-hub'
                    shutil.copyfile(rollback/'seesize-hub-linux-amd64',replacement);replacement.chmod(0o700)
                    os.replace(replacement,live)
                    run('systemctl','--user','start','seesize-hub')
                    print('Old Hub restored; investigate deployment failure.',flush=True)
                raise
        report={'status':'PASS','started_at':started,'finished_at':dt.datetime.now(dt.timezone.utc).isoformat(),
                'hub_sha256':EXPECTED,'rollback':str(rollback),'heartbeat_before':before['last_seen'],
                'heartbeat_after':after['last_seen'],'trend_points':after['points'],
                'agent_pid_unchanged':True,'credentials_and_config_unchanged':True}
        (STAGE/'deployment-result.json').write_text(json.dumps(report,indent=2))
        print(json.dumps(report,indent=2),flush=True)
    finally:
        for timer,previous in original.items():
            if previous=='active':run('systemctl','--user','start',timer)

if __name__=='__main__':
    main()
