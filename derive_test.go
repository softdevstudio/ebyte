package ebyte

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// boxSchema uses every derive form. The fixtures below were laid out by hand,
// byte by byte, from this declaration; none was produced by the codec.
const boxSchema = `{"schema_version":1,"name":"box","endian":"little","fields":[
{"name":"magic","type":"bytes","length":2,"expect":"4258"},
{"name":"total","type":"u16","derive":{"size_of":{"from":"magic","to":"tail"}}},
{"name":"name_len","type":"u8","derive":{"size_of":"name"}},
{"name":"name","type":"string","encoding":"ascii","length":{"field":"name_len"}},
{"name":"items_off","type":"u16","derive":{"offset_of":"items"}},
{"name":"items_len","type":"u16","derive":{"size_of":"items"}},
{"name":"count","type":"u8","derive":{"count_of":"items"}},
{"name":"body_size","type":"u16","derive":{"size_of":{"from":"name_len","to":"tail"},"add":"4"}},
{"name":"items","type":"repeat","until":"tag_mismatch","fields":[
 {"name":"tag","type":"bytes","length":1,"expect":"49"},
 {"name":"vlen","type":"u8","derive":{"size_of":"v"}},
 {"name":"v","type":"bytes","length":{"field":"vlen"}}]},
{"name":"tail","type":"bytes","length":2,"expect":"454e"}]}`

// 0 magic, 2 total=23, 4 name_len=2, 5 "ab", 7 items_off=14, 9 items_len=7,
// 11 count=2, 12 body_size=23-4+4=23, 14 items (3+4 bytes), 21 tail; 23 bytes.
const boxHex = "4258 1700 02 6162 0e00 0700 02 1700 490101 49020203 454e"

func boxValues(name string, items ...string) map[string]any {
	list := []any{}
	for _, v := range items {
		list = append(list, map[string]any{"tag": "49", "v": v})
	}
	return map[string]any{"magic": "4258", "name": name, "items": list, "tail": "454e"}
}

func TestDeriveGolden(t *testing.T) {
	p := repeatPlan(t, boxSchema, DefaultLimits())
	input := unhex(t, boxHex)
	doc, trace, e := p.Decode(context.Background(), bytes.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(doc.Values, boxValues("ab", "01", "0203")) {
		t.Fatalf("values %#v", doc.Values)
	}
	want := map[string]string{"/total": "23", "/name_len": "2", "/items_off": "14", "/items_len": "7", "/count": "2", "/body_size": "23", "/items/0/vlen": "1", "/items/1/vlen": "2"}
	for _, entry := range trace.Entries {
		if v, ok := want[entry.Path]; ok {
			if entry.Value == nil || *entry.Value != v {
				t.Fatalf("%s: trace value %v, want %s", entry.Path, entry.Value, v)
			}
			delete(want, entry.Path)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing trace entries %v", want)
	}
	var out bytes.Buffer
	if e := p.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), input) {
		t.Fatalf("round trip: %v %x", e, out.Bytes())
	}

	// A longer name and a third item move everything after them. Hand layout:
	// 0 magic, 2 total=28, 4 name_len=4, 5 "abcd", 9 items_off=16, 11 items_len=10,
	// 13 count=3, 14 body_size=28-4+4=28, 16 items (3+4+3), 26 tail.
	doc.Values = boxValues("abcd", "01", "0203", "ff")
	out.Reset()
	if e := p.Encode(context.Background(), doc, &out); e != nil {
		t.Fatal(e)
	}
	if want := strings.ReplaceAll("4258 1c00 04 61626364 1000 0a00 03 1c00 490101 49020203 4901ff 454e", " ", ""); hex.EncodeToString(out.Bytes()) != want {
		t.Fatalf("edited %x, want %s", out.Bytes(), want)
	}
	again, _, e := p.Decode(context.Background(), bytes.NewReader(out.Bytes()))
	if e != nil || !reflect.DeepEqual(again.Values, doc.Values) {
		t.Fatalf("edited output did not decode back: %v", e)
	}
}

