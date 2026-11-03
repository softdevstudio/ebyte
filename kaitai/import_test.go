package kaitai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/softdevstudio/ebyte"
)

// schemaField is the test's own reading of an imported schema, independent of
// the importer's types, used to walk decoded values in schema order.
type schemaField struct {
	Name   string          `json:"name"`
	Type   string          `json:"type"`
	Derive json.RawMessage `json:"derive"`
	Fields []schemaField   `json:"fields"`
}

// flatten lists a decoded document's stored values in schema order, taking
// derived integers (absent from the document) from the trace.
func flatten(fields []schemaField, values map[string]any, path string, traced map[string]string, types map[string]string, out *[]string) {
	for _, f := range fields {
		p := path + "/" + f.Name
		if f.Derive != nil {
			*out = append(*out, traced[p])
			types[strconv.Itoa(len(*out)-1)] = f.Type
			continue
		}
		v, ok := values[f.Name]
		if !ok {
			continue
		}
		if f.Fields != nil {
			for i, e := range v.([]any) {
				flatten(f.Fields, e.(map[string]any), p+"/"+strconv.Itoa(i), traced, types, out)
			}
			continue
		}
		*out = append(*out, v.(string))
		types[strconv.Itoa(len(*out)-1)] = f.Type
	}
}

func sameValue(kind, ours, theirs string) bool {
	if ours == theirs {
		return true
	}
	if kind == "f32" || kind == "f64" {
		bits := 64
		if kind == "f32" {
			bits = 32
		}
		a, e1 := strconv.ParseFloat(ours, bits)
		b, e2 := strconv.ParseFloat(theirs, 64)
		return e1 == nil && e2 == nil && (a == b || math.IsNaN(a) && math.IsNaN(b))
	}
	return false
}

// TestGalleryAgainstKaitai imports each supported gallery file, decodes the
// sample with EByte and compares every stored value with what the official
// Kaitai compiler and runtime read (testdata/generate.cjs), then encodes the
// document back to the sample's exact bytes.
func TestGalleryAgainstKaitai(t *testing.T) {
	raw, e := os.ReadFile("testdata/expected.json")
	if e != nil {
		t.Fatal(e)
	}
	var expected map[string][]string
	if e := json.Unmarshal(raw, &expected); e != nil {
		t.Fatal(e)
	}
	files, _ := filepath.Glob("testdata/gallery/*.ksy")
	if len(files) != len(expected) || len(files) == 0 {
		t.Fatalf("%d gallery files, %d expected sets", len(files), len(expected))
	}
	for _, file := range files {
		id := strings.TrimSuffix(filepath.Base(file), ".ksy")
		t.Run(id, func(t *testing.T) {
			src, _ := os.ReadFile(file)
			result, e := Import(src)
			if e != nil {
				t.Fatal(e)
			}
			plan, e := ebyte.Compile(result.Schema, ebyte.DefaultLimits())
			if e != nil {
				t.Fatal(e)
			}
			sample, _ := os.ReadFile("testdata/samples/" + id + ".bin")
			doc, trace, e := plan.Decode(context.Background(), bytes.NewReader(sample))
			if e != nil {
				t.Fatal(e)
			}
			traced := map[string]string{}
			for _, entry := range trace.Entries {
				if entry.Value != nil {
					traced[entry.Path] = *entry.Value
				}
			}
			var schema struct {
				Fields []schemaField `json:"fields"`
			}
			if e := json.Unmarshal(result.Schema, &schema); e != nil {
				t.Fatal(e)
			}
			var got []string
			types := map[string]string{}
			flatten(schema.Fields, doc.Values, "", traced, types, &got)
			want := expected[id]
			if len(got) != len(want) {
				t.Fatalf("%d values, Kaitai read %d", len(got), len(want))
			}
			for i := range want {
				if !sameValue(types[strconv.Itoa(i)], got[i], want[i]) {
					t.Fatalf("value %d: EByte %q, Kaitai %q", i, got[i], want[i])
				}
			}
			var out bytes.Buffer
			if e := plan.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), sample) {
				t.Fatalf("round trip: %v", e)
			}
		})
	}
}

func refusal(t *testing.T, src string) []Reason {
	t.Helper()
	_, e := Import([]byte(src))
	var r *Refusal
	if !errors.As(e, &r) {
		t.Fatalf("not refused: %v", e)
	}
	return r.Reasons
}

