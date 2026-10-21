#!/usr/bin/env python3
"""Download every tag/latest asset, check hashes and run a downloaded native CLI."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from release_common import ASSETS, REPO, api, request, unpack


def verify(version, revision, output):
    # Authenticated API reads avoid anonymous rate limits on shared runners;
    # asset downloads stay anonymous so they match what users actually get.
    authenticated = bool(os.environ.get('GH_TOKEN'))
    root = Path(__file__).resolve().parents[1]
    expected_name = 'EByte v' + version
    tag = 'v' + version
    ref = api('/git/ref/tags/' + tag, auth=authenticated)
    assert ref['object']['type'] == 'commit' and ref['object']['sha'] == revision
    evidence = {'version': version, 'source_sha': revision, 'endpoints': {}, 'downloaded_native_smoke': None}
    with tempfile.TemporaryDirectory() as temporary:
        work = Path(temporary)
        downloads = {}
        checksums = None
        manifest = None
        for endpoint in ('tags/' + tag, 'latest'):
            release = api('/releases/' + endpoint, auth=authenticated)
            assert release['name'] == expected_name and release['tag_name'] == tag
            assert not release['draft'] and not release['prerelease']
            assets = {item['name']: item for item in release['assets']}
            assert set(assets) == ASSETS and len(release['assets']) == len(ASSETS)
            base = 'https://github.com/' + REPO + '/releases/'
            base += 'latest/download/' if endpoint == 'latest' else 'download/' + tag + '/'
            checksum_bytes = request(base + 'SHA256SUMS')
            if checksums is not None:
                assert checksum_bytes == checksums
            checksums = checksum_bytes
            expected = {}
            for line in checksum_bytes.decode('ascii').splitlines():
                digest, name = line.split('  ')
                assert len(digest) == 64 and all(c in '0123456789abcdef' for c in digest)
                assert name in ASSETS - {'SHA256SUMS'} and name not in expected
                expected[name] = digest
            assert set(expected) == ASSETS - {'SHA256SUMS'}
            observations = []
            for name in sorted(ASSETS):
                raw = checksum_bytes if name == 'SHA256SUMS' else request(base + name)
                digest = hashlib.sha256(raw).hexdigest()
                assert len(raw) == assets[name]['size']
                if name != 'SHA256SUMS':
                    assert digest == expected[name]
                if assets[name].get('digest'):
                    assert assets[name]['digest'] == 'sha256:' + digest
                if endpoint == 'latest':
                    path = work / name
                    path.write_bytes(raw)
                    downloads[name] = path
                if name == 'RELEASE-MANIFEST.json':
                    parsed = json.loads(raw)
                    assert parsed['version'] == version and parsed['source_sha'] == revision
                    if manifest is not None:
                        assert parsed == manifest
                    manifest = parsed
                observations.append({'name': name, 'bytes': len(raw), 'sha256': digest})
            evidence['endpoints'][endpoint] = {'release_id': release['id'], 'assets': observations}
        hostos = subprocess.check_output(['go', 'env', 'GOHOSTOS']).decode().strip()
        hostarch = subprocess.check_output(['go', 'env', 'GOHOSTARCH']).decode().strip()
        native = next(item for item in manifest['archives'] if item['metadata']['goos'] == hostos and item['metadata']['goarch'] == hostarch)
        archive = downloads[native['archive']]
        assert hashlib.sha256(archive.read_bytes()).hexdigest() == native['sha256']
        extracted = work / 'extracted'
        extracted.mkdir()
        unpack(archive, extracted)
        binary = extracted / 'ebyte' / ('ebyte.exe' if hostos == 'windows' else 'ebyte')
        assert hashlib.sha256(binary.read_bytes()).hexdigest() == native['metadata']['binary_sha256']
        build = subprocess.check_output(['go', 'version', '-m', str(binary)]).decode()
        assert 'vcs.revision=' + revision in build and 'vcs.modified=false' in build
        smoke = subprocess.check_output([sys.executable, str(root / 'scripts/release_smoke.py'), str(binary), version])
        evidence['downloaded_native_smoke'] = json.loads(smoke)
        wasm = manifest['wasm']
        wasm_zip = downloads['ebyte-wasm.zip']
        assert hashlib.sha256(wasm_zip.read_bytes()).hexdigest() == wasm['sha256']
        wasm_dir = work / 'wasm'
        wasm_dir.mkdir()
        unpack(wasm_zip, wasm_dir)
        assert hashlib.sha256((wasm_dir / 'ebyte' / 'ebyte.wasm').read_bytes()).hexdigest() == wasm['metadata']['wasm_sha256']
        if shutil.which('node'):
            node = subprocess.check_output(['node', str(root / 'scripts/wasm_smoke.mjs'), str(wasm_dir / 'ebyte'), version])
            evidence['downloaded_wasm_smoke'] = json.loads(node)
        else:
            evidence['downloaded_wasm_smoke'] = None
    output.write_text(json.dumps(evidence, indent=2) + '\n', encoding='utf-8')
    print('PASS: all ' + str(len(ASSETS)) + ' assets checked at tag and latest; downloaded native binary and WebAssembly smoke passed')
    return evidence


if __name__ == '__main__':
    verify(sys.argv[1], sys.argv[2], Path(sys.argv[3]))
