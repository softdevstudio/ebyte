package ebyte

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func documentFixture(t *testing.T, name string, p *Plan) *Document {
	t.Helper()
	raw, e := os.ReadFile("testdata/" + name + ".document.json")
	if e != nil {
		t.Fatal(e)
	}
	doc, e := p.LoadDocument(context.Background(), raw)
	if e != nil {
		t.Fatal(e)
	}
	return doc
}
func TestIndependentEncodeGoldensAndRoundTrips(t *testing.T) {
	for _, name := range []string{"packet", "wide", "empty", "stored"} {
		t.Run(name, func(t *testing.T) {
			p := fixturePlan(t, name, DefaultLimits())
			doc := documentFixture(t, name, p)
			var out bytes.Buffer
			if e := p.Encode(context.Background(), doc, &out); e != nil {
				t.Fatal(e)
			}
			want := fixtureBytes(t, name)
			if !bytes.Equal(out.Bytes(), want) {
				t.Fatalf("encoded %x want %x", out.Bytes(), want)
			}
			decoded, _, e := p.Decode(context.Background(), bytes.NewReader(want))
			if e != nil {
				t.Fatal(e)
			}
			raw, e := json.Marshal(decoded)
			if e != nil {
				t.Fatal(e)
			}
			loaded, e := p.LoadDocument(context.Background(), raw)
			if e != nil {
				t.Fatal(e)
			}
			out.Reset()
			if e = p.Encode(context.Background(), loaded, &out); e != nil || !bytes.Equal(out.Bytes(), want) {
				t.Fatalf("round trip: %x %v", out.Bytes(), e)
			}
		})
	}
}
func TestEncodeAcceptanceVectors(t *testing.T) {
	raw, e := os.ReadFile("testdata/cases.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Name, Operation, Schema, Hex string
		Values                       map[string]any
		Error                        *struct {
			Code, Path string
			Offset     *int64
			FieldStart *int64 `json:"field_start"`
		}
	}
	if e = json.Unmarshal(raw, &cases); e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, tc := range cases {
		if tc.Operation != "encode" {
			continue
		}
		count++
		t.Run(tc.Name, func(t *testing.T) {
			p := fixturePlan(t, tc.Schema, DefaultLimits())
			doc := &Document{1, p.digest, tc.Values}
			var out bytes.Buffer
			e := p.Encode(context.Background(), doc, &out)
			if tc.Error == nil {
				want, _ := hex.DecodeString(tc.Hex)
				if e != nil || !bytes.Equal(out.Bytes(), want) {
					t.Fatalf("got %x %v", out.Bytes(), e)
				}
				return
			}
			d, ok := e.(*Diagnostic)
			if !ok || d.Code != tc.Error.Code || d.Path != tc.Error.Path || d.Offset != nil || d.FieldStart != nil || out.Len() != 0 {
				t.Fatalf("got %#v", e)
			}
		})
	}
	if count != 3 {
		t.Fatal("encode vector coverage")
	}
}
func TestDocumentStrictJSON(t *testing.T) {
	p := fixturePlan(t, "empty", DefaultLimits())
	valid := `{"document_version":1,"schema_sha256":"` + p.digest + `","values":{}}`
	for _, raw := range []string{
		strings.Replace(valid, `"document_version":1`, `"document_version":1.0`, 1),
		strings.Replace(valid, `"values":{}`, `"values":null`, 1),
		strings.Replace(valid, `"values":{}`, `"values":{},"values":{}`, 1),
		strings.Replace(valid, `"values":{}`, `"values":{},"va\u006cues":{}`, 1),
		strings.Replace(valid, `"values":{}`, `"values":{},"unknown":"secret"`, 1),
		strings.Replace(valid, `"values":{}`, `"values":{"unknown":"secret"}`, 1),
		strings.Replace(valid, `"values":{}`, `"values":{"bad":"\ud800"}`, 1),
		strings.Replace(valid, `"values":{}`, `"values":{"bad":"\udc00"}`, 1),
		strings.Replace(valid, `"schema_sha256":"`+p.digest+`"`, `"schema_sha256":null`, 1),
		valid + ` {}`, "\xef\xbb\xbf" + valid, "\xff" + valid, `{}`, `[]`,
	} {
		doc, e := p.LoadDocument(context.Background(), []byte(raw))
		d, ok := e.(*Diagnostic)
		if !ok || d.Code != "invalid_document" || doc != nil || d.Offset != nil || d.FieldStart != nil || strings.Contains(d.Error(), "secret") {
			t.Fatalf("accepted or leaked %q: %#v", raw, e)
		}
	}
	changed := strings.Replace(valid, p.digest, strings.Repeat("0", 64), 1)
	if _, e := p.LoadDocument(context.Background(), []byte(changed)); e.(*Diagnostic).Code != "schema_mismatch" {
		t.Fatal(e)
	}
	// Envelope order and value order do not carry meaning.
	reordered := `{"values":{},"schema_sha256":"` + p.digest + `","document_version":1}`
	if _, e := p.LoadDocument(context.Background(), []byte(reordered)); e != nil {
		t.Fatal(e)
	}
}
func TestEncodeRejectsEditsBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name, code, path string
		edit             func(*Document)
	}{
		{"derived present", "invalid_document", "/record_count", func(d *Document) { d.Values["record_count"] = "2" }},
		{"inactive present", "invalid_document", "/note", func(d *Document) { d.Values["flags"] = "0" }},
		{"missing active", "invalid_document", "/label", func(d *Document) { delete(d.Values, "label") }},
		{"null active", "invalid_document", "/label", func(d *Document) { d.Values["label"] = nil }},
		{"unknown field", "invalid_document", "", func(d *Document) { d.Values["secret"] = "private" }},
		{"numeric integer", "invalid_document", "/version", func(d *Document) { d.Values["version"] = float64(1) }},
		{"negative zero", "invalid_document", "/version", func(d *Document) { d.Values["version"] = "-0" }},
		{"integer overflow", "value_out_of_range", "/flags", func(d *Document) { d.Values["flags"] = "256" }},
		{"negative unsigned", "value_out_of_range", "/flags", func(d *Document) { d.Values["flags"] = "-1" }},
		{"integer assertion", "constraint_mismatch", "/version", func(d *Document) { d.Values["version"] = "2" }},
		{"byte assertion", "constraint_mismatch", "/magic", func(d *Document) { d.Values["magic"] = "4543" }},
		{"uppercase hex", "invalid_document", "/magic", func(d *Document) { d.Values["magic"] = "454B" }},
		{"odd hex", "invalid_document", "/reserved", func(d *Document) { d.Values["reserved"] = "aaa" }},
		{"invalid hex", "invalid_document", "/reserved", func(d *Document) { d.Values["reserved"] = "zzzz" }},
		{"wrong width", "value_out_of_range", "/label", func(d *Document) { d.Values["label"] = "short" }},
		{"invalid ASCII", "invalid_encoding", "/label", func(d *Document) { d.Values["label"] = "éaa" }},
		{"invalid UTF8", "invalid_document", "/label", func(d *Document) { d.Values["label"] = "\xffaaa" }},
		{"bad element", "invalid_document", "/records/1", func(d *Document) { d.Values["records"].([]any)[1] = "bad" }},
		{"array null", "invalid_document", "/records", func(d *Document) { d.Values["records"] = []any(nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixturePlan(t, "packet", DefaultLimits())
			d := documentFixture(t, "packet", p)
			tc.edit(d)
			w := &countWriter{}
			e := p.Encode(context.Background(), d, w)
			diagnostic, ok := e.(*Diagnostic)
			if !ok || diagnostic.Code != tc.code || diagnostic.Path != tc.path || w.calls != 0 {
				t.Fatalf("got %#v writes %d", e, w.calls)
			}
		})
	}
}

