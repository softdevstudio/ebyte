package ebyte

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
)

// Fixtures below were written out by hand from the format descriptions, not
// produced by the codec: offsets, lengths and values are the oracle.

const chunkSchema = `{"schema_version":1,"name":"chunks","endian":"little","fields":[
{"name":"magic","type":"bytes","length":2,"expect":"4348"},
{"name":"chunks","type":"repeat","until":"end_of_input","fields":[
 {"name":"id","type":"u8"},
 {"name":"size","type":"u8"},
 {"name":"body","type":"bytes","length":{"field":"size"}}]}]}`

const recordSchema = `{"schema_version":1,"name":"records","endian":"little","fields":[
{"name":"entries","type":"repeat","until":"tag_mismatch","fields":[
 {"name":"tag","type":"bytes","length":2,"expect":"5245"},
 {"name":"value","type":"u16"}]},
{"name":"trailer","type":"bytes","length":2,"expect":"454e"}]}`

func repeatPlan(t *testing.T, schema string, limits Limits) *Plan {
	t.Helper()
	p, e := Compile([]byte(schema), limits)
	if e != nil {
		t.Fatalf("compile: %v", e)
	}
	return p
}
func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, e := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func diagnosticOf(t *testing.T, e error) *Diagnostic {
	t.Helper()
	d, ok := e.(*Diagnostic)
	if !ok {
		t.Fatalf("not a diagnostic: %v", e)
	}
	return d
}
func at(d *Diagnostic) string {
	o, s := "null", "null"
	if d.Offset != nil {
		o = strconv.FormatInt(*d.Offset, 10)
	}
	if d.FieldStart != nil {
		s = strconv.FormatInt(*d.FieldStart, 10)
	}
	return d.Code + " " + d.Path + " " + o + " " + s
}

func TestRepeatUntilEndGolden(t *testing.T) {
	p := repeatPlan(t, chunkSchema, DefaultLimits())
	input := unhex(t, "4348 0102aabb 0200 0301ff")
	want := map[string]any{"magic": "4348", "chunks": []any{
		map[string]any{"id": "1", "size": "2", "body": "aabb"},
		map[string]any{"id": "2", "size": "0", "body": ""},
		map[string]any{"id": "3", "size": "1", "body": "ff"},
	}}
	type span struct {
		path, kind string
		offset     int64
		length     int64
		value      string
	}
	wantTrace := []span{
		{"", "struct", 0, 11, ""}, {"/magic", "bytes", 0, 2, ""}, {"/chunks", "repeat", 2, 9, ""},
		{"/chunks/0", "struct", 2, 4, ""}, {"/chunks/0/id", "u8", 2, 1, "1"}, {"/chunks/0/size", "u8", 3, 1, "2"}, {"/chunks/0/body", "bytes", 4, 2, ""},
		{"/chunks/1", "struct", 6, 2, ""}, {"/chunks/1/id", "u8", 6, 1, "2"}, {"/chunks/1/size", "u8", 7, 1, "0"}, {"/chunks/1/body", "bytes", 8, 0, ""},
		{"/chunks/2", "struct", 8, 3, ""}, {"/chunks/2/id", "u8", 8, 1, "3"}, {"/chunks/2/size", "u8", 9, 1, "1"}, {"/chunks/2/body", "bytes", 10, 1, ""},
	}
	// A one-byte reader proves peeking assembles tags across short reads.
	for name, reader := range map[string]func() *bytes.Reader{"whole": func() *bytes.Reader { return bytes.NewReader(input) }} {
		for _, wrap := range []bool{false, true} {
			var r = reader()
			var doc *Document
			var trace *Trace
			var e error
			if wrap {
				doc, trace, e = p.Decode(context.Background(), iotest.OneByteReader(r))
			} else {
				doc, trace, e = p.Decode(context.Background(), r)
			}
			if e != nil {
				t.Fatalf("%s: %v", name, e)
			}
			if !reflect.DeepEqual(doc.Values, want) {
				t.Fatalf("values %#v", doc.Values)
			}
			if len(trace.Entries) != len(wantTrace) {
				t.Fatalf("trace has %d entries, want %d", len(trace.Entries), len(wantTrace))
			}
			for i, w := range wantTrace {
				g := trace.Entries[i]
				v := ""
				if g.Value != nil {
					v = *g.Value
				}
				if g.Path != w.path || g.Kind != w.kind || g.Offset != w.offset || g.Length != w.length || v != w.value || g.State != "complete" {
					t.Fatalf("entry %d: got %+v value %q, want %+v", i, g, v, w)
				}
			}
			var out bytes.Buffer
			if e := p.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), input) {
				t.Fatalf("round trip: %v %x", e, out.Bytes())
			}
		}
	}
	doc, _, e := p.Decode(context.Background(), bytes.NewReader(unhex(t, "4348")))
	if e != nil || !reflect.DeepEqual(doc.Values["chunks"], []any{}) {
		t.Fatalf("empty repeat: %v %#v", e, doc)
	}
}

