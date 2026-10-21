package ebyte

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"strconv"
	"unicode/utf8"
)

// Document is the editable result of a successful decode. Integer values are
// decimal strings; derived counts are omitted. No document is returned on failure.
type Document struct {
	DocumentVersion int            `json:"document_version"`
	SchemaSHA256    string         `json:"schema_sha256"`
	Values          map[string]any `json:"values"`
}

// Trace contains observations in schema preorder, without raw bytes or strings.
// A failed decode can return a failed trace, never a complete editable document.
type Trace struct {
	TraceVersion int          `json:"trace_version"`
	SchemaSHA256 string       `json:"schema_sha256"`
	State        string       `json:"state"`
	Entries      []TraceEntry `json:"entries"`
	Diagnostic   *Diagnostic  `json:"diagnostic"`
}

type TraceEntry struct {
	Path   string  `json:"path"`
	Kind   string  `json:"kind"`
	Offset int64   `json:"offset"`
	Length int64   `json:"length"`
	State  string  `json:"state"`
	Value  *string `json:"value,omitempty"`
}

type decoder struct {
	plan       *Plan
	ctx        context.Context
	reader     io.Reader
	eof        bool
	pending    []byte // bytes peeked by a repeat but not yet consumed
	pos        int64
	nodes      int
	docBytes   int
	traceBytes int
	trace      Trace
}

// Decode reads one complete binary representation. It returns no document on any
// failure. The trace is nil if even its failed envelope cannot fit MaxTraceBytes.
// Context cancellation is checked between reads; it cannot interrupt a blocked
// Reader. A Reader must obey the io.Reader contract and must not be nil.
func (p *Plan) Decode(ctx context.Context, r io.Reader) (*Document, *Trace, error) {
	d := decoder{plan: p, ctx: ctx, reader: r}
	d.trace = Trace{TraceVersion: 1, SchemaSHA256: p.digest, State: "complete", Entries: []TraceEntry{}}
	// Fixed envelope sizes include the empty values object / entries array.
	d.docBytes = len(`{"document_version":1,"schema_sha256":"`) + len(p.digest) + len(`","values":{}}`)
	d.traceBytes = traceSize(&d.trace)
	values, err := d.object(p.fields, "")
	if err == nil {
		err = d.probe()
	}
	if err == nil {
		err = d.cancel("", nil)
	}
	if err != nil {
		diagnostic := err.(*Diagnostic)
		d.trace.State = "failed"
		d.trace.Diagnostic = diagnostic
		if len(d.trace.Entries) > 0 {
			d.trace.Entries[0].State = "failed"
		}
		if traceSize(&d.trace) > p.limits.MaxTraceBytes {
			return nil, nil, err
		}
		return nil, &d.trace, err
	}
	return &Document{DocumentVersion: 1, SchemaSHA256: p.digest, Values: values}, &d.trace, nil
}

// Inspect uses exactly the same bounded traversal and validation as Decode.
// Both editable-value and trace budgets apply, even when values are discarded.
func (p *Plan) Inspect(ctx context.Context, r io.Reader) (*Trace, error) {
	_, trace, err := p.Decode(ctx, r)
	return trace, err
}

