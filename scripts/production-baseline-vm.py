#!/usr/bin/env python3
"""Exact production binary rehearsal on fixed disposable VM; no cloud access."""
import hashlib
import json
from pathlib import Path
import shutil
import socket
import sqlite3
import subprocess
import time
import urllib.request

root=Path('/home/wsr/seesize-upgrade-lab')
baseline=Path('/home/wsr/production-baseline-20260927')
kit=Path('/home/wsr/upgrade-kit-fixed')
original=root/'pre-vm-switch-01'
expected={'hub':'78beb8d7f29a387898f695ffcd1fa318a99ea9a27a93f091b4ee4df5e2aa714d','agent':'a5aa4047fbbd8aebbb375991c9c46b7dd918291c664a53f9129e9c20a89c84eb'}
def binary(folder,c): return folder/f'seesize-{c}-linux-amd64'
def digest(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def run(*args): return subprocess.run(args,check=True,capture_output=True,text=True,timeout=120)
if socket.gethostname()!='wsr' or root.resolve()!=root or not original.is_dir():
    raise SystemExit('Wrong lab')
manifest=json.loads((kit/'build-manifest.json').read_text())
for c in expected:
    if digest(binary(baseline,c))!=expected[c]: raise SystemExit('Baseline mismatch')
    if digest(binary(kit,c))!=manifest['sha256'][binary(kit,c).name]: raise SystemExit('Candidate mismatch')
    if digest(binary(root,c))!=digest(binary(original,c)): raise SystemExit('Original lab mismatch')
    state=subprocess.run(['systemctl','--user','is-active','seesize-'+c],capture_output=True,text=True).stdout.strip()
    if state!='inactive': raise SystemExit('Expected stopped service')
    command=run('systemctl','--user','show','seesize-'+c,'-p','ExecStart','--value').stdout
    if str(binary(root,c)) not in command: raise SystemExit('Wrong service path')
release='vm-production-01'
if (root/('pre-'+release)).exists(): raise SystemExit('Already tested release')
report={'status':'FAILED','scope':'local VM; production binaries only, synthetic local data','baseline_hashes':expected,'stages':{}}
def replace(c,folder):
    src=binary(folder,c)
    dst=binary(root,c)
    tmp=dst.with_name(dst.name+'.production-test-next')
    if tmp.exists() or tmp.is_symlink(): raise RuntimeError('Staging exists')
    shutil.copyfile(src,tmp); tmp.chmod(0o700)
    if digest(tmp)!=digest(src): raise RuntimeError('Copy mismatch')
    tmp.replace(dst)
def start(c):
    run('systemctl','--user','start','seesize-'+c)
    time.sleep(2)
def stop(c): run('systemctl','--user','stop','seesize-'+c)
def observe(label):
    rows=[]
    for i in range(4):
        if i: time.sleep(5)
        with urllib.request.urlopen('http://127.0.0.1:28081/healthz',timeout=3) as r:
            if r.status!=200: raise RuntimeError('Unhealthy')
        with sqlite3.connect((root/'data/seesize.db').as_uri()+'?mode=ro',uri=True) as db:
            rows.append(list(db.execute('SELECT count(*),max(collected_at_ns) FROM metric_samples WHERE agent_id=?',('upgrade-lab-only',)).fetchone()))
    report['stages'][label]=rows
    if any(b[0]<=a[0] or b[1]<=a[1] for a,b in zip(rows,rows[1:])): raise RuntimeError('No progress: '+label)
    print('PASS '+label+' '+json.dumps(rows),flush=True)
try:
    replace('hub',baseline); replace('agent',baseline)
    start('hub'); start('agent'); observe('production-baseline')
    args=['python3',str(kit/'upgrade-prepare.py'),'--root',str(root),'--release',release,'--prepare']
    for c in expected: args+=['--candidate',c+'='+str(binary(kit,c)),'--sha256',c+'='+digest(binary(kit,c))]
    run(*args)
    bundle=root/('pre-'+release)
    for c in expected:
        if digest(binary(bundle,c))!=expected[c]: raise RuntimeError('Rollback bundle mismatch')
    stop('hub'); replace('hub',bundle/'candidates'); start('hub'); observe('new-hub-production-agent')
    stop('agent'); replace('agent',bundle/'candidates'); start('agent'); observe('new-pair')
    stop('agent'); replace('agent',bundle); start('agent'); observe('rollback-agent')
    stop('hub'); replace('hub',bundle); start('hub'); observe('rollback-pair')
    report['status']='PASS'
finally:
    stop('agent'); stop('hub')
    replace('hub',original); replace('agent',original)
    report['final_hashes']={c:digest(binary(root,c)) for c in expected}
    (root/'production-baseline-acceptance.json').write_text(json.dumps(report,indent=2))
print(json.dumps(report),flush=True)
