# EByte command reference

The current release version is 0.6.0, licensed under Apache License 2.0. Build it from the repository with Go pinned by go.mod: go build -o /tmp/ebyte ./cmd/ebyte. Commands share the library interpreter and its strict schema/document contracts. They never guess a binary format. Pass its schema explicitly.

## Commands

```sh
ebyte validate-schema --schema format.json
ebyte decode --schema format.json --input opaque.bin --output editable.json
ebyte encode --schema format.json --input edited.json --output rebuilt.bin
ebyte inspect --schema format.json --input opaque.bin --format json
ebyte inspect --schema format.json --input opaque.bin --format text
ebyte diff --schema format.json --before old.bin --after new.bin --format text
```

Every command requires --schema FILE; schema stdin is deliberately unsupported so the schema is validated before input is opened. Decode, encode and inspect require --input FILE or --input - for stdin. The optional --output FILE writes to a new destination; omitting it or passing --output - selects stdout. Both --option=value and --option value are accepted. Unknown/duplicate flags, empty values, extra positional arguments and inappropriate options fail with usage exit 2. Format is available only for inspect, with json as its default. Validate-schema rejects --input and emits compact JSON with schema_version, name, schema_sha256 and valid true. --help, help, --version and COMMAND --help do not open inputs.

Decode emits a compact editable document. Encode accepts the strict editable document, checks its exact schema-byte identity, and emits binary bytes. Inspect emits a separate JSON trace or a text trace whose header explicitly says complete or failed. Integer observations are included; raw strings and byte fields are not. Output JSON has no added newline, so compact serialized byte ceilings stay exact. Text traces end their lines with newlines and have a separate check against the configured trace byte ceiling; the library's JSON trace checks still apply.

A failed decode/encode never emits a success result or publishes its output file. Inspect can deliberately emit or publish a labelled failed trace, then return a nonzero exit and a JSON diagnostic on stderr. If even the failed trace cannot fit its budget, stdout is empty and only the diagnostic is emitted. When the library returned a failed trace but its text or JSON rendering does not fit, inspect reports the original decode diagnostic rather than a rendering limit. A stdout write can fail after writing a prefix, so callers must discard output on failure. A failed file-publication cleanup reports that a temporary staging file may remain, even if cleanup follows an earlier failure. If linking already completed, its diagnostic also warns that the destination may exist. Existing destinations, including symlinks and directories, are never replaced.

## Limits and exits

Limits are nonnegative decimal integers and may only lower the defaults in schema-v1.md. Available flags are --max-schema-bytes, --max-schema-fields, --max-depth, --max-input-bytes, --max-output-bytes, --max-field-bytes, --max-array-elements, --max-value-nodes, --max-trace-entries, --max-document-bytes and --max-trace-bytes. Zero is an actual ceiling. A ceiling above the hard default returns limit_exceeded with exit 4; malformed numeric flags are usage errors. There is no unbounded mode.

Schema/document reads are bounded before parsing and consume at most one additional byte to reject oversized input. Binary decoding uses the library's field/input limits and one-byte trailing-data probe. Decode/inspect apply both document and trace budgets. Encode applies document/output budgets; validate-schema's metadata JSON uses the document byte ceiling. All success results are prepared in bounded memory before their first output write. This implementation does not promise streaming or constant memory.

Exit 0 means completion; for diff it means the inputs are the same, and exit 5 means diff completed and found differences (docs/diff-v1.md). Exit 1 is runtime/I/O failure, including destination_exists, failure writing stdout/stderr and a schema or document reader that fails mid-read (read_failed). Exit 2 is usage or schema/document/representation validation. Exit 3 is cancellation and exit 4 is a resource ceiling. Operational diagnostics on stderr are a single JSON object followed by a newline; usage output is a static line. Diagnostic fields do not echo command arguments, filenames, reader/writer error text or raw file contents. Paths inside diagnostics identify schema/value fields, not filesystem paths.

Ctrl-C cancels the command, and on Unix so does SIGTERM. CLI stdin reads use an owned temporary read buffer so a pending read cannot prevent cancellation or later mutate a caller's buffer. A blocked read worker can remain until process exit. The first cancelling signal also restores default handling, allowing a further signal to stop other blocking filesystem operations; a process stopped that way, or by SIGKILL, can leave a .ebyte-*.tmp staging file in the output directory; library context checks alone cannot interrupt those calls. On Unix, SIGPIPE is ignored so broken stdout pipes take the normal write_failed/exit-1 path. Cancellation after a write preserves warnings that output may already be written or that staging cleanup failed, along with known field/byte locations. Other blocking filesystem operations can still delay cooperative cancellation.

File publication is enabled on native-tested Linux, macOS and, since 0.6.0, Windows, and requires same-directory hard-link support (NTFS on Windows; FAT fails closed). Windows has no Unix permission bits, so the mode steps below only keep the file writable there, and names containing a colon (alternate data streams) are a usage error. It stages a private 0600 file, sets the finished file to 0666 minus the umask, syncs and closes it, then links to a new name without replacing anything. A destination ending in a path separator or naming ., .. or / is a usage error (exit 2) before any input is read. The filesystem must also allow changing the staged file's mode. The opened parent directory must be trusted against hostile modification of its entries. Parent renames stay anchored to the opened directory. There is no power-loss directory durability promise, and unsupported platforms/filesystems fail closed.

## Reproducible sample

```sh
go build -o /tmp/ebyte ./cmd/ebyte
python3 scripts/demo.py /tmp/ebyte /tmp/ebyte-demo.json
bash scripts/record-results.sh
```

The demo uses testdata/packet.schema.json and the independent packet.hex oracle. It creates a 21-byte opaque sample, decodes it with the compiled binary, re-encodes it unchanged and compares every byte. It then adds record id 3, temperature -1, without inserting the derived count. The expected edited bytes were specified in testdata/cases.json before the codec: 25 bytes, stored count 3 and note offset 23 rather than 19. The saved evidence records real hashes, platform/architecture, documents and traces; the recorder writes it to test/results/latest/demo.json. Temporary input/output files are removed. This demonstrates the small declared subset with a synthetic fixture, and supplies no benchmark or automatic format-discovery claim.
