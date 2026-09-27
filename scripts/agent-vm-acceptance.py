#!/usr/bin/env python3
"""Fixed-scope acceptance for the disposable local VM, never production."""
import hashlib
import json
from pathlib import Path
import shutil
import socket
import sqlite3
import subprocess
import time
import urllib.request

root = Path('/home/wsr/seesize-upgrade-lab')
kit = Path('/home/wsr/upgrade-kit-fixed')
if socket.gethostname() != 'wsr' or root.is_symlink() or not (root/'pre-vm-switch-01').is_dir():
    raise SystemExit('Unexpected lab environment')
def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=30)
def digest(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()
units = ('seesize-hub', 'seesize-agent')
for unit in units:
    state = subprocess.run(['systemctl','--user','is-active',unit],capture_output=True,text=True).stdout.strip()
    if state != 'inactive':
        raise SystemExit('Expected stopped test service: '+unit)
    command = run('systemctl','--user','show',unit,'-p','ExecStart','--value').stdout
    if str(root/f'{unit}-linux-amd64') not in command:
        raise SystemExit('Unexpected service command')
candidate = kit/'seesize-agent-linux-amd64'
manifest = json.loads((kit/'build-manifest.json').read_text())
if digest(candidate) != manifest['sha256'][candidate.name]:
    raise SystemExit('Candidate hash mismatch')
live = root/candidate.name
old_hash = digest(live)
bundle = root/'pre-vm-agent-pair-02'
if bundle.exists():
    raise SystemExit('Release already exists')
report = {'old_hash':old_hash,'new_hash':digest(candidate),'scope':'local VM only','stages':{}}
def observe(name):
    samples=[]
    for i in range(4):
        if i: time.sleep(5)
        with urllib.request.urlopen('http://127.0.0.1:28081/healthz',timeout=3) as response:
            if response.status != 200: raise RuntimeError('Unhealthy hub')
        with sqlite3.connect((root/'data/seesize.db').as_uri()+'?mode=ro',uri=True) as db:
            row=db.execute('SELECT count(*),max(collected_at_ns) FROM metric_samples WHERE agent_id=?',('upgrade-lab-only',)).fetchone()
        samples.append(list(row))
    report['stages'][name]=samples
    if any(b[0]<=a[0] or b[1]<=a[1] for a,b in zip(samples,samples[1:])):
        raise RuntimeError('Samples not advancing: '+name)
    print('PASS '+name+' '+json.dumps(samples),flush=True)
def replace(source):
    temp=root/'seesize-agent-linux-amd64.acceptance-next'
    if temp.exists(): raise RuntimeError('Staging exists')
    shutil.copyfile(source,temp)
    temp.chmod(0o700)
    if digest(temp)!=digest(source): raise RuntimeError('Copy mismatch')
    temp.replace(live)
switched=False
hub_live=root/'seesize-hub-linux-amd64'
hub_old=root/'pre-vm-switch-01/seesize-hub-linux-amd64'
hub_new=kit/'seesize-hub-linux-amd64'
if digest(hub_live)!=digest(hub_old) or digest(hub_new)!=manifest['sha256'][hub_new.name]:
    raise SystemExit('Unexpected Hub baseline or candidate')
def replace_hub(source):
    temporary=root/'seesize-hub-linux-amd64.acceptance-next'
    if temporary.exists(): raise RuntimeError('Hub staging exists')
    shutil.copyfile(source,temporary)
    temporary.chmod(0o700)
    if digest(temporary)!=digest(source): raise RuntimeError('Hub copy mismatch')
    temporary.replace(hub_live)
try:
    replace_hub(hub_new)
    run('systemctl','--user','start','seesize-hub')
    for attempt in range(20):
        try:
            urllib.request.urlopen('http://127.0.0.1:28081/healthz',timeout=1).close()
            break
        except OSError: time.sleep(0.5)
    run('systemctl','--user','start','seesize-agent')
    time.sleep(3)
    observe('old-agent')
    run('python3',str(kit/'upgrade-prepare.py'),'--root',str(root),'--release','vm-agent-pair-02','--candidate','agent='+str(candidate),'--sha256','agent='+digest(candidate),'--prepare')
    run('systemctl','--user','stop','seesize-agent')
    switched=True
    replace(bundle/'candidates'/candidate.name)
    run('systemctl','--user','start','seesize-agent')
    time.sleep(3)
    observe('new-agent')
    run('systemctl','--user','stop','seesize-agent')
    replace(bundle/candidate.name)
    run('systemctl','--user','start','seesize-agent')
    time.sleep(3)
    observe('rollback-agent')
    if digest(live)!=old_hash: raise RuntimeError('Rollback hash mismatch')
    report['status']='PASS'
finally:
    run('systemctl','--user','stop','seesize-agent')
    if switched and digest(live)!=old_hash:
        replace(bundle/candidate.name)
    run('systemctl','--user','stop','seesize-hub')
    replace_hub(hub_old)
    report['final_hash']=digest(live)
    report['final_hub_hash']=digest(hub_live)
    (root/'agent-vm-pair-acceptance.json').write_text(json.dumps(report,indent=2))
print(json.dumps(report,indent=2),flush=True)
