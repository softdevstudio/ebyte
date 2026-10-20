# Recorded results

latest/ contains the newest scripts/record-results.sh run and is replaced each
run. Git history preserves older runs; no timestamped result folders are used.
The log records time, toolchain, source fingerprint, checks and real outcomes.
demo.json saves the compiled CLI round-trip/edit demonstration, including
hashes, actual documents and before/after traces. It is regenerated each run.
resources.json records ten fresh-process, single-sample pipeline observations.
Allocation totals and heap snapshots are not peak RSS or hard memory guarantees.
ebyte-0.1.0-release/ contains tag/latest asset and downloaded-binary verification,
the public manifest/checksums and exact-source CI/native packaging results.
Each future release uses one ebyte-{version}-release folder, separate from latest/.
