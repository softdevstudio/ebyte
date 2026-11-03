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

// indexSchema keeps a table of element offsets after the elements, as a ZIP
// central directory does. The fixtures below were laid out by hand.
const indexSchema = `{"schema_version":1,"name":"index","endian":"little","fields":[
{"name":"records","type":"repeat","until":"tag_mismatch","fields":[
 {"name":"tag","type":"bytes","length":1,"expect":"52"},
 {"name":"len","type":"u8","derive":{"size_of":"data"}},
 {"name":"data","type":"bytes","length":{"field":"len"}}]},
{"name":"slots","type":"repeat","until":"tag_mismatch","fields":[
 {"name":"tag","type":"bytes","length":1,"expect":"53"},
 {"name":"at","type":"u16","derive":{"offset_of":{"element_of":"records"}}},
 {"name":"past","type":"u16","derive":{"offset_of":{"element_of":"records"},"add":"2"}}]},
{"name":"end","type":"bytes","length":1,"expect":"45"}]}`

// 0 R len=1 aa, 3 R len=2 bbcc, 7 S at=0 past=2, 12 S at=3 past=5, 17 E; 18 bytes.
const indexHex = "52 01 aa 52 02 bbcc 53 0000 0200 53 0300 0500 45"

func indexValues(data ...string) map[string]any {
	records, slots := []any{}, []any{}
	for _, d := range data {
		records = append(records, map[string]any{"tag": "52", "data": d})
		slots = append(slots, map[string]any{"tag": "53"})
	}
	return map[string]any{"records": records, "slots": slots, "end": "45"}
}

func TestElementOffsetsGolden(t *testing.T) {
	p := repeatPlan(t, indexSchema, DefaultLimits())
	input := unhex(t, indexHex)
	doc, trace, e := p.Decode(context.Background(), bytes.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(doc.Values, indexValues("aa", "bbcc")) {
		t.Fatalf("values %#v", doc.Values)
	}
	want := map[string]string{"/slots/0/at": "0", "/slots/0/past": "2", "/slots/1/at": "3", "/slots/1/past": "5"}
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
	// A longer first record moves the second. Hand layout: 0 R len=3 010203,
	// 5 R len=2 bbcc, 9 R len=0, 11 S 0 2, 16 S 5 7, 21 S 9 11, 26 E.
	doc.Values = indexValues("010203", "bbcc", "")
	out.Reset()
	if e := p.Encode(context.Background(), doc, &out); e != nil {
		t.Fatal(e)
	}
	if want := strings.ReplaceAll("52 03 010203 52 02 bbcc 52 00 53 0000 0200 53 0500 0700 53 0900 0b00 45", " ", ""); hex.EncodeToString(out.Bytes()) != want {
		t.Fatalf("edited %x, want %s", out.Bytes(), want)
	}
}

func TestElementOffsetsFailDecode(t *testing.T) {
	p := repeatPlan(t, indexSchema, DefaultLimits())
	for _, c := range []struct {
		name, hex, want string
	}{
		{"stale first", "52 01 aa 52 02 bbcc 53 0100 0200 53 0300 0500 45", "constraint_mismatch /slots/0/at 8 8"},
		{"stale add", "52 01 aa 52 02 bbcc 53 0000 0200 53 0300 0600 45", "constraint_mismatch /slots/1/past 15 15"},
		{"fewer slots", "52 01 aa 52 02 bbcc 53 0000 0200 45", "constraint_mismatch /slots 7 7"},
		{"more slots", "52 01 aa 53 0000 0200 53 0300 0500 45", "constraint_mismatch /slots 3 3"},
	} {
		_, trace, e := p.Decode(context.Background(), bytes.NewReader(unhex(t, c.hex)))
		if got := at(diagnosticOf(t, e)); got != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, got, c.want)
		}
		if strings.HasPrefix(c.name, "stale") {
			failed := false
			for _, entry := range trace.Entries {
				failed = failed || entry.State == "failed" && strings.HasPrefix(entry.Path, "/slots/")
			}
			if !failed {
				t.Fatalf("%s: trace entry not marked failed", c.name)
			}
		}
	}
	// Encoding refuses documents whose element counts differ.
	doc := &Document{DocumentVersion: 1, SchemaSHA256: p.SchemaSHA256(), Values: indexValues("aa")}
	doc.Values["slots"] = []any{}
	if e := p.Encode(context.Background(), doc, &bytes.Buffer{}); diagnosticOf(t, e).Code != "constraint_mismatch" || diagnosticOf(t, e).Path != "/slots" {
		t.Fatalf("count mismatch encoded: %v", e)
	}
}