func TestDeriveStaleValuesFailDecode(t *testing.T) {
	p := repeatPlan(t, boxSchema, DefaultLimits())
	for _, c := range []struct {
		name string
		at   int
		b    byte
		want string
	}{
		{"total", 2, 0x16, "constraint_mismatch /total 2 2"},
		{"items_off", 7, 0x0d, "constraint_mismatch /items_off 7 7"},
		{"items_len", 9, 0x08, "constraint_mismatch /items_len 9 9"},
		{"count", 11, 0x03, "constraint_mismatch /count 11 11"},
		{"body_size", 12, 0x18, "constraint_mismatch /body_size 12 12"},
	} {
		input := unhex(t, boxHex)
		input[c.at] = c.b
		_, trace, e := p.Decode(context.Background(), bytes.NewReader(input))
		if got := at(diagnosticOf(t, e)); got != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, got, c.want)
		}
		for _, entry := range trace.Entries {
			if entry.Path == "/"+c.name && entry.State != "failed" {
				t.Fatalf("%s: trace entry not marked failed", c.name)
			}
		}
	}
}

func TestDeriveOffsetsAreAbsolute(t *testing.T) {
	p := repeatPlan(t, `{"schema_version":1,"name":"o","endian":"little","fields":[
{"name":"pad","type":"bytes","length":1},
{"name":"rows","type":"array","count":2,"fields":[
 {"name":"here","type":"u8","derive":{"offset_of":"b"}},
 {"name":"b","type":"bytes","length":1}]}]}`, DefaultLimits())
	input := unhex(t, "aa 02bb 04cc")
	doc, _, e := p.Decode(context.Background(), bytes.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := p.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), input) {
		t.Fatalf("round trip %v %x", e, out.Bytes())
	}
	if _, _, e := p.Decode(context.Background(), bytes.NewReader(unhex(t, "aa 01bb 04cc"))); at(diagnosticOf(t, e)) != "constraint_mismatch /rows/0/here 1 1" {
		t.Fatalf("relative offset accepted: %v", e)
	}
}

func TestDeriveEncodeFailures(t *testing.T) {
	small := repeatPlan(t, `{"schema_version":1,"name":"s","endian":"little","fields":[
{"name":"n","type":"u8","derive":{"size_of":"b"}},
{"name":"len","type":"u16"},
{"name":"b","type":"bytes","length":{"field":"len"}}]}`, DefaultLimits())
	doc := &Document{DocumentVersion: 1, SchemaSHA256: small.SchemaSHA256(), Values: map[string]any{"len": "300", "b": strings.Repeat("00", 300)}}
	var out bytes.Buffer
	if got := at(diagnosticOf(t, small.Encode(context.Background(), doc, &out))); got != "value_out_of_range /n 0 0" || out.Len() != 0 {
		t.Fatalf("too large: %q", got)
	}
	neg := repeatPlan(t, `{"schema_version":1,"name":"s","endian":"little","fields":[
{"name":"b","type":"bytes","length":1},
{"name":"n","type":"u8","derive":{"offset_of":"b","add":"-1"}}]}`, DefaultLimits())
	doc = &Document{DocumentVersion: 1, SchemaSHA256: neg.SchemaSHA256(), Values: map[string]any{"b": "00"}}
	if got := at(diagnosticOf(t, neg.Encode(context.Background(), doc, &out))); got != "value_out_of_range /n 1 1" {
		t.Fatalf("negative: %q", got)
	}
	box := repeatPlan(t, boxSchema, DefaultLimits())
	for _, name := range []string{"total", "name_len", "count", "items_off"} {
		values := boxValues("ab")
		values[name] = "1"
		doc = &Document{DocumentVersion: 1, SchemaSHA256: box.SchemaSHA256(), Values: values}
		if got := at(diagnosticOf(t, box.Encode(context.Background(), doc, &out))); got != "invalid_document /"+name+" null null" {
			t.Fatalf("%s supplied: %q", name, got)
		}
		raw := []byte(`{"document_version":1,"schema_sha256":"` + box.SchemaSHA256() + `","values":{"magic":"4258","` + name + `":"1","name":"ab","items":[],"tail":"454e"}}`)
		if _, e := box.LoadDocument(context.Background(), raw); at(diagnosticOf(t, e)) != "invalid_document /"+name+" null null" {
			t.Fatalf("%s loaded: %v", name, e)
		}
	}
}