func TestRepeatUntilTagGolden(t *testing.T) {
	p := repeatPlan(t, recordSchema, DefaultLimits())
	for _, c := range []struct {
		hex    string
		values map[string]any
	}{
		{"5245 0100 5245 0200 454e", map[string]any{"entries": []any{
			map[string]any{"tag": "5245", "value": "1"}, map[string]any{"tag": "5245", "value": "2"}}, "trailer": "454e"}},
		{"454e", map[string]any{"entries": []any{}, "trailer": "454e"}},
	} {
		input := unhex(t, c.hex)
		doc, trace, e := p.Decode(context.Background(), iotest.OneByteReader(bytes.NewReader(input)))
		if e != nil || !reflect.DeepEqual(doc.Values, c.values) {
			t.Fatalf("%s: %v %#v", c.hex, e, doc)
		}
		last := trace.Entries[len(trace.Entries)-1]
		if last.Path != "/trailer" || last.Offset != int64(len(input)-2) || last.Length != 2 {
			t.Fatalf("trailer span %+v", last)
		}
		var out bytes.Buffer
		if e := p.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), input) {
			t.Fatalf("round trip %s: %v", c.hex, e)
		}
	}
}

func TestRepeatDecodeFailures(t *testing.T) {
	chunks := repeatPlan(t, chunkSchema, DefaultLimits())
	records := repeatPlan(t, recordSchema, DefaultLimits())
	small := DefaultLimits()
	small.MaxArrayElements = 2
	limited := repeatPlan(t, chunkSchema, small)
	onlyTag := repeatPlan(t, `{"schema_version":1,"name":"t","endian":"little","fields":[
{"name":"entries","type":"repeat","until":"tag_mismatch","fields":[{"name":"tag","type":"bytes","length":2,"expect":"5245"},{"name":"value","type":"u16"}]}]}`, DefaultLimits())
	empty := repeatPlan(t, `{"schema_version":1,"name":"e","endian":"little","fields":[
{"name":"items","type":"repeat","until":"end_of_input","fields":[{"name":"body","type":"bytes","length":0}]}]}`, DefaultLimits())
	for _, c := range []struct {
		name  string
		plan  *Plan
		input string
		want  string
	}{
		{"truncated inside an element", chunks, "4348 0102aa", "truncated_input /chunks/0/body 5 4"},
		{"partial tag before a trailer", records, "5245 0100 52", "truncated_input /trailer 5 4"},
		{"wrong trailer after records", records, "5245 0100 4142", "constraint_mismatch /trailer 4 4"},
		{"element ceiling", limited, "4348 0100 0200 0300", "limit_exceeded /chunks/2 6 6"},
		{"peeked bytes are trailing data", onlyTag, "5245 0100 41", "trailing_data  4 null"},
		{"element that reads nothing", empty, "00", "constraint_mismatch /items/0 0 0"},
	} {
		_, trace, e := c.plan.Decode(context.Background(), iotest.OneByteReader(bytes.NewReader(unhex(t, c.input))))
		if e == nil {
			t.Fatalf("%s: accepted", c.name)
		}
		if got := at(diagnosticOf(t, e)); got != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, got, c.want)
		}
		if trace != nil {
			for _, entry := range trace.Entries {
				if entry.Offset+entry.Length > int64(len(unhex(t, c.input))) {
					t.Fatalf("%s: span past consumed input %+v", c.name, entry)
				}
			}
		}
	}
}

