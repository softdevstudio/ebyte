package ebyte

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func schema(fields string) []byte {
	return []byte(`{"schema_version":1,"name":"test","endian":"little","fields":` + fields + `}`)
}
func TestCompileGolden(t *testing.T) {
	for _, name := range []string{"packet", "wide", "empty", "stored"} {
		t.Run(name, func(t *testing.T) {
			raw, e := os.ReadFile("testdata/" + name + ".schema.json")
			if e != nil {
				t.Fatal(e)
			}
			p, e := Compile(raw, DefaultLimits())
			if e != nil {
				t.Fatal(e)
			}
			sum := sha256.Sum256(raw)
			if p.SchemaSHA256() != hex.EncodeToString(sum[:]) {
				t.Fatal("identity")
			}
			raw[0] = '!'
			if p.Name() == "" || p.Limits() != DefaultLimits() {
				t.Fatal("plan metadata")
			}
		})
	}
}
func TestCompileReject(t *testing.T) {
	cases := []struct{ name, fields, path string }{
		{"unknown", `[{"name":"x","type":"u8","secret":"private"}]`, `/fields/0`},
		{"conditional source", `[{"name":"flag","type":"u8"},{"name":"n","type":"u8","when":{"field":"flag","equals":"1"}},{"name":"data","type":"bytes","length":{"field":"n"}}]`, `/fields/2/length/field`},
		{"parent source", `[{"name":"n","type":"u8"},{"name":"items","type":"array","count":1,"fields":[{"name":"data","type":"bytes","length":{"field":"n"}}]}]`, `/fields/1/fields/0/length/field`},
		{"fraction", `[{"name":"x","type":"bytes","length":1.0}]`, `/fields/0/length`},
		{"exponent", `[{"name":"x","type":"bytes","length":1e0}]`, `/fields/0/length`},
		{"negative zero", `[{"name":"x","type":"u8","expect":"-0"}]`, `/fields/0/expect`},
		{"overflow", `[{"name":"x","type":"i8","expect":"128"}]`, `/fields/0/expect`},
		{"dynamic assertion", `[{"name":"n","type":"u8"},{"name":"x","type":"bytes","length":{"field":"n"},"expect":""}]`, `/fields/1/expect`},
		{"derived reuse", `[{"name":"n","type":"u8","derive":{"count_of":"items"}},{"name":"items","type":"array","count":{"field":"n"},"fields":[]},{"name":"data","type":"bytes","length":{"field":"n"}}]`, `/fields/0/derive/count_of`},
		{"signed derived", `[{"name":"n","type":"i8","derive":{"count_of":"items"}},{"name":"items","type":"array","count":{"field":"n"},"fields":[]}]`, `/fields/0/derive`},
		{"duplicate names", `[{"name":"x","type":"u8"},{"name":"x","type":"u8"}]`, `/fields/1/name`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, e := Compile(schema(tc.fields), DefaultLimits())
			d, ok := e.(*Diagnostic)
			if !ok || d.Code != "invalid_schema" || d.Path != tc.path {
				t.Fatalf("got %#v", e)
			}
			if d.Offset != nil || d.FieldStart != nil {
				t.Fatal("schema binary offsets")
			}
		})
	}
}
func TestStrictJSON(t *testing.T) {
	raws := [][]byte{
		[]byte(`{"schema_version":1,"name":"a","na\u006de":"b","endian":"little","fields":[]}`),
		[]byte(`{"schema_version":1,"name":"\ud800","endian":"little","fields":[]}`),
		[]byte(`{"schema_version":1,"name":"\udc00","endian":"little","fields":[]}`),
		append(schema(`[]`), 'x'), append([]byte{0xef, 0xbb, 0xbf}, schema(`[]`)...),
		schema(`null`), schema(`[null]`), schema(`[true]`), []byte{0xff},
	}
	for _, raw := range raws {
		if _, e := Compile(raw, DefaultLimits()); e == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	// A valid surrogate pair in an unknown member must still be rejected by grammar.
	if !validEscapes([]byte(`"\ud83d\ude00"`)) {
		t.Fatal("valid pair rejected")
	}
}
func TestCompileLimits(t *testing.T) {
	nested := `[]`
	for i := 0; i < 8; i++ {
		nested = `[{"name":"a","type":"array","count":0,"fields":` + nested + `}]`
	}
	if _, e := Compile(schema(nested), DefaultLimits()); e != nil {
		t.Fatal(e)
	}
	nested = `[{"name":"a","type":"array","count":0,"fields":` + nested + `}]`
	if _, e := Compile(schema(nested), DefaultLimits()); e == nil {
		t.Fatal("ninth array")
	}
	for _, test := range []struct {
		fields string
		change func(*Limits)
	}{
		{`[]`, func(l *Limits) { l.MaxSchemaBytes = 1 }},
		{`[{"name":"a","type":"u8"}]`, func(l *Limits) { l.MaxSchemaFields = 0 }},
		{`[{"name":"a","type":"bytes","length":2}]`, func(l *Limits) { l.MaxFieldBytes = 1 }},
		{`[{"name":"a","type":"array","count":2,"fields":[]}]`, func(l *Limits) { l.MaxArrayElements = 1 }},
		{`[]`, func(l *Limits) { l.MaxDepth = 9 }},
		{`[]`, func(l *Limits) { l.MaxTraceBytes = -1 }},
	} {
		l := DefaultLimits()
		test.change(&l)
		_, e := Compile(schema(test.fields), l)
		d, ok := e.(*Diagnostic)
		if !ok || d.Code != "limit_exceeded" {
			t.Fatalf("got %#v", e)
		}
	}
}
func TestResolvedPlan(t *testing.T) {
	p, e := Compile(schema(`[{"name":"n","type":"u16","derive":{"count_of":"items"}},{"name":"items","type":"array","count":{"field":"n"},"fields":[{"name":"x","type":"u16","endian":"big"}]}]`), DefaultLimits())
	if e != nil {
		t.Fatal(e)
	}
	if p.fields[0].derive != 1 || p.fields[1].length.ref != 0 || p.fields[1].children[0].endian != "big" {
		t.Fatal("unresolved plan")
	}
}
func TestCompileAcceptanceVectors(t *testing.T) {
	raw, e := os.ReadFile("testdata/cases.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Name       string `json:"name"`
		Operation  string `json:"operation"`
		SchemaText string `json:"schema_text"`
		Error      struct {
			Code string `json:"code"`
			Path string `json:"path"`
		} `json:"error"`
	}
	if e = json.Unmarshal(raw, &cases); e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, tc := range cases {
		if tc.Operation != "compile" {
			continue
		}
		count++
		t.Run(tc.Name, func(t *testing.T) {
			_, e := Compile([]byte(tc.SchemaText), DefaultLimits())
			d, ok := e.(*Diagnostic)
			if !ok || d.Code != tc.Error.Code || d.Path != tc.Error.Path || d.Offset != nil || d.FieldStart != nil {
				t.Fatalf("got %#v", e)
			}
		})
	}
	if count != 3 {
		t.Fatalf("expected three compile vectors, got %d", count)
	}
}
func FuzzCompile(f *testing.F) {
	f.Add(schema(`[]`))
	f.Add(schema(`[{"name":"n","type":"u64"}]`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, e := Compile(raw, DefaultLimits())
		if e == nil && (p == nil || len(p.SchemaSHA256()) != 64) {
			t.Fatal("invalid success")
		}
		if e != nil {
			d, ok := e.(*Diagnostic)
			if !ok || len(d.Expected) > 96 || len(d.Observed) > 96 || strings.Contains(d.Error(), "private") {
				t.Fatal("invalid diagnostic")
			}
		}
	})
}

func TestDeclarationAndDepthBoundaries(t *testing.T) {
	fields := make([]string, 1025)
	for i := range fields {
		fields[i] = fmt.Sprintf(`{"name":"f%d","type":"u8"}`, i)
	}
	if _, e := Compile(schema("["+strings.Join(fields[:1024], ",")+"]"), DefaultLimits()); e != nil {
		t.Fatal(e)
	}
	if _, e := Compile(schema("["+strings.Join(fields, ",")+"]"), DefaultLimits()); e == nil {
		t.Fatal("1025 declarations accepted")
	}
	l := DefaultLimits()
	l.MaxDepth = 0
	if _, e := Compile(schema(`[{"name":"x","type":"u8"}]`), l); e != nil {
		t.Fatal(e)
	}
	if _, e := Compile(schema(`[{"name":"x","type":"array","count":0,"fields":[]}]`), l); e == nil {
		t.Fatal("array at zero depth")
	}
}
func TestInvalidBindings(t *testing.T) {
	for _, fields := range []string{
		`[{"name":"items","type":"array","count":{"field":"n"},"fields":[]},{"name":"n","type":"u8"}]`,
		`[{"name":"n","type":"u8","derive":{"count_of":"missing"}}]`,
		`[{"name":"n","type":"u8","derive":{"count_of":"items"}},{"name":"items","type":"array","count":1,"fields":[]}]`,
		`[{"name":"flag","type":"u8"},{"name":"n","type":"u8","derive":{"count_of":"items"}},{"name":"items","type":"array","count":{"field":"n"},"fields":[],"when":{"field":"flag","equals":"1"}}]`,
		`[{"name":"n","type":"u8","expect":"1","derive":{"count_of":"items"}},{"name":"items","type":"array","count":{"field":"n"},"fields":[]}]`,
	} {
		if _, e := Compile(schema(fields), DefaultLimits()); e == nil {
			t.Fatalf("accepted %s", fields)
		}
	}
}