func TestDeriveSchemaRules(t *testing.T) {
	head := `{"schema_version":1,"name":"s","endian":"little","fields":[`
	for _, c := range []struct{ name, fields, path string }{
		{"measured used as a length", `{"name":"n","type":"u8","derive":{"size_of":"x"}},{"name":"x","type":"bytes","length":2},{"name":"b","type":"bytes","length":{"field":"n"}}`, "/fields/0/derive/size_of"},
		{"measured used by when", `{"name":"n","type":"u8","derive":{"offset_of":"x"}},{"name":"x","type":"bytes","length":2},{"name":"b","type":"u8","when":{"field":"n","equals":"0"}}`, "/fields/0/derive/offset_of"},
		{"add on a driving size", `{"name":"n","type":"u8","derive":{"size_of":"b","add":"1"}},{"name":"b","type":"bytes","length":{"field":"n"}}`, "/fields/0/derive/add"},
		{"add on a driving count", `{"name":"n","type":"u8","derive":{"count_of":"a","add":"1"}},{"name":"a","type":"array","count":{"field":"n"},"fields":[]}`, "/fields/0/derive/add"},
		{"noncanonical add", `{"name":"n","type":"u8","derive":{"offset_of":"x","add":"+1"}},{"name":"x","type":"u8"}`, "/fields/0/derive/add"},
		{"add out of range", `{"name":"n","type":"u8","derive":{"offset_of":"x","add":"9223372036854775808"}},{"name":"x","type":"u8"}`, "/fields/0/derive/add"},
		{"numeric add", `{"name":"n","type":"u8","derive":{"offset_of":"x","add":1}},{"name":"x","type":"u8"}`, "/fields/0/derive/add"},
		{"range backwards", `{"name":"a","type":"u8"},{"name":"b","type":"u8"},{"name":"n","type":"u8","derive":{"size_of":{"from":"b","to":"a"}}}`, "/fields/2/derive/size_of"},
		{"range missing to", `{"name":"a","type":"u8"},{"name":"n","type":"u8","derive":{"size_of":{"from":"a"}}}`, "/fields/1/derive/size_of/to"},
		{"range unknown from", `{"name":"a","type":"u8"},{"name":"n","type":"u8","derive":{"size_of":{"from":"zz","to":"a"}}}`, "/fields/1/derive/size_of/from"},
		{"unknown size target", `{"name":"n","type":"u8","derive":{"size_of":"zz"}}`, "/fields/0/derive/size_of"},
		{"unknown offset target", `{"name":"n","type":"u8","derive":{"offset_of":"zz"}}`, "/fields/0/derive/offset_of"},
		{"two forms", `{"name":"x","type":"u8"},{"name":"n","type":"u8","derive":{"size_of":"x","offset_of":"x"}}`, "/fields/1/derive"},
		{"no form", `{"name":"x","type":"u8"},{"name":"n","type":"u8","derive":{"add":"1"}}`, "/fields/1/derive"},
		{"signed measured", `{"name":"x","type":"u8"},{"name":"n","type":"i8","derive":{"offset_of":"x"}}`, "/fields/1/derive"},
		{"driving a conditional field", `{"name":"f","type":"u8"},{"name":"n","type":"u8","derive":{"size_of":"b"}},{"name":"b","type":"bytes","length":{"field":"n"},"when":{"field":"f","equals":"1"}}`, "/fields/1/derive/size_of"},
		{"driving two fields", `{"name":"n","type":"u8","derive":{"size_of":"b"}},{"name":"b","type":"bytes","length":{"field":"n"}},{"name":"c","type":"bytes","length":{"field":"n"}}`, "/fields/0/derive/size_of"},
	} {
		_, e := Compile([]byte(head+c.fields+`]}`), DefaultLimits())
		if e == nil {
			t.Fatalf("%s: accepted", c.name)
		}
		if d := diagnosticOf(t, e); d.Code != "invalid_schema" || d.Path != c.path {
			t.Fatalf("%s: %s at %q, want %q", c.name, d.Code, d.Path, c.path)
		}
	}
	// A driving size may still be used by a later when, like a driving count.
	ok := head + `{"name":"n","type":"u8","derive":{"size_of":"b"}},{"name":"b","type":"string","encoding":"utf8","length":{"field":"n"}},{"name":"z","type":"u8","when":{"field":"n","equals":"0"}}]}`
	p, e := Compile([]byte(ok), DefaultLimits())
	if e != nil {
		t.Fatal(e)
	}
	// UTF-8 strings are sized in bytes: "é" is two.
	var out bytes.Buffer
	if e := p.Encode(context.Background(), &Document{DocumentVersion: 1, SchemaSHA256: p.SchemaSHA256(), Values: map[string]any{"b": "é"}}, &out); e != nil || hex.EncodeToString(out.Bytes()) != "02c3a9" {
		t.Fatalf("utf8 size: %v %x", e, out.Bytes())
	}
	out.Reset()
	if e := p.Encode(context.Background(), &Document{DocumentVersion: 1, SchemaSHA256: p.SchemaSHA256(), Values: map[string]any{"b": "", "z": "7"}}, &out); e != nil || hex.EncodeToString(out.Bytes()) != "0007" {
		t.Fatalf("when on driving size: %v %x", e, out.Bytes())
	}
}

