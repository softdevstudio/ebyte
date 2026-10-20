# EByte

EByte is a binary schema engine being built in Go. Describe a format once, decode it to editable JSON, encode it back and inspect the field and byte offset where an assumption fails. The library and CLI share one interpreter, without generated code or a hosted service.

## Current functionality

This 0.0.0-dev build compiles bounded schemas, decodes binary input to editable values and field traces, and encodes validated documents through the Go library. The CLI provides validate-schema, decode, encode and inspect, including JSON/text traces and stdin/stdout. No executable release is available; license selection is pending. Unchanged round trips are checked against independent byte fixtures and bounded fuzz tests.

```sh
go build -o /tmp/ebyte ./cmd/ebyte
/tmp/ebyte --version
/tmp/ebyte --help
bash scripts/record-results.sh
```

Every command requires --schema FILE. Decode, encode and inspect also require --input FILE or --input - for stdin. Output defaults to stdout; --output FILE publishes a new file and refuses any existing destination. Schema validation completes before the binary/document input opens. See [CLI reference](docs/cli.md) for commands, limits, failure output and exit codes.

```sh
/tmp/ebyte validate-schema --schema testdata/packet.schema.json
python3 scripts/demo.py /tmp/ebyte /tmp/ebyte-demo.json
```

The built-binary demo starts with the independently specified 21-byte packet, decodes it, verifies an unchanged byte-identical re-encode, adds one record and checks the specified 25-byte output. Its saved evidence includes hashes, values and before/after traces: the derived count changes from 2 to 3 and the note moves from offset 19 to 23. This is a small synthetic format demonstration, not a throughput or general-format compatibility claim. The recorder saves its real run to test/results/latest/demo.json.

## Library schema compilation

```go
limits := ebyte.DefaultLimits()
limits.MaxSchemaBytes = 64 * 1024
plan, err := ebyte.Compile(schemaBytes, limits)
if err != nil {
    // A *ebyte.Diagnostic contains the schema property path and stable code.
    return err
}
fmt.Println(plan.Name(), plan.SchemaSHA256())
```

Import github.com/softdevstudio/ebyte. Compile copies validated strings into an immutable plan and retains no caller buffer. Its SHA-256 identifies the exact validated bytes, including whitespace. Limits returns a copy of the plan's ceilings. Start with DefaultLimits and lower individual ceilings; zero is a real limit, and negative or above-default settings fail with limit_exceeded. Binary ceilings are checked during execution, rather than against speculative runtime sizes during compilation.

Compilation rejects unknown, duplicate or null members, invalid UTF-8 and unpaired Unicode escapes, noncanonical dimensions, invalid assertions and unsafe references. Same-object references resolve only to earlier unconditional integers. A derived unsigned count binds to one later unconditional array and cannot drive another dimension. Diagnostics use schema-stage JSON property paths and null binary offsets; unknown member names are omitted from paths to avoid reflecting arbitrary input. Schema input is bounded before JSON parsing, JSON nesting has an additional safety ceiling of 40, and declarations/array scope depth and literal dimensions are checked before compiled-plan allocation. The schema byte ceiling also bounds the temporary JSON tree; this is not a streaming or constant-memory compiler.

## Library decoding and inspection

```go
document, trace, err := plan.Decode(ctx, reader)
if err != nil {
    // document is nil. trace can contain labelled failed observations.
    // A *ebyte.Diagnostic identifies the field and relevant byte positions.
    return err
}
jsonBytes, err := json.Marshal(document)
// Or request the same validation and trace without retaining the document:
trace, err = plan.Inspect(ctx, anotherReader)
```

Decode returns (*Document, *Trace, error); Inspect returns (*Trace, error). Both use the same traversal and enforce both editable-value and trace budgets. Values materialize in memory; Inspect currently constructs and discards those values too. Plans can be shared across concurrent calls with separate readers. Results belong to the caller and may be edited before encoding. Do not change a result concurrently with serialization.

Integers decode to decimal strings, byte fields to lowercase hex, and valid ASCII/UTF-8 strings retain embedded NUL and trailing spaces. Derived counts are omitted from editable values and observed in traces. Conditional fields that are absent have no value or trace entry. Limits cover binary bytes, field widths, array elements, independent value-node/trace-entry counts and the exact compact encoding/json byte sizes, including hex and escaping expansion. Pretty printing, added newlines or caller edits may increase serialized sizes beyond the checked compact result.

