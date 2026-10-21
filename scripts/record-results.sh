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
go test -run='^$' -fuzz=FuzzLoadAndEncode -fuzztime=5s -parallel=2 -timeout=60s .
go test -run='^$' -fuzz=FuzzLosslessPacket -fuzztime=5s -parallel=2 -timeout=60s .
go test -run='^$' -fuzz=FuzzLosslessShapes -fuzztime=5s -parallel=2 -timeout=60s .
go test -run='^$' -fuzz=FuzzCompile -fuzztime=5s -parallel=2 -timeout=60s .
go test -run='^$' -fuzz=FuzzRepeat -fuzztime=5s -parallel=2 -timeout=60s .
go test -run='^$' -fuzz=FuzzDerivedSizes -fuzztime=5s -parallel=2 -timeout=60s .
go test -run='^$' -fuzz=FuzzChecksums -fuzztime=5s -parallel=2 -timeout=60s .
go test -run='^$' -fuzz=FuzzDiff -fuzztime=5s -parallel=2 -timeout=60s .
go test -run='^$' -fuzz=FuzzCLIArguments -fuzztime=5s -parallel=2 -timeout=60s ./cmd/ebyte
build_dir="$(mktemp -d)"
trap 'rm -rf "$build_dir"' EXIT
go build -o "$build_dir/ebyte" ./cmd/ebyte
python3 scripts/smoke.py "$build_dir/ebyte"
python3 scripts/demo.py "$build_dir/ebyte" test/results/latest/demo.json
go build -o "$build_dir/resourcecheck" ./internal/resourcecheck
python3 scripts/resources.py "$build_dir/resourcecheck" test/results/latest/resources.json
printf 'PASS: formatting, vet, race, independent fixture/decoder oracles, ten bounded fuzz targets, build and operational CLI smoke and saved round-trip demo and resource observations\n'
