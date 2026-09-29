#!/usr/bin/env python3
"""Create non-release fixtures on an isolated Linux VM; never modifies services."""
import hashlib
import json
from pathlib import Path
import shutil

source = Path('/opt/seesize-agent')
target = Path.home() / 'seesize-update-fixtures-20260929'
target.mkdir(mode=0o700)  # Refuse to overwrite previous evidence.
for label in ('good', 'bad'):
    folder = target / label
    folder.mkdir()
    metadata = json.loads((source / 'agent-release.json').read_text())
    metadata['version'] = 'v0.1.0-update-' + label
    metadata['hub_requirement'] = 'LAB ONLY: same protocol-v1 agent; not a published release'
    for name in metadata['sha256']:
        original = Path('/usr/bin/false') if label == 'bad' and name == 'seesize-agent-linux-amd64' else source / name
        shutil.copyfile(original, folder / name)
        metadata['sha256'][name] = hashlib.sha256((folder / name).read_bytes()).hexdigest()
    (folder / 'agent-release.json').write_text(json.dumps(metadata))
print(target)
