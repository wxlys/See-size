#!/usr/bin/env python3
"""Bounded, opt-in real scans and dedicated demo identities on authorized wsrser."""
import datetime as dt
from http.cookies import SimpleCookie
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import threading
import time
import urllib.error
import urllib.request

ROOT = Path('/home/wsr/seesize')
WORK = ROOT/'restore-check-disk-validation-20261021'
BASE = 'https://8.148.5.169'
IDS = {'success':'disk-scan-success-20261021','partial':'disk-scan-incomplete-20261021','logs':'disk-scan-varlog-20261021'}
SCANNER_HASH = '9c39c47e4ecac542e0fc26fbc268133a414374ef3df71af1248208fbe8f28027'

class Validation:
    def __init__(self):
        self.cookie = None
        self.report = {'started_at':dt.datetime.now(dt.timezone.utc).isoformat(),'status':'RUNNING','devices':IDS,'cases':[]}
        self.stop = threading.Event()
        self.health = []

    def request(self, path, data=None):
        headers={'X-SeeSize-Request':'1'}
        if self.cookie:headers['Cookie']=self.cookie
        body=None if data is None else json.dumps(data).encode()
        if body is not None:headers['Content-Type']='application/json'
        with urllib.request.urlopen(urllib.request.Request(BASE+path,data=body,headers=headers),timeout=15) as response:
            return json.load(response),response.headers

    def save(self):
        (WORK/'results.json').write_text(json.dumps(self.report,indent=2))

    def monitor(self):
        while not self.stop.is_set():
            started=time.monotonic()
            try:
                with urllib.request.urlopen('http://127.0.0.1:18081/healthz',timeout=3) as response:
                    ok=response.status==200 and json.load(response).get('status')=='ok'
            except Exception:ok=False
            self.health.append({'ok':ok,'seconds':time.monotonic()-started})
            self.stop.wait(1)

    def server_state(self):
        output={}
        for name in ('hub','agent'):
            pid=subprocess.check_output(['systemctl','--user','show','seesize-'+name,'-p','MainPID','--value'],text=True).strip()
            rss=next(line.split()[1] for line in (Path('/proc')/pid/'status').read_text().splitlines() if line.startswith('VmRSS:'))
            output[name]={'pid':int(pid),'rss_kib':int(rss)}
        with sqlite3.connect('file:'+str(ROOT/'data/seesize.db')+'?mode=ro',uri=True) as db:
            output['main_last_sample_ns']=db.execute("select max(collected_at_ns) from metric_samples where agent_id='wsrser-main'").fetchone()[0]
        return output

    def scan(self,label,root,identity=None,rate=1000,limit=20000,timeout='15s',depth=3,upload=False,invalid=False):
        folder=WORK/label;folder.mkdir()
        args=['nice','-n','10','ionice','-c','3',str(WORK/'seesize-scan-linux-amd64'),'-root',str(root),'-depth',str(depth),'-max-entries',str(limit),'-rate',str(rate),'-timeout',timeout]
        if identity:args+=['-id',identity]
        if upload:args+=['-hub',BASE,'-token-file',str(WORK/('invalid.token' if invalid else identity+'.token'))]
        started=time.monotonic();observed={}
        print('Scanning '+label,flush=True)
        with (folder/'snapshot.json').open('xb') as output,(folder/'stderr.log').open('xb') as errors:
            process=subprocess.Popen(args,stdout=output,stderr=errors)
            while True:
                child,status,usage=os.wait4(process.pid,os.WNOHANG)
                if child:
                    process.returncode=os.WEXITSTATUS(status) if os.WIFEXITED(status) else -os.WTERMSIG(status)
                    break
                try:
                    for line in (Path('/proc')/str(process.pid)/'io').read_text().splitlines():
                        key,value=line.split(':');observed[key]=max(observed.get(key,0),int(value))
                except (FileNotFoundError,PermissionError,ProcessLookupError):pass
                if time.monotonic()-started>80:
                    process.kill();os.wait4(process.pid,0);raise RuntimeError('scan exceeded outer process budget')
                time.sleep(.02)
        elapsed=time.monotonic()-started
        raw=(folder/'snapshot.json').read_bytes().rstrip(b'\n')
        snap=json.loads(raw) if raw else None
        result={'label':label,'root':str(root),'rate_entries_per_second':rate,'max_entries':limit,'timeout':timeout,'display_depth':depth,
                'upload_attempted':upload,'exit_code':process.returncode,'elapsed_seconds':round(elapsed,4),
                'cpu_seconds':round(usage.ru_utime+usage.ru_stime,6),'cpu_average_one_core_percent':round((usage.ru_utime+usage.ru_stime)/elapsed*100,3),
                'peak_rss_kib':usage.ru_maxrss,'filesystem_input_blocks':usage.ru_inblock,'filesystem_output_blocks':usage.ru_oublock,
                'observed_proc_io':observed,'snapshot_bytes':len(raw),'complete':snap.get('complete') if snap else None,
                'entries':snap.get('entries') if snap else None,'skipped':snap.get('skipped') if snap else None,
                'reason':snap.get('reason') if snap else None,'directories':len(snap['nodes']) if snap else 0,
                'root_logical_bytes':next((n['bytes'] for n in snap['nodes'] if n['path']=='.'),None) if snap else None}
        if process.returncode:result['error']=(folder/'stderr.log').read_text().strip()
        self.report['cases'].append(result);self.save()
        print(json.dumps({k:result[k] for k in ('label','exit_code','complete','elapsed_seconds','peak_rss_kib','snapshot_bytes','reason')}),flush=True)
        return snap,result

    def register(self):
        credential=(ROOT/'admin.token').read_text().strip()
        _,headers=self.request('/api/v1/login',{'credential':credential})
        cookies=SimpleCookie();cookies.load(headers['Set-Cookie']);self.cookie='seesize_session='+cookies['seesize_session'].value
        devices,_=self.request('/api/v1/devices')
        assert not set(IDS.values())&{d['agent_id'] for d in devices['devices']}, 'test identity exists; refuse reuse'
        for identity in IDS.values():
            code,_=self.request('/api/v1/enrollments',{'agent_id':identity})
            registered,_=self.request('/api/v1/agents/register',{'agent_id':identity,'code':code['code']})
            fd=os.open(WORK/(identity+'.token'),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
            with os.fdopen(fd,'w') as stream:stream.write(registered['token'])
            subprocess.run(['systemd-run','--user','--unit','seesize-'+identity,'--property=RuntimeMaxSec=24h',
                '--property=Nice=10','--property=IOSchedulingClass=idle','--property=UMask=0077',
                str(ROOT/'seesize-agent-linux-amd64'),'-hub',BASE,'-id',identity,'-token-file',str(WORK/(identity+'.token')),'-interval','10s'],check=True,capture_output=True,text=True)

    def fixture(self,name):
        root=WORK/name;root.mkdir()
        for path,size in [('app-logs/app.log',8<<20),('app-logs/service/detail.log',1<<20),('cache/tmp.bin',4<<20),('archive/old.bin',2<<20),('docs/readme.txt',4096)]:
            target=root/path;target.parent.mkdir(parents=True,exist_ok=True)
            with target.open('xb') as stream:stream.truncate(size)
        return root

    def run(self):
        assert WORK.resolve()==WORK and WORK.stat().st_mode&0o077==0
        assert hashlib.sha256((WORK/'seesize-scan-linux-amd64').read_bytes()).hexdigest()==SCANNER_HASH
        assert shutil.disk_usage(WORK).free>2*1024**3
        assert not (WORK/'results.json').exists()
        self.report['before']=self.server_state();self.save()
        thread=threading.Thread(target=self.monitor,daemon=True);thread.start()
        try:
            self.register()
            success=self.fixture('success-files');partial=self.fixture('partial-files')
            self.scan('success-baseline',success,IDS['success'],upload=True)
            with (success/'app-logs/app.log').open('r+b') as stream:stream.truncate(136<<20)
            with (success/'cache/tmp.bin').open('r+b') as stream:stream.truncate(1<<20)
            (success/'archive/old.bin').unlink();(success/'archive').rmdir()
            self.scan('success-growth',success,IDS['success'],upload=True)
            data,_=self.request('/api/v1/servers/'+IDS['success']+'/disk')
            assert data['comparable'] and len(data['alerts'])==2
            changes={c['path']:c['delta'] for c in data['changes']}
            assert changes['app-logs']==128<<20 and changes['cache']==-(3<<20) and changes['archive']==-(2<<20)
            self.report['success_comparison']={'changes':data['changes'],'alerts':data['alerts']}
            baseline,_=self.scan('partial-baseline',partial,IDS['partial'],upload=True)
            snapshots=[]
            for label,settings in [('entry-limit',{'limit':3}),('soft-timeout',{'rate':1,'timeout':'100ms'})]:
                snap,_=self.scan(label,partial,IDS['partial'],upload=True,**settings)
                assert not snap['complete'];snapshots.append(snap)
                response,_=self.request('/api/v1/servers/'+IDS['partial']+'/disk')
                assert response['reference_snapshot']['at']==baseline['at'] and not response['comparable'] and not response['alerts']
            denied=partial/'restricted';denied.mkdir();(denied/'file.bin').write_bytes(b'x'*4096);denied.chmod(0)
            try:denied_snap,_=self.scan('permission-denied',partial,IDS['partial'],upload=True)
            finally:denied.chmod(0o700)
            assert not denied_snap['complete'] and denied_snap['skipped']>0
            response,_=self.request('/api/v1/servers/'+IDS['partial']+'/disk')
            assert response['reference_snapshot']['at']==baseline['at'] and not response['comparable'] and not response['changes']
            self.report['partial_reference']={'latest_reason':response['snapshots'][0]['reason'],'reference_at':baseline['at'],'comparable':response['comparable']}
            _,missing=self.scan('missing-directory',WORK/'does-not-exist',IDS['partial'])
            assert missing['exit_code']!=0 and missing['snapshot_bytes']==0
            (WORK/'invalid.token').write_text('invalid-dedicated-test-token');(WORK/'invalid.token').chmod(0o600)
            _,invalid=self.scan('upload-rejected',partial,IDS['partial'],upload=True,invalid=True)
            assert invalid['exit_code']!=0 and '401' in invalid['error']
            response2,_=self.request('/api/v1/servers/'+IDS['partial']+'/disk')
            assert response2['snapshots'][0]['at']==response['snapshots'][0]['at']
            for count in (1000,5000,10000):
                root=WORK/('scale-'+str(count));root.mkdir()
                for i in range(count):
                    parent=root/('group-%02d'%(i%20))/('shard-%02d'%((i//20)%5));parent.mkdir(parents=True,exist_ok=True)
                    (parent/('file-%05d.bin'%i)).write_bytes(b'x'*4096)
                snap,result=self.scan('scale-'+str(count)+'-rate1000',root)
                assert snap['complete'] and result['root_logical_bytes']==count*4096
                if count==1000:
                    shallow,_=self.scan('scale-1000-depth1',root,depth=1)
                    assert shallow['nodes'][0]['bytes']==snap['nodes'][0]['bytes'] and len(shallow['nodes'])<len(snap['nodes'])
                if count==10000:
                    slow,_=self.scan('scale-10000-rate200',root,rate=200,timeout='60s')
                    assert slow['complete']
            self.scan('real-backups',ROOT/'backups',rate=200,limit=2000,timeout='10s')
            self.scan('real-var-log',Path('/var/log'),IDS['logs'],rate=200,limit=2000,timeout='10s',upload=True)
            with sqlite3.connect('file:'+str(ROOT/'data/seesize.db')+'?mode=ro',uri=True) as db:
                self.report['stored_snapshots']=[dict(zip(('agent_id','count','json_bytes','max_json_bytes'),row)) for row in db.execute('select agent_id,count(*),sum(length(cast(snapshot_json as blob))),max(length(cast(snapshot_json as blob))) from disk_snapshots where agent_id in (?,?,?) group by agent_id',tuple(IDS.values()))]
                start=int(dt.datetime.fromisoformat(self.report['started_at']).timestamp()*1e9)-20*10**9
                times=[row[0] for row in db.execute("select collected_at_ns from metric_samples where agent_id='wsrser-main' and collected_at_ns>=? order by collected_at_ns",(start,))]
                self.report['main_heartbeat_max_gap_seconds']=max(((b-a)/1e9 for a,b in zip(times,times[1:])),default=None)
            self.report['after']=self.server_state()
            assert self.report['after']['main_last_sample_ns']>self.report['before']['main_last_sample_ns']
            assert all(self.report['after'][name]['pid']==self.report['before'][name]['pid'] for name in ('hub','agent'))
            self.report['fixture_allocated_bytes']=int(subprocess.check_output(['du','-s','--block-size=1',str(WORK)],text=True).split()[0])
            self.report['status']='PASS'
        finally:
            self.stop.set();thread.join(timeout=5)
            self.report['health_checks']={'count':len(self.health),'failures':sum(not x['ok'] for x in self.health),'max_seconds':max((x['seconds'] for x in self.health),default=None)}
            self.report['finished_at']=dt.datetime.now(dt.timezone.utc).isoformat();self.save()
            if self.cookie:
                try:self.request('/api/v1/logout',{})
                except Exception:pass
        print(json.dumps({'status':self.report['status'],'report':str(WORK/'results.json'),'devices':IDS}),flush=True)

if __name__=='__main__':
    Validation().run()