// FuzzDerivedSizes builds documents from fuzz data, encodes them and checks
// every derived integer against the output with encoding/binary, then checks
// that the output decodes back to the same values.
func FuzzDerivedSizes(f *testing.F) {
	f.Add([]byte("ab"), []byte{1, 2, 3})
	f.Add([]byte{}, []byte{})
	f.Add([]byte("a longer name"), bytes.Repeat([]byte{7}, 40))
	p, e := Compile([]byte(boxSchema), DefaultLimits())
	if e != nil {
		f.Fatal(e)
	}
	f.Fuzz(func(t *testing.T, name, data []byte) {
		if len(name) > 200 || len(data) > 2000 {
			return
		}
		for i := range name {
			name[i] = 'a' + name[i]%26
		}
		// Split data into items: each byte n starts an item of n%8 bytes.
		var items []string
		for len(data) > 0 {
			n := 1 + int(data[0]%8)
			if n > len(data) {
				n = len(data)
			}
			items = append(items, hex.EncodeToString(data[:n]))
			data = data[n:]
		}
		values := boxValues(string(name), items...)
		var out bytes.Buffer
		if e := p.Encode(context.Background(), &Document{DocumentVersion: 1, SchemaSHA256: p.SchemaSHA256(), Values: values}, &out); e != nil {
			// Only the u16/u8 widths may be exceeded.
			if d := diagnosticOf(t, e); d.Code != "value_out_of_range" {
				t.Fatalf("encode: %v", e)
			}
			return
		}
		b := out.Bytes()
		itemsLen := 0
		for _, v := range items {
			itemsLen += 2 + len(v)/2
		}
		itemsOff := 14 + len(name) - 2
		if int(binary.LittleEndian.Uint16(b[2:])) != len(b) || int(b[4]) != len(name) ||
			int(binary.LittleEndian.Uint16(b[5+len(name):])) != itemsOff ||
			int(binary.LittleEndian.Uint16(b[7+len(name):])) != itemsLen || int(b[9+len(name)]) != len(items) ||
			int(binary.LittleEndian.Uint16(b[10+len(name):])) != len(b) {
			t.Fatalf("derived values disagree with the output %x", b)
		}
		doc, _, e := p.Decode(context.Background(), bytes.NewReader(b))
		if e != nil || !reflect.DeepEqual(doc.Values, values) {
			t.Fatalf("decode back: %v", e)
		}
	})
}
