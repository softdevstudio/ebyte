#!/usr/bin/env python3
"""Exercise operational commands through the compiled binary."""
from pathlib import Path
import json
import re
import subprocess
import sys
import tempfile

binary = str(Path(sys.argv[1]).resolve())
root = Path(__file__).resolve().parents[1]
schema = str(root / 'testdata/packet.schema.json')
original = bytes.fromhex((root / 'testdata/packet.hex').read_text())


def run(args, data=None):
    return subprocess.run([binary, *args], input=data, capture_output=True, timeout=15)


version = re.search(r'const Version = "([^"]+)"', (root / 'version.go').read_text()).group(1)
assert run(['--version']).stdout == ('ebyte ' + version + '\n').encode()
assert run(['--help']).returncode == 0
result = run(['decode', '--schema', schema, '--input=-'], original)
assert result.returncode == 0 and not result.stderr
assert 'record_count' not in json.loads(result.stdout)['values']
encoded = run(['encode', '--schema', schema, '--input=-'], result.stdout)
assert encoded.returncode == 0 and encoded.stdout == original and not encoded.stderr
for command in ('decode', 'inspect'):
    failed = run([command, '--schema', schema, '--input=-'], original[:-1])
    assert failed.returncode == 2 and json.loads(failed.stderr)['code'] == 'truncated_input'
    if command == 'decode':
        assert not failed.stdout
    else:
        assert json.loads(failed.stdout)['state'] == 'failed'
limited = run(['decode', '--schema', schema, '--input=-', '--max-input-bytes=1'], original)
assert limited.returncode == 4 and not limited.stdout
assert json.loads(limited.stderr)['code'] == 'limit_exceeded'
with tempfile.TemporaryDirectory() as directory:
    canary = Path(directory) / 'input'
    canary.write_bytes(b'unchanged')
    collision = run(['decode', '--schema', schema, '--input=-', '--output=' + str(canary)], original)
    assert collision.returncode == 1 and not collision.stdout
    assert json.loads(collision.stderr)['code'] == 'destination_exists'
    for command in ('validate-schema', 'decode', 'encode', 'inspect'):
        invalid = run([command, '--unknown=SECRET', '--output=' + str(canary)])
        assert invalid.returncode == 2 and not invalid.stdout and b'SECRET' not in invalid.stderr
        assert canary.read_bytes() == b'unchanged' and list(Path(directory).iterdir()) == [canary]
print('PASS: compiled CLI stdin round trip, failed traces, limit exits, collision protection and argument privacy')

# Exercise process-level behavior that custom io.Writer tests cannot reproduce.
if sys.platform in ('linux', 'darwin'):
    import array
    import errno
    import fcntl
    import os
    import signal
    import termios
    import time

    for args, data in ((['--help'], None), (['decode', '--schema', schema, '--input=-'], original)):
        read_fd, write_fd = os.pipe()
        os.close(read_fd)
        try:
            process = subprocess.Popen([binary, *args], stdin=subprocess.PIPE, stdout=write_fd, stderr=subprocess.PIPE)
        finally:
            os.close(write_fd)
        _, diagnostic = process.communicate(data, timeout=10)
        assert process.returncode == 1, (process.returncode, diagnostic)
        assert json.loads(diagnostic)['code'] == 'write_failed'

    with tempfile.TemporaryDirectory() as directory:
        fifo = Path(directory) / 'schema.fifo'
        os.mkfifo(fifo)
        process = subprocess.Popen([binary, 'decode', '--schema', str(fifo), '--input=-'],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            deadline = time.monotonic() + 10
            # The open handshake proves signal handlers were installed before signalling.
            while True:
                try:
                    schema_fd = os.open(fifo, os.O_WRONLY | os.O_NONBLOCK)
                    break
                except OSError as error:
                    if error.errno != errno.ENXIO or time.monotonic() >= deadline:
                        raise
                    time.sleep(0.01)
            try:
                os.write(schema_fd, (root / 'testdata/packet.schema.json').read_bytes())
            finally:
                os.close(schema_fd)
            process.stdin.write(original[:1])
            process.stdin.flush()
            while True:
                available = array.array('i', [0])
                fcntl.ioctl(process.stdin.fileno(), termios.FIONREAD, available, True)
                if available[0] == 0:
                    break
                assert process.poll() is None and time.monotonic() < deadline
                time.sleep(0.01)
            time.sleep(0.05)
            process.send_signal(signal.SIGINT)
            # Keep stdin open: EOF would conceal a cancellation hang.
            process.wait(timeout=5)
            assert process.returncode == 3, process.returncode
            assert not process.stdout.read()
            assert json.loads(process.stderr.read())['code'] == 'cancelled'
        finally:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=5)
            for stream in (process.stdin, process.stdout, process.stderr):
                stream.close()
    print('PASS: broken stdout pipes exit 1 with diagnostics; SIGINT exits 3 while stdin remains open')