func TestRepeatEncodeFailuresAndEdits(t *testing.T) {
	chunks := repeatPlan(t, chunkSchema, DefaultLimits())
	doc, _, e := chunks.Decode(context.Background(), bytes.NewReader(unhex(t, "4348 0102aabb")))
	if e != nil {
		t.Fatal(e)
	}
	// Adding and removing elements moves later bytes; the expected bytes are hand-written.
	doc.Values["chunks"] = append(doc.Values["chunks"].([]any), map[string]any{"id": "9", "size": "3", "body": "010203"})
	var out bytes.Buffer
	if e := chunks.Encode(context.Background(), doc, &out); e != nil || hex.EncodeToString(out.Bytes()) != "43480102aabb0903010203" {
		t.Fatalf("append: %v %x", e, out.Bytes())
	}
	doc.Values["chunks"] = []any{}
	out.Reset()
	if e := chunks.Encode(context.Background(), doc, &out); e != nil || hex.EncodeToString(out.Bytes()) != "4348" {
		t.Fatalf("remove all: %v %x", e, out.Bytes())
	}

	plainTrailer := repeatPlan(t, `{"schema_version":1,"name":"p","endian":"little","fields":[
{"name":"entries","type":"repeat","until":"tag_mismatch","fields":[{"name":"tag","type":"bytes","length":2,"expect":"5245"},{"name":"value","type":"u16"}]},
{"name":"trailer","type":"bytes","length":2}]}`, DefaultLimits())
	ambiguous := &Document{DocumentVersion: 1, SchemaSHA256: plainTrailer.SchemaSHA256(), Values: map[string]any{
		"entries": []any{map[string]any{"tag": "5245", "value": "1"}}, "trailer": "5245"}}
	out.Reset()
	e = plainTrailer.Encode(context.Background(), ambiguous, &out)
	if got := at(diagnosticOf(t, e)); got != "constraint_mismatch /entries 4 4" || out.Len() != 0 {
		t.Fatalf("ambiguous trailer: %q, wrote %d", got, out.Len())
	}

	empty := repeatPlan(t, `{"schema_version":1,"name":"e","endian":"little","fields":[
{"name":"items","type":"repeat","until":"end_of_input","fields":[{"name":"body","type":"bytes","length":0}]}]}`, DefaultLimits())
	e = empty.Encode(context.Background(), &Document{DocumentVersion: 1, SchemaSHA256: empty.SchemaSHA256(), Values: map[string]any{"items": []any{map[string]any{"body": ""}}}}, &out)
	if got := at(diagnosticOf(t, e)); got != "constraint_mismatch /items/0 null null" {
		t.Fatalf("empty element: %q", got)
	}

	records := repeatPlan(t, recordSchema, DefaultLimits())
	wrongTag := &Document{DocumentVersion: 1, SchemaSHA256: records.SchemaSHA256(), Values: map[string]any{
		"entries": []any{map[string]any{"tag": "5246", "value": "1"}}, "trailer": "454e"}}
	if got := at(diagnosticOf(t, records.Encode(context.Background(), wrongTag, &out))); got != "constraint_mismatch /entries/0/tag 1 0" {
		t.Fatalf("wrong tag: %q", got)
	}

	// LoadDocument accepts repeat arrays like any array and enforces the element ceiling.
	raw, _ := json.Marshal(map[string]any{"document_version": 1, "schema_sha256": chunks.SchemaSHA256(), "values": map[string]any{"magic": "4348", "chunks": []any{map[string]any{"id": "1", "size": "1", "body": "00"}}}})
	if _, e := chunks.LoadDocument(context.Background(), raw); e != nil {
		t.Fatalf("load: %v", e)
	}
}

