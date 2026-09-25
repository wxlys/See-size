import datetime as dt
import importlib.util
from pathlib import Path
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('maintenance',Path(__file__).with_name('storage-maintenance.py'))
m=importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

class RetentionTests(unittest.TestCase):
    def test_keep_three_and_expire_registered_only(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory).resolve()
            entries=[]
            for i in range(4):
                name=f'pre-test-{i}'
                (root/name).mkdir()
                entries.append({'name':name,'kind':'rollback','created_at':f'2026-09-2{i}T00:00:00Z'})
            (root/'migration').mkdir()
            (root/'unknown').mkdir()
            entries.append({'name':'migration','kind':'temporary','expires_at':'2026-09-24T00:00:00+00:00'})
            now=dt.datetime(2026,9,25,tzinfo=dt.timezone.utc)
            self.assertEqual(m.plan(root,entries,now),['migration','pre-test-0'])
            self.assertTrue((root/'migration').exists()) # Planning is read-only.
    def test_reject_protected_and_traversal(self):
        with tempfile.TemporaryDirectory() as directory:
            for name in ('data','backups','../other','agent.token','.'):
                with self.assertRaises(ValueError):
                    m.plan(Path(directory).resolve(),[{'name':name,'kind':'temporary'}],dt.datetime.now(dt.timezone.utc))

if __name__=='__main__': unittest.main()