func (d *decoder) diagnostic(code, path string, start *int64, offset int64, expected, observed string) error {
	stage := "decode"
	if code == "read_failed" {
		stage = "io"
	}
	// Copy the start position so a returned diagnostic never aliases decoder state.
	var fieldStart *int64
	if start != nil {
		value := *start
		fieldStart = &value
	}
	return &Diagnostic{DiagnosticVersion: 1, Stage: stage, Code: code, Path: path, Offset: &offset, FieldStart: fieldStart, Expected: expected, Observed: observed}
}
func (d *decoder) limit(path string, start *int64) error {
	return d.diagnostic("limit_exceeded", path, start, d.pos, "within configured resource ceilings", "resource ceiling exceeded")
}
func (d *decoder) cancel(path string, start *int64) error {
	if d.ctx.Err() != nil {
		return d.diagnostic("cancelled", path, start, d.pos, "active operation", "context cancelled")
	}
	return nil
}
func (d *decoder) documentBudget(path string, start *int64, nodes, bytes int) error {
	if nodes > d.plan.limits.MaxValueNodes-d.nodes || bytes > d.plan.limits.MaxDocumentBytes-d.docBytes {
		return d.limit(path, start)
	}
	d.nodes += nodes
	d.docBytes += bytes
	return nil
}
func (d *decoder) begin(path, kind string, start int64) (int, error) {
	if e := d.cancel(path, &start); e != nil {
		return -1, e
	}
	entry := TraceEntry{Path: path, Kind: kind, Offset: start, State: "complete"}
	n := entrySize(entry)
	if len(d.trace.Entries) > 0 {
		n++
	}
	if len(d.trace.Entries) >= d.plan.limits.MaxTraceEntries || n > d.plan.limits.MaxTraceBytes-d.traceBytes {
		return -1, d.limit(path, &start)
	}
	d.traceBytes += n
	d.trace.Entries = append(d.trace.Entries, entry)
	return len(d.trace.Entries) - 1, nil
}
func (d *decoder) finish(index int, err error) error {
	entry := &d.trace.Entries[index]
	old := entrySize(*entry)
	entry.Length = d.pos - entry.Offset
	if err != nil {
		entry.State = "failed"
	}
	d.traceBytes += entrySize(*entry) - old
	if err == nil && d.traceBytes > d.plan.limits.MaxTraceBytes {
		entry.State = "failed"
		return d.limit(entry.Path, &entry.Offset)
	}
	return err
}
func (d *decoder) object(fields []field, path string) (values map[string]any, err error) {
	start := d.pos
	if e := d.documentBudget(path, &start, 1, 0); e != nil {
		return nil, e
	}
	index, e := d.begin(path, "struct", start)
	if e != nil {
		return nil, e
	}
	defer func() { err = d.finish(index, err) }()
	values = map[string]any{}
	observed := make([]string, len(fields))
	// Field spans and trace entries, for checking measured integers at the end.
	starts, ends, entries := make([]int64, len(fields)), make([]int64, len(fields)), make([]int, len(fields))
	for i, f := range fields {
		fieldPath := path + "/" + f.name
		starts[i], ends[i] = d.pos, d.pos
		if e = d.cancel(fieldPath, &d.pos); e != nil {
			return nil, e
		}
		if f.when != nil && observed[f.when.ref] != f.when.equals {
			continue
		}
		// Count the member name, colon and separator before allocating its value.
		memberBytes := 0
		if !f.derived() {
			memberBytes = len(f.name) + 3
			if len(values) > 0 {
				memberBytes++
			}
		}
		if e = d.documentBudget(fieldPath, &d.pos, 0, memberBytes); e != nil {
			return nil, e
		}
		entries[i] = len(d.trace.Entries)
		value, integerValue, e := d.value(f, fieldPath, observed)
		if e != nil {
			return nil, e
		}
		ends[i] = d.pos
		observed[i] = integerValue
		if !f.derived() {
			values[f.name] = value
		}
	}
	for i, f := range fields {
		if f.measure == nil {
			continue
		}
		if measured(f.measure, starts, ends, values, fields) != observed[i] {
			old := entrySize(d.trace.Entries[entries[i]])
			d.trace.Entries[entries[i]].State = "failed"
			d.traceBytes += entrySize(d.trace.Entries[entries[i]]) - old
			return nil, d.diagnostic("constraint_mismatch", path+"/"+f.name, &starts[i], starts[i], "stored value equals measured fields", "stored value differs")
		}
	}
	return values, nil
}

