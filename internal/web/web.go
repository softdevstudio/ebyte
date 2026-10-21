// Package web is the browser-facing surface of EByte: each call takes schema
// text and file bytes and returns one JSON envelope, so the WebAssembly glue
// in cmd/ebyte-wasm stays a thin adapter. See docs/wasm.md.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/softdevstudio/ebyte"
)

// Envelope is the JSON every call returns. Exactly one of Result or
// Diagnostic is set, except that a failed inspect keeps its partial trace.
type Envelope struct {
	OK         bool              `json:"ok"`
	Schema     *SchemaInfo       `json:"schema"`
	Result     any               `json:"result"`
	Input      string            `json:"input,omitempty"`
	Diagnostic *ebyte.Diagnostic `json:"diagnostic"`
}

// SchemaInfo identifies the compiled schema.
type SchemaInfo struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func marshal(e Envelope) string {
	raw, err := json.Marshal(e)
	if err != nil {
		raw, _ = json.Marshal(Envelope{Diagnostic: &ebyte.Diagnostic{DiagnosticVersion: 1, Stage: "io", Code: "write_failed", Expected: "serializable result", Observed: "JSON serialization failed"}})
	}
	return string(raw)
}

func failed(info *SchemaInfo, input string, err error) string {
	var d *ebyte.Diagnostic
	var de *ebyte.DiffError
	switch {
	case errors.As(err, &de):
		input, d = de.Input, de.Diagnostic
	case errors.As(err, &d):
	default:
		d = &ebyte.Diagnostic{DiagnosticVersion: 1, Stage: "io", Code: "read_failed", Expected: "complete operation", Observed: "operation failed"}
	}
	return marshal(Envelope{Schema: info, Input: input, Diagnostic: d})
}

// Usage reports a call whose arguments the browser build can't use.
func Usage(expected string) string {
	return marshal(Envelope{Diagnostic: &ebyte.Diagnostic{DiagnosticVersion: 1, Stage: "io", Code: "invalid_argument", Expected: expected, Observed: "unusable call arguments"}})
}

// TooLarge reports an input over the default input ceiling before it is copied.
func TooLarge() string {
	return marshal(Envelope{Diagnostic: &ebyte.Diagnostic{DiagnosticVersion: 1, Stage: "io", Code: "limit_exceeded", Expected: "within configured resource ceilings", Observed: "input exceeds the byte ceiling"}})
}

func compile(schema string) (*ebyte.Plan, *SchemaInfo, error) {
	plan, err := ebyte.Compile([]byte(schema), ebyte.DefaultLimits())
	if err != nil {
		return nil, nil, err
	}
	return plan, &SchemaInfo{plan.Name(), plan.SchemaSHA256()}, nil
}

// Validate compiles a schema.
func Validate(schema string) string {
	_, info, err := compile(schema)
	if err != nil {
		return failed(nil, "", err)
	}
	return marshal(Envelope{OK: true, Schema: info, Result: info})
}

// Inspect returns the field trace; on failure the partial trace is kept.
func Inspect(ctx context.Context, schema string, input []byte) string {
	plan, info, err := compile(schema)
	if err != nil {
		return failed(nil, "", err)
	}
	trace, err := plan.Inspect(ctx, bytes.NewReader(input))
	if err != nil {
		var d *ebyte.Diagnostic
		if !errors.As(err, &d) {
			d = &ebyte.Diagnostic{DiagnosticVersion: 1, Stage: "io", Code: "read_failed", Expected: "complete operation", Observed: "operation failed"}
		}
		return marshal(Envelope{Schema: info, Result: trace, Diagnostic: d})
	}
	return marshal(Envelope{OK: true, Schema: info, Result: trace})
}

// Decode returns the editable document.
func Decode(ctx context.Context, schema string, input []byte) string {
	plan, info, err := compile(schema)
	if err != nil {
		return failed(nil, "", err)
	}
	doc, _, err := plan.Decode(ctx, bytes.NewReader(input))
	if err != nil {
		return failed(info, "", err)
	}
	return marshal(Envelope{OK: true, Schema: info, Result: doc})
}

// Encode loads an edited document and returns the binary bytes with an
// envelope whose result is the output length.
func Encode(ctx context.Context, schema, document string) ([]byte, string) {
	plan, info, err := compile(schema)
	if err != nil {
		return nil, failed(nil, "", err)
	}
	doc, err := plan.LoadDocument(ctx, []byte(document))
	if err != nil {
		return nil, failed(info, "", err)
	}
	var out bytes.Buffer
	if err := plan.Encode(ctx, doc, &out); err != nil {
		return nil, failed(info, "", err)
	}
	return out.Bytes(), marshal(Envelope{OK: true, Schema: info, Result: map[string]int{"bytes": out.Len()}})
}

// Diff compares two inputs through the schema.
func Diff(ctx context.Context, schema string, before, after []byte) string {
	plan, info, err := compile(schema)
	if err != nil {
		return failed(nil, "", err)
	}
	result, err := plan.Diff(ctx, bytes.NewReader(before), bytes.NewReader(after))
	if err != nil {
		return failed(info, "", err)
	}
	return marshal(Envelope{OK: true, Schema: info, Result: result})
}
