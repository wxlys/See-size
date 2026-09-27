import contextlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('upgrade',Path(__file__).with_name('upgrade-prepare.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

class UpgradeTests(unittest.TestCase):
    def test_hash_and_architecture(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d).resolve()/'candidate';p.write_bytes(b'invalid')
            with self.assertRaises(ValueError):m.validate_candidate(p,m.digest(p))
            b=bytearray(20);b[:6]=b'\x7fELF\x02\x01';b[18:20]=(62).to_bytes(2,'little');p.write_bytes(b)
            self.assertEqual(m.validate_candidate(p,m.digest(p)),p)
            with self.assertRaises(ValueError):m.validate_candidate(p,'0'*64)
    def test_duplicate_component(self):
        with self.assertRaises(ValueError):m.mapping(['hub=a','hub=b'])
    def test_prepare_success_failure_and_dry_run(self):
        # Mock external services/backup command; filesystem transactions are real.
        for mode in ('dry','success','failure'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as directory:
                root=Path(directory).resolve();root.chmod(0o700)
                (root/'data').mkdir();(root/'data/seesize.db').write_bytes(b'test')
                (root/'maintenance-artifacts.json').write_text('[]')
                b=bytearray(20);b[:6]=b'\x7fELF\x02\x01';b[18:20]=(62).to_bytes(2,'little')
                for c in m.COMPONENTS:(root/f'seesize-{c}-linux-amd64').write_bytes(b)
                candidate=root/'candidate';candidate.write_bytes(b)
                argv=['upgrade','--root',str(root),'--release','test','--candidate','hub='+str(candidate),'--sha256','hub='+m.digest(candidate)]
                if mode!='dry':argv.append('--prepare')
                def backup(args,**kwargs):
                    if mode=='failure':raise m.subprocess.CalledProcessError(1,args)
                    target=Path(args[args.index('-dir')+1]);(target/'seesize-test.db').write_bytes(b'snapshot')
                fake_lock=types.SimpleNamespace(LOCK_EX=1,LOCK_NB=2,flock=lambda *args:None)
                with patch.object(sys,'argv',argv),patch.dict(sys.modules,{'fcntl':fake_lock}),patch.object(m.subprocess,'check_output',return_value='test unit'),patch.object(m.subprocess,'run',side_effect=backup),contextlib.redirect_stdout(io.StringIO()):
                    # Windows mode bits cannot model private Unix directories.
                    original=Path.stat
                    def info(p,*args,**kwargs):
                        result=original(p,*args,**kwargs)
                        if p==root:
                            return types.SimpleNamespace(st_mode=0o40700)
                        return result
                    with patch.object(Path,'stat',info):
                        if mode=='failure':
                            with self.assertRaises(m.subprocess.CalledProcessError):m.main()
                        else:m.main()
                self.assertEqual((root/'pre-test').exists(),mode=='success')
                entries=json.loads((root/'maintenance-artifacts.json').read_text())
                self.assertEqual(len(entries),1 if mode=='success' else 0)
                self.assertEqual((root/'data/seesize.db').read_bytes(),b'test')
                self.assertEqual((root/'seesize-hub-linux-amd64').read_bytes(),bytes(b))
                if mode=='dry':self.assertFalse((root/'.maintenance.lock').exists())

if __name__=='__main__':unittest.main()