A failure returns no editable document. Its trace marks affected ancestors failed and records only consumed bytes, keeping earlier completed siblings intact. Integer observations appear only after a full representation is read; raw byte/string content never appears in traces or diagnostics. If the failed trace envelope and diagnostic cannot fit the trace byte limit, the trace is nil and the diagnostic is still returned. Diagnostic stage is io for reader failures and decode for truncation, representation, limit or cancellation failures.

Readers need no seeking. A field that exceeds the remaining input budget fails before reading that field. Successful decoding also checks for trailing data, consuming at most one extra byte; an extra probe byte at the input ceiling reports limit_exceeded. EOF with the last field's bytes requires no further read. A hundred consecutive zero-byte reads without an error report read_failed. Context cancellation is checked between reads and during traversal; it cannot interrupt a Reader blocked inside Read. Pass a non-nil context and a Reader obeying Go's io.Reader contract.

## Library encoding

```go
document, err := plan.LoadDocument(ctx, documentJSON)
if err != nil {
    return err
}
// Edit document.Values using strings, map[string]any and []any.
err = plan.Encode(ctx, document, writer)
```

LoadDocument(ctx, rawJSON) returns (*Document, error). It rejects unknown, duplicate, null and malformed members, invalid Unicode, noncanonical integer values and any schema identity mismatch. It also validates the complete values tree, including missing or inactive fields, assertions, widths and counts. Its raw JSON byte ceiling includes whitespace; the compact output size is checked independently. Array values use non-nil []any and object values use non-nil map[string]any. Empty arrays and objects are valid; nil represents null and fails.

Encode(ctx, document, writer) returns error. It revalidates all values, so edits made after loading receive the same checks. Derived count values must be absent; the encoder calculates them from their bound arrays and checks the integer range. Stored counts must match the supplied arrays. Hex stays lowercase, strings retain their exact encoded width, and assertions validate rather than rewrite values. The encoder checks value nodes, compact document bytes, field/array ceilings and aggregate binary output bytes. Trace and input byte budgets apply to decoding, not encoding.

The encoder prepares at most MaxOutputBytes of binary data before the first write. Any validation failure writes nothing. LoadDocument uses the same preparation to validate, then discards those bytes; Encode prepares them again so caller edits are checked. This is a bounded in-memory implementation, not a streaming encoder. Writer errors and short writes report write_failed; partial output may remain and must be discarded. Invalid writer counts have unknown (null) offsets. Cancellation after a write can return cancelled even if bytes have already been written. Context checks cannot interrupt a blocked Write. Known assertion and ASCII errors identify their position in the prepared binary; this does not mean those bytes reached the destination.

The CLI publication helper stages a private 0600 file in an opened parent directory, syncs and closes it, then creates a new hard link without replacing a destination. Tests cover existing files, symlinks, directories and destinations created during staging, producer/sync failures, cancellation and renamed parents. The helper is enabled only on native-tested Linux/macOS and fails on filesystems without hard-link support. The parent directory's entries must be trusted against hostile concurrent modification. This does not promise directory durability across power loss. If cleanup fails after linking, an error says the destination may already exist. All CLI file outputs use this helper; it is not exposed as a library API.

## Contracts and fixtures

Read [schema-v1](docs/schema-v1.md), [document-v1](docs/document-v1.md) and [errors-v1](docs/errors-v1.md). [Fixture reference](docs/fixtures.md) explains independently specified examples and expected failures. Fixed-width strings preserve embedded NUL and trailing spaces; raw byte fields preserve opaque data. Integers use decimal strings in editable JSON. Derived array counts are absent from editable values and calculated by the encoder; inspection exposes the stored count.

The initial subset is integers, ASCII/UTF-8 strings, bytes, counted arrays and equality conditions on earlier integer siblings. An unchanged accepted input must round-trip exactly under identical schema bytes. An edit can change derived counts and move later fields. There is no support for arbitrary expressions, seeks, recursive schemas, executable plugins, graphical editing or structural diff in v1. JSON output materializes bounded data and may be much larger than binary input.

## Development and delivery

CLAUDE.md is the only plan and handoff. AGENTS.md points to it. Follow the supplied chatgpt-project-standards.md. Linux/macOS CI runs formatting, vet, race tests, fixture checks, three bounded fuzz runs, process-level CLI smoke and a saved compiled-binary demo; the recorder replaces test/results/latest. A pre-release native/platform gate and license selection are required before automatic publication. No workflow currently publishes a release.

Website work is deferred to the final session. The site repository and ebyte.org connection will be supplied later; no site or studio listing is being published during code prototyping.
