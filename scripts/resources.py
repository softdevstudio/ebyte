#!/usr/bin/env python3
"""Save isolated single-sample resource observations, not benchmark claims."""
import json
from pathlib import Path
import subprocess
import sys

binary = str(Path(sys.argv[1]).resolve())
cases = ('bytes-0', 'bytes-64KiB', 'bytes-1MiB', 'bytes-8MiB', 'bytes-16MiB',
         'ascii-escape-1MiB', 'empty-elements', 'node-limit', 'trace-limit', 'huge-field-limit')
observations = []
for case in cases:
    result = subprocess.run([binary, case], capture_output=True, check=True, timeout=60)
    assert not result.stderr
    observed = json.loads(result.stdout)
    assert observed['case'] == case and observed['samples'] == 1
    if case.endswith('-limit'):
        assert observed['diagnostic_code'] == 'limit_exceeded'
        assert not observed['round_trip_identical'] and observed['output_bytes'] == 0
    else:
        assert observed['round_trip_identical']
        assert observed['input_sha256'] == observed['output_sha256']
        assert observed['input_bytes'] == observed['output_bytes']
    observations.append(observed)
Path(sys.argv[2]).write_text(json.dumps({
    'measurement': 'isolated single-sample library pipeline observations',
    'includes': 'decode, compact document JSON, strict reload, encode, equality check and compact trace JSON',
    'excludes': 'fixture/schema construction, input/output hashes, process startup and OS RSS measurement',
    'limitations': 'cumulative Go allocations and heap snapshots are not peak RSS, hard memory limits or throughput guarantees; heap snapshots include live fixtures and compiled plans',
    'observations': observations,
}, indent=2) + '\n', encoding='utf-8')
print('PASS: 10 isolated resource cases, exact 16 MiB payload round trip, escape expansion and bounded rejection')
