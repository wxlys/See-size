#!/usr/bin/env python3
"""One-off authorized rollout, pinned hashes and paths; not a generic installer."""
import datetime as dt
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import time
import urllib.error
import urllib.request

ROOT=Path('/home/wsr/seesize')
STAGE=ROOT/'migrate-secret-fix-20260927'
RELEASE='secret-fix-20260927'
BUNDLE=ROOT/('pre-'+RELEASE)
OLD={'hub':'78beb8d7f29a387898f695ffcd1fa318a99ea9a27a93f091b4ee4df5e2aa714d','agent':'a5aa4047fbbd8aebbb375991c9c46b7dd918291c664a53f9129e9c20a89c84eb'}
NEW={'hub':'56e9bb7955992d7054d25098d05bfe660ad641cf3d9b6898418ba8d20a052859','agent':'8951d75c847e210202ba668a4f3d55d00b144ca698efe850560e6f2c70a54d91'}
TIMERS=('seesize-backup.timer','seesize-maintenance.timer')
SERVICES=('seesize-hub.service','seesize-agent.service')
BASE='http://127.0.0.1:18081'
def run(*args): return subprocess.run(args,check=True,capture_output=True,text=True,timeout=360)
def sha(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def binary(folder,c): return folder/f'seesize-{c}-linux-amd64'
def state(unit): return run('systemctl','--user','show',unit,'-p','ActiveState','--value').stdout.strip()
def configs():
    return {u:hashlib.sha256(run('systemctl','--user','cat',u).stdout.encode()).hexdigest() for u in (*SERVICES,*TIMERS,'seesize-backup.service','seesize-maintenance.service')}
def health():
    for _ in range(30):
        try:
            with urllib.request.urlopen(BASE+'/healthz',timeout=2) as r:
                if r.status==200: return
        except OSError: pass
        time.sleep(1)
    raise RuntimeError('health timeout')
def latest():
    with sqlite3.connect((ROOT/'data/seesize.db').as_uri()+'?mode=ro',uri=True) as db:
        return db.execute('SELECT max(collected_at_ns) FROM metric_samples WHERE agent_id=?',('wsrser-main',)).fetchone()[0]
report={'release':RELEASE,'started_at':dt.datetime.now(dt.timezone.utc).isoformat(),'status':'RUNNING','observations':{}}
def observe(name):
    health()
    times=[latest()]
    for _ in range(3):
        time.sleep(12)
        health()
        times.append(latest())
    report['observations'][name]=times
    if any(a is None or b is None or b<=a for a,b in zip(times,times[1:])):
        raise RuntimeError('sampling stalled: '+name)
    print('PASS sampling '+name,flush=True)
def api_check():
    try:
        urllib.request.urlopen(BASE+'/api/v1/servers',timeout=10)
        raise RuntimeError('Unauthenticated access allowed')
    except urllib.error.HTTPError as e:
        if e.code!=401: raise
    opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    headers={'Content-Type':'application/json','X-SeeSize-Request':'1'}
    body=json.dumps({'credential':(ROOT/'admin.token').read_text().strip()}).encode()
    with opener.open(urllib.request.Request(BASE+'/api/v1/login',data=body,headers=headers),timeout=10) as r:
        if r.status!=200: raise RuntimeError('Login failed')
    try:
        def get(path):
            with opener.open(BASE+path,timeout=20) as r: return json.load(r)
        servers=get('/api/v1/servers')['servers']
        if len(servers)!=1 or servers[0]['agent_id']!='wsrser-main' or not servers[0]['online']:
            raise RuntimeError('Unexpected server state')
        if servers[0]['metrics']['network'].get('interfaces')!=['eth0']:
            raise RuntimeError('Network scope changed')
        backup=get('/api/v1/backup-status')
        if not backup.get('configured') or backup.get('files')!=7 or backup.get('latest',{}).get('state')!='success':
            raise RuntimeError('Backup state unexpected')
        for period in ('30m','1h','24h'):
            if not get('/api/v1/servers/wsrser-main/trend?range='+period).get('points'):
                raise RuntimeError('Empty trend '+period)
        with opener.open(BASE+'/',timeout=10) as r:
            if r.status!=200: raise RuntimeError('UI unavailable')
    finally:
        with opener.open(urllib.request.Request(BASE+'/api/v1/logout',data=b'{}',headers=headers),timeout=10): pass
    try:
        opener.open(BASE+'/api/v1/servers',timeout=10)
        raise RuntimeError('Logout did not invalidate access')
    except urllib.error.HTTPError as e:
        if e.code!=401: raise
    print('PASS authentication, eth0, trends, backup API, page and logout',flush=True)
def replace(c,folder,expected):
    target=binary(ROOT,c); source=binary(folder,c)
    temp=target.with_name(target.name+'.secret-fix-next')
    if temp.exists() or temp.is_symlink(): raise RuntimeError('Staging collision')
    shutil.copyfile(source,temp); temp.chmod(0o700)
    if sha(temp)!=expected: raise RuntimeError('Copy hash mismatch')
    temp.replace(target)
def switch(c,folder,expected):
    run('systemctl','--user','stop','seesize-'+c)
    replace(c,folder,expected)
    run('systemctl','--user','start','seesize-'+c)

if ROOT.resolve()!=ROOT or ROOT.stat().st_uid!=os.getuid() or ROOT.stat().st_mode & 0o077:
    raise SystemExit('Unsafe production root')
if STAGE.resolve()!=STAGE or BUNDLE.exists(): raise SystemExit('Unsafe or reused release')
for c in OLD:
    if sha(binary(ROOT,c))!=OLD[c] or sha(binary(STAGE,c))!=NEW[c]: raise SystemExit('Unexpected build')
for u in (*SERVICES,*TIMERS):
    if state(u)!='active': raise SystemExit('Expected active '+u)
before_configs=configs()
health()
api_check()
changed=False
paused=False
try:
    paused=True
    for timer in TIMERS: run('systemctl','--user','stop',timer)
    for service in ('seesize-backup.service','seesize-maintenance.service'):
        if state(service)!='inactive': raise RuntimeError('Background task active; aborting')
    args=['python3',str(STAGE/'upgrade-prepare.py'),'--root',str(ROOT),'--release',RELEASE,'--prepare']
    for c in NEW: args+=['--candidate',c+'='+str(binary(STAGE,c)),'--sha256',c+'='+NEW[c]]
    run(*args)
    shutil.copyfile(STAGE/'upgrade-prepare.py',BUNDLE/'upgrade-prepare.py')
    print('PASS verified rollback bundle created',flush=True)
    changed=True
    switch('hub',BUNDLE/'candidates',NEW['hub'])
    observe('hub-upgraded'); api_check()
    switch('agent',BUNDLE/'candidates',NEW['agent'])
    observe('pair-upgraded'); api_check()
    if configs()!=before_configs: raise RuntimeError('Service configuration changed')
    report['status']='PASS'
except Exception as e:
    report['error_type']=type(e).__name__
    if changed:
        print('Rollout failed; rolling back Agent then Hub',flush=True)
        switch('agent',BUNDLE,OLD['agent'])
        switch('hub',BUNDLE,OLD['hub'])
        observe('rollback'); api_check()
        report['status']='ROLLED_BACK'
    else: report['status']='ABORTED'
    raise
finally:
    timer_errors=[]
    if paused:
        for timer in TIMERS:
            try: run('systemctl','--user','start',timer)
            except Exception: timer_errors.append(timer)
    report['timer_restore_errors']=timer_errors
    report['finished_at']=dt.datetime.now(dt.timezone.utc).isoformat()
    report['final_hashes']={c:sha(binary(ROOT,c)) for c in OLD}
    report['service_states']={u:state(u) for u in (*SERVICES,*TIMERS)}
    destination=BUNDLE/'deployment-result.json' if BUNDLE.is_dir() else STAGE/'deployment-result.json'
    destination.write_text(json.dumps(report,indent=2))
    print(json.dumps(report),flush=True)
    if timer_errors: raise RuntimeError('Timer restoration failed')
