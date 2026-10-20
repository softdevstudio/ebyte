"""Small release helpers shared by publication and public verification."""
import json
import os
from pathlib import Path
import tarfile
import urllib.error
import urllib.parse
import urllib.request
import zipfile

REPO = 'softdevstudio/ebyte'
ASSETS = {'ebyte-linux-amd64.tar.gz', 'ebyte-linux-arm64.tar.gz',
          'ebyte-darwin-amd64.tar.gz', 'ebyte-darwin-arm64.tar.gz',
          'ebyte-windows-amd64.zip', 'RELEASE-MANIFEST.json', 'SHA256SUMS'}


class SafeRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        if urllib.parse.urlsplit(newurl).scheme != 'https':
            raise ValueError('non-HTTPS redirect refused')
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)
        if redirected and urllib.parse.urlsplit(req.full_url).netloc != urllib.parse.urlsplit(newurl).netloc:
            redirected.remove_header('Authorization')
        return redirected


def request(url, method='GET', data=None, auth=False, content_type='application/vnd.github+json', accept='application/vnd.github+json'):
    headers = {'Accept': accept, 'User-Agent': 'EByte-release-check'}
    if data is not None:
        headers['Content-Type'] = content_type
    if auth:
        assert urllib.parse.urlsplit(url).netloc in ('api.github.com', 'uploads.github.com')
        headers['Authorization'] = 'Bearer ' + os.environ['GH_TOKEN']
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    with urllib.request.build_opener(SafeRedirect()).open(req, timeout=60) as response:
        return response.read()


def api(path, method='GET', value=None, auth=True, missing=False):
    try:
        raw = request('https://api.github.com/repos/' + REPO + path, method,
                      None if value is None else json.dumps(value).encode(), auth,
                      'application/json')
        return json.loads(raw) if raw else None
    except urllib.error.HTTPError as error:
        if missing and error.code == 404:
            return None
        raise


def unpack(archive, destination):
    def safe(name):
        return (name == 'ebyte' or name.startswith('ebyte/')) and '..' not in name.split('/') and '\\' not in name and ':' not in name
    if archive.suffix == '.zip':
        with zipfile.ZipFile(archive) as packed:
            for item in packed.infolist():
                assert safe(item.filename) and ((item.external_attr >> 16) & 0o170000) != 0o120000
            packed.extractall(destination)
    else:
        with tarfile.open(archive) as packed:
            for item in packed.getmembers():
                assert safe(item.name) and (item.isfile() or item.isdir())
            packed.extractall(destination, filter='data')


def releases_for(version, fetch=None, max_pages=10):
    """Every release, drafts included, that claims this version by tag or title.

    /releases/tags/{tag} ignores drafts, because a draft has no tag yet, so a
    draft left behind by a failed cleanup is only visible in the full listing.
    """
    fetch = fetch or api
    tag, title = 'v' + version, 'EByte v' + version
    found = []
    for page in range(1, max_pages + 1):
        batch = fetch('/releases?per_page=100&page=' + str(page))
        if not batch:
            return found
        found.extend(item for item in batch if item.get('tag_name') == tag or item.get('name') == title)
        if len(batch) < 100:
            return found
    raise RuntimeError('release listing longer than expected; refusing to guess')
