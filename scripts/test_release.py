import hashlib
import io
import json
import os
from pathlib import Path
import runpy
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import urllib.request
import zipfile

import release_common
import verify_release


class ReleaseSafetyTests(unittest.TestCase):
    def test_archive_paths_and_links_are_refused(self):
        for kind in ('zip-traversal', 'zip-backslash', 'tar-traversal', 'tar-symlink'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temporary:
                base = Path(temporary)
                archive = base / ('bad.zip' if kind.startswith('zip') else 'bad.tar.gz')
                if kind.startswith('zip'):
                    with zipfile.ZipFile(archive, 'w') as packed:
                        packed.writestr('ebyte/../../escape' if kind == 'zip-traversal' else 'ebyte\\escape', b'bad')
                else:
                    with tarfile.open(archive, 'w:gz') as packed:
                        info = tarfile.TarInfo('ebyte/../../escape' if kind == 'tar-traversal' else 'ebyte/link')
                        if kind == 'tar-symlink':
                            info.type = tarfile.SYMTYPE
                            info.linkname = '/outside'
                            packed.addfile(info)
                        else:
                            info.size = 3
                            packed.addfile(info, io.BytesIO(b'bad'))
                with self.assertRaises(AssertionError):
                    release_common.unpack(archive, base / 'out')
                self.assertFalse((base / 'escape').exists())

    def test_redirects_strip_credentials_and_refuse_http(self):
        req = urllib.request.Request('https://api.github.com/a', headers={'Authorization': 'Bearer secret'})
        redirected = release_common.SafeRedirect().redirect_request(req, None, 302, '', {}, 'https://objects.githubusercontent.com/file')
        self.assertIsNone(redirected.get_header('Authorization'))
        with self.assertRaises(ValueError):
            release_common.SafeRedirect().redirect_request(req, None, 302, '', {}, 'http://example.com/file')

    def test_failed_upload_cleans_only_owned_draft_and_tag(self):
        outcome, calls = self.publish_case('upload')
        self.assertTrue(outcome['failed_draft_deleted'])
        self.assertIn(('DELETE', '/releases/1'), calls)
        self.assertIn(('DELETE', '/git/refs/tags/v0.1.0'), calls)

    def test_public_verification_failure_keeps_published_release(self):
        outcome, calls = self.publish_case('verification')
        self.assertIn('published_release_needs_verification', outcome)
        self.assertFalse(any(method == 'DELETE' for method, _ in calls))

    def test_failed_cleanup_is_reported(self):
        outcome, _ = self.publish_case('cleanup')
        self.assertEqual(outcome['release_id_needs_investigation'], 1)
        self.assertEqual(outcome['tag_needs_investigation'], 'v0.1.0')
        self.assertIn('cleanup_error_type', outcome)

    def test_leftover_draft_blocks_a_second_publication(self):
        draft = {'id': 7, 'draft': True, 'tag_name': 'v0.1.0', 'name': 'EByte v0.1.0'}
        outcome, calls = self.publish_case('none', [draft])
        self.assertNotIn(('POST', '/releases'), calls)
        self.assertEqual(outcome['verification'], 'failed')
        self.assertFalse(any(method == 'DELETE' for method, _ in calls))
        self.assertNotIn('failed_draft_deleted', outcome)

    def test_releases_for_finds_untagged_drafts_and_pages(self):
        pages = {1: [{'tag_name': 'v0.0.9', 'name': 'EByte v0.0.9'}] * 100,
                 2: [{'tag_name': 'untagged-1a2b', 'name': 'EByte v0.1.1', 'draft': True}]}
        seen = []

        def fetch(path):
            seen.append(path)
            return pages.get(int(path.rsplit('=', 1)[1]), [])
        found = release_common.releases_for('0.1.1', fetch)
        self.assertEqual(len(found), 1)
        self.assertEqual(len(seen), 2)
        self.assertEqual(release_common.releases_for('0.2.0', fetch), [])

    def wasm_case(self, dist, tamper=None):
        root = Path(__file__).resolve().parents[1]
        revision = 'b' * 40
        wasm, glue, notice = b'\0asm fake', b'// glue', b'go licence'
        meta = {'version': '0.7.0', 'source_sha': revision, 'source_dirty': False, 'go_version': 'go1.27.3',
                'goos': 'js', 'goarch': 'wasm', 'wasm_sha256': hashlib.sha256(wasm).hexdigest(),
                'wasm_exec_sha256': hashlib.sha256(glue).hexdigest(), 'verification': 'Node smoke passed',
                'go_notices': {'LICENSE': hashlib.sha256(notice).hexdigest()}}
        files = {'ebyte/ebyte.wasm': wasm, 'ebyte/wasm_exec.js': glue, 'ebyte/GO-NOTICES/LICENSE': notice,
                 'ebyte/release.json': json.dumps(meta).encode()}
        for name in ('LICENSE', 'NOTICE', 'THIRD_PARTY_LICENSES.md'):
            files['ebyte/' + name] = (root / name).read_bytes()
        if tamper == 'wasm':
            files['ebyte/ebyte.wasm'] = b'\0asm other'
        with zipfile.ZipFile(dist / 'ebyte-wasm.zip', 'w') as packed:
            for name, data in files.items():
                packed.writestr(name, data)
        build = {'go_version': 'go1.27.3', 'GOOS': 'js', 'GOARCH': 'wasm', 'vcs.revision': revision, 'vcs.modified': 'false'}
        if tamper == 'modified':
            build['vcs.modified'] = 'true'
        entry = {'archive': 'ebyte-wasm.zip', 'sha256': hashlib.sha256((dist / 'ebyte-wasm.zip').read_bytes()).hexdigest(),
                 'metadata': meta, 'wasm_smoke': {'wasm_smoke': 'passed', 'version': '0.7.0', 'build': build}}
        if tamper == 'archive':
            entry['sha256'] = '0' * 64
        (dist / 'manifest-wasm.json').write_text(json.dumps(entry))
        return release_common.check_wasm(dist, '0.7.0', revision, root, b'// other glue' if tamper == 'glue' else glue)

    def test_wasm_check_accepts_a_consistent_build_and_refuses_tampering(self):
        with tempfile.TemporaryDirectory() as temporary:
            self.assertEqual(self.wasm_case(Path(temporary))['archive'], 'ebyte-wasm.zip')
        for tamper in ('wasm', 'glue', 'modified', 'archive'):
            with self.subTest(tamper=tamper), tempfile.TemporaryDirectory() as temporary:
                with self.assertRaises(AssertionError):
                    self.wasm_case(Path(temporary), tamper)

    def test_wasm_smoke_skip_is_refused_in_actions(self):
        source = (Path(__file__).resolve().parent / 'verify_release.py').read_text()
        self.assertIn("os.environ.get('GITHUB_ACTIONS') != 'true', 'node is required", source)

    def publish_case(self, failure, listing=()):
        listing = list(listing)
        script = Path(__file__).resolve().parent / 'publish_release.py'
        sha = 'a' * 40
        calls = []
        state = {'created': False, 'draft': True, 'assets': []}

        def api(path, method='GET', value=None, **kwargs):
            calls.append((method, path))
            if path == '/git/ref/heads/main':
                return {'object': {'sha': sha}}
            if path == '/releases/tags/v0.1.0':
                return None
            if path.startswith('/releases?') and method == 'GET':
                return listing if path.endswith('page=1') else []
            if path == '/git/ref/tags/v0.1.0':
                return {'object': {'type': 'commit', 'sha': sha}} if state['created'] else None
            if path == '/releases' and method == 'POST':
                state['created'] = True
                return {'id': 1, 'upload_url': 'https://uploads.github.com/upload{?name}'}
            if path == '/releases/1':
                if method == 'DELETE' and failure == 'cleanup':
                    raise RuntimeError('cleanup failed')
                if method == 'PATCH':
                    state['draft'] = False
                return {'id': 1, 'draft': state['draft'], 'assets': state['assets'], 'html_url': 'https://github.com/softdevstudio/ebyte/releases/tag/v0.1.0'}
            if method == 'DELETE' and path == '/git/refs/tags/v0.1.0':
                return None
            raise AssertionError((method, path))

        def upload(url, method='GET', data=None, *args, **kwargs):
            if failure in ('upload', 'cleanup'):
                raise RuntimeError('upload failed')
            name = url.split('name=')[1]
            item = {'name': name, 'size': len(data), 'digest': 'sha256:' + hashlib.sha256(data).hexdigest()}
            state['assets'].append(item)
            return json.dumps(item).encode()

        with tempfile.TemporaryDirectory() as temporary:
            previous = Path.cwd()
            os.chdir(temporary)
            try:
                dist = Path('dist')
                dist.mkdir()
                for name in release_common.ASSETS | {'release-notes.md'}:
                    (dist / name).write_bytes(b'candidate')
                with patch.object(release_common, 'api', api), patch.object(release_common, 'request', upload), \
                     patch.object(verify_release, 'verify', side_effect=RuntimeError('verification failed')), \
                     patch('sys.argv', [str(script), '0.1.0', sha]), \
                     patch('sys.stderr', new_callable=io.StringIO):
                    with self.assertRaises(RuntimeError):
                        runpy.run_path(str(script), run_name='__main__')
                outcome = json.loads((dist / 'public-verification.json').read_text())
            finally:
                os.chdir(previous)
        return outcome, calls
