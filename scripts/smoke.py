#!/usr/bin/env python3
"""Exercise implemented skeleton behavior; do not imply codec functionality."""
from pathlib import Path
import subprocess
import sys
import tempfile
binary = str(Path(sys.argv[1]).resolve())
result = subprocess.run([binary, '--version'], capture_output=True, text=True, check=True, timeout=10)
assert result.stdout == 'ebyte 0.0.0-dev\n'
assert subprocess.run([binary, '--help'], capture_output=True, text=True, check=True, timeout=10).stdout
with tempfile.TemporaryDirectory() as directory:
    canary = Path(directory) / 'input'
    canary.write_bytes(b'unchanged')
    for command in ('validate-schema', 'decode', 'encode', 'inspect'):
        result = subprocess.run([binary, command, '--input=SECRET', '--output=' + str(canary)], cwd=directory, capture_output=True, text=True, timeout=10)
        assert result.returncode == 2 and not result.stdout and 'SECRET' not in result.stderr
        assert canary.read_bytes() == b'unchanged' and list(Path(directory).iterdir()) == [canary]
print('PASS: built CLI version/help, unavailable operations fail without touching files or echoing inputs')