func TestImportMapping(t *testing.T) {
	src := `meta:
  id: demo
  endian: be
  encoding: UTF-8
  title: Demo # dropped with a note
enums:
  kind:
    1: small
    2: large
seq:
  - id: magic
    contents: [0x7f, "EB"]
  - id: version
    type: u2le
    valid: 3
  - id: kind
    type: u1
    enum: kind
  - id: len_name
    type: u1
  - id: name
    type: str
    size: len_name
  - id: extra
    type: u4
    if: kind == kind::large
  - id: flags
    type: b3
  - id: rest
    type: b5
  - id: num_points
    type: u2
  - id: points
    type: point
    repeat: expr
    repeat-expr: num_points
  - id: samples
    type: s2
    repeat: eos
types:
  point:
    seq:
      - id: x
        type: f4
      - id: y
        type: f8le
`
	r, e := Import([]byte(src))
	if e != nil {
		t.Fatal(e)
	}
	want := `{
  "schema_version": 1,
  "name": "demo",
  "endian": "big",
  "fields": [
    {"name": "magic", "type": "bytes", "length": 3, "expect": "7f4542"},
    {"name": "version", "type": "u16", "endian": "little", "expect": "3"},
    {"name": "kind", "type": "u8"},
    {"name": "len_name", "type": "u8", "derive": {"size_of": "name"}},
    {"name": "name", "type": "string", "length": {"field": "len_name"}, "encoding": "utf8"},
    {"name": "extra", "type": "u32", "when": {"field": "kind", "equals": "2"}},
    {"name": "flags", "type": "bits", "bits": 3},
    {"name": "rest", "type": "bits", "bits": 5},
    {"name": "num_points", "type": "u16", "derive": {"count_of": "points"}},
    {"name": "points", "type": "array", "count": {"field": "num_points"}, "fields": [
      {"name": "x", "type": "f32"},
      {"name": "y", "type": "f64", "endian": "little"}]},
    {"name": "samples", "type": "repeat", "until": "end_of_input", "fields": [
      {"name": "value", "type": "i16"}]}
  ]
}`
	var a, b any
	json.Unmarshal(r.Schema, &a)
	if e := json.Unmarshal([]byte(want), &b); e != nil {
		t.Fatal(e)
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if !bytes.Equal(ja, jb) {
		t.Fatalf("schema\n%s\nwant\n%s", ja, jb)
	}
	notes := strings.Join(r.Notes, "|")
	for _, n := range []string{"meta title dropped", "enums dropped"} {
		if !strings.Contains(notes, n) {
			t.Fatalf("notes %q lack %q", notes, n)
		}
	}
}

func TestImportUserTypes(t *testing.T) {
	r, e := Import([]byte(`meta: {id: t, endian: le}
seq:
  - id: head
    type: header
    size: 6
  - id: tail
    type: u1
types:
  header:
    types:
      inner:
        seq:
          - {id: a, type: u2}
    seq:
      - id: one
        type: inner
      - id: b
        type: u4
`))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(r.Schema), `"name": "head",`) || !strings.Contains(string(r.Schema), `"count": 1`) {
		t.Fatalf("schema %s", r.Schema)
	}
	reasons := refusal(t, `meta: {id: t, endian: le}
seq:
  - {id: head, type: header, size: 5}
types:
  header:
    seq:
      - {id: b, type: u4}
`)
	if reasons[0].Path != "/seq/0/size" {
		t.Fatalf("size mismatch: %v", reasons)
	}
	reasons = refusal(t, `meta: {id: t, endian: le}
seq:
  - {id: n, type: node}
types:
  node:
    seq:
      - {id: next, type: node}
`)
	if !strings.Contains(reasons[0].Reason, "recursive") {
		t.Fatalf("recursion: %v", reasons)
	}
}

