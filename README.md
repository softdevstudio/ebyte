# EByte

EByte is a binary schema engine being built in Go. Describe a format once, decode it to editable JSON, encode it back and inspect the field and byte offset where an assumption fails. The library and CLI will share one interpreter, without generated code or a hosted service.

## Current functionality

This is session one's 0.0.0-dev skeleton. Only --version and --help work. Schema compilation, decoding, encoding and field inspection are not implemented. No executable release is available; license selection is pending. An unchanged byte-identical round trip is the implementation's acceptance target, not a current feature claim.

```sh
go build -o /tmp/ebyte ./cmd/ebyte
/tmp/ebyte --version
/tmp/ebyte --help
bash scripts/record-results.sh
```

The command refuses unavailable operations without opening input/output paths or echoing arguments. Planned commands are validate-schema, decode, encode and inspect. Their options will be specified when the shared interpreter exists.

## Contracts and fixtures

Read [schema-v1](docs/schema-v1.md), [document-v1](docs/document-v1.md) and [errors-v1](docs/errors-v1.md). [Fixture reference](docs/fixtures.md) explains independently specified examples and expected failures. Fixed-width strings preserve embedded NUL and trailing spaces; raw byte fields preserve opaque data. Integers use decimal strings in editable JSON. Derived array counts are absent from editable values and calculated by the encoder; inspection exposes the stored count.

The initial subset is integers, ASCII/UTF-8 strings, bytes, counted arrays and equality conditions on earlier integer siblings. An unchanged accepted input must round-trip exactly under identical schema bytes. An edit can change derived counts and move later fields. There is no support for arbitrary expressions, seeks, recursive schemas, executable plugins, graphical editing or structural diff in v1. JSON output materializes bounded data and may be much larger than binary input.

## Development and delivery

CLAUDE.md is the only plan and handoff. AGENTS.md points to it. Follow the supplied chatgpt-project-standards.md. Linux/macOS CI runs formatting, vet, race tests, fixture checks and a built-command smoke; the recorder replaces test/results/latest. A pre-release native/platform gate and license selection are required before automatic publication. No workflow currently publishes a release.

Website work is deferred to the final session. The site repository and ebyte.org connection will be supplied later; no site or studio listing is being published during code prototyping.