func TestRepeatSchemaRules(t *testing.T) {
	head := `{"schema_version":1,"name":"s","endian":"little","fields":[`
	tagged := `{"name":"r","type":"repeat","until":"tag_mismatch","fields":[{"name":"tag","type":"bytes","length":1,"expect":"41"}]}`
	for _, c := range []struct {
		name, fields, path string
	}{
		{"end not last", `{"name":"r","type":"repeat","until":"end_of_input","fields":[{"name":"a","type":"u8"}]},{"name":"z","type":"u8"}`, "/fields/0/until"},
		{"end inside array", `{"name":"a","type":"array","count":1,"fields":[{"name":"r","type":"repeat","until":"end_of_input","fields":[{"name":"b","type":"u8"}]}]}`, "/fields/0/fields/0/until"},
		{"end with when", `{"name":"f","type":"u8"},{"name":"r","type":"repeat","until":"end_of_input","when":{"field":"f","equals":"1"},"fields":[{"name":"a","type":"u8"}]}`, "/fields/1/until"},
		{"unknown stop rule", `{"name":"r","type":"repeat","until":"forever","fields":[{"name":"a","type":"u8"}]}`, "/fields/0/until"},
		{"tag is an integer", `{"name":"r","type":"repeat","until":"tag_mismatch","fields":[{"name":"a","type":"u8","expect":"1"}]}`, "/fields/0/fields/0"},
		{"tag without expect", `{"name":"r","type":"repeat","until":"tag_mismatch","fields":[{"name":"a","type":"bytes","length":1}]}`, "/fields/0/fields/0"},
		{"tag longer than 16 bytes", `{"name":"r","type":"repeat","until":"tag_mismatch","fields":[{"name":"a","type":"bytes","length":17,"expect":"` + strings.Repeat("41", 17) + `"}]}`, "/fields/0/fields/0"},
		{"no element fields", `{"name":"r","type":"repeat","until":"tag_mismatch","fields":[]}`, "/fields/0/fields"},
		{"repeat with a count", `{"name":"r","type":"repeat","until":"end_of_input","count":1,"fields":[{"name":"a","type":"u8"}]}`, "/fields/0"},
		{"repeat count used as a length", `{"name":"n","type":"u8","derive":{"count_of":"r"}},` + tagged + `,{"name":"b","type":"bytes","length":{"field":"n"}}`, "/fields/0/derive/count_of"},
	} {
		_, e := Compile([]byte(head+c.fields+`]}`), DefaultLimits())
		if e == nil {
			t.Fatalf("%s: accepted", c.name)
		}
		if d := diagnosticOf(t, e); d.Code != "invalid_schema" || d.Path != c.path {
			t.Fatalf("%s: %s at %q", c.name, d.Code, d.Path)
		}
	}
	// A conditional tag repeat in the middle of a schema is allowed.
	ok := head + `{"name":"f","type":"u8"},{"name":"r","type":"repeat","until":"tag_mismatch","when":{"field":"f","equals":"1"},"fields":[{"name":"tag","type":"bytes","length":1,"expect":"41"},{"name":"v","type":"u8"}]},{"name":"z","type":"u8"}]}`
	p, e := Compile([]byte(ok), DefaultLimits())
	if e != nil {
		t.Fatal(e)
	}
	for in, want := range map[string]string{"01 4107 4108 09": `[{"tag":"41","v":"7"},{"tag":"41","v":"8"}]`, "00 09": `absent`} {
		doc, _, e := p.Decode(context.Background(), bytes.NewReader(unhex(t, in)))
		if e != nil {
			t.Fatalf("%s: %v", in, e)
		}
		got := "absent"
		if v, present := doc.Values["r"]; present {
			raw, _ := json.Marshal(v)
			got = string(raw)
		}
		if got != want {
			t.Fatalf("%s: %s", in, got)
		}
	}
}

