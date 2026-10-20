package ebyte

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func fixturePlan(t *testing.T, name string, limits Limits) *Plan {
	t.Helper()
	raw, e := os.ReadFile("testdata/" + name + ".schema.json")
	if e != nil {
		t.Fatal(e)
	}
	p, e := Compile(raw, limits)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func equivalentJSON(t *testing.T, a, b []byte) {
	t.Helper()
	var x, y any
	if e := json.Unmarshal(a, &x); e != nil {
		t.Fatal(e)
	}
	if e := json.Unmarshal(b, &y); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("JSON mismatch\ngot %s\nwant %s", a, b)
	}
}
func TestDecodeIndependentGoldens(t *testing.T) {
	for _, name := range []string{"packet", "wide", "empty", "stored"} {
		t.Run(name, func(t *testing.T) {
			p := fixturePlan(t, name, DefaultLimits())
			doc, trace, e := p.Decode(context.Background(), bytes.NewReader(fixtureBytes(t, name)))
			if e != nil {
				t.Fatal(e)
			}
			got, e := json.Marshal(doc)
			if e != nil {
				t.Fatal(e)
			}
			want, e := os.ReadFile("testdata/" + name + ".document.json")
			if e != nil {
				t.Fatal(e)
			}
			equivalentJSON(t, got, want)
			checkTraceSize(t, trace)
			if name == "packet" {
				got, _ = json.Marshal(trace)
				want, e = os.ReadFile("testdata/packet.trace.json")
				if e != nil {
					t.Fatal(e)
				}
				equivalentJSON(t, got, want)
			}
			inspected, e := p.Inspect(context.Background(), bytes.NewReader(fixtureBytes(t, name)))
			if e != nil || !reflect.DeepEqual(trace, inspected) {
				t.Fatalf("inspect diverged: %v", e)
			}
		})
	}
}
func checkTraceSize(t *testing.T, trace *Trace) {
	t.Helper()
	if trace == nil {
		t.Fatal("missing trace")
	}
	raw, e := json.Marshal(trace)
	if e != nil {
		t.Fatal(e)
	}
	if len(raw) != traceSize(trace) {
		t.Fatalf("trace size: %d != %d", len(raw), traceSize(trace))
	}
}
func TestDecodeAcceptanceVectors(t *testing.T) {
	raw, e := os.ReadFile("testdata/cases.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Name, Operation, Schema, Hex string
		Limits                       map[string]int
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
		if tc.Operation != "decode" {
			continue
		}
		count++
		t.Run(tc.Name, func(t *testing.T) {
			limits := DefaultLimits()
			if n, ok := tc.Limits["max_array_elements"]; ok {
				limits.MaxArrayElements = n
			}
			p := fixturePlan(t, tc.Schema, limits)
			input, e := hex.DecodeString(tc.Hex)
			if e != nil {
				t.Fatal(e)
			}
			doc, trace, e := p.Decode(context.Background(), bytes.NewReader(input))
			if tc.Error == nil {
				if e != nil || !reflect.DeepEqual(doc.Values, tc.Values) {
					t.Fatalf("got %#v %v", doc, e)
				}
				return
			}
			d, ok := e.(*Diagnostic)
			if !ok {
				t.Fatalf("got %v", e)
			}
			if doc != nil || d.Code != tc.Error.Code || d.Path != tc.Error.Path || !reflect.DeepEqual(d.Offset, tc.Error.Offset) || !reflect.DeepEqual(d.FieldStart, tc.Error.FieldStart) {
				t.Fatalf("got %#v want %#v", d, tc.Error)
			}
			if trace == nil || trace.State != "failed" || trace.Entries[0].State != "failed" {
				t.Fatal("invalid failure trace")
			}
			checkTraceSize(t, trace)
		})
	}
	if count != 6 {
		t.Fatalf("decode vectors %d", count)
	}
}
func TestEveryPacketTruncation(t *testing.T) {
	p := fixturePlan(t, "packet", DefaultLimits())
	raw := fixtureBytes(t, "packet")
	for n := 0; n < len(raw); n++ {
		doc, trace, e := p.Decode(context.Background(), bytes.NewReader(raw[:n]))
		d, ok := e.(*Diagnostic)
		if !ok || d.Code != "truncated_input" || d.Offset == nil || *d.Offset != int64(n) || doc != nil {
			t.Fatalf("cut %d: %#v", n, e)
		}
		if trace.Entries[0].Length != int64(n) {
			t.Fatal("root span")
		}
		for _, entry := range trace.Entries {
			if entry.Offset+entry.Length > int64(n) {
				t.Fatalf("fabricated span %#v", entry)
			}
			if entry.State == "failed" {
				if _, _, ok := integer(entry.Kind); ok && entry.Value != nil {
					t.Fatal("truncated integer value")
				}
			}
		}
		checkTraceSize(t, trace)
	}
}
func planFor(t *testing.T, fields string, l Limits) *Plan {
	t.Helper()
	p, e := Compile(schema(fields), l)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestSignedAndEndianDecode(t *testing.T) {
	p := planFor(t, `[{"name":"a","type":"i8"},{"name":"b","type":"i16","endian":"big"},{"name":"c","type":"i32"},{"name":"d","type":"i64","endian":"big"},{"name":"e","type":"u32","endian":"big"}]`, DefaultLimits())
	raw, _ := hex.DecodeString("808000ffffffffffffffffffffffffffffffff")
	doc, _, e := p.Decode(context.Background(), bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]any{"a": "-128", "b": "-32768", "c": "-1", "d": "-1", "e": "4294967295"}
	if !reflect.DeepEqual(doc.Values, want) {
		t.Fatalf("got %#v", doc.Values)
	}
}
func TestNegativeAndHugeDynamicDimensions(t *testing.T) {
	for _, tc := range []struct{ kind, hex, code string }{{"i8", "ff", "value_out_of_range"}, {"u64", "ffffffffffffffff", "limit_exceeded"}} {
		p := planFor(t, `[{"name":"n","type":"`+tc.kind+`"},{"name":"data","type":"bytes","length":{"field":"n"}}]`, DefaultLimits())
		b, _ := hex.DecodeString(tc.hex)
		r := &countReader{Reader: bytes.NewReader(b)}
		doc, trace, e := p.Decode(context.Background(), r)
		d, ok := e.(*Diagnostic)
		if !ok || d.Code != tc.code || d.Path != "/data" || doc != nil || r.bytes != len(b) {
			t.Fatalf("%#v read %d", e, r.bytes)
		}
		if trace.Entries[len(trace.Entries)-1].Length != 0 {
			t.Fatal("dimension consumed bytes")
		}
	}
}
func TestInvalidUTF8AndAssertions(t *testing.T) {
	for _, tc := range []struct {
		fields, hex, code string
		offset            int64
	}{{`[{"name":"s","type":"string","length":3,"encoding":"utf8"}]`, "61c328", "invalid_encoding", 1}, {`[{"name":"s","type":"string","length":2,"encoding":"utf8"}]`, "e282", "invalid_encoding", 0}, {`[{"name":"n","type":"u16","expect":"2"}]`, "0100", "constraint_mismatch", 0}} {
		p := planFor(t, tc.fields, DefaultLimits())
		b, _ := hex.DecodeString(tc.hex)
		doc, trace, e := p.Decode(context.Background(), bytes.NewReader(b))
		d, ok := e.(*Diagnostic)
		if !ok || d.Code != tc.code || *d.Offset != tc.offset || doc != nil {
			t.Fatalf("got %#v", e)
		}
		last := trace.Entries[len(trace.Entries)-1]
		if last.Length != int64(len(b)) {
			t.Fatal("assertion span")
		}
		if last.Kind == "u16" && (last.Value == nil || *last.Value != "1") {
			t.Fatal("missing observed integer")
		}
		checkTraceSize(t, trace)
	}
}

type countReader struct {
	io.Reader
	bytes, calls int
}

func (r *countReader) Read(p []byte) (int, error) {
	r.calls++
	n, e := r.Reader.Read(p)
	r.bytes += n
	return n, e
}

type errorReader struct {
	data []byte
	err  error
}

func (r *errorReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, r.err
}

type stalledReader struct{}

func (stalledReader) Read([]byte) (int, error) { return 0, nil }

type cancelReader struct{ cancel context.CancelFunc }

func (r cancelReader) Read(p []byte) (int, error) { p[0] = 1; r.cancel(); return 1, nil }

type oneByteReader struct{ io.Reader }

func (r oneByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}
func TestReadErrorsCancellationAndProgress(t *testing.T) {
	p := planFor(t, `[{"name":"n","type":"u16"}]`, DefaultLimits())
	for _, r := range []io.Reader{&errorReader{[]byte{1, 2}, errors.New("secret reader failure")}, stalledReader{}} {
		doc, trace, e := p.Decode(context.Background(), r)
		d, ok := e.(*Diagnostic)
		if !ok || d.Code != "read_failed" || d.Stage != "io" || doc != nil || strings.Contains(d.Error(), "secret") {
			t.Fatalf("got %#v", e)
		}
		checkTraceSize(t, trace)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &countReader{Reader: bytes.NewReader([]byte{1, 2})}
	doc, _, e := p.Decode(ctx, r)
	if e.(*Diagnostic).Code != "cancelled" || doc != nil || r.calls != 0 {
		t.Fatal("pre-cancel read")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	doc, trace, e := p.Decode(ctx, cancelReader{cancel})
	if e.(*Diagnostic).Code != "cancelled" || doc != nil || trace.Entries[1].Length != 1 {
		t.Fatal("mid-read cancellation")
	}
	doc, _, e = p.Decode(context.Background(), oneByteReader{bytes.NewReader([]byte{1, 2})})
	if e != nil || doc.Values["n"] != "513" {
		t.Fatalf("short reads: %v", e)
	}
}
func TestTrailingProbeIsBounded(t *testing.T) {
	p := planFor(t, `[]`, DefaultLimits())
	r := &countReader{Reader: strings.NewReader(strings.Repeat("x", 10000))}
	_, trace, e := p.Decode(context.Background(), r)
	if e.(*Diagnostic).Code != "trailing_data" || r.bytes != 1 || trace.Entries[0].Length != 0 {
		t.Fatal("unbounded probe")
	}
	checkTraceSize(t, trace)
	l := DefaultLimits()
	l.MaxInputBytes = 0
	p = planFor(t, `[]`, l)
	if _, _, e = p.Decode(context.Background(), strings.NewReader("")); e != nil {
		t.Fatal(e)
	}
	if _, _, e = p.Decode(context.Background(), strings.NewReader("x")); e.(*Diagnostic).Code != "limit_exceeded" {
		t.Fatal(e)
	}
	p = planFor(t, `[]`, DefaultLimits())
	_, _, e = p.Decode(context.Background(), &errorReader{nil, errors.New("failed EOF probe")})
	if e.(*Diagnostic).Code != "read_failed" {
		t.Fatal(e)
	}
}
func TestIndependentDecodeBudgets(t *testing.T) {
	raw := fixtureBytes(t, "packet")
	p := fixturePlan(t, "packet", DefaultLimits())
	doc, trace, e := p.Decode(context.Background(), bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(doc)
	traceRaw, _ := json.Marshal(trace)
	// Exact JSON ceilings accept, one less rejects, including escaping and hex.
	for _, mode := range []string{"document", "trace"} {
		for _, delta := range []int{0, -1} {
			l := DefaultLimits()
			if mode == "document" {
				l.MaxDocumentBytes = len(b) + delta
			} else {
				l.MaxTraceBytes = len(traceRaw) + delta
			}
			p := fixturePlan(t, "packet", l)
			doc, tr, e := p.Decode(context.Background(), bytes.NewReader(raw))
			if delta == 0 {
				if e != nil || doc == nil || tr == nil {
					t.Fatalf("%s exact: %v", mode, e)
				}
			} else if e == nil || doc != nil || e.(*Diagnostic).Code != "limit_exceeded" {
				t.Fatalf("%s short: %v", mode, e)
			}
		}
	}
	for _, tc := range []struct {
		name   string
		modify func(*Limits)
	}{{"nodes", func(l *Limits) { l.MaxValueNodes = 1 }}, {"entries", func(l *Limits) { l.MaxTraceEntries = 1 }}, {"input", func(l *Limits) { l.MaxInputBytes = 1 }}, {"document", func(l *Limits) { l.MaxDocumentBytes = 0 }}, {"trace", func(l *Limits) { l.MaxTraceBytes = 0 }}} {
		t.Run(tc.name, func(t *testing.T) {
			l := DefaultLimits()
			tc.modify(&l)
			p := fixturePlan(t, "packet", l)
			r := &countReader{Reader: bytes.NewReader(raw)}
			doc, trace, e := p.Decode(context.Background(), r)
			if e == nil || e.(*Diagnostic).Code != "limit_exceeded" || doc != nil || r.bytes != 0 {
				t.Fatalf("got %v read %d", e, r.bytes)
			}
			if trace != nil {
				b, _ := json.Marshal(trace)
				if len(b) > l.MaxTraceBytes || len(trace.Entries) > l.MaxTraceEntries {
					t.Fatal("over-budget trace")
				}
			}
		})
	}
}
func TestEscapedDocumentBudget(t *testing.T) {
	input := []byte{'<', '>', '&', '\n', 0, 0xe2, 0x80, 0xa8, 0xf0, 0x9f, 0x98, 0x80}
	fields := `[{"name":"text","type":"string","length":12,"encoding":"utf8"}]`
	p := planFor(t, fields, DefaultLimits())
	doc, _, e := p.Decode(context.Background(), bytes.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(doc)
	l := DefaultLimits()
	l.MaxDocumentBytes = len(b)
	p = planFor(t, fields, l)
	if _, _, e = p.Decode(context.Background(), bytes.NewReader(input)); e != nil {
		t.Fatal(e)
	}
	l.MaxDocumentBytes--
	p = planFor(t, fields, l)
	if doc, _, e = p.Decode(context.Background(), bytes.NewReader(input)); e == nil || doc != nil {
		t.Fatal("escape budget bypass")
	}
}
func TestConcurrentPlanDecode(t *testing.T) {
	p := fixturePlan(t, "packet", DefaultLimits())
	input := fixtureBytes(t, "packet")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 10; j++ {
				doc, _, e := p.Decode(context.Background(), bytes.NewReader(input))
				if e != nil || doc.Values["version"] != "1" {
					t.Errorf("concurrent decode: %v", e)
				}
			}
		})
	}
	wg.Wait()
}
func FuzzDecodePacket(f *testing.F) {
	raw, _ := os.ReadFile("testdata/packet.hex")
	seed, _ := hex.DecodeString(strings.TrimSpace(string(raw)))
	f.Add(seed)
	schemaBytes, _ := os.ReadFile("testdata/packet.schema.json")
	p, e := Compile(schemaBytes, DefaultLimits())
	if e != nil {
		f.Fatal(e)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		doc, trace, e := p.Decode(context.Background(), bytes.NewReader(input))
		if e != nil && doc != nil {
			t.Fatal("partial document")
		}
		if e == nil && (doc == nil || trace == nil || trace.State != "complete") {
			t.Fatal("invalid success")
		}
		if trace != nil {
			for _, entry := range trace.Entries {
				if entry.Offset < 0 || entry.Length < 0 || entry.Offset+entry.Length > int64(len(input)) {
					t.Fatal("invalid span")
				}
			}
			checkTraceSize(t, trace)
		}
	})
}

func TestNestedScopeConditionsAndEmptyEntries(t *testing.T) {
	p := planFor(t, `[{"name":"n","type":"u8"},{"name":"items","type":"array","count":{"field":"n"},"fields":[{"name":"flag","type":"u8"},{"name":"text","type":"string","length":0,"encoding":"ascii","when":{"field":"flag","equals":"1"}},{"name":"children","type":"array","count":1,"fields":[{"name":"value","type":"i8"}]}]}]`, DefaultLimits())
	doc, trace, e := p.Decode(context.Background(), bytes.NewReader([]byte{2, 1, 255, 0, 128}))
	if e != nil {
		t.Fatal(e)
	}
	got := doc.Values["items"].([]any)
	first := got[0].(map[string]any)
	second := got[1].(map[string]any)
	if first["text"] != "" {
		t.Fatal("zero string missing")
	}
	if _, ok := second["text"]; ok {
		t.Fatal("inactive field present")
	}
	found := false
	for _, entry := range trace.Entries {
		if entry.Path == "/items/0/text" {
			found = true
			if entry.Length != 0 || entry.Offset != 2 {
				t.Fatal("zero span")
			}
		}
		if entry.Path == "/items/1/text" {
			t.Fatal("inactive trace")
		}
	}
	if !found {
		t.Fatal("missing empty trace")
	}
	checkTraceSize(t, trace)
	_, trace, e = p.Decode(context.Background(), bytes.NewReader([]byte{2, 1, 255, 0}))
	if e.(*Diagnostic).Code != "truncated_input" {
		t.Fatal(e)
	}
	for _, entry := range trace.Entries {
		wantFailed := entry.Path == "" || entry.Path == "/items" || entry.Path == "/items/1" || strings.HasPrefix(entry.Path, "/items/1/children")
		if (entry.State == "failed") != wantFailed {
			t.Fatalf("parent chain %#v", entry)
		}
	}
}
func TestNodeEntryAndInputExactCeilings(t *testing.T) {
	raw := fixtureBytes(t, "packet")
	for _, tc := range []struct {
		nodes, entries, input int
		success               bool
	}{{14, 15, len(raw), true}, {13, 15, len(raw), false}, {14, 14, len(raw), false}, {14, 15, len(raw) - 1, false}} {
		l := DefaultLimits()
		l.MaxValueNodes = tc.nodes
		l.MaxTraceEntries = tc.entries
		l.MaxInputBytes = tc.input
		p := fixturePlan(t, "packet", l)
		doc, trace, e := p.Decode(context.Background(), bytes.NewReader(raw))
		if tc.success {
			if e != nil || doc == nil {
				t.Fatal(e)
			}
			checkTraceSize(t, trace)
		} else if e == nil || e.(*Diagnostic).Code != "limit_exceeded" || doc != nil {
			t.Fatal("budget bypass")
		}
	}
	// Empty objects consume nodes even when they consume no binary bytes.
	l := DefaultLimits()
	l.MaxValueNodes = 3
	p := planFor(t, `[{"name":"a","type":"array","count":2,"fields":[]}]`, l)
	r := &countReader{Reader: strings.NewReader("")}
	if doc, _, e := p.Decode(context.Background(), r); e == nil || doc != nil || r.calls != 0 {
		t.Fatal("empty object node budget")
	}
}

type finalEOFReader struct {
	data []byte
	done bool
}

func (r *finalEOFReader) Read(p []byte) (int, error) {
	if r.done {
		panic("read after terminal EOF")
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		r.done = true
		return n, io.EOF
	}
	return n, nil
}
func TestDataWithEOF(t *testing.T) {
	p := planFor(t, `[{"name":"x","type":"u16"}]`, DefaultLimits())
	if _, _, e := p.Decode(context.Background(), &finalEOFReader{data: []byte{1, 2}}); e != nil {
		t.Fatal(e)
	}
	p = planFor(t, `[{"name":"x","type":"u8"},{"name":"y","type":"u8"}]`, DefaultLimits())
	doc, trace, e := p.Decode(context.Background(), &finalEOFReader{data: []byte{1}})
	if doc != nil || e.(*Diagnostic).Code != "truncated_input" || trace.Entries[1].Value == nil || trace.Entries[2].Value != nil {
		t.Fatal("EOF handling")
	}
}

func TestUnexpectedEOFIsTruncation(t *testing.T) {
	p := planFor(t, `[{"name":"x","type":"u32"}]`, DefaultLimits())
	doc, trace, e := p.Decode(context.Background(), &errorReader{data: []byte{1, 2}, err: io.ErrUnexpectedEOF})
	d, ok := e.(*Diagnostic)
	if !ok || d.Code != "truncated_input" || d.Stage != "decode" || *d.Offset != 2 || *d.FieldStart != 0 || d.Expected != "4 bytes" || d.Observed != "2 bytes" || doc != nil {
		t.Fatalf("got %#v", e)
	}
	if trace.Entries[0].Length != 2 || trace.Entries[1].Length != 2 || trace.Entries[1].Value != nil {
		t.Fatal("unexpected EOF spans")
	}
	checkTraceSize(t, trace)
	p = planFor(t, `[]`, DefaultLimits())
	if _, _, e = p.Decode(context.Background(), &errorReader{err: io.ErrUnexpectedEOF}); e.(*Diagnostic).Code != "truncated_input" {
		t.Fatal(e)
	}
}

func TestConditionalZeroWidthNodeCeilings(t *testing.T) {
	fields := `[{"name":"flag","type":"u8"},{"name":"zero","type":"bytes","length":0,"when":{"field":"flag","equals":"1"}},{"name":"empty","type":"array","count":2,"fields":[]}]`
	for _, flag := range []byte{0, 1} {
		count := 5 + int(flag)
		for _, short := range []string{"none", "nodes", "entries"} {
			l := DefaultLimits()
			l.MaxValueNodes = count
			l.MaxTraceEntries = count
			if short == "nodes" {
				l.MaxValueNodes--
			}
			if short == "entries" {
				l.MaxTraceEntries--
			}
			p := planFor(t, fields, l)
			doc, trace, e := p.Decode(context.Background(), bytes.NewReader([]byte{flag}))
			if short == "none" {
				if e != nil || doc == nil || len(trace.Entries) != count {
					t.Fatalf("flag %d: %v", flag, e)
				}
				b, _ := json.Marshal(doc)
				l.MaxDocumentBytes = len(b)
				p = planFor(t, fields, l)
				if _, _, e = p.Decode(context.Background(), bytes.NewReader([]byte{flag})); e != nil {
					t.Fatal(e)
				}
			} else if e == nil || doc != nil || e.(*Diagnostic).Code != "limit_exceeded" {
				t.Fatalf("flag %d short %s: %v", flag, short, e)
			}
		}
	}
}

func TestDiagnosticFieldStartDoesNotAliasDecoder(t *testing.T) {
	d := &decoder{pos: 15}
	err := d.diagnostic("truncated_input", "/x", &d.pos, d.pos, "2 bytes", "1 bytes").(*Diagnostic)
	d.pos = 99
	if err.FieldStart == nil || *err.FieldStart != 15 || err.FieldStart == &d.pos {
		t.Fatalf("field start aliases decoder state: %v", err.FieldStart)
	}
}