// measured calculates a measured integer from sibling spans as a decimal
// string, or "" when the result is negative.
func measured(m *measure, starts, ends []int64, values map[string]any, fields []field) string {
	var n int64
	switch m.kind {
	case "size":
		n = ends[m.to] - starts[m.from]
	case "offset":
		n = starts[m.from]
	default:
		a, _ := values[fields[m.from].name].([]any)
		n = int64(len(a))
	}
	// Spans are bounded by the input ceiling, so only add can overflow.
	if (m.add > 0 && n > math.MaxInt64-m.add) || n+m.add < 0 {
		return ""
	}
	return strconv.FormatInt(n+m.add, 10)
}
func (d *decoder) dimension(f field, observed []string, path string) (int, error) {
	n := f.length.literal
	if f.length.dynamic {
		s := observed[f.length.ref]
		if len(s) > 0 && s[0] == '-' {
			return 0, d.diagnostic("value_out_of_range", path, &d.pos, d.pos, "nonnegative dimension", "negative dimension")
		}
		var e error
		n, e = strconv.ParseUint(s, 10, 64)
		if e != nil {
			return 0, d.limit(path, &d.pos)
		}
	}
	cap := d.plan.limits.MaxFieldBytes
	if f.kind == "array" {
		cap = d.plan.limits.MaxArrayElements
	}
	if n > uint64(cap) {
		return 0, d.limit(path, &d.pos)
	}
	return int(n), nil
}
func (d *decoder) value(f field, path string, observed []string) (value any, integerValue string, err error) {
	start := d.pos
	index, e := d.begin(path, f.kind, start)
	if e != nil {
		return nil, "", e
	}
	defer func() { err = d.finish(index, err) }()
	bits, unsigned, isInt := integer(f.kind)
	if isInt {
		if !f.derived() {
			if e = d.documentBudget(path, &start, 1, 3); e != nil {
				return nil, "", e
			}
		}
		if len(`,"value":"0"`) > d.plan.limits.MaxTraceBytes-d.traceBytes {
			return nil, "", d.limit(path, &start)
		}
		raw, e := d.read(bits/8, path, start)
		if e != nil {
			return nil, "", e
		}
		var n uint64
		for i := range raw {
			j := i
			if f.endian == "little" {
				j = len(raw) - 1 - i
			}
			n = n<<8 | uint64(raw[j])
		}
		s := strconv.FormatUint(n, 10)
		if !unsigned {
			if bits < 64 && n&(uint64(1)<<(bits-1)) != 0 {
				n |= ^uint64(0) << bits
			}
			s = strconv.FormatInt(int64(n), 10)
		}
		old := entrySize(d.trace.Entries[index])
		d.trace.Entries[index].Value = &s
		d.traceBytes += entrySize(d.trace.Entries[index]) - old
		if f.hasExpect && s != f.expect {
			return nil, "", d.diagnostic("constraint_mismatch", path, &start, start, "integer assertion satisfied", "integer assertion failed")
		}
		if !f.derived() {
			if e = d.documentBudget(path, &start, 0, len(s)-1); e != nil {
				return nil, "", e
			}
		}
		return s, s, nil
	}
	if f.kind == "repeat" {
		return d.repeat(f, path, start)
	}
	n, e := d.dimension(f, observed, path)
	if e != nil {
		return nil, "", e
	}
	if f.kind == "array" {
		// Each element needs an object node and braces; charge braces here, nodes
		// individually before each object allocation. No count-sized preallocation.
		bytes := 2 + 2*n
		if n > 0 {
			bytes += n - 1
		}
		if n > d.plan.limits.MaxValueNodes-d.nodes-1 {
			return nil, "", d.limit(path, &start)
		}
		if e = d.documentBudget(path, &start, 1, bytes); e != nil {
			return nil, "", e
		}
		a := []any{}
		for i := 0; i < n; i++ {
			obj, e := d.object(f.children, path+"/"+strconv.Itoa(i))
			if e != nil {
				return nil, "", e
			}
			a = append(a, obj)
		}
		return a, "", nil
	}
	minimum := n + 2
	if f.kind == "bytes" {
		minimum = 2*n + 2
	}
	if e = d.documentBudget(path, &start, 1, minimum); e != nil {
		return nil, "", e
	}
	raw, e := d.read(n, path, start)
	if e != nil {
		return nil, "", e
	}
	if f.kind == "bytes" {
		if f.hasExpect {
			for i, b := range raw {
				hi := hexDigit(f.expect[2*i])
				lo := hexDigit(f.expect[2*i+1])
				if b != hi<<4|lo {
					return nil, "", d.diagnostic("constraint_mismatch", path, &start, start+int64(i), "byte assertion satisfied", "byte assertion failed")
				}
			}
		}
		return hex.EncodeToString(raw), "", nil
	}
	bad := -1
	if f.encoding == "ascii" {
		for i, b := range raw {
			if b >= 128 {
				bad = i
				break
			}
		}
	} else {
		for i := 0; i < len(raw); {
			r, n := utf8.DecodeRune(raw[i:])
			if r == utf8.RuneError && n == 1 {
				bad = i
				break
			}
			i += n
		}
	}
	if bad >= 0 {
		return nil, "", d.diagnostic("invalid_encoding", path, &start, start+int64(bad), "valid declared string encoding", "invalid string encoding")
	}
	// Count JSON escapes before converting the input buffer to an owned string.
	extra := jsonStringBytes(raw) - minimum
	if e = d.documentBudget(path, &start, 0, extra); e != nil {
		return nil, "", e
	}
	return string(raw), "", nil
}

