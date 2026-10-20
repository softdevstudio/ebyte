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


publish = False
if re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)', version):
    existing = get('/releases/tags/v' + version)
    if existing:
        assert not existing['draft'] and not existing['prerelease'], 'an unpublished release needs investigation'
    else:
        assert get('/git/ref/tags/v' + version) is None, 'an orphan tag needs investigation'
        publish = get('/git/ref/heads/main')['object']['sha'] == sha
with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
    output.write('publish=' + str(publish).lower() + '\nversion=' + version + '\n')
print(json.dumps({'version': version, 'publish': publish, 'source_sha': sha}))
