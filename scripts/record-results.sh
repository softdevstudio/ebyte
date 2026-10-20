#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# The checkout is trusted against concurrent changes; reject linked ancestors.
python3 scripts/results.py
exec > >(tee test/results/latest/checks.log) 2>&1
printf 'UTC: %s\n' "$(date -u +%FT%TZ)"
go version
go env GOOS GOARCH CGO_ENABLED
python3 - <<'PY'
from pathlib import Path
import hashlib, subprocess
h = hashlib.sha256()
# Include untracked first-session source without ignored/generated files.
paths = subprocess.check_output(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard']).split(b'\0')
for name in sorted(set(paths)):
    if not name or name.startswith(b'test/results/'):
        continue
    p = Path(name.decode())
    if p.is_file():
        h.update(name + b'\0' + p.read_bytes() + b'\0')
print('Source SHA256:', h.hexdigest())
PY
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  printf 'Unformatted files:\n%s\n' "$unformatted"
  exit 1
fi
python3 -m unittest discover -s scripts -p "test_*.py" -v
go vet ./...
go test -race -count=1 -v -timeout=60s ./...
go test -run='^$' -fuzz=FuzzDecodePacket -fuzztime=5s -parallel=2 -timeout=60s .
build_dir="$(mktemp -d)"
trap 'rm -rf "$build_dir"' EXIT
go build -o "$build_dir/ebyte" ./cmd/ebyte
python3 scripts/smoke.py "$build_dir/ebyte"
printf 'PASS: formatting, vet, race, independent fixture/decoder oracles, bounded decoder fuzz, build and executable skeleton smoke\n'
