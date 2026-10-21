package ebyte

import (
	"context"
	"encoding/hex"
	"io"
	"strconv"
)

// DiffResult compares two inputs decoded through one plan. See docs/diff-v1.md.
type DiffResult struct {
	DiffVersion  int      `json:"diff_version"`
	SchemaSHA256 string   `json:"schema_sha256"`
	State        string   `json:"state"`
	SizeBefore   int64    `json:"size_before"`
	SizeAfter    int64    `json:"size_after"`
	Changes      []Change `json:"changes"`
}

// Change is one field that differs between the inputs, in schema order.
type Change struct {
	Path            string `json:"path"`
	Kind            string `json:"kind"`
	Change          string `json:"change"`
	Derived         bool   `json:"derived"`
	Before          *Span  `json:"before"`
	After           *Span  `json:"after"`
	FirstDifference *int64 `json:"first_difference,omitempty"`
}

// Span locates a field in one input; Value is set only for integers.
type Span struct {
	Offset int64   `json:"offset"`
	Length int64   `json:"length"`
	Value  *string `json:"value,omitempty"`
}

// DiffError reports which input failed to decode.
type DiffError struct {
	Input      string
	Diagnostic *Diagnostic
}

func (e *DiffError) Error() string { return e.Input + " input: " + e.Diagnostic.Error() }
func (e *DiffError) Unwrap() error { return e.Diagnostic }

// Diff decodes before and then after completely and compares them field by
// field. Arrays and repeats are matched by position.
func (p *Plan) Diff(ctx context.Context, before, after io.Reader) (*DiffResult, error) {
	sides := [2]struct {
		doc   *Document
		trace *Trace
		spans map[string]TraceEntry
	}{}
	for i, r := range []io.Reader{before, after} {
		doc, trace, e := p.Decode(ctx, r)
		if e != nil {
			d, ok := e.(*Diagnostic)
			if !ok {
				d = &Diagnostic{DiagnosticVersion: 1, Stage: "decode", Code: "read_failed", Expected: "readable input", Observed: "reader error"}
			}
			return nil, &DiffError{Input: [2]string{"before", "after"}[i], Diagnostic: d}
		}
		sides[i].doc, sides[i].trace = doc, trace
		sides[i].spans = make(map[string]TraceEntry, len(trace.Entries))
		for _, entry := range trace.Entries {
			sides[i].spans[entry.Path] = entry
		}
	}
	w := differ{p: p, ctx: ctx, a: sides[0].spans, b: sides[1].spans, changes: []Change{}}
	if e := w.object(p.fields, sides[0].doc.Values, sides[1].doc.Values, ""); e != nil {
		return nil, e
	}
	state := "same"
	if len(w.changes) > 0 {
		state = "different"
	}
	return &DiffResult{1, p.digest, state, sides[0].spans[""].Length, sides[1].spans[""].Length, w.changes}, nil
}

type differ struct {
	p       *Plan
	ctx     context.Context
	a, b    map[string]TraceEntry
	changes []Change
}

func span(e TraceEntry) *Span {
	return &Span{Offset: e.Offset, Length: e.Length, Value: e.Value}
}

func (w *differ) cancelled(path string) error {
	if w.ctx.Err() != nil {
		return &Diagnostic{DiagnosticVersion: 1, Stage: "diff", Code: "cancelled", Path: path, Expected: "active operation", Observed: "context cancelled"}
	}
	return nil
}

func (w *differ) add(c Change) error {
	if len(w.changes) >= w.p.limits.MaxTraceEntries {
		return &Diagnostic{DiagnosticVersion: 1, Stage: "diff", Code: "limit_exceeded", Path: c.Path, Expected: "within configured resource ceilings", Observed: "resource ceiling exceeded"}
	}
	w.changes = append(w.changes, c)
	return nil
}

func (w *differ) object(fields []field, va, vb map[string]any, path string) error {
	for _, f := range fields {
		p := path + "/" + f.name
		if e := w.cancelled(p); e != nil {
			return e
		}
		ea, inA := w.a[p]
		eb, inB := w.b[p]
		switch {
		case !inA && !inB:
			continue
		case !inB:
			if e := w.add(Change{Path: p, Kind: ea.Kind, Change: "removed", Derived: f.derived(), Before: span(ea)}); e != nil {
				return e
			}
			continue
		case !inA:
			if e := w.add(Change{Path: p, Kind: eb.Kind, Change: "added", Derived: f.derived(), After: span(eb)}); e != nil {
				return e
			}
			continue
		}
		changed := Change{Path: p, Kind: ea.Kind, Change: "changed", Derived: f.derived(), Before: span(ea), After: span(eb)}
		if _, _, isInt := integer(f.kind); isInt {
			if *ea.Value != *eb.Value {
				if e := w.add(changed); e != nil {
					return e
				}
			}
			continue
		}
		switch f.kind {
		case "bytes", "string":
			x, y := va[f.name].(string), vb[f.name].(string)
			if f.kind == "bytes" {
				rx, _ := hex.DecodeString(x)
				ry, _ := hex.DecodeString(y)
				x, y = string(rx), string(ry)
			}
			if x == y {
				continue
			}
			i := 0
			for i < len(x) && i < len(y) && x[i] == y[i] {
				i++
			}
			n := int64(i)
			changed.FirstDifference = &n
			if e := w.add(changed); e != nil {
				return e
			}
		default:
			la, _ := va[f.name].([]any)
			lb, _ := vb[f.name].([]any)
			for i := 0; i < len(la) || i < len(lb); i++ {
				ep := p + "/" + strconv.Itoa(i)
				var e error
				switch {
				case i >= len(lb):
					e = w.add(Change{Path: ep, Kind: "struct", Change: "removed", Before: span(w.a[ep])})
				case i >= len(la):
					e = w.add(Change{Path: ep, Kind: "struct", Change: "added", After: span(w.b[ep])})
				default:
					e = w.object(f.children, la[i].(map[string]any), lb[i].(map[string]any), ep)
				}
				if e != nil {
					return e
				}
			}
		}
	}
	return nil
}
