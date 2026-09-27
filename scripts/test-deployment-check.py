import importlib.util
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('preflight',Path(__file__).with_name('deployment-check.py'))
m=importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

class PreflightTests(unittest.TestCase):
    def test_architecture_and_missing_binary(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'candidate'
            self.assertFalse(m.executable_check(p)[0])
            p.write_bytes(b'not an executable')
            p.chmod(0o700)
            self.assertFalse(m.executable_check(p)[0])
            header=bytearray(20);header[:6]=b'\x7fELF\x02\x01';header[18:20]=(62).to_bytes(2,'little')
            p.write_bytes(header)
            self.assertTrue(m.executable_check(p)[0])
            header[18:20]=(183).to_bytes(2,'little');p.write_bytes(header)
            self.assertFalse(m.executable_check(p)[0])
    def test_nonexistent_command(self):
        self.assertEqual(m.command(['seesize-nonexistent-test-command-123'])[0],-1)
    def test_credential_permissions_without_reading_contents(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'token';p.write_text('test-only')
            info=p.stat()
            with patch.object(m.os,'getuid',return_value=info.st_uid,create=True):
                with patch.object(m.stat,'S_IMODE',return_value=0o644):
                    self.assertFalse(m.credential_check(p)[0])
                with patch.object(m.stat,'S_IMODE',return_value=0o600):
                    self.assertTrue(m.credential_check(p)[0])
                    p.write_text('')
                    self.assertFalse(m.credential_check(p)[0])

if __name__=='__main__': unittest.main()