type countWriter struct{ calls int }

func (w *countWriter) Write(p []byte) (int, error) { w.calls++; return len(p), nil }

type failingWriter struct {
	n     int
	err   error
	calls int
}

func (w *failingWriter) Write(p []byte) (int, error) { w.calls++; return w.n, w.err }

type cancellingWriter struct {
	cancel  context.CancelFunc
	written int
}

func (w *cancellingWriter) Write(p []byte) (int, error) {
	w.written = len(p)
	w.cancel()
	return len(p), nil
}
func TestEncodeWriterErrorsAndCancellation(t *testing.T) {
	p := fixturePlan(t, "packet", DefaultLimits())
	doc := documentFixture(t, "packet", p)
	for _, tc := range []struct {
		n   int
		err error
	}{{0, nil}, {5, nil}, {5, errors.New("secret write error")}, {21, errors.New("flush failure")}, {-1, nil}, {22, nil}} {
		w := &failingWriter{n: tc.n, err: tc.err}
		e := p.Encode(context.Background(), doc, w)
		d, ok := e.(*Diagnostic)
		if !ok || d.Stage != "io" || d.Code != "write_failed" || w.calls != 1 || strings.Contains(d.Error(), "secret") {
			t.Fatalf("got %#v", e)
		}
		if tc.n < 0 || tc.n > 21 {
			if d.Offset != nil {
				t.Fatal("unknown write offset fabricated")
			}
		} else if d.Offset == nil || *d.Offset != int64(tc.n) {
			t.Fatal("write offset")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := &countWriter{}
	if e := p.Encode(ctx, doc, w); e.(*Diagnostic).Code != "cancelled" || w.calls != 0 {
		t.Fatal("pre-cancel write")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	cw := &cancellingWriter{cancel: cancel}
	if e := p.Encode(ctx, doc, cw); e.(*Diagnostic).Code != "cancelled" || cw.written != 21 {
		t.Fatal("post-write cancellation")
	}
}
func TestEncodeBudgets(t *testing.T) {
	p := fixturePlan(t, "packet", DefaultLimits())
	doc := documentFixture(t, "packet", p)
	compact, _ := json.Marshal(doc)
	for _, mode := range []string{"output", "nodes", "document"} {
		for _, delta := range []int{0, -1} {
			l := DefaultLimits()
			switch mode {
			case "output":
				l.MaxOutputBytes = 21 + delta
			case "nodes":
				l.MaxValueNodes = 14 + delta
			case "document":
				l.MaxDocumentBytes = len(compact) + delta
			}
			p = fixturePlan(t, "packet", l)
			w := &countWriter{}
			e := p.Encode(context.Background(), doc, w)
			if delta == 0 {
				if e != nil || w.calls != 1 {
					t.Fatalf("%s exact %v", mode, e)
				}
				if _, e = p.LoadDocument(context.Background(), compact); e != nil {
					t.Fatalf("%s load exact %v", mode, e)
				}
			} else if e == nil || e.(*Diagnostic).Code != "limit_exceeded" || w.calls != 0 {
				t.Fatalf("%s short %v", mode, e)
			}
		}
	}
	// Raw JSON whitespace is charged independently of compact in-memory values.
	l := DefaultLimits()
	l.MaxDocumentBytes = len(compact)
	p = fixturePlan(t, "packet", l)
	if _, e := p.LoadDocument(context.Background(), append(compact, ' ')); e.(*Diagnostic).Code != "limit_exceeded" {
		t.Fatal(e)
	}
}
func TestCountDimensionsAndNestedRoundTrip(t *testing.T) {
	fields := `[{"name":"n","type":"u8"},{"name":"items","type":"array","count":{"field":"n"},"fields":[{"name":"length","type":"u8"},{"name":"raw","type":"bytes","length":{"field":"length"}},{"name":"nested","type":"array","count":1,"fields":[{"name":"v","type":"i8"}]}]}]`
	p := planFor(t, fields, DefaultLimits())
	input := []byte{2, 2, 0, 255, 128, 0, 127}
	doc, _, e := p.Decode(context.Background(), bytes.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e = p.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), input) {
		t.Fatalf("nested %x %v", out.Bytes(), e)
	}
	doc.Values["n"] = "1"
	out.Reset()
	if e = p.Encode(context.Background(), doc, &out); e.(*Diagnostic).Code != "count_mismatch" || out.Len() != 0 {
		t.Fatal(e)
	}
	p = planFor(t, `[{"name":"n","type":"i8"},{"name":"raw","type":"bytes","length":{"field":"n"}}]`, DefaultLimits())
	doc = &Document{1, p.digest, map[string]any{"n": "-1", "raw": ""}}
	if e = p.Encode(context.Background(), doc, &out); e.(*Diagnostic).Code != "value_out_of_range" {
		t.Fatal(e)
	}
}
func TestDerivedCountOverflowAndZero(t *testing.T) {
	p := planFor(t, `[{"name":"n","type":"u8","derive":{"count_of":"items"}},{"name":"items","type":"array","count":{"field":"n"},"fields":[]}]`, DefaultLimits())
	for _, count := range []int{0, 255, 256} {
		a := make([]any, count)
		for i := range a {
			a[i] = map[string]any{}
		}
		doc := &Document{1, p.digest, map[string]any{"items": a}}
		var out bytes.Buffer
		e := p.Encode(context.Background(), doc, &out)
		if count == 256 {
			if e.(*Diagnostic).Code != "value_out_of_range" || out.Len() != 0 {
				t.Fatal(e)
			}
		} else if e != nil || !bytes.Equal(out.Bytes(), []byte{byte(count)}) {
			t.Fatal(e)
		}
	}
}
func TestLoaderNestedDuplicatesNullsAndNumbers(t *testing.T) {
	p := fixturePlan(t, "stored", DefaultLimits())
	prefix := `{"document_version":1,"schema_sha256":"` + p.digest + `","values":`
	for _, values := range []string{`{"count":"1","items":[{"value":"2","va\u006cue":"3"}]}`, `{"count":"1","items":[null]}`, `{"count":"1","items":[{"value":2}]}`, `{"count":"1","items":[{"value":null}]}`, `{"count":"1","items":[{"value":"+2"}]}`, `{"count":"1","items":[{"value":"02"}]}`, `{"count":"1","items":[{"value":"2.0"}]}`, `{"count":"1","items":[{"value":"2e0"}]}`} {
		if doc, e := p.LoadDocument(context.Background(), []byte(prefix+values+`}`)); e == nil || doc != nil {
			t.Fatal("invalid document accepted")
		}
	}
	l := DefaultLimits()
	l.MaxArrayElements = 0
	p = fixturePlan(t, "stored", l)
	if _, e := p.LoadDocument(context.Background(), []byte(prefix+`{"count":"1","items":[{"value":"2"}]}}`)); e.(*Diagnostic).Code != "limit_exceeded" {
		t.Fatal(e)
	}
}
func FuzzLoadAndEncode(f *testing.F) {
	raw, _ := os.ReadFile("testdata/packet.document.json")
	f.Add(raw)
	schemaBytes, _ := os.ReadFile("testdata/packet.schema.json")
	p, e := Compile(schemaBytes, DefaultLimits())
	if e != nil {
		f.Fatal(e)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		doc, e := p.LoadDocument(context.Background(), raw)
		if e != nil {
			if doc != nil {
				t.Fatal("failed load returned document")
			}
			return
		}
		var out bytes.Buffer
		if e = p.Encode(context.Background(), doc, &out); e != nil {
			t.Fatal(e)
		}
		decoded, _, e := p.Decode(context.Background(), bytes.NewReader(out.Bytes()))
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(decoded.Values, doc.Values) {
			t.Fatal("encoded values differ")
		}
	})
}
func FuzzLosslessPacket(f *testing.F) {
	raw, _ := os.ReadFile("testdata/packet.hex")
	seed, _ := hex.DecodeString(strings.TrimSpace(string(raw)))
	f.Add(seed)
	schemaBytes, _ := os.ReadFile("testdata/packet.schema.json")
	p, e := Compile(schemaBytes, DefaultLimits())
	if e != nil {
		f.Fatal(e)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		doc, _, e := p.Decode(context.Background(), bytes.NewReader(raw))
		if e != nil {
			return
		}
		var out bytes.Buffer
		if e = p.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), raw) {
			t.Fatalf("lossless round trip failed: %v", e)
		}
	})
}

