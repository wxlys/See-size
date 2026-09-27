#!/usr/bin/env python3
"""Read-only Linux amd64 user-service preflight; never installs or restarts."""
import argparse
import json
import os
from pathlib import Path
import platform
import shutil
import socket
import stat
import subprocess
import urllib.request

def executable_check(path):
    if path.is_symlink() or not path.is_file():
        return False, 'missing regular executable'
    if not os.access(path, os.X_OK):
        return False, 'execute permission missing'
    with path.open('rb') as f:
        header = f.read(20)
    if len(header)<20 or header[:4]!=b'\x7fELF' or header[4:6]!=b'\x02\x01' or int.from_bytes(header[18:20],'little')!=62:
        return False, 'expected Linux x86_64 ELF binary'
    return True, 'ELF x86_64; permissions OK (binary not executed)'

def credential_check(path):
    if path.is_symlink() or not path.is_file():
        return False, 'missing regular credential file'
    info=path.stat()
    if info.st_uid!=os.getuid() or stat.S_IMODE(info.st_mode)&0o077:
        return False, 'credential must belong to current user and deny group/other access'
    if not os.access(path,os.R_OK) or info.st_size==0:
        return False, 'credential empty or unreadable'
    return True, 'owner-only, readable, nonempty; content not displayed'

def command(args):
    try:
        r=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,timeout=8)
        return r.returncode,r.stdout.strip()
    except (OSError,subprocess.TimeoutExpired):
        return -1,''

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--root',required=True,help='existing dedicated deployment/staging directory')
    p.add_argument('--mode',choices=['install','installed'],default='installed')
    p.add_argument('--port',type=int,default=18081)
    args=p.parse_args()
    if not 1024<=args.port<=65535: p.error('port must be 1024..65535')
    checks=[]
    def add(name,ok,detail,warning=False):
        checks.append({'check':name,'status':'PASS' if ok else ('WARN' if warning else 'FAIL'),'detail':detail})
    add('platform',platform.system()=='Linux' and platform.machine()=='x86_64','Linux x86_64 user-service deployment required')
    path=Path(args.root).absolute();root=path.resolve()
    valid=path==root and root.is_dir() and root not in (Path('/'),Path.home())
    add('root',valid,'dedicated existing directory, no symbolic path components')
    if valid:
        add('root_permissions',root.stat().st_uid==os.getuid() and not(stat.S_IMODE(root.stat().st_mode)&0o077),'current-user owned; group/other access denied')
        free=shutil.disk_usage(root).free
        add('free_disk',free>=2*1024**3,f'{free} bytes free; minimum 2 GiB')
        add('root_writable',os.access(root,os.W_OK|os.X_OK),'permission check only; no probe file written')
        for name in ['hub','agent','backup']:
            ok,detail=executable_check(root/f'seesize-{name}-linux-amd64');add('binary_'+name,ok,detail)
        for name in ['admin.token','agent.token']:
            ok,detail=credential_check(root/name);add(name,ok,detail)
        for name in ['data','backups']:
            target=root/name
            ok=target.is_dir() and not target.is_symlink() and target.resolve().parent==root and os.access(target,os.R_OK|os.W_OK|os.X_OK)
            add('directory_'+name,ok,'existing writable real directory required')
    code,user=command(['id','-un'])
    code,linger=command(['loginctl','show-user',user,'-p','Linger','--value'])
    add('linger',code==0 and linger=='yes','required for user services without an interactive login')
    code,state=command(['systemctl','--user','is-system-running'])
    add('user_systemd',state in ('running','degraded'),'user manager state: '+(state or 'unavailable'))
    if state=='degraded':add('user_manager_health',False,'some user unit failed; inspect separately',True)
    for unit in ['seesize-hub.service','seesize-agent.service','seesize-backup.timer']:
        if args.mode=='installed':
            code,state=command(['systemctl','--user','is-active',unit]);add(unit,code==0 and state=='active',state or 'unavailable')
            code,enabled=command(['systemctl','--user','is-enabled',unit]);add(unit+'_enabled',code==0 and enabled=='enabled',enabled or 'unavailable')
        else:
            code,loaded=command(['systemctl','--user','show',unit,'-p','LoadState','--value'])
            add(unit+'_unused',loaded=='not-found','existing service must not be overwritten; LoadState='+loaded)
    if args.mode=='install':
        try:
            with socket.socket() as sock: sock.bind(('127.0.0.1',args.port))
            add('port',True,'loopback port available at check time; installer must recheck')
        except OSError: add('port',False,'loopback port occupied')
    else:
        try:
            with urllib.request.urlopen(f'http://127.0.0.1:{args.port}/healthz',timeout=5) as r:
                healthy=r.status==200 and json.load(r).get('status')=='ok'
            add('healthz',healthy,'read-only loopback health check; not full application verification')
        except Exception: add('healthz',False,'unavailable or invalid health response')
    blockers=sum(c['status']=='FAIL' for c in checks)
    print(json.dumps({'mode':args.mode,'read_only':True,'blockers':blockers,'checks':checks},ensure_ascii=False,indent=2))
    return 1 if blockers else 0

if __name__=='__main__': raise SystemExit(main())
