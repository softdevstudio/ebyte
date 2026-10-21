# EByte releases

EByte v0.1.0 was the first implemented release under Apache License 2.0; v0.1.1 is a fix release, v0.2.0 adds repeating blocks, v0.3.0 adds derived sizes and offsets, v0.4.0 adds checksums, v0.5.0 adds structural diff v0.6.0 adds floats, bit fields, forward jumps and Windows file output, and v0.7.0 adds a WebAssembly build. Public release titles use EByte vX.Y.Z and archives have no version in their filenames. Linux amd64/arm64 and macOS amd64/arm64 use tar.gz; Windows amd64 uses zip. Each archive contains the CLI, manual, license/notice files, examples, release.json and GO-NOTICES from the exact building toolchain. Since 0.7.0, ebyte-wasm.zip holds the WebAssembly build with the same licences and notices, and SHA256SUMS and RELEASE-MANIFEST.json cover it too. No external Go modules are linked.

Release waits for successful CI (Linux and macOS, and Windows since 0.6.0) on the same current main-push SHA. Workflow actions are pinned to commit SHAs and Python to 3.13, and post-publication API reads are authenticated when a token is available while asset downloads stay anonymous. Independent review is a session gate recorded in CLAUDE.md. Separate packaging jobs test the library and native CLI on Linux, macOS and Windows. Native binaries are checked again after extraction; the other architectures are explicitly cross-built and unexecuted. release.json and RELEASE-MANIFEST.json record actual host verification rather than implying every archive ran natively. A clean checkout and embedded Go VCS revision bind binaries to the tested source. Build candidates using --local are forbidden in Actions and rejected by the publication validator.

The final gate validates all five archives and the WebAssembly build, project licenses, exact-toolchain notice hashes and embedded build information. SHA256SUMS covers the five archives, ebyte-wasm.zip and RELEASE-MANIFEST.json; it excludes itself. Before creating anything, publication scans the full release listing, drafts included, and stops if any release already uses the tag or the EByte vX.Y.Z title, because /releases/tags/{tag} cannot see an untagged draft left by a failed cleanup. Uploads go to a draft first, and successful uploads are verified by server digest or an authenticated download before publication. A failed draft is deleted, along with an owned matching tag if created. A failure after publication is reported for investigation and never silently deletes the published release. A cleanup failure records the remaining release/tag identifiers. No version bump is used merely to retry a gate before publication.

After publication, automation downloads every asset at the explicit tag and latest, compares hashes, checks the exact tag/source/name and executes a downloaded native CLI through schema validation, round trip, inspect, failure, limit and file-output behavior. Public verification is saved in one test/results/ebyte-{version}-release folder; latest/ remains the ordinary recorder output. Those checks do not constitute a signature or a general supply-chain guarantee.

File publication on Linux, macOS and, since 0.6.0, Windows requires same-directory hard links and a trusted parent directory; FAT and other filesystems without hard links fail closed. Native smoke checks publication and the collision refusal on all three. Windows ignores Unix permission bits, so a published file there is an ordinary writable file. There is no power-loss directory durability promise, no automatic format discovery and no graphical editor, and structural diff (since 0.5.0) matches list elements by position only. Context checks cannot interrupt arbitrary blocked library I/O; full-pipeline allocation evidence is not a peak-memory or throughput guarantee.

## Changes

Release notes include the section for their own version from this list.

### EByte v0.7.0

- New release asset ebyte-wasm.zip: the library compiled for GOOS=js GOARCH=wasm with the pinned toolchain, its matching wasm_exec.js, licences and release.json; docs/wasm.md describes the JavaScript calls.
- The WebAssembly build is smoke-tested with Node on every push, before and after packing, and again after publication.
- It powers the browser demo on ebyte.org, which runs entirely in the visitor's browser.
- Release notes no longer list structural diff as missing.

### EByte v0.6.0

- Types f32 and f64 (IEEE 754): finite values as shortest round-trip decimals, inf and -inf, and NaN as nan: with every bit, so all patterns round-trip.
- Type bits: consecutive bit fields packed into one to eight bytes, most significant bit first; members work like unsigned integers in lengths, counts and conditions.
- A bytes field can run up to a declared absolute offset with {"until_offset": ...}, keeping the bytes in between.
- --output FILE now works on Windows with the same never-replace rule; CI tests Windows on every push.
- An eleventh fuzz target checks floats and bit fields against math and encoding/binary.

### EByte v0.5.0

- New command diff and Plan.Diff: two inputs decoded through one schema and compared field by field, with each side's offset and length, integer values, derived values marked, and the first differing byte of changed bytes and strings.
- Arrays and repeats are compared by position; extra elements are added or removed.
- diff exits 0 when the inputs are the same and 5 when they differ; a broken input is reported with which side failed.
- A tenth fuzz target checks that accepted pairs differ exactly when their bytes do.

### EByte v0.4.0

- derive gains checksum: crc32, adler32, crc16_arc, crc16_modbus or crc16_xmodem over one sibling or a from/to range, in an integer of exactly the algorithm's width.
- Decoding hashes the range as it reads and reports checksum_mismatch at a stale checksum; encoding writes checksums after the sizes and offsets in their range.
- Schema library: png.schema.json reads every PNG chunk and derives each length and CRC-32; the one-file stored ZIP schema now derives both CRC-32 copies and every size and offset.
- A ninth fuzz target checks chunk lengths and CRCs against hash/crc32.

### EByte v0.3.0

- derive gains size_of (one sibling or a from/to range of siblings), offset_of a sibling, count_of a repeat and an optional add, all calculated on encode.
- A size_of that a later bytes or string field takes its length from is written from that field's value, like count_of for arrays.
- Measured sizes, offsets and counts are checked on decode: a stale value fails with constraint_mismatch instead of round-tripping silently.
- The library schemas now derive WAV, BMP and ZIP sizes, counts and directory offsets; per-entry ZIP local header offsets and CRC-32 are still edited by hand.
- An eighth fuzz target checks derived values against the encoded bytes independently of the codec.

### EByte v0.2.0

- New field type repeat: a block of fields repeated until the end of the input, or while the next bytes match a tag of 1 to 16 bytes.
- Elements can be added or removed in the editable JSON with no count to keep in step; encode refuses output that would decode differently.
- Schema library: riff-wave.schema.json reads any WAV as a list of chunks, including LIST metadata, and zip.schema.json reads archives with any number of entries.
- A seventh fuzz target checks that accepted repeat inputs re-encode to the same bytes.

### EByte v0.1.1

- --output refuses directory-style destinations such as out/, ., .. and / instead of creating out/out.
- Published files get 0666 minus the umask (usually 0644); the staging file stays private until complete.
- SIGTERM cancels like Ctrl-C on Linux and macOS, so staging files are cleaned up.
- inspect reports the original decode failure when a failed trace is too large to render, instead of a limit error.
- A schema or document reader failing mid-read is read_failed with exit 1, matching the documented exit codes.
- Decoder diagnostics no longer share memory with the decoder.
- Release automation detects a leftover draft before publishing, workflow actions are pinned to commit SHAs and release checks use authenticated API reads.
