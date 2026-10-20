#!/usr/bin/env python3
"""Publish only a new stable version on the current, successfully tested main SHA."""
import json
import os
from pathlib import Path
import re
import urllib.error
import urllib.request

repo = os.environ['GITHUB_REPOSITORY']
assert repo == 'softdevstudio/ebyte'
sha = os.environ['EBYTE_RELEASE_SHA']
assert re.fullmatch('[0-9a-f]{40}', sha)
version = re.search(r'const Version = "([^"]+)"', Path('version.go').read_text()).group(1)


def get(path):
    req = urllib.request.Request('https://api.github.com/repos/' + repo + path,
                                 headers={'Authorization': 'Bearer ' + os.environ['GH_TOKEN'], 'Accept': 'application/vnd.github+json'})
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return None
        raise


def releases_for(version):
    # Same rule as release_common.releases_for: drafts are invisible to
    # /releases/tags/{tag}, so scan the listing for the tag or the title.
    tag, title = 'v' + version, 'EByte v' + version
    found = []
    for page in range(1, 11):
        batch = get('/releases?per_page=100&page=' + str(page)) or []
        found.extend(item for item in batch if item.get('tag_name') == tag or item.get('name') == title)
        if len(batch) < 100:
            return found
    raise RuntimeError('release listing longer than expected; refusing to guess')


publish = False
if re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)', version):
    existing = get('/releases/tags/v' + version)
    if existing:
        assert not existing['draft'] and not existing['prerelease'], 'an unpublished release needs investigation'
    else:
        assert get('/git/ref/tags/v' + version) is None, 'an orphan tag needs investigation'
        assert not releases_for(version), 'a leftover draft for this version needs investigation'
        publish = get('/git/ref/heads/main')['object']['sha'] == sha
with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
    output.write('publish=' + str(publish).lower() + '\nversion=' + version + '\n')
print(json.dumps({'version': version, 'publish': publish, 'source_sha': sha}))
