# EByte

EByte is a binary schema engine being built in Go. Describe a format once, decode it to editable JSON, encode it back and inspect the field and byte offset where an assumption fails. The library and CLI will share one interpreter, without generated code or a hosted service.

## Current functionality

This 0.0.0-dev build compiles bounded schemas and decodes binary input to editable values and field traces through the Go library. Only --version and --help work in the CLI. Encoding and operational CLI commands are not implemented. No executable release is available; license selection is pending. An unchanged byte-identical round trip is the implementation's acceptance target, not a current feature claim.

```sh
go build -o /tmp/ebyte ./cmd/ebyte
/tmp/ebyte --version
/tmp/ebyte --help
bash scripts/record-results.sh
```

The command refuses unavailable operations without opening input/output paths or echoing arguments. Planned commands are validate-schema, decode, encode and inspect. Their options will be specified when the shared interpreter exists.

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

Decode returns (*Document, *Trace, error); Inspect returns (*Trace, error). Both use the same traversal and enforce both editable-value and trace budgets. Values materialize in memory; Inspect currently constructs and discards those values too. Plans can be shared across concurrent calls with separate readers. Results belong to the caller and may be edited, but there is no encoder yet. Do not change a result concurrently with serialization.

Integers decode to decimal strings, byte fields to lowercase hex, and valid ASCII/UTF-8 strings retain embedded NUL and trailing spaces. Derived counts are omitted from editable values and observed in traces. Conditional fields that are absent have no value or trace entry. Limits cover binary bytes, field widths, array elements, independent value-node/trace-entry counts and the exact compact encoding/json byte sizes, including hex and escaping expansion. Pretty printing, added newlines or caller edits may increase serialized sizes beyond the checked compact result.

A failure returns no editable document. Its trace marks affected ancestors failed and records only consumed bytes, keeping earlier completed siblings intact. Integer observations appear only after a full representation is read; raw byte/string content never appears in traces or diagnostics. If the failed trace envelope and diagnostic cannot fit the trace byte limit, the trace is nil and the diagnostic is still returned. Diagnostic stage is io for reader failures and decode for truncation, representation, limit or cancellation failures.

Readers need no seeking. A field that exceeds the remaining input budget fails before reading that field. Successful decoding also checks for trailing data, consuming at most one extra byte; an extra probe byte at the input ceiling reports limit_exceeded. EOF with the last field's bytes requires no further read. A hundred consecutive zero-byte reads without an error report read_failed. Context cancellation is checked between reads and during traversal; it cannot interrupt a Reader blocked inside Read. Pass a non-nil context and a Reader obeying Go's io.Reader contract.

## Contracts and fixtures

Read [schema-v1](docs/schema-v1.md), [document-v1](docs/document-v1.md) and [errors-v1](docs/errors-v1.md). [Fixture reference](docs/fixtures.md) explains independently specified examples and expected failures. Fixed-width strings preserve embedded NUL and trailing spaces; raw byte fields preserve opaque data. Integers use decimal strings in editable JSON. Derived array counts are absent from editable values and calculated by the encoder; inspection exposes the stored count.

The initial subset is integers, ASCII/UTF-8 strings, bytes, counted arrays and equality conditions on earlier integer siblings. An unchanged accepted input must round-trip exactly under identical schema bytes. An edit can change derived counts and move later fields. There is no support for arbitrary expressions, seeks, recursive schemas, executable plugins, graphical editing or structural diff in v1. JSON output materializes bounded data and may be much larger than binary input.

## Development and delivery

CLAUDE.md is the only plan and handoff. AGENTS.md points to it. Follow the supplied chatgpt-project-standards.md. Linux/macOS CI runs formatting, vet, race tests, fixture checks and a built-command smoke; the recorder replaces test/results/latest. A pre-release native/platform gate and license selection are required before automatic publication. No workflow currently publishes a release.

Website work is deferred to the final session. The site repository and ebyte.org connection will be supplied later; no site or studio listing is being published during code prototyping.
