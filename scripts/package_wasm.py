#!/usr/bin/env python3
"""Build ebyte-wasm.zip from the release commit; smoke-test it with Node before and after packing."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import zipfile

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument('--local', action='store_true', help='allow a dirty local review build; never permitted in Actions')
args = parser.parse_args()
assert not args.local or os.environ.get('GITHUB_ACTIONS') != 'true'


def command(argv, **kwargs):
    return subprocess.check_output(argv, cwd=root, **kwargs).decode().strip()


version = re.search(r'const Version = "([^"]+)"', (root / 'version.go').read_text()).group(1)
revision = command(['git', 'rev-parse', 'HEAD'])
dirty = bool(command(['git', 'status', '--porcelain', '--untracked-files=normal']))
goversion = command(['go', 'env', 'GOVERSION'])
if not args.local:
    assert not dirty, 'release checkout must be clean'
    assert revision == os.environ['EBYTE_RELEASE_SHA']
    assert goversion == 'go1.27.3'
goroot = Path(command(['go', 'env', 'GOROOT']))
notices = {}
for p in goroot.rglob('*'):
    if p.is_file() and (p.name in ('LICENSE', 'PATENTS', 'COPYING', 'NOTICE') or p.name.startswith(('LICENSE.', 'COPYING.', 'NOTICE.'))):
        if p.parent == goroot or p.is_relative_to(goroot / 'src'):
            notices[str(p.relative_to(goroot)).replace('\\', '/')] = p.read_bytes()
assert 'LICENSE' in notices
out = root / 'dist'
out.mkdir(exist_ok=True)
with tempfile.TemporaryDirectory() as temporary:
    work = Path(temporary)
    content = work / 'ebyte'
    content.mkdir()
    env = os.environ.copy()
    env.update(GOOS='js', GOARCH='wasm', CGO_ENABLED='0')
    subprocess.run(['go', 'build', '-trimpath', '-buildvcs=true', '-ldflags=-s -w', '-o', str(content / 'ebyte.wasm'), './cmd/ebyte-wasm'], cwd=root, env=env, check=True)
    shutil.copyfile(goroot / 'lib' / 'wasm' / 'wasm_exec.js', content / 'wasm_exec.js')
    expect = [] if args.local else [revision, goversion]
    smoke = json.loads(command(['node', 'scripts/wasm_smoke.mjs', str(content), version, *expect]))
    for name in ('LICENSE', 'NOTICE', 'THIRD_PARTY_LICENSES.md'):
        shutil.copyfile(root / name, content / name)
    shutil.copyfile(root / 'docs' / 'wasm.md', content / 'WASM.md')
    for name, data in notices.items():
        dest = content / 'GO-NOTICES' / name
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_bytes(data)
    metadata = {'version': version, 'source_sha': revision, 'source_dirty': dirty, 'go_version': goversion,
                'goos': 'js', 'goarch': 'wasm',
                'wasm_sha256': hashlib.sha256((content / 'ebyte.wasm').read_bytes()).hexdigest(),
                'wasm_exec_sha256': hashlib.sha256((content / 'wasm_exec.js').read_bytes()).hexdigest(),
                'verification': 'Node smoke passed before and after packing',
                'go_notices': {name: hashlib.sha256(data).hexdigest() for name, data in sorted(notices.items())}}
    (content / 'release.json').write_text(json.dumps(metadata, indent=2) + '\n', encoding='utf-8')
    destination = out / 'ebyte-wasm.zip'
    with zipfile.ZipFile(destination, 'w', zipfile.ZIP_DEFLATED) as archive:
        for file in sorted(content.rglob('*')):
            if file.is_file():
                archive.write(file, file.relative_to(work).as_posix())
    extracted = work / 'extracted'
    extracted.mkdir()
    with zipfile.ZipFile(destination) as archive:
        archive.extractall(extracted)
    packed = extracted / 'ebyte'
    assert hashlib.sha256((packed / 'ebyte.wasm').read_bytes()).hexdigest() == metadata['wasm_sha256']
    command(['node', 'scripts/wasm_smoke.mjs', str(packed), version, *expect])
item = {'archive': 'ebyte-wasm.zip', 'sha256': hashlib.sha256(destination.read_bytes()).hexdigest(), 'metadata': metadata, 'wasm_smoke': smoke}
(out / 'manifest-wasm.json').write_text(json.dumps(item, indent=2) + '\n', encoding='utf-8')
print(json.dumps({'archive': 'ebyte-wasm.zip', 'wasm_smoke': smoke['wasm_smoke']}))
