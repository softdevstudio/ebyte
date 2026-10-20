#!/usr/bin/env python3
"""Publish only verified archives, clean failed drafts and verify public downloads."""
import hashlib
import json
from pathlib import Path
import sys
import urllib.parse

from release_common import ASSETS, api, request
from verify_release import verify

version, revision = sys.argv[1:3]
dist = Path('dist')
tag = 'v' + version
assert api('/git/ref/heads/main')['object']['sha'] == revision, 'main advanced; do not publish stale code'
assert api('/releases/tags/' + tag, missing=True) is None
assert api('/git/ref/tags/' + tag, missing=True) is None
release = None
try:
    release = api('/releases', 'POST', {'tag_name': tag, 'target_commitish': revision,
                  'name': 'EByte v' + version, 'body': (dist / 'release-notes.md').read_text(),
                  'draft': True, 'prerelease': False})
    upload = release['upload_url'].split('{')[0]
    for name in sorted(ASSETS):
        raw = (dist / name).read_bytes()
        uploaded = json.loads(request(upload + '?name=' + urllib.parse.quote(name), 'POST', raw, True, 'application/octet-stream'))
        assert uploaded['name'] == name and uploaded['size'] == len(raw)
        if uploaded.get('digest'):
            assert uploaded['digest'] == 'sha256:' + hashlib.sha256(raw).hexdigest()
        else:
            received = request('https://api.github.com/repos/softdevstudio/ebyte/releases/assets/' + str(uploaded['id']), auth=True, accept='application/octet-stream')
            assert hashlib.sha256(received).digest() == hashlib.sha256(raw).digest()
    ready = api('/releases/' + str(release['id']))
    assert {item['name'] for item in ready['assets']} == ASSETS and len(ready['assets']) == len(ASSETS)
    assert api('/git/ref/heads/main')['object']['sha'] == revision, 'main advanced during upload'
    api('/releases/' + str(release['id']), 'PATCH', {'draft': False, 'make_latest': 'true'})
    verify(version, revision, dist / 'public-verification.json')
except Exception as error:
    outcome = {'version': version, 'source_sha': revision, 'verification': 'failed', 'error_type': type(error).__name__}
    if release:
        try:
            state = api('/releases/' + str(release['id']), missing=True)
            if state and state['draft']:
                api('/releases/' + str(release['id']), 'DELETE')
                ref = api('/git/ref/tags/' + tag, missing=True)
                if ref and ref['object']['type'] == 'commit' and ref['object']['sha'] == revision:
                    api('/git/refs/tags/' + tag, 'DELETE')
                outcome['failed_draft_deleted'] = True
            elif state:
                outcome['published_release_needs_verification'] = state['html_url']
        except Exception as cleanup_error:
            outcome['cleanup_error_type'] = type(cleanup_error).__name__
            outcome['release_id_needs_investigation'] = release['id']
            outcome['tag_needs_investigation'] = tag
    (dist / 'public-verification.json').write_text(json.dumps(outcome, indent=2) + '\n', encoding='utf-8')
    print(json.dumps(outcome), file=sys.stderr)
    raise