// The named container may come after the one holding the offsets, may be an
// array, and the pair may sit inside an element of an outer container.
func TestElementOffsetsForwardAndNested(t *testing.T) {
	p := repeatPlan(t, `{"schema_version":1,"name":"n","endian":"big","fields":[
{"name":"groups","type":"array","count":2,"fields":[
 {"name":"n","type":"u8"},
 {"name":"index","type":"array","count":{"field":"n"},"fields":[
  {"name":"to","type":"u8","derive":{"offset_of":{"element_of":"blobs"}}}]},
 {"name":"blobs","type":"array","count":{"field":"n"},"fields":[
  {"name":"b","type":"bytes","length":2}]}]}]}`, DefaultLimits())
	// 0 n=2 [3 5] 3 aaaa 5 bbbb; 7 n=1 [9] 9 cccc; 11 bytes.
	input := unhex(t, "02 03 05 aaaa bbbb 01 09 cccc")
	doc, _, e := p.Decode(context.Background(), bytes.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := p.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), input) {
		t.Fatalf("round trip %v %x", e, out.Bytes())
	}
	if _, _, e := p.Decode(context.Background(), bytes.NewReader(unhex(t, "02 03 05 aaaa bbbb 01 08 cccc"))); at(diagnosticOf(t, e)) != "constraint_mismatch /groups/1/index/0/to 8 8" {
		t.Fatalf("nested stale offset: %v", e)
	}
}

func TestElementOffsetSchemaRules(t *testing.T) {
	head := `{"schema_version":1,"name":"r","endian":"little","fields":[`
	items := `{"name":"items","type":"array","count":1,"fields":[{"name":"b","type":"u8"}]}`
	for _, c := range []struct {
		name, fields, path string
	}{
		{"top level", items + `,{"name":"o","type":"u8","derive":{"offset_of":{"element_of":"items"}}}]}`, "/fields/1/derive/offset_of"},
		{"unknown", `{"name":"t","type":"array","count":1,"fields":[{"name":"o","type":"u8","derive":{"offset_of":{"element_of":"nope"}}}]}]}`, "/fields/0/fields/0/derive/offset_of/element_of"},
		{"own container", `{"name":"t","type":"array","count":1,"fields":[{"name":"o","type":"u8","derive":{"offset_of":{"element_of":"t"}}}]}]}`, "/fields/0/fields/0/derive/offset_of/element_of"},
		{"not a container", `{"name":"x","type":"u8"},{"name":"t","type":"array","count":1,"fields":[{"name":"o","type":"u8","derive":{"offset_of":{"element_of":"x"}}}]}]}`, "/fields/1/fields/0/derive/offset_of/element_of"},
		{"extra key", items + `,{"name":"t","type":"array","count":1,"fields":[{"name":"o","type":"u8","derive":{"offset_of":{"element_of":"items","x":"1"}}}]}]}`, "/fields/1/fields/0/derive/offset_of"},
		{"not a name", items + `,{"name":"t","type":"array","count":1,"fields":[{"name":"o","type":"u8","derive":{"offset_of":{"element_of":3}}}]}]}`, "/fields/1/fields/0/derive/offset_of/element_of"},
		{"signed", items + `,{"name":"t","type":"array","count":1,"fields":[{"name":"o","type":"i8","derive":{"offset_of":{"element_of":"items"}}}]}]}`, "/fields/1/fields/0/derive"},
		{"length", items + `,{"name":"t","type":"array","count":1,"fields":[{"name":"o","type":"u8","derive":{"offset_of":{"element_of":"items"}}},{"name":"d","type":"bytes","length":{"field":"o"}}]}]}`, "/fields/1/fields/0/derive/offset_of"},
		{"when", items + `,{"name":"t","type":"array","count":1,"fields":[{"name":"o","type":"u8","derive":{"offset_of":{"element_of":"items"}}},{"name":"d","type":"u8","when":{"field":"o","equals":"0"}}]}]}`, "/fields/1/fields/0/derive/offset_of"},
		{"checksum range", items + `,{"name":"t","type":"array","count":1,"fields":[{"name":"o","type":"u16","derive":{"offset_of":{"element_of":"items"}}},{"name":"c","type":"u32","derive":{"checksum":"crc32","of":"o"}}]}]}`, "/fields/1/fields/1/derive/of"},
	} {
		_, e := Compile([]byte(head+c.fields), DefaultLimits())
		if d := diagnosticOf(t, e); d.Code != "invalid_schema" || d.Path != c.path {
			t.Fatalf("%s: %s at %q, want %q", c.name, d.Code, d.Path, c.path)
		}
	}
}