func TestImportRefusals(t *testing.T) {
	head := "meta:\n  id: r\n  endian: le\nseq:\n"
	for _, c := range []struct {
		name, seq, path, reason string
	}{
		{"expression size", "  - {id: a, type: u1}\n  - {id: b, size: a * 2}\n", "/seq/1/size", "expression"},
		{"later field", "  - {id: b, size: a}\n  - {id: a, type: u1}\n", "/seq/0/size", "expression"},
		{"switch", "  - {id: a, type: u1}\n  - id: b\n    type:\n      switch-on: a\n      cases: {1: u2}\n", "/seq/1/type", "switch-on"},
		{"strz", "  - {id: s, type: strz, encoding: ASCII}\n", "/seq/0/type", "strz"},
		{"terminator", "  - {id: s, terminator: 0}\n", "/seq/0/terminator", "terminated"},
		{"process", "  - {id: s, size: 4, process: xor(1)}\n", "/seq/0/process", "process"},
		{"size eos", "  - {id: s, size-eos: true}\n", "/seq/0/size-eos", "size-eos"},
		{"repeat until", "  - {id: s, type: u1, repeat: until, repeat-until: _ == 0}\n", "/seq/0/repeat-until", "repeat until"},
		{"eos not last", "  - {id: s, type: u1, repeat: eos}\n  - {id: t, type: u1}\n", "/seq/0/repeat", "last top-level"},
		{"encoding", "  - {id: s, type: str, size: 2, encoding: SJIS}\n", "/seq/0/encoding", "ASCII and UTF-8"},
		{"if expression", "  - {id: a, type: u1}\n  - {id: b, type: u1, if: a > 1}\n", "/seq/1/if", "if"},
		{"valid range", "  - id: a\n    type: u1\n    valid: {min: 1}\n", "/seq/0/valid", "valid eq"},
		{"params type", "  - {id: a, type: thing(1)}\n", "/seq/0/type", "parameterized"},
		{"unknown type", "  - {id: a, type: nothing}\n", "/seq/0/type", "unknown type"},
		{"no id", "  - {type: u1}\n", "/seq/0/id", "id"},
	} {
		reasons := refusal(t, head+c.seq)
		if reasons[0].Path != c.path || !strings.Contains(reasons[0].Reason, c.reason) {
			t.Fatalf("%s: %v", c.name, reasons)
		}
	}
	for _, c := range []struct{ name, src, path string }{
		{"imports", "meta: {id: r, endian: le, imports: [a]}\nseq: [{id: a, type: u1}]\n", "/meta/imports"},
		{"pos instance", "meta: {id: r, endian: le}\nseq: [{id: a, type: u1}]\ninstances:\n  x: {pos: 0, type: u1}\n", "/instances/x"},
		{"no endian", "meta: {id: r}\nseq: [{id: a, type: u2}]\n", "/meta/endian"},
		{"bit endian", "meta: {id: r, endian: le, bit-endian: le}\nseq: [{id: a, type: b8}]\n", "/meta/bit-endian"},
		{"anchor", "meta: {id: r, endian: le}\nseq:\n  - &x {id: a, type: u1}\n", "/"},
		{"result refused", "meta: {id: r, endian: le}\nseq: [{id: a, type: b3}]\n", "/"},
	} {
		reasons := refusal(t, c.src)
		if reasons[0].Path != c.path {
			t.Fatalf("%s: %v", c.name, reasons)
		}
	}
	// Every reason is reported, not only the first.
	if reasons := refusal(t, head+"  - {id: a, type: strz, encoding: ASCII}\n  - {id: b, size-eos: true}\n"); len(reasons) != 2 {
		t.Fatalf("reasons %v", reasons)
	}
}

func TestYAMLSubset(t *testing.T) {
	n, e := parseYAML([]byte("a: 'it''s'\nb: \"x\\ty\\u00e9\"\nc: |\n  one\n   two\n\n  three\nd: >-\n  folded\n  line\n\n  para\ne: [1, [2, 3], {k: v}]\nf:\n- x\n- y: 1\n  z: 2\n# comment\ng: plain # trailing\n"))
	if e != nil {
		t.Fatal(e)
	}
	for k, want := range map[string]string{"a": "it's", "b": "x\tyé", "c": "one\n two\n\nthree\n", "d": "folded line\npara", "g": "plain"} {
		if got := n.get(k).text; got != want {
			t.Fatalf("%s: %q, want %q", k, got, want)
		}
	}
	if e := n.get("e"); len(e.items) != 3 || e.items[1].items[1].text != "3" || e.items[2].get("k").text != "v" {
		t.Fatalf("flow %+v", e)
	}
	if f := n.get("f"); len(f.items) != 2 || f.items[1].get("z").text != "2" {
		t.Fatalf("sequence %+v", f)
	}
	for _, bad := range []string{"a: *x\n", "a: !tag x\n", "? a\n", "a: 1\na: 2\n", "a:\n\t- x\n", "a: [1, 2\n", "a: \"open\n"} {
		if _, e := parseYAML([]byte(bad)); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

// TestGallerySurvey runs the importer over a full checkout of the Kaitai
// Struct gallery when KAITAI_GALLERY names one, and reports the count.
func TestGallerySurvey(t *testing.T) {
	dir := os.Getenv("KAITAI_GALLERY")
	if dir == "" {
		t.Skip("KAITAI_GALLERY not set")
	}
	total, imported := 0, 0
	filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
		if e != nil || d.IsDir() || !strings.HasSuffix(p, ".ksy") {
			return e
		}
		total++
		src, _ := os.ReadFile(p)
		if _, e := Import(src); e == nil {
			imported++
			t.Logf("imported %s", strings.TrimPrefix(p, dir+"/"))
		}
		return nil
	})
	t.Logf("imported %d of %d", imported, total)
}