// repeat decodes elements until the input ends (end_of_input) or the next
// bytes no longer equal the element's leading tag (tag_mismatch). Elements are
// charged to the same budgets as array elements, one at a time.
func (d *decoder) repeat(f field, path string, start int64) (any, string, error) {
	if e := d.documentBudget(path, &start, 1, 2); e != nil {
		return nil, "", e
	}
	a := []any{}
	for {
		if e := d.cancel(path, &start); e != nil {
			return nil, "", e
		}
		more := false
		if f.until == untilEnd {
			next, e := d.peek(1, path, start)
			if e != nil {
				return nil, "", e
			}
			more = len(next) == 1
		} else {
			next, e := d.peek(len(f.tag), path, start)
			if e != nil {
				return nil, "", e
			}
			more = bytes.Equal(next, f.tag)
		}
		if !more {
			return a, "", nil
		}
		elementPath := path + "/" + strconv.Itoa(len(a))
		if len(a) >= d.plan.limits.MaxArrayElements {
			return nil, "", d.limit(elementPath, &d.pos)
		}
		separator := 2
		if len(a) > 0 {
			separator++
		}
		if e := d.documentBudget(elementPath, &d.pos, 0, separator); e != nil {
			return nil, "", e
		}
		before := d.pos
		obj, e := d.object(f.children, elementPath)
		if e != nil {
			return nil, "", e
		}
		if d.pos == before {
			// An element that reads nothing would repeat forever and can't round-trip.
			return nil, "", d.diagnostic("constraint_mismatch", elementPath, &before, before, "repeat element reads at least one byte", "empty repeat element")
		}
		a = append(a, obj)
	}
}
func hexDigit(b byte) byte {
	if b <= '9' {
		return b - '0'
	}
	return b - 'a' + 10
}
func (d *decoder) read(n int, path string, start int64) ([]byte, error) {
	if int64(n) > int64(d.plan.limits.MaxInputBytes)-d.pos {
		return nil, d.limit(path, &start)
	}
	if e := d.cancel(path, &start); e != nil {
		return nil, e
	}
	raw := make([]byte, n)
	done := copy(raw, d.pending)
	d.pending = d.pending[done:]
	d.pos += int64(done)
	empty := 0
	for done < n {
		if e := d.cancel(path, &start); e != nil {
			return nil, e
		}
		if d.eof {
			return nil, d.diagnostic("truncated_input", path, &start, d.pos, fmt.Sprintf("%d bytes", n), fmt.Sprintf("%d bytes", done))
		}
		got, e := d.reader.Read(raw[done:])
		if got < 0 || got > n-done {
			return nil, d.diagnostic("read_failed", path, &start, d.pos, "valid reader result", "invalid reader count")
		}
		done += got
		d.pos += int64(got)
		if e == io.EOF {
			d.eof = true
		}
		if e == io.ErrUnexpectedEOF {
			return nil, d.diagnostic("truncated_input", path, &start, d.pos, fmt.Sprintf("%d bytes", n), fmt.Sprintf("%d bytes", done))
		}
		if e != nil && e != io.EOF {
			return nil, d.diagnostic("read_failed", path, &start, d.pos, "readable input", "reader error")
		}
		if done == n {
			break
		}
		if e == io.EOF {
			return nil, d.diagnostic("truncated_input", path, &start, d.pos, fmt.Sprintf("%d bytes", n), fmt.Sprintf("%d bytes", done))
		}
		if got == 0 {
			empty++
			if empty >= 100 {
				return nil, d.diagnostic("read_failed", path, &start, d.pos, "reader progress", "reader made no progress")
			}
		} else {
			empty = 0
		}
	}
	if e := d.cancel(path, &start); e != nil {
		return nil, e
	}
	return raw, nil
}

