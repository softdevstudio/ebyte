# Kaitai Struct import

Since EByte 0.8.0, `ebyte import-ksy` turns a Kaitai Struct `.ksy` file into an EByte schema, for the part of Kaitai an EByte schema can express. Anything outside that part is refused with every reason found. A file is never imported approximately.

```sh
ebyte import-ksy --input stl.ksy --output stl.schema.json
ebyte decode --schema stl.schema.json --input model.stl
```

The schema goes to stdout, or to a new file with `--output`, which never replaces an existing file. On success, notes on what was left out go to stderr as one JSON object, `{"notes":[...]}`. A refusal exits 2 with nothing on stdout and one JSON object on stderr: `{"diagnostic_version":1,"stage":"import","code":"unsupported_construct","reasons":[{"path":"/types/record/seq/2/type","reason":"switch-on types are not supported"}]}`. Paths point into the .ksy file and are built from its keys, type names and seq positions; reasons never quote values from the file, such as expressions. When the converted schema is valid Kaitai but EByte refuses it (for example a bit group that does not fill whole bytes before a user type), the single reason has the path / and names the EByte schema path in its text. The library call is `kaitai.Import(src)` in package `github.com/softdevstudio/ebyte/kaitai`; it returns the schema and notes, or a `*kaitai.Refusal` listing the reasons. Every imported schema has already passed `ebyte.Compile`. Input is limited to 1 MiB, and inlined user types to 1024 fields.

## How much of the gallery imports

Measured on 2026-11-03 against the Kaitai Struct gallery (kaitai_struct_formats at commit 7d17566, 191 .ksy files): 9 files import. They are monomakh_sapr_chg, android_nanoapp_header, mbr_partition_table, shapefile_index, aix_utmp, glibc_utmp, stl, vp8_duck_ivf and windows_systemtime. Most of the gallery uses something EByte has no equivalent for. Counting each reason once per file, the most common are instances read at a position (76 files), switch-on types (55), size or count expressions (47), a size given to a user type of variable length (40), imports (33), conditions other than field == value (31), repeat until (23), valid other than eq (21), size-eos (20) and strz (13). A file usually has several. Rerun the count with `KAITAI_GALLERY=path/to/kaitai_struct_formats go test -run TestGallerySurvey -v ./kaitai`.

## What maps to what

| Kaitai | EByte |
|---|---|
| `meta/id` | `name` (must be an EByte name) |
| `meta/endian: le` or `be` | `endian`; required when a multi-byte type has no le/be suffix |
| `meta/encoding` | the default for `str` |
| `u1`..`u8`, `s1`..`s8`, with `le`/`be` | `u8`..`u64`, `i8`..`i64`, with a field `endian` when it differs |
| `f4`, `f8` | `f32`, `f64` |
| `b1`..`b64` (big-endian bit order) | `bits`; the group must fill whole bytes, at most 8 |
| `str` with `size` and ASCII or UTF-8 | `string` with `length` and `encoding` |
| `size` without `type` | `bytes` |
| `contents`: a word or quoted text as UTF-8, or a list whose unquoted integers and quoted decimal or 0x numbers are bytes (-128 to 255) and whose other items are UTF-8 text, as in the Kaitai compiler | `bytes` with that length and `expect` |
| `valid: N` or `valid: {eq: N}` on an integer | `expect` |
| `if: field == N` or `field == enum::member` | `when` on that earlier field |
| a user type | an `array` of one element, whose fields are the type's |
| `repeat: expr` with a number or an earlier integer field | `array` with that `count` |
| `repeat: eos` on the last top-level field | `repeat` with `until: end_of_input` |
| a repeated primitive | elements with one field, `value` |
| `size` with a user type | accepted only when it equals the type's fixed size |

A size or count read from an earlier unsigned field that nothing else uses gets a driving `derive`, so the number is absent from the editable document and is written from the data on encode. Type and enum names resolve where the type is defined: its own `types` and `enums`, then those of the type that defines it, out to the top level, as Kaitai resolves them. Nested and repeated user types are inlined at each use.

Dropped, with a note: `doc`, `doc-ref`, keys starting with `-` (such as `-orig-id`), `enum` on a field (the value stays a number), value instances (they are computed, not stored), and meta fields other than id, endian, encoding and bit-endian.

Refused: expressions of any other kind (`_parent`, `_root`, arithmetic, method calls), switch-on types, `strz`, `terminator`, `pad-right`, `process`, `params` and parameterized types, `imports`, `size-eos`, `repeat: until`, instances read at a position, encodings other than ASCII and UTF-8, `bit-endian: le`, recursive types, nesting deeper than EByte's 8 levels, and YAML outside the subset below.

## Differences that remain after import

Kaitai decodes strings leniently. EByte requires valid UTF-8 in a UTF-8 string and 7-bit bytes in an ASCII string, so a file that Kaitai reads with replacement characters fails in EByte at that field. Keep such a field as bytes by removing `type: str` and `encoding` from the .ksy before importing. A user type is a one-element array, so a document holds `"header": [{...}]` where Kaitai has `header.x`. Enum names are not carried over; values are numbers. Kaitai stops after the last top-level field and ignores any bytes after it, while EByte refuses a file with trailing bytes (trailing_data); a disk image with data after an imported MBR is one example. A negative count or size read from a signed field makes Kaitai read nothing, while EByte refuses the file (value_out_of_range). These are refusals, not different readings.

## YAML subset

The reader handles what the gallery uses: block mappings and sequences, flow sequences and mappings (also across lines), plain, single- and double-quoted scalars, literal (`|`) and folded (`>`) block scalars with chomping indicators, comments and a leading `---`. It refuses anchors, aliases, tags, complex keys, directives, explicit block indentation, duplicate keys and tabs in indentation. Checked on 2026-11-03 by a comparison script outside the test suite, it read all 191 gallery files to the same structure as js-yaml 4.1.0 with its failsafe schema. It is more lenient than YAML in a few invalid cases (such as a second colon in a plain value); those values are then refused or dropped by the importer. `null` and `~` read as empty values. Long flow collections and quoted scalars are read in one pass, so time grows with file size. Numbers follow YAML 1.2 as js-yaml reads them (`017` is 17, `0o17` is 15). The Kaitai compiler on the JVM uses YAML 1.1 rules, where `017` is octal; such a file can mean different bytes to the two Kaitai builds, and EByte follows the JavaScript one.

## How the import is tested

`kaitai/testdata/gallery` holds the nine importable gallery files, each under its own licence (eight CC0-1.0, android_nanoapp_header Apache-2.0), with samples built byte by byte by `kaitai/testdata/generate.cjs`. That script also parses each sample with the official Kaitai Struct compiler and JavaScript runtime 0.11.0 and records every stored value in `expected.json`. The Go test imports each file, decodes the sample with EByte, requires the same values in the same order (floats compared as numbers) and encodes the document back to the sample's exact bytes. Mapping and refusal cases are hand-written, and FuzzImport feeds arbitrary text to the importer, requiring no panic and a compiling schema whenever it succeeds.
