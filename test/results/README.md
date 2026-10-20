# Recorded results

latest/ contains the newest scripts/record-results.sh run and is replaced each
run. Git history preserves older runs; no timestamped result folders are used.
The log records time, toolchain, source fingerprint, checks and real outcomes.
demo.json saves the compiled CLI round-trip/edit demonstration, including
hashes, actual documents and before/after traces. It is regenerated each run.
resources.json records ten fresh-process, single-sample pipeline observations.
Allocation totals and heap snapshots are not peak RSS or hard memory guarantees.
There is no release yet. A release's public verification will live in exactly
one folder named ebyte-{version}-release, separate from latest/.