func TestEncodePreciseDiagnosticPositions(t *testing.T) {
	p := planFor(t, `[{"name":"prefix","type":"bytes","length":1},{"name":"integer","type":"u16","expect":"2"},{"name":"magic","type":"bytes","length":2,"expect":"aabb"},{"name":"text","type":"string","length":3,"encoding":"ascii"}]`, DefaultLimits())
	for _, tc := range []struct {
		name, value, code string
		start, offset     int64
	}{{"integer", "1", "constraint_mismatch", 1, 1}, {"magic", "aacc", "constraint_mismatch", 3, 4}, {"text", "aé", "invalid_encoding", 5, 6}} {
		values := map[string]any{"prefix": "ff", "integer": "2", "magic": "aabb", "text": "abc"}
		values[tc.name] = tc.value
		writer := &countWriter{}
		err := p.Encode(context.Background(), &Document{1, p.digest, values}, writer)
		d, ok := err.(*Diagnostic)
		if !ok || d.Code != tc.code || d.Path != "/"+tc.name || d.FieldStart == nil || *d.FieldStart != tc.start || d.Offset == nil || *d.Offset != tc.offset || writer.calls != 0 {
			t.Fatalf("got %#v", err)
		}
	}
}
func TestEncodeEscapeExpandedBudget(t *testing.T) {
	input := []byte{'<', '>', '&', '\n', 0, 0xe2, 0x80, 0xa8, 0xf0, 0x9f, 0x98, 0x80}
	fields := `[{"name":"text","type":"string","length":12,"encoding":"utf8"}]`
	p := planFor(t, fields, DefaultLimits())
	doc, _, err := p.Decode(context.Background(), bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	compact, _ := json.Marshal(doc)
	for _, delta := range []int{0, -1} {
		l := DefaultLimits()
		l.MaxDocumentBytes = len(compact) + delta
		p = planFor(t, fields, l)
		var out bytes.Buffer
		err = p.Encode(context.Background(), doc, &out)
		if delta == 0 {
			if err != nil || !bytes.Equal(out.Bytes(), input) {
				t.Fatal(err)
			}
		} else if err == nil || err.(*Diagnostic).Code != "limit_exceeded" || out.Len() != 0 {
			t.Fatal("escape budget bypass")
		}
	}
}

type traversalContext struct {
	context.Context
	remaining int
	done      chan struct{}
	cancelled bool
}

func (c *traversalContext) Done() <-chan struct{} { return c.done }
func (c *traversalContext) Err() error {
	if c.cancelled {
		return context.Canceled
	}
	c.remaining--
	if c.remaining <= 0 {
		c.cancelled = true
		close(c.done)
		return context.Canceled
	}
	return nil
}
func TestEncodeCancellationDuringTraversal(t *testing.T) {
	p := fixturePlan(t, "packet", DefaultLimits())
	doc := documentFixture(t, "packet", p)
	ctx := &traversalContext{Context: context.Background(), remaining: 15, done: make(chan struct{})}
	w := &countWriter{}
	err := p.Encode(ctx, doc, w)
	d, ok := err.(*Diagnostic)
	if !ok || d.Code != "cancelled" || d.Path == "" || w.calls != 0 {
		t.Fatalf("got %#v", err)
	}
	raw, _ := os.ReadFile("testdata/packet.document.json")
	ctx = &traversalContext{Context: context.Background(), remaining: 15, done: make(chan struct{})}
	if doc, err = p.LoadDocument(ctx, raw); err == nil || err.(*Diagnostic).Code != "cancelled" || doc != nil {
		t.Fatal("load cancellation")
	}
}
func TestEncodeDynamicAndAggregateLimits(t *testing.T) {
	fields := `[{"name":"n","type":"u64"},{"name":"data","type":"bytes","length":{"field":"n"}}]`
	p := planFor(t, fields, DefaultLimits())
	w := &countWriter{}
	doc := &Document{1, p.digest, map[string]any{"n": "18446744073709551615", "data": ""}}
	if err := p.Encode(context.Background(), doc, w); err == nil || err.(*Diagnostic).Code != "limit_exceeded" || w.calls != 0 {
		t.Fatal("dynamic limit")
	}
	l := DefaultLimits()
	l.MaxOutputBytes = 3
	p = planFor(t, `[{"name":"a","type":"u16"},{"name":"b","type":"u16"}]`, l)
	doc = &Document{1, p.digest, map[string]any{"a": "1", "b": "2"}}
	if err := p.Encode(context.Background(), doc, w); err == nil || err.(*Diagnostic).Code != "limit_exceeded" || w.calls != 0 {
		t.Fatal("aggregate output limit")
	}
}
