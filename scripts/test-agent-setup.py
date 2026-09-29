import importlib.util
import hashlib
import json
from pathlib import Path
import sys
import types
import unittest
from unittest.mock import patch

if sys.platform=='win32': sys.modules.setdefault('pwd',types.SimpleNamespace())
spec=importlib.util.spec_from_file_location('setup',Path(__file__).with_name('agent-setup.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

class InstallerTests(unittest.TestCase):
    def test_https(self):
        self.assertEqual(m.hub_url('https://8.148.5.169/'),'https://8.148.5.169')
        self.assertEqual(m.hub_url('https://example.com:443'),'https://example.com:443')
        for value in ('http://example.com','https://a:b@example.com','https://a/path','https://a?x=1','https://a\n','https://a/%h','https://a:70000'):
            with self.subTest(value=value),self.assertRaises(ValueError): m.hub_url(value)
    def test_id(self):
        self.assertEqual(m.device_id('server-02'),'server-02')
        for value in ('','a b','%h','../x','a\nExecStart=x','a'*129):
            with self.assertRaises(ValueError): m.device_id(value)
    def test_version(self):
        self.assertEqual(m.version('v0.1.0-rc.1'),'v0.1.0-rc.1')
        for value in ('latest','../x','v1','v1.2.3/x'):
            with self.assertRaises(ValueError): m.version(value)
    def test_metadata(self):
        data={'schema':1,'version':'v0.1.0','platform':'linux-amd64','sha256':{n:'a'*64 for n in m.COMPONENTS},'hub_requirement':'verified Hub required'}
        self.assertEqual(m.manifest(json.dumps(data),'v0.1.0')['schema'],1)
        for key,value in (('schema',2),('version','v0.2.0'),('platform','linux-arm64'),('sha256',{}),('hub_requirement','')):
            invalid=dict(data);invalid[key]=value
            with self.assertRaises(ValueError): m.manifest(json.dumps(invalid),'v0.1.0')
    def test_binary(self):
        body=bytearray(32);body[:6]=b'\x7fELF\x02\x01';body[18:20]=(62).to_bytes(2,'little')
        m.validate_binary(body,hashlib.sha256(body).hexdigest())
        with self.assertRaises(ValueError):m.validate_binary(body,'0'*64)
        with self.assertRaises(ValueError):m.validate_binary(b'invalid','0'*64)
    def test_no_http_download(self):
        with self.assertRaises(ValueError):m.fetch('http://example.com',100)
        with self.assertRaises(ValueError):m.HTTPSRedirect().redirect_request(None,None,302,'',{},'http://example.com')
    def test_check_does_not_install(self):
        import tempfile
        data={'schema':1,'version':'v0.1.0','platform':'linux-amd64','sha256':{n:'a'*64 for n in m.COMPONENTS},'hub_requirement':'verified Hub required'}
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);(root/'agent-release.json').write_text(json.dumps(data))
            with patch.object(m,'ROOT',root),patch.object(m,'fetch',return_value=json.dumps(data).encode()),patch.object(m,'run') as run:
                m.check_update(types.SimpleNamespace(version='v0.1.0',bundle=None))
                run.assert_not_called()
    def test_atomic_copy(self):
        import tempfile
        with tempfile.TemporaryDirectory() as d:
            source=Path(d)/'source'; dest=Path(d)/'destination'
            source.write_bytes(b'new');dest.write_bytes(b'old')
            m.atomic_copy(source,dest)
            self.assertEqual(dest.read_bytes(),b'new')
            self.assertFalse(list(Path(d).glob('.replace-*')))
    def test_workspace_retention(self):
        import tempfile
        with tempfile.TemporaryDirectory() as d,patch.object(m,'ROOT',Path(d)):
            with m.update_workspace() as work:
                (work/'file').write_text('cancelled preparation')
            self.assertFalse(work.exists())
            with self.assertRaises(RuntimeError):
                with m.update_workspace() as retained:
                    (retained/'restore-required').write_text('keep')
                    raise RuntimeError('simulated restore failure')
            self.assertTrue(retained.exists())
            with self.assertRaises(ValueError):
                with m.update_workspace():pass

if __name__=='__main__':unittest.main()