// FuzzElementOffsets encodes record lists built from fuzz data and checks every
// slot against the output with encoding/binary, then decodes the output back.
func FuzzElementOffsets(f *testing.F) {
	f.Add([]byte{1, 0xaa, 2, 0xbb, 0xcc})
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0})
	p, e := Compile([]byte(indexSchema), DefaultLimits())
	if e != nil {
		f.Fatal(e)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var parts []string
		for len(data) > 0 && len(parts) < 64 {
			n := int(data[0]) % 8
			data = data[1:]
			if n > len(data) {
				n = len(data)
			}
			parts = append(parts, hex.EncodeToString(data[:n]))
			data = data[n:]
		}
		doc := &Document{DocumentVersion: 1, SchemaSHA256: p.SchemaSHA256(), Values: indexValues(parts...)}
		var out bytes.Buffer
		if e := p.Encode(context.Background(), doc, &out); e != nil {
			t.Fatal(e)
		}
		b := out.Bytes()
		pos, starts := 0, []int{}
		for range parts {
			starts = append(starts, pos)
			pos += 2 + int(b[pos+1])
		}
		for k, s := range starts {
			slot := pos + 5*k
			if b[slot] != 0x53 || int(binary.LittleEndian.Uint16(b[slot+1:])) != s || int(binary.LittleEndian.Uint16(b[slot+3:])) != s+2 {
				t.Fatalf("slot %d: %x, want offset %d", k, b[slot:slot+5], s)
			}
		}
		again, _, e := p.Decode(context.Background(), bytes.NewReader(b))
		if e != nil || !reflect.DeepEqual(again.Values, doc.Values) {
			t.Fatalf("decode back: %v", e)
		}
	})
}

// A conditional container of offsets may be absent while its targets are
// present; when present, its counts must match.
func TestElementOffsetsOptionalIndex(t *testing.T) {
	p := repeatPlan(t, `{"schema_version":1,"name":"w","endian":"little","fields":[
{"name":"n","type":"u8"},
{"name":"has","type":"u8"},
{"name":"recs","type":"array","count":{"field":"n"},"fields":[{"name":"b","type":"bytes","length":1}]},
{"name":"idx","type":"array","count":{"field":"n"},"when":{"field":"has","equals":"1"},"fields":[
 {"name":"at","type":"u8","derive":{"offset_of":{"element_of":"recs"}}}]}]}`, DefaultLimits())
	for _, input := range []string{"02 00 aabb", "02 01 aabb 0203"} {
		doc, _, e := p.Decode(context.Background(), bytes.NewReader(unhex(t, input)))
		if e != nil {
			t.Fatalf("%s: %v", input, e)
		}
		var out bytes.Buffer
		if e := p.Encode(context.Background(), doc, &out); e != nil || hex.EncodeToString(out.Bytes()) != strings.ReplaceAll(input, " ", "") {
			t.Fatalf("%s: round trip %v %x", input, e, out.Bytes())
		}
	}
	if _, _, e := p.Decode(context.Background(), bytes.NewReader(unhex(t, "02 01 aabb 0303"))); at(diagnosticOf(t, e)) != "constraint_mismatch /idx/0/at 4 4" {
		t.Fatalf("stale offset in present index: %v", e)
	}
}
