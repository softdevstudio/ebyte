#!/usr/bin/env python3
"""Native checks for a candidate or extracted release binary, including Windows."""
import hashlib
import json
from pathlib import Path
import platform
import subprocess
import sys
import tempfile

binary = str(Path(sys.argv[1]).resolve())
version = sys.argv[2]
root = Path(__file__).resolve().parents[1]
schema = str(root / 'testdata/packet.schema.json')
raw = bytes.fromhex((root / 'testdata/packet.hex').read_text())


def run(args, data=None):
    return subprocess.run([binary, *args], input=data, capture_output=True, timeout=20)


assert run(['--version']).stdout.decode().strip() == 'ebyte ' + version
assert run(['--help']).returncode == 0
valid = run(['validate-schema', '--schema', schema])
assert valid.returncode == 0 and json.loads(valid.stdout)['valid']
decoded = run(['decode', '--schema', schema, '--input=-'], raw)
assert decoded.returncode == 0 and not decoded.stderr
encoded = run(['encode', '--schema', schema, '--input=-'], decoded.stdout)
assert encoded.returncode == 0 and encoded.stdout == raw and not encoded.stderr
trace = run(['inspect', '--schema', schema, '--input=-'], raw)
assert trace.returncode == 0 and json.loads(trace.stdout)['state'] == 'complete'
text = run(['inspect', '--schema', schema, '--input=-', '--format=text'], raw)
assert text.returncode == 0 and b'state=complete' in text.stdout
failed = run(['decode', '--schema', schema, '--input=-'], raw[:-1])
assert failed.returncode == 2 and not failed.stdout
assert json.loads(failed.stderr)['code'] == 'truncated_input'
limited = run(['decode', '--schema', schema, '--input=-', '--max-input-bytes=1'], raw)
assert limited.returncode == 4 and not limited.stdout
with tempfile.TemporaryDirectory() as directory:
    output = Path(directory) / 'result.json'
    result = run(['decode', '--schema', schema, '--input=-', '--output', str(output)], raw)
    if platform.system() in ('Linux', 'Darwin'):
        assert result.returncode == 0 and not result.stdout and output.is_file()
        before = output.read_bytes()
        collision = run(['decode', '--schema', schema, '--input=-', '--output', str(output)], raw)
        assert collision.returncode == 1 and json.loads(collision.stderr)['code'] == 'destination_exists'
        assert output.read_bytes() == before
    else:
        assert result.returncode == 1 and json.loads(result.stderr)['code'] == 'write_failed'
        assert not output.exists()
print(json.dumps({'native_smoke': 'passed', 'version': version,
                  'platform': platform.system(), 'architecture': platform.machine(),
                  'round_trip_sha256': hashlib.sha256(raw).hexdigest(),
                  'file_publication': 'enabled' if platform.system() in ('Linux', 'Darwin') else 'unsupported; stdout only'}))
