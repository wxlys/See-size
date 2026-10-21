#!/usr/bin/env python3
"""Reproduce one failure on the previously created dedicated scan demo only."""
import datetime as dt
import importlib.util
import json
from http.cookies import SimpleCookie
from pathlib import Path
import sqlite3
import sys

spec=importlib.util.spec_from_file_location('validation',Path(__file__).with_name('disk-live-validation.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
case=sys.argv[1]
assert case in ('entry-limit','soft-timeout','permission-denied')
validation=module.Validation();validation.report=json.loads((module.WORK/'results.json').read_text())
_,headers=validation.request('/api/v1/login',{'credential':(module.ROOT/'admin.token').read_text().strip()})
cookies=SimpleCookie();cookies.load(headers['Set-Cookie']);validation.cookie='seesize_session='+cookies['seesize_session'].value
try:
    root=module.WORK/'partial-files';denied=root/'restricted'
    settings={'limit':3} if case=='entry-limit' else {'rate':1,'timeout':'100ms'} if case=='soft-timeout' else {}
    if case=='permission-denied':denied.chmod(0)
    try:snap,_=validation.scan('ui-'+case,root,module.IDS['partial'],upload=True,**settings)
    finally:
        if case=='permission-denied':denied.chmod(0o700)
    assert not snap['complete']
    response,_=validation.request('/api/v1/servers/'+module.IDS['partial']+'/disk')
    assert response['reference_snapshot'] and not response['comparable'] and not response['alerts'] and not response['changes']
    with sqlite3.connect('file:'+str(module.ROOT/'data/seesize.db')+'?mode=ro',uri=True) as db:
        validation.report['stored_snapshots']=[dict(zip(('agent_id','count','json_bytes','max_json_bytes'),row)) for row in db.execute('select agent_id,count(*),sum(length(cast(snapshot_json as blob))),max(length(cast(snapshot_json as blob))) from disk_snapshots where agent_id in (?,?,?) group by agent_id',tuple(module.IDS.values()))]
    validation.report['additional_ui_verification_at']=dt.datetime.now(dt.timezone.utc).isoformat();validation.save()
finally:validation.request('/api/v1/logout',{})
