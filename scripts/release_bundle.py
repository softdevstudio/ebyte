#!/usr/bin/env python3
"""Verify all candidate archives and create the checksum manifest and release notes."""
import hashlib
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import zipfile

root = Path(__file__).resolve().parents[1]
dist = root / 'dist'
version, revision = sys.argv[1:3]
expected = {'ebyte-linux-amd64.tar.gz', 'ebyte-linux-arm64.tar.gz',
            'ebyte-darwin-amd64.tar.gz', 'ebyte-darwin-arm64.tar.gz', 'ebyte-windows-amd64.zip'}
items = []
for family in ('linux', 'darwin', 'windows'):
    items.extend(json.loads((dist / ('manifest-' + family + '.json')).read_text()))
assert len(items) == 5 and {item['archive'] for item in items} == expected
for item in items:
    metadata = item['metadata']
    assert metadata['version'] == version and metadata['source_sha'] == revision
    assert not metadata['source_dirty'] and metadata['go_version'] == 'go1.27.3'
    assert bool(item['native_smoke']) == metadata['native_tested']
    if item['native_smoke']:
        assert item['native_smoke']['native_smoke'] == 'passed' and item['native_smoke']['version'] == version
    archive = dist / item['archive']
    assert hashlib.sha256(archive.read_bytes()).hexdigest() == item['sha256']
    with tempfile.TemporaryDirectory() as temporary:
        target = Path(temporary)
        if archive.suffix == '.zip':
            with zipfile.ZipFile(archive) as packed:
                for info in packed.infolist():
                    assert info.filename.startswith('ebyte/') and '..' not in info.filename.split('/') and '\\' not in info.filename
                packed.extractall(target)
        else:
            with tarfile.open(archive) as packed:
                for info in packed.getmembers():
                    assert (info.name == 'ebyte' or info.name.startswith('ebyte/')) and '..' not in info.name.split('/')
                    assert info.isfile() or info.isdir()
                packed.extractall(target, filter='data')
        content = target / 'ebyte'
        actual = json.loads((content / 'release.json').read_text())
        assert actual == metadata
        for name in ('LICENSE', 'NOTICE', 'THIRD_PARTY_LICENSES.md'):
            assert (content / name).read_bytes() == (root / name).read_bytes()
        assert 'Apache License' in (content / 'LICENSE').read_text()
        assert 'LICENSE' in metadata['go_notices']
        for name, digest in metadata['go_notices'].items():
            assert not Path(name).is_absolute() and '..' not in Path(name).parts
            assert hashlib.sha256((content / 'GO-NOTICES' / name).read_bytes()).hexdigest() == digest
        binary = content / ('ebyte.exe' if metadata['goos'] == 'windows' else 'ebyte')
        assert hashlib.sha256(binary.read_bytes()).hexdigest() == metadata['binary_sha256']
        build = subprocess.check_output(['go', 'version', '-m', str(binary)]).decode()
        assert 'go1.27.3' in build and 'vcs.revision=' + revision in build and 'vcs.modified=false' in build
        assert 'GOOS=' + metadata['goos'] in build and 'GOARCH=' + metadata['goarch'] in build
# Three distinct native hosts must have exercised their own archive.
for family in ('linux', 'darwin', 'windows'):
    assert sum(item['metadata']['native_tested'] for item in items if item['metadata']['goos'] == family) == 1
manifest = {'version': version, 'source_sha': revision, 'archives': sorted(items, key=lambda item: item['archive'])}
(dist / 'RELEASE-MANIFEST.json').write_text(json.dumps(manifest, indent=2) + '\n', encoding='utf-8')
files = sorted(expected | {'RELEASE-MANIFEST.json'})
(dist / 'SHA256SUMS').write_text(''.join(hashlib.sha256((dist / name).read_bytes()).hexdigest() + '  ' + name + '\n' for name in files), encoding='utf-8')
lines = ['EByte ' + version, '', 'Apache License 2.0. One schema for decoding, editing, encoding and field inspection.', '',
         'Source commit: ' + revision, '', 'Archive verification:']
for item in manifest['archives']:
    lines.append('- ' + item['archive'] + ': ' + item['metadata']['verification'])
changes = re.search(r'^### EByte v' + re.escape(version) + r'\n(.*?)(?=^### |\Z)',
                    (root / 'docs' / 'releases.md').read_text(encoding='utf-8'), re.M | re.S)
if changes and changes.group(1).strip():
    lines.extend(['', 'Changes:', changes.group(1).strip()])
lines.extend(['', 'Windows supports stdin/stdout and file input; CLI --output FILE publication remains disabled there.',
              'Linux/macOS file publication requires hard links and a trusted output directory. Blocking filesystem calls may delay cancellation.',
              'Documents, traces and encoded output materialize in bounded memory. Allocation totals are not peak memory or throughput guarantees.',
              'Structural diff, automatic format discovery and graphical editing are outside v1.', '',
              'Each archive includes examples, Apache LICENSE/NOTICE, exact-toolchain GO-NOTICES and release.json. Verify downloads with SHA256SUMS.'])
(dist / 'release-notes.md').write_text('\n'.join(lines) + '\n', encoding='utf-8')
print('PASS: five archives, exact source/build metadata, licenses/notices and three native smoke results')