// FuzzRepeat checks that any input the repeat schemas accept encodes back to
// the same bytes, and that trace spans never claim more than the input.
func FuzzRepeat(f *testing.F) {
	for _, seed := range []string{"43480102aabb02000301ff", "4348", "52450100524502004548", "454e", "5245010052"} {
		b, _ := hex.DecodeString(seed)
		f.Add(b)
	}
	chunks, e1 := Compile([]byte(chunkSchema), DefaultLimits())
	records, e2 := Compile([]byte(recordSchema), DefaultLimits())
	if e1 != nil || e2 != nil {
		f.Fatal(e1, e2)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 4096 {
			return
		}
		for _, p := range []*Plan{chunks, records} {
			doc, trace, e := p.Decode(context.Background(), bytes.NewReader(input))
			if trace != nil {
				for _, entry := range trace.Entries {
					if entry.Offset < 0 || entry.Length < 0 || entry.Offset+entry.Length > int64(len(input)) {
						t.Fatalf("span %+v outside %d input bytes", entry, len(input))
					}
				}
			}
			if e != nil {
				continue
			}
			var out bytes.Buffer
			if e := p.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), input) {
				t.Fatalf("accepted input did not round-trip: %v", e)
			}
		}
	})
}

// The library samples were made by Python's wave and zipfile modules, so their
// chunk and entry lists are known without asking the codec.
func TestRepeatLibrarySamples(t *testing.T) {
	list := func(schema, sample, repeat, field string) []string {
		raw, e := os.ReadFile("schemas/" + schema)
		if e != nil {
			t.Fatal(e)
		}
		input, e := os.ReadFile("schemas/samples/" + sample)
		if e != nil {
			t.Fatal(e)
		}
		doc, _, e := repeatPlan(t, string(raw), DefaultLimits()).Decode(context.Background(), bytes.NewReader(input))
		if e != nil {
			t.Fatalf("%s: %v", sample, e)
		}
		var got []string
		for _, element := range doc.Values[repeat].([]any) {
			got = append(got, element.(map[string]any)[field].(string))
		}
		return got
	}
	if got := list("riff-wave.schema.json", "tagged.wav", "chunks", "id"); !reflect.DeepEqual(got, []string{"fmt ", "data", "LIST"}) {
		t.Fatalf("wav chunks %q", got)
	}
	names := []string{"readme.txt", "notes/a.txt", "notes/b.txt"}
	if got := list("zip.schema.json", "three.zip", "entries", "name"); !reflect.DeepEqual(got, names) {
		t.Fatalf("zip entries %q", got)
	}
	if got := list("zip.schema.json", "three.zip", "central_directory", "name"); !reflect.DeepEqual(got, names) {
		t.Fatalf("zip directory %q", got)
	}
	// Pillow wrote these chunks, in this order, for the sample image.
	if got := list("png.schema.json", "stripes.png", "chunks", "type"); !reflect.DeepEqual(got, []string{"IHDR", "tEXt", "pHYs", "IDAT", "IEND"}) {
		t.Fatalf("png chunks %q", got)
	}
}

type unexpectedEOFReader struct{ data []byte }

func (r *unexpectedEOFReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// A reader's unexpected EOF met while looking ahead is truncation, as it is
// for every other read, and look-ahead stops one byte past the input ceiling.
func TestRepeatLookAheadEdges(t *testing.T) {
	chunks := repeatPlan(t, chunkSchema, DefaultLimits())
	_, _, e := chunks.Decode(context.Background(), &unexpectedEOFReader{unhex(t, "4348 0102aabb")})
	if got := at(diagnosticOf(t, e)); got != "truncated_input /chunks 6 2" {
		t.Fatalf("unexpected EOF: %q", got)
	}
	small := DefaultLimits()
	small.MaxInputBytes = 5
	tagged := repeatPlan(t, `{"schema_version":1,"name":"t","endian":"little","fields":[
{"name":"entries","type":"repeat","until":"tag_mismatch","fields":[{"name":"tag","type":"bytes","length":4,"expect":"52455045"},{"name":"v","type":"u8"}]}]}`, small)
	counted := &countingReader{r: bytes.NewReader(unhex(t, "52455045 01 52455045 02"))}
	_, _, e = tagged.Decode(context.Background(), counted)
	if got := at(diagnosticOf(t, e)); got != "limit_exceeded  5 null" {
		t.Fatalf("ceiling: %q", got)
	}
	if counted.n > 6 {
		t.Fatalf("read %d bytes with a 5-byte ceiling", counted.n)
	}
}

type countingReader struct {
	r *bytes.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, e := c.r.Read(p)
	c.n += n
	return n, e
}
