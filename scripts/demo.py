#!/usr/bin/env python3
"""Run the built CLI against independently specified sample bytes and save evidence."""
import hashlib
import json
from pathlib import Path
import platform
import subprocess
import sys
import tempfile


def main():
    binary = str(Path(sys.argv[1]).resolve())
    evidence = Path(sys.argv[2])
    root = Path(__file__).resolve().parents[1]
    schema = root / 'testdata/packet.schema.json'
    original = bytes.fromhex((root / 'testdata/packet.hex').read_text())

    def run(*args):
        result = subprocess.run([binary, *map(str, args)], capture_output=True, timeout=20)
        assert result.returncode == 0, (args, result.returncode, result.stderr)
        assert not result.stderr
        return result.stdout

    with tempfile.TemporaryDirectory() as temporary:
        work = Path(temporary)
        opaque = work / 'opaque.bin'
        document = work / 'editable.json'
        restored = work / 'restored.bin'
        edited = work / 'edited.json'
        changed = work / 'changed.bin'
        opaque.write_bytes(original)
        validated = json.loads(run('validate-schema', '--schema', schema))
        assert not run('decode', '--schema', schema, '--input', opaque, '--output', document)
        decoded = json.loads(document.read_bytes())
        before = json.loads(run('inspect', '--schema', schema, '--input', opaque))
        assert not run('encode', '--schema', schema, '--input', document, '--output', restored)
        assert restored.read_bytes() == original
        decoded['values']['records'].append({'id': '3', 'temperature': '-1'})
        edited.write_text(json.dumps(decoded), encoding='utf-8')
        assert not run('encode', '--schema', schema, '--input', edited, '--output', changed)
        changed_bytes = changed.read_bytes()
        after = json.loads(run('inspect', '--schema', schema, '--input', changed))
        # This expected edit was specified before the codec implementation.
        cases = json.loads((root / 'testdata/cases.json').read_text())
        expected = next(case['hex'] for case in cases if case['name'] == 'derived count edit')
        assert changed_bytes.hex() == expected
        entry = lambda trace, path: next(item for item in trace['entries'] if item['path'] == path)
        assert entry(before, '/record_count')['value'] == '2'
        assert entry(after, '/record_count')['value'] == '3'
        assert entry(before, '/note')['offset'] == 19
        assert entry(after, '/note')['offset'] == 23
        result = {
            'tool_version': run('--version').decode().strip(),
            'platform': platform.system(),
            'architecture': platform.machine(),
            'schema_sha256': validated['schema_sha256'],
            'original_bytes': len(original),
            'edited_bytes': len(changed_bytes),
            'original_sha256': hashlib.sha256(original).hexdigest(),
            'restored_sha256': hashlib.sha256(restored.read_bytes()).hexdigest(),
            'edited_sha256': hashlib.sha256(changed_bytes).hexdigest(),
            'unchanged_round_trip_identical': True,
            'original_hex': original.hex(),
            'edited_hex': changed_bytes.hex(),
            'original_document': json.loads(document.read_bytes()),
            'edited_document': decoded,
            'original_trace': before,
            'edited_trace': after,
        }
    evidence.write_text(json.dumps(result, indent=2) + '\n', encoding='utf-8')
    print('PASS: built-CLI demo, 21-byte exact round trip; added record gives 25 bytes, count 3, note offset 23')


if __name__ == '__main__':
    main()
