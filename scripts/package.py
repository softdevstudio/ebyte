#!/usr/bin/env python3
"""Build five archive variants; smoke-test native binaries before and after packing."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import sys
import subprocess
import tarfile
import tempfile
import zipfile

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument('family', choices=('linux', 'darwin', 'windows'))
parser.add_argument('--local', action='store_true', help='allow a dirty local review build; never permitted in Actions')
args = parser.parse_args()
assert not args.local or os.environ.get('GITHUB_ACTIONS') != 'true'


def command(argv, **kwargs):
    return subprocess.check_output(argv, cwd=root, **kwargs).decode().strip()


version = re.search(r'const Version = "([^"]+)"', (root / 'version.go').read_text()).group(1)
revision = command(['git', 'rev-parse', 'HEAD'])
dirty = bool(command(['git', 'status', '--porcelain', '--untracked-files=normal']))
if not args.local:
    assert not dirty, 'release checkout must be clean'
    assert revision == os.environ['EBYTE_RELEASE_SHA']
    assert re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', version)
goversion = command(['go', 'env', 'GOVERSION'])
assert goversion == 'go1.27.3'
gomod = (root / 'go.mod').read_text()
assert 'go 1.27.3' in gomod
hostos = command(['go', 'env', 'GOHOSTOS'])
hostarch = command(['go', 'env', 'GOHOSTARCH'])
goroot = Path(command(['go', 'env', 'GOROOT']))
notices = {}
for p in goroot.rglob('*'):
    if p.is_file() and (p.name in ('LICENSE', 'PATENTS', 'COPYING', 'NOTICE') or p.name.startswith(('LICENSE.', 'COPYING.', 'NOTICE.'))):
        if p.parent == goroot or p.is_relative_to(goroot / 'src'):
            notices[str(p.relative_to(goroot)).replace('\\', '/')] = p.read_bytes()
assert 'LICENSE' in notices
out = root / 'dist'
out.mkdir(exist_ok=True)
archives = []
for arch in (('amd64',) if args.family == 'windows' else ('amd64', 'arm64')):
    target = args.family + '-' + arch
    native = args.family == hostos and arch == hostarch
    archive_name = 'ebyte-' + target + ('.zip' if args.family == 'windows' else '.tar.gz')
    with tempfile.TemporaryDirectory() as temporary:
        work = Path(temporary)
        content = work / 'ebyte'
        content.mkdir()
        binary_name = 'ebyte.exe' if args.family == 'windows' else 'ebyte'
        binary = content / binary_name
        env = os.environ.copy()
        env.update(GOOS=args.family, GOARCH=arch, CGO_ENABLED='0')
        subprocess.run(['go', 'build', '-trimpath', '-buildvcs=true', '-ldflags=-s -w', '-o', str(binary), './cmd/ebyte'], cwd=root, env=env, check=True)
        smoke = None
        if native:
            smoke = json.loads(command([sys.executable, 'scripts/release_smoke.py', str(binary), version]))
        for name in ('LICENSE', 'NOTICE', 'README.md', 'THIRD_PARTY_LICENSES.md'):
            shutil.copyfile(root / name, content / name)
        shutil.copytree(root / 'docs', content / 'docs')
        examples = content / 'examples'
        examples.mkdir()
        for name in ('packet.schema.json', 'packet.document.json', 'packet.hex'):
            shutil.copyfile(root / 'testdata' / name, examples / name)
        (examples / 'packet.bin').write_bytes(bytes.fromhex((examples / 'packet.hex').read_text()))
        (content / 'QUICKSTART.txt').write_text('EByte ' + version + '\n\nRun: ' + binary_name + ' decode --schema examples/packet.schema.json --input examples/packet.bin\nOutput defaults to stdout; --output FILE creates a new file and never replaces one.\nSee README.md and https://github.com/softdevstudio/ebyte for the manual.\n', encoding='utf-8')
        for name, data in notices.items():
            dest = content / 'GO-NOTICES' / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(data)
        metadata = {'version': version, 'source_sha': revision, 'source_dirty': dirty,
                    'go_version': goversion, 'goos': args.family, 'goarch': arch,
                    'native_tested': native, 'verification': 'native smoke passed' if native else 'cross-built; not executed',
                    'file_publication': args.family in ('linux', 'darwin', 'windows'),
                    'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
                    'go_notices': {name: hashlib.sha256(data).hexdigest() for name, data in sorted(notices.items())}}
        (content / 'release.json').write_text(json.dumps(metadata, indent=2) + '\n', encoding='utf-8')
        destination = out / archive_name
        if destination.suffix == '.zip':
            with zipfile.ZipFile(destination, 'w', zipfile.ZIP_DEFLATED) as archive:
                for file in sorted(content.rglob('*')):
                    if file.is_file():
                        archive.write(file, file.relative_to(work).as_posix())
        else:
            with tarfile.open(destination, 'w:gz') as archive:
                archive.add(content, arcname='ebyte')
        extracted = work / 'extracted'
        extracted.mkdir()
        if destination.suffix == '.zip':
            with zipfile.ZipFile(destination) as archive:
                archive.extractall(extracted)
        else:
            with tarfile.open(destination) as archive:
                archive.extractall(extracted, filter='data')
        packed = extracted / 'ebyte'
        assert hashlib.sha256((packed / binary_name).read_bytes()).hexdigest() == metadata['binary_sha256']
        assert (packed / 'LICENSE').read_bytes() == (root / 'LICENSE').read_bytes()
        if native:
            command([sys.executable, 'scripts/release_smoke.py', str(packed / binary_name), version])
        archives.append({'archive': archive_name, 'sha256': hashlib.sha256(destination.read_bytes()).hexdigest(),
                         'metadata': metadata, 'native_smoke': smoke})
assert any(item['metadata']['native_tested'] for item in archives), 'runner did not natively test any target'
(out / ('manifest-' + args.family + '.json')).write_text(json.dumps(archives, indent=2) + '\n', encoding='utf-8')
print(json.dumps({'family': args.family, 'archives': [item['archive'] for item in archives]}))