// peek returns up to n upcoming bytes without consuming them. Fewer than n
// bytes means the input ends first. Peeked bytes stay in d.pending and are
// served to the next read, so offsets and traces are unaffected.
func (d *decoder) peek(n int, path string, start int64) ([]byte, error) {
	// Never look further than one byte past the input ceiling, like probe.
	if room := int64(d.plan.limits.MaxInputBytes) + 1 - d.pos; int64(n) > room {
		n = int(room)
	}
	empty := 0
	for len(d.pending) < n && !d.eof {
		if e := d.cancel(path, &start); e != nil {
			return nil, e
		}
		buf := make([]byte, n-len(d.pending))
		got, e := d.reader.Read(buf)
		if got < 0 || got > len(buf) {
			return nil, d.diagnostic("read_failed", path, &start, d.pos, "valid reader result", "invalid reader count")
		}
		d.pending = append(d.pending, buf[:got]...)
		if e == io.ErrUnexpectedEOF {
			return nil, d.diagnostic("truncated_input", path, &start, d.pos+int64(len(d.pending)), "end of input", "unexpected EOF")
		}
		if e == io.EOF {
			d.eof = true
		} else if e != nil {
			return nil, d.diagnostic("read_failed", path, &start, d.pos, "readable input", "reader error")
		}
		if got == 0 && !d.eof {
			empty++
			if empty >= 100 {
				return nil, d.diagnostic("read_failed", path, &start, d.pos, "reader progress", "reader made no progress")
			}
		} else {
			empty = 0
		}
	}
	if len(d.pending) < n {
		return d.pending, nil
	}
	return d.pending[:n], nil
}
func (d *decoder) probe() error {
	if len(d.pending) > 0 {
		if d.pos+int64(len(d.pending)) > int64(d.plan.limits.MaxInputBytes) {
			return d.limit("", nil)
		}
		return d.diagnostic("trailing_data", "", nil, d.pos, "end of input", "additional byte")
	}
	var buf [1]byte
	for empty := 0; empty < 100; empty++ {
		if e := d.cancel("", nil); e != nil {
			return e
		}
		if d.eof {
			return nil
		}
		n, e := d.reader.Read(buf[:])
		if n < 0 || n > 1 {
			return d.diagnostic("read_failed", "", nil, d.pos, "valid reader result", "invalid reader count")
		}
		if e == io.ErrUnexpectedEOF {
			return d.diagnostic("truncated_input", "", nil, d.pos, "end of input", "unexpected EOF")
		}
		if e != nil && e != io.EOF {
			return d.diagnostic("read_failed", "", nil, d.pos, "readable input", "reader error")
		}
		if n > 0 {
			if d.pos >= int64(d.plan.limits.MaxInputBytes) {
				return d.limit("", nil)
			}
			return d.diagnostic("trailing_data", "", nil, d.pos, "end of input", "additional byte")
		}
		if e == io.EOF {
			return nil
		}
	}
	return d.diagnostic("read_failed", "", nil, d.pos, "reader progress", "reader made no progress")
}

// These sizes match compact encoding/json output (including HTML escaping),
// without first allocating serialized documents or content previews.
func jsonStringBytes(raw []byte) int {
	n := 2
	for i := 0; i < len(raw); {
		b := raw[i]
		if b < utf8.RuneSelf {
			switch b {
			case '"', '\\', '\b', '\f', '\n', '\r', '\t':
				n += 2
			case '<', '>', '&':
				n += 6
			default:
				if b < 32 {
					n += 6
				} else {
					n++
				}
			}
			i++
			continue
		}
		r, k := utf8.DecodeRune(raw[i:])
		if r == 0x2028 || r == 0x2029 {
			n += 6
		} else {
			n += k
		}
		i += k
	}
	return n
}
func entrySize(e TraceEntry) int {
	n := len(`{"path":`) + jsonStringBytes([]byte(e.Path)) + len(`,"kind":`) + jsonStringBytes([]byte(e.Kind)) + len(`,"offset":`) + len(strconv.FormatInt(e.Offset, 10)) + len(`,"length":`) + len(strconv.FormatInt(e.Length, 10)) + len(`,"state":`) + jsonStringBytes([]byte(e.State)) + 1
	if e.Value != nil {
		n += len(`,"value":`) + jsonStringBytes([]byte(*e.Value))
	}
	return n
}
func traceSize(t *Trace) int {
	n := len(`{"trace_version":1,"schema_sha256":`) + jsonStringBytes([]byte(t.SchemaSHA256)) + len(`,"state":`) + jsonStringBytes([]byte(t.State)) + len(`,"entries":[]`) + len(`,"diagnostic":`) + 1
	for i, e := range t.Entries {
		n += entrySize(e)
		if i > 0 {
			n++
		}
	}
	if t.Diagnostic == nil {
		return n + 4
	}
	d := t.Diagnostic
	n += len(`{"diagnostic_version":1,"stage":`) + jsonStringBytes([]byte(d.Stage)) + len(`,"code":`) + jsonStringBytes([]byte(d.Code)) + len(`,"path":`) + jsonStringBytes([]byte(d.Path)) + len(`,"offset":`) + len(`,"field_start":`) + len(`,"expected":`) + jsonStringBytes([]byte(d.Expected)) + len(`,"observed":`) + jsonStringBytes([]byte(d.Observed)) + 1
	for _, p := range []*int64{d.Offset, d.FieldStart} {
		if p == nil {
			n += 4
		} else {
			n += len(strconv.FormatInt(*p, 10))
		}
	}
	return n
}
