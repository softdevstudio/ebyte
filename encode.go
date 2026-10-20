package ebyte

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

func encodeFailure(code, path string) error {
	expected, observed := "schema-matching editable values", "invalid document representation"
	switch code {
	case "schema_mismatch":
		expected, observed = "matching exact schema identity", "different schema identity"
	case "value_out_of_range":
		expected, observed = "value within declared type or byte width", "value outside declared range"
	case "count_mismatch":
		expected, observed = "array length equals declared count", "array length and count differ"
	case "constraint_mismatch":
		expected, observed = "declared assertion satisfied", "assertion failed"
	case "limit_exceeded":
		expected, observed = "within configured resource ceilings", "resource ceiling exceeded"
	case "cancelled":
		expected, observed = "active operation", "context cancelled"
	}
	return &Diagnostic{DiagnosticVersion: 1, Stage: "encode", Code: code, Path: path, Expected: expected, Observed: observed}
}
func encodePosition(code, path string, start, offset int) error {
	d := encodeFailure(code, path).(*Diagnostic)
	begin, at := int64(start), int64(offset)
	d.FieldStart = &begin
	d.Offset = &at
	return d
}
func encodeCancel(ctx context.Context, path string) error {
	if ctx.Err() != nil {
		return encodeFailure("cancelled", path)
	}
	return nil
}
func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := range s {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

// LoadDocument strictly parses and validates an editable JSON document against
// this plan. The raw JSON byte limit includes whitespace. No output is written.
func (p *Plan) LoadDocument(ctx context.Context, raw []byte) (*Document, error) {
	if e := encodeCancel(ctx, ""); e != nil {
		return nil, e
	}
	if len(raw) > p.limits.MaxDocumentBytes {
		return nil, encodeFailure("limit_exceeded", "")
	}
	if !utf8.Valid(raw) || !validEscapes(raw) {
		return nil, encodeFailure("invalid_document", "")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return nil, encodeFailure("invalid_document", "")
	}
	doc := &Document{}
	seen := map[string]bool{}
	loader := documentLoader{plan: p, ctx: ctx, decoder: d}
	for d.More() {
		if e = encodeCancel(ctx, ""); e != nil {
			return nil, e
		}
		keyToken, e := d.Token()
		key, ok := keyToken.(string)
		if e != nil || !ok {
			return nil, encodeFailure("invalid_document", "")
		}
		switch key {
		case "document_version", "schema_sha256", "values":
		default:
			return nil, encodeFailure("invalid_document", "")
		}
		path := "/" + key
		if seen[key] {
			return nil, encodeFailure("invalid_document", path)
		}
		seen[key] = true
		if key == "values" {
			doc.Values, e = loader.object(p.fields, "")
			if e != nil {
				return nil, e
			}
			continue
		}
		v, e := d.Token()
		if e != nil {
			return nil, encodeFailure("invalid_document", path)
		}
		if key == "document_version" {
			n, ok := v.(json.Number)
			if !ok || n.String() != "1" {
				return nil, encodeFailure("invalid_document", path)
			}
			doc.DocumentVersion = 1
		} else {
			s, ok := v.(string)
			if !ok || !validDigest(s) {
				return nil, encodeFailure("invalid_document", path)
			}
			doc.SchemaSHA256 = s
		}
	}
	tok, e = d.Token()
	if e != nil || tok != json.Delim('}') {
		return nil, encodeFailure("invalid_document", "")
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, encodeFailure("invalid_document", "")
	}
	for _, key := range []string{"document_version", "schema_sha256", "values"} {
		if !seen[key] {
			return nil, encodeFailure("invalid_document", "/"+key)
		}
	}
	if _, e = p.prepare(ctx, doc); e != nil {
		return nil, e
	}
	return doc, nil
}

type documentLoader struct {
	plan    *Plan
	ctx     context.Context
	decoder *json.Decoder
	nodes   int
}

func (l *documentLoader) node(path string) error {
	if e := encodeCancel(l.ctx, path); e != nil {
		return e
	}
	if l.nodes >= l.plan.limits.MaxValueNodes {
		return encodeFailure("limit_exceeded", path)
	}
	l.nodes++
	return nil
}
func (l *documentLoader) object(fields []field, path string) (map[string]any, error) {
	if e := l.node(path); e != nil {
		return nil, e
	}
	tok, e := l.decoder.Token()
	if e != nil || tok != json.Delim('{') {
		return nil, encodeFailure("invalid_document", path)
	}
	lookup := make(map[string]field, len(fields))
	for _, f := range fields {
		lookup[f.name] = f
	}
	m := map[string]any{}
	for l.decoder.More() {
		tok, e := l.decoder.Token()
		key, ok := tok.(string)
		if e != nil || !ok {
			return nil, encodeFailure("invalid_document", path)
		}
		f, known := lookup[key]
		if !known {
			return nil, encodeFailure("invalid_document", path)
		}
		childPath := path + "/" + key
		if _, exists := m[key]; exists || f.derive >= 0 {
			return nil, encodeFailure("invalid_document", childPath)
		}
		value, e := l.value(f, childPath)
		if e != nil {
			return nil, e
		}
		m[key] = value
	}
	tok, e = l.decoder.Token()
	if e != nil || tok != json.Delim('}') {
		return nil, encodeFailure("invalid_document", path)
	}
	return m, nil
}
func (l *documentLoader) value(f field, path string) (any, error) {
	if e := l.node(path); e != nil {
		return nil, e
	}
	tok, e := l.decoder.Token()
	if e != nil {
		return nil, encodeFailure("invalid_document", path)
	}
	if f.kind != "array" && f.kind != "repeat" {
		s, ok := tok.(string)
		if !ok {
			return nil, encodeFailure("invalid_document", path)
		}
		return s, nil
	}
	if tok != json.Delim('[') {
		return nil, encodeFailure("invalid_document", path)
	}
	a := []any{}
	for l.decoder.More() {
		if len(a) >= l.plan.limits.MaxArrayElements {
			return nil, encodeFailure("limit_exceeded", path)
		}
		v, e := l.object(f.children, path+"/"+strconv.Itoa(len(a)))
		if e != nil {
			return nil, e
		}
		a = append(a, v)
	}
	tok, e = l.decoder.Token()
	if e != nil || tok != json.Delim(']') {
		return nil, encodeFailure("invalid_document", path)
	}
	return a, nil
}

// Encode validates the entire document and prepares bounded binary output before
// its first write. A writer failure or cancellation during Write can leave partial
// output; callers must discard it. Do not mutate doc concurrently with this call.
func (p *Plan) Encode(ctx context.Context, doc *Document, w io.Writer) error {
	raw, e := p.prepare(ctx, doc)
	if e != nil {
		return e
	}
	if e = encodeCancel(ctx, ""); e != nil {
		return e
	}
	if len(raw) > 0 {
		n, err := w.Write(raw)
		var offset *int64
		if n >= 0 && n <= len(raw) {
			at := int64(n)
			offset = &at
		}
		if err != nil || n != len(raw) {
			return &Diagnostic{DiagnosticVersion: 1, Stage: "io", Code: "write_failed", Offset: offset, Expected: "complete binary output", Observed: "write failed; partial output may remain"}
		}
	}
	if ctx.Err() != nil {
		offset := int64(len(raw))
		return &Diagnostic{DiagnosticVersion: 1, Stage: "encode", Code: "cancelled", Offset: &offset, Expected: "active operation", Observed: "context cancelled; output may already be written"}
	}
	return nil
}

type encoder struct {
	plan            *Plan
	ctx             context.Context
	nodes, docBytes int
	out             []byte
	// tagEnds records where each tag_mismatch repeat ended, so prepare can
	// reject output whose next bytes would read as one more element.
	tagEnds []tagEnd
}

type tagEnd struct {
	path   string
	offset int
	tag    []byte
}

func (p *Plan) prepare(ctx context.Context, doc *Document) ([]byte, error) {
	if e := encodeCancel(ctx, ""); e != nil {
		return nil, e
	}
	if doc == nil || doc.DocumentVersion != 1 || doc.Values == nil {
		return nil, encodeFailure("invalid_document", "")
	}
	if !validDigest(doc.SchemaSHA256) {
		return nil, encodeFailure("invalid_document", "/schema_sha256")
	}
	if doc.SchemaSHA256 != p.digest {
		return nil, encodeFailure("schema_mismatch", "/schema_sha256")
	}
	enc := encoder{plan: p, ctx: ctx, docBytes: len(`{"document_version":1,"schema_sha256":"`) + len(p.digest) + len(`","values":`) + 1, out: []byte{}}
	if e := enc.object(p.fields, doc.Values, ""); e != nil {
		return nil, e
	}
	for _, t := range enc.tagEnds {
		if bytes.HasPrefix(enc.out[t.offset:], t.tag) {
			d := encodePosition("constraint_mismatch", t.path, t.offset, t.offset).(*Diagnostic)
			d.Expected, d.Observed = "bytes after a repeat differ from its tag", "following bytes repeat the tag"
			return nil, d
		}
	}
	return enc.out, nil
}
func (e *encoder) budget(path string, nodes, size int) error {
	if err := encodeCancel(e.ctx, path); err != nil {
		return err
	}
	if nodes > e.plan.limits.MaxValueNodes-e.nodes || size > e.plan.limits.MaxDocumentBytes-e.docBytes {
		return encodeFailure("limit_exceeded", path)
	}
	e.nodes += nodes
	e.docBytes += size
	return nil
}
func (e *encoder) stringBudget(path, s string) error {
	if len(s) > e.plan.limits.MaxDocumentBytes-e.docBytes-2 {
		return encodeFailure("limit_exceeded", path)
	}
	return e.budget(path, 1, jsonStringBytes([]byte(s)))
}
func (e *encoder) output(path string, size int) error {
	if size > e.plan.limits.MaxOutputBytes-len(e.out) {
		return encodeFailure("limit_exceeded", path)
	}
	return nil
}
func (e *encoder) object(fields []field, m map[string]any, path string) error {
	if m == nil {
		return encodeFailure("invalid_document", path)
	}
	if err := e.budget(path, 1, 2); err != nil {
		return err
	}
	names := make(map[string]bool, len(fields))
	for _, f := range fields {
		names[f.name] = true
	}
	for name := range m {
		if !names[name] {
			return encodeFailure("invalid_document", path)
		}
	}
	observed := make([]string, len(fields))
	members := 0
	for i, f := range fields {
		p := path + "/" + f.name
		if err := encodeCancel(e.ctx, p); err != nil {
			return err
		}
		v, present := m[f.name]
		if f.derive >= 0 {
			if present {
				return encodeFailure("invalid_document", p)
			}
			target := fields[f.derive].name
			a, ok := m[target].([]any)
			if !ok || a == nil {
				return encodeFailure("invalid_document", path+"/"+target)
			}
			s := strconv.Itoa(len(a))
			if !fits(s, f.kind) {
				return encodeFailure("value_out_of_range", p)
			}
			if err := e.integer(f, s, p); err != nil {
				return err
			}
			observed[i] = s
			continue
		}
		if f.when != nil && observed[f.when.ref] != f.when.equals {
			if present {
				return encodeFailure("invalid_document", p)
			}
			continue
		}
		if !present || v == nil {
			return encodeFailure("invalid_document", p)
		}
		size := len(f.name) + 3
		if members > 0 {
			size++
		}
		members++
		if err := e.budget(p, 0, size); err != nil {
			return err
		}
		if _, _, isInt := integer(f.kind); isInt {
			s, ok := v.(string)
			if !ok {
				return encodeFailure("invalid_document", p)
			}
			if err := e.stringBudget(p, s); err != nil {
				return err
			}
			if err := e.integer(f, s, p); err != nil {
				return err
			}
			observed[i] = s
			continue
		}
		if f.kind == "repeat" {
			if err := e.repeat(f, v, p); err != nil {
				return err
			}
			continue
		}
		n, err := e.dimension(f, observed, p)
		if err != nil {
			return err
		}
		if f.kind == "array" {
			a, ok := v.([]any)
			if !ok || a == nil {
				return encodeFailure("invalid_document", p)
			}
			if len(a) != n {
				return encodeFailure("count_mismatch", p)
			}
			if n > e.plan.limits.MaxValueNodes-e.nodes-1 {
				return encodeFailure("limit_exceeded", p)
			}
			size := 2
			if n > 0 {
				size += n - 1
			}
			if err := e.budget(p, 1, size); err != nil {
				return err
			}
			for j, item := range a {
				child := p + "/" + strconv.Itoa(j)
				obj, ok := item.(map[string]any)
				if !ok {
					return encodeFailure("invalid_document", child)
				}
				if err := e.object(f.children, obj, child); err != nil {
					return err
				}
			}
			continue
		}
		s, ok := v.(string)
		if !ok || !utf8.ValidString(s) {
			return encodeFailure("invalid_document", p)
		}
		if err := e.stringBudget(p, s); err != nil {
			return err
		}
		if f.kind == "bytes" {
			if len(s)%2 != 0 {
				return encodeFailure("invalid_document", p)
			}
			for j := range s {
				b := s[j]
				if !(b >= '0' && b <= '9' || b >= 'a' && b <= 'f') {
					return encodeFailure("invalid_document", p)
				}
			}
			if len(s)/2 != n {
				return encodeFailure("value_out_of_range", p)
			}
			if f.hasExpect && s != f.expect {
				index := 0
				for s[index] == f.expect[index] {
					index++
				}
				return encodePosition("constraint_mismatch", p, len(e.out), len(e.out)+index/2)
			}
			if err := e.output(p, n); err != nil {
				return err
			}
			start := len(e.out)
			e.out = append(e.out, make([]byte, n)...)
			_, _ = hex.Decode(e.out[start:], []byte(s))
		} else {
			if len(s) != n {
				return encodeFailure("value_out_of_range", p)
			}
			if f.encoding == "ascii" {
				for j := range s {
					if s[j] >= 128 {
						return encodePosition("invalid_encoding", p, len(e.out), len(e.out)+j)
					}
				}
			}
			if err := e.output(p, n); err != nil {
				return err
			}
			e.out = append(e.out, s...)
		}
	}
	return nil
}

// repeat encodes every element in order. Each element must produce at least one
// byte, matching the decoder, which never reads an empty element.
func (e *encoder) repeat(f field, v any, path string) error {
	a, ok := v.([]any)
	if !ok || a == nil {
		return encodeFailure("invalid_document", path)
	}
	if len(a) > e.plan.limits.MaxArrayElements || len(a) > e.plan.limits.MaxValueNodes-e.nodes-1 {
		return encodeFailure("limit_exceeded", path)
	}
	size := 2
	if len(a) > 0 {
		size += len(a) - 1
	}
	if err := e.budget(path, 1, size); err != nil {
		return err
	}
	for j, item := range a {
		child := path + "/" + strconv.Itoa(j)
		obj, ok := item.(map[string]any)
		if !ok {
			return encodeFailure("invalid_document", child)
		}
		before := len(e.out)
		if err := e.object(f.children, obj, child); err != nil {
			return err
		}
		if len(e.out) == before {
			d := encodeFailure("value_out_of_range", child).(*Diagnostic)
			d.Expected, d.Observed = "repeat element encodes at least one byte", "empty repeat element"
			return d
		}
	}
	if f.until == untilTag {
		e.tagEnds = append(e.tagEnds, tagEnd{path, len(e.out), f.tag})
	}
	return nil
}
func (e *encoder) dimension(f field, observed []string, path string) (int, error) {
	n := f.length.literal
	if f.length.dynamic {
		s := observed[f.length.ref]
		if len(s) > 0 && s[0] == '-' {
			return 0, encodeFailure("value_out_of_range", path)
		}
		var err error
		n, err = strconv.ParseUint(s, 10, 64)
		if err != nil {
			return 0, encodeFailure("limit_exceeded", path)
		}
	}
	cap := e.plan.limits.MaxFieldBytes
	if f.kind == "array" {
		cap = e.plan.limits.MaxArrayElements
	}
	if n > uint64(cap) {
		return 0, encodeFailure("limit_exceeded", path)
	}
	return int(n), nil
}
func (e *encoder) integer(f field, s, path string) error {
	if !decimal.MatchString(s) {
		return encodeFailure("invalid_document", path)
	}
	if !fits(s, f.kind) {
		return encodeFailure("value_out_of_range", path)
	}
	if f.hasExpect && s != f.expect {
		return encodePosition("constraint_mismatch", path, len(e.out), len(e.out))
	}
	bits, unsigned, _ := integer(f.kind)
	var n uint64
	if unsigned {
		n, _ = strconv.ParseUint(s, 10, bits)
	} else {
		signed, _ := strconv.ParseInt(s, 10, bits)
		n = uint64(signed)
	}
	size := bits / 8
	if err := e.output(path, size); err != nil {
		return err
	}
	var raw [8]byte
	for i := 0; i < size; i++ {
		j := i
		if f.endian == "big" {
			j = size - 1 - i
		}
		raw[j] = byte(n)
		n >>= 8
	}
	e.out = append(e.out, raw[:size]...)
	return nil
}