// FuzzImport feeds arbitrary text to the importer: it must not panic, and
// whatever it returns must compile.
func FuzzImport(f *testing.F) {
	files, _ := filepath.Glob("testdata/gallery/*.ksy")
	for _, file := range files {
		src, _ := os.ReadFile(file)
		f.Add(src)
	}
	f.Add([]byte("meta: {id: x, endian: le}\nseq:\n  - id: a\n    type: u1\n    repeat: eos\n"))
	f.Fuzz(func(t *testing.T, src []byte) {
		r, e := Import(src)
		if e != nil {
			var refused *Refusal
			if !errors.As(e, &refused) || len(refused.Reasons) == 0 {
				t.Fatalf("error without reasons: %v", e)
			}
			return
		}
		if _, e := ebyte.Compile(r.Schema, ebyte.DefaultLimits()); e != nil {
			t.Fatal(e)
		}
	})
}

// Type and enum names resolve where a type is defined, not where it is used,
// as Kaitai resolves them. Both cases were wrong imports found in review.
func TestImportResolvesNamesAtDefinition(t *testing.T) {
	decodeOne := func(src string, input []byte) map[string]any {
		t.Helper()
		r, e := Import([]byte(src))
		if e != nil {
			t.Fatal(e)
		}
		p, e := ebyte.Compile(r.Schema, ebyte.DefaultLimits())
		if e != nil {
			t.Fatal(e)
		}
		doc, _, e := p.Decode(context.Background(), bytes.NewReader(input))
		if e != nil {
			t.Fatal(e)
		}
		return doc.Values
	}
	// baz is used inside foo, but defined at the top, so its bar is the top-level bar.
	v := decodeOne(`meta: {id: scope, endian: le}
seq: [{id: f, type: foo}]
types:
  foo:
    seq: [{id: z, type: baz}]
    types:
      bar:
        seq: [{id: wide, type: u2}]
  baz:
    seq: [{id: q, type: bar}]
  bar:
    seq: [{id: narrow, type: u1}]
`, []byte{1})
	got, _ := json.Marshal(v)
	if string(got) != `{"f":[{"z":[{"q":[{"narrow":"1"}]}]}]}` {
		t.Fatalf("scope: %s", got)
	}
	// A type's own enum shadows a top-level enum of the same name.
	v = decodeOne(`meta: {id: en, endian: le}
seq: [{id: t, type: inner}]
types:
  inner:
    seq:
      - {id: k, type: u1, enum: kind}
      - {id: extra, type: u1, if: k == kind::special}
    enums:
      kind: {1: special, 2: other}
enums:
  kind: {2: special, 1: other}
`, []byte{1, 7})
	if got, _ := json.Marshal(v); string(got) != `{"t":[{"extra":"7","k":"1"}]}` {
		t.Fatalf("enum: %s", got)
	}
}

// Unclosed flow collections and quoted scalars spread over a whole 1 MiB file
// are read in one pass; review measured over three minutes before the fix.
func TestYAMLLongRunsAreLinear(t *testing.T) {
	for _, src := range []string{
		"seq: [\n" + strings.Repeat("  x,\n", 250000),
		"doc: \"" + strings.Repeat("  word\n", 140000),
	} {
		start := time.Now()
		if _, e := Import([]byte(src)); e == nil {
			t.Fatal("accepted")
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Fatalf("took %v", d)
		}
	}
}

// Refusal reasons never quote values from the file.
func TestRefusalsDoNotEchoContent(t *testing.T) {
	reasons := refusal(t, "meta: {id: r, endian: le}\nseq:\n  - {id: a, type: u1}\n  - {id: b, size: hunter2 + 1}\n  - {id: c, type: secret_type}\n  - {id: d, size: 1, hunter3: x}\n")
	for _, r := range reasons {
		if strings.Contains(r.Reason, "hunter") || strings.Contains(r.Reason, "secret") {
			t.Fatalf("echoed: %v", r)
		}
	}
	_, e := parseYAML([]byte("hunter2: 1\nhunter2: 2\n"))
	if e == nil || strings.Contains(e.Error(), "hunter") {
		t.Fatalf("duplicate key: %v", e)
	}
}
