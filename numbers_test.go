package ebyte

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func oneField(t *testing.T, field string) *Plan {
	t.Helper()
	return repeatPlan(t, `{"schema_version":1,"name":"t","endian":"big","fields":[`+field+`]}`, DefaultLimits())
}

func encodeValues(t *testing.T, p *Plan, values map[string]any) ([]byte, error) {
	t.Helper()
	var out bytes.Buffer
	e := p.Encode(context.Background(), &Document{DocumentVersion: 1, SchemaSHA256: p.SchemaSHA256(), Values: values}, &out)
	return out.Bytes(), e
}

// IEEE 754 bit patterns written out by hand, with the text each must decode to.
func TestFloatGolden(t *testing.T) {
	for _, c := range []struct{ kind, hex, text string }{
		{"f32", "3fc00000", "1.5"},
		{"f32", "80000000", "-0"},
		{"f32", "7f800000", "inf"},
		{"f32", "ff800000", "-inf"},
		{"f32", "7fc00001", "nan:7fc00001"},
		{"f32", "7f7fffff", "3.4028235e+38"},
		{"f32", "00000001", "1e-45"},
		{"f32", "3dcccccd", "0.1"},
		{"f64", "3fb999999999999a", "0.1"},
		{"f64", "fff8000000000000", "nan:fff8000000000000"},
		{"f64", "4415af1d78b58c40", "1e+20"},
		{"f64", "0000000000000000", "0"},
	} {
		p := oneField(t, `{"name":"x","type":"`+c.kind+`"}`)
		input := unhex(t, c.hex)
		doc, trace, e := p.Decode(context.Background(), bytes.NewReader(input))
		if e != nil || doc.Values["x"] != c.text || *trace.Entries[1].Value != c.text {
			t.Fatalf("%s %s: %v %#v", c.kind, c.hex, e, doc)
		}
		out, e := encodeValues(t, p, doc.Values)
		if e != nil || !bytes.Equal(out, input) {
			t.Fatalf("%s %s round trip: %v %x", c.kind, c.hex, e, out)
		}
	}
	// The field's endian applies: 1.5 as a little-endian f32.
	p := oneField(t, `{"name":"x","type":"f32","endian":"little"}`)
	if out, e := encodeValues(t, p, map[string]any{"x": "1.5"}); e != nil || hex.EncodeToString(out) != "0000c03f" {
		t.Fatalf("little endian %v %x", e, out)
	}
}

func TestFloatEncodeRules(t *testing.T) {
	f32 := oneField(t, `{"name":"x","type":"f32"}`)
	for text, want := range map[string]string{
		"0.1": "3dcccccd", "1.50": "3fc00000", "-2.5e0": "c0200000", "1e-50": "00000000", "1E2": "42c80000",
	} {
		if out, e := encodeValues(t, f32, map[string]any{"x": text}); e != nil || hex.EncodeToString(out) != want {
			t.Fatalf("%s: %v %x, want %s", text, e, out, want)
		}
	}
	for text, code := range map[string]string{
		"1e39": "value_out_of_range", "NaN": "invalid_document", "Infinity": "invalid_document", "+1": "invalid_document",
		"1.": "invalid_document", ".5": "invalid_document", "01": "invalid_document", "0x1p3": "invalid_document",
		"nan:7f800000": "invalid_document", "nan:7FC00000": "invalid_document", "nan:7fc0": "invalid_document", "": "invalid_document",
	} {
		if _, e := encodeValues(t, f32, map[string]any{"x": text}); diagnosticOf(t, e).Code != code {
			t.Fatalf("%q: %v, want %s", text, e, code)
		}
	}
}

func TestBitFieldsGolden(t *testing.T) {
	// The first bytes of an IPv4 header, hand-decoded: version 4, IHL 5, TOS 0,
	// then flags and fragment offset in one 16-bit group, 0x4005: reserved 0,
	// don't-fragment 1, more-fragments 0, offset 5. The IHL then sizes "rest".
	p := oneField(t, `{"name":"version","type":"bits","bits":4,"expect":"4"},
{"name":"ihl","type":"bits","bits":4},
{"name":"tos","type":"u8"},
{"name":"reserved","type":"bits","bits":1},
{"name":"dont_fragment","type":"bits","bits":1},
{"name":"more_fragments","type":"bits","bits":1},
{"name":"fragment_offset","type":"bits","bits":13},
{"name":"rest","type":"bytes","length":{"field":"ihl"}},
{"name":"note","type":"u8","when":{"field":"dont_fragment","equals":"1"}}`)
	input := unhex(t, "45 00 4005 0102030405 09")
	doc, trace, e := p.Decode(context.Background(), bytes.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]any{"version": "4", "ihl": "5", "tos": "0", "reserved": "0", "dont_fragment": "1", "more_fragments": "0", "fragment_offset": "5", "rest": "0102030405", "note": "9"}
	if !reflect.DeepEqual(doc.Values, want) {
		t.Fatalf("values %#v", doc.Values)
	}
	spans := map[string]string{}
	for _, entry := range trace.Entries {
		spans[entry.Path] = entry.Kind + " " + string(rune('0'+entry.Offset)) + " " + string(rune('0'+entry.Length))
	}
	if spans["/ihl"] != "bits 0 1" || spans["/version"] != "bits 0 1" || spans["/fragment_offset"] != "bits 2 2" || spans["/reserved"] != "bits 2 2" {
		t.Fatalf("spans %v", spans)
	}
	out, e := encodeValues(t, p, doc.Values)
	if e != nil || !bytes.Equal(out, input) {
		t.Fatalf("round trip %v %x", e, out)
	}
	// Clear don't-fragment (the note disappears) and set the largest offset.
	edited := map[string]any{"version": "4", "ihl": "2", "tos": "0", "reserved": "0", "dont_fragment": "0", "more_fragments": "1", "fragment_offset": "8191", "rest": "aabb"}
	if out, e := encodeValues(t, p, edited); e != nil || hex.EncodeToString(out) != "42003fffaabb" {
		t.Fatalf("edited %v %x", e, out)
	}
	for name, c := range map[string][2]string{"ihl": {"16", "value_out_of_range"}, "version": {"5", "constraint_mismatch"}, "fragment_offset": {"-1", "value_out_of_range"}, "reserved": {"x", "invalid_document"}} {
		bad := map[string]any{}
		for k, v := range edited {
			bad[k] = v
		}
		bad[name] = c[0]
		if _, e := encodeValues(t, p, bad); diagnosticOf(t, e).Code != c[1] {
			t.Fatalf("%s=%s: %v", name, c[0], e)
		}
	}
	if _, _, e := p.Decode(context.Background(), bytes.NewReader(unhex(t, "55 00 4005"))); at(diagnosticOf(t, e)) != "constraint_mismatch /version 0 0" {
		t.Fatalf("version 5: %v", e)
	}
}

func TestBitFieldsWithMeasuredValues(t *testing.T) {
	// A size or checksum naming one member covers its whole group.
	p := oneField(t, `{"name":"n","type":"u8","derive":{"size_of":"lo"}},
{"name":"c","type":"u16","derive":{"checksum":"crc16_xmodem","of":"lo"}},
{"name":"hi","type":"bits","bits":4},
{"name":"lo","type":"bits","bits":12}`)
	out, e := encodeValues(t, p, map[string]any{"hi": "1", "lo": "564"})
	sum := newChecksum("crc16_xmodem")
	_, _ = sum.Write([]byte{0x12, 0x34})
	want := []byte{2, byte(sum.Sum32() >> 8), byte(sum.Sum32()), 0x12, 0x34}
	if e != nil || !bytes.Equal(out, want) {
		t.Fatalf("got %x %v, want %x", out, e, want)
	}
	if _, _, e := p.Decode(context.Background(), bytes.NewReader(out)); e != nil {
		t.Fatal(e)
	}
}

func TestUntilOffset(t *testing.T) {
	p := oneField(t, `{"name":"head","type":"bytes","length":2},
{"name":"off","type":"u8"},
{"name":"gap","type":"bytes","length":{"until_offset":"off"}},
{"name":"body","type":"bytes","length":2}`)
	// off = 5 at offset 2, so gap is offsets 3 and 4 and body starts at 5.
	input := unhex(t, "aabb 05 ccdd eeff")
	doc, _, e := p.Decode(context.Background(), bytes.NewReader(input))
	if e != nil || doc.Values["gap"] != "ccdd" || doc.Values["body"] != "eeff" {
		t.Fatalf("%v %#v", e, doc)
	}
	if out, e := encodeValues(t, p, doc.Values); e != nil || !bytes.Equal(out, input) {
		t.Fatalf("round trip %v", e)
	}
	if doc, _, e := p.Decode(context.Background(), bytes.NewReader(unhex(t, "aabb 03 eeff"))); e != nil || doc.Values["gap"] != "" {
		t.Fatalf("empty gap %v", e)
	}
	if _, _, e := p.Decode(context.Background(), bytes.NewReader(unhex(t, "aabb 02 eeff"))); at(diagnosticOf(t, e)) != "constraint_mismatch /gap 3 3" {
		t.Fatalf("behind: %v", e)
	}
	moved := map[string]any{"head": "aabb", "off": "6", "gap": "ccdd", "body": "eeff"}
	if _, e := encodeValues(t, p, moved); at(diagnosticOf(t, e)) != "constraint_mismatch /gap 3 3" {
		t.Fatalf("encode mismatch: %v", e)
	}
	moved["gap"] = "ccdd00"
	if out, e := encodeValues(t, p, moved); e != nil || hex.EncodeToString(out) != "aabb06ccdd00eeff" {
		t.Fatalf("encode moved %v %x", e, out)
	}
	literal := oneField(t, `{"name":"a","type":"bytes","length":1},{"name":"pad","type":"bytes","length":{"until_offset":4}},{"name":"b","type":"u8"}`)
	if doc, _, e := literal.Decode(context.Background(), bytes.NewReader(unhex(t, "01 000000 07"))); e != nil || doc.Values["pad"] != "000000" || doc.Values["b"] != "7" {
		t.Fatalf("literal %v", e)
	}
}

func TestNewTypeSchemaRules(t *testing.T) {
	head := `{"schema_version":1,"name":"s","endian":"little","fields":[`
	for _, c := range []struct{ name, fields, path string }{
		{"group of 12 bits", `{"name":"a","type":"bits","bits":4},{"name":"b","type":"bits","bits":8}`, "/fields/1/bits"},
		{"group of 72 bits", `{"name":"a","type":"bits","bits":64},{"name":"b","type":"bits","bits":8}`, "/fields/1/bits"},
		{"group broken by a byte", `{"name":"a","type":"bits","bits":4},{"name":"x","type":"u8"},{"name":"b","type":"bits","bits":4}`, "/fields/0/bits"},
		{"zero bits", `{"name":"a","type":"bits","bits":0}`, "/fields/0/bits"},
		{"65 bits", `{"name":"a","type":"bits","bits":65}`, "/fields/0/bits"},
		{"bits as text", `{"name":"a","type":"bits","bits":"8"}`, "/fields/0/bits"},
		{"bits with when", `{"name":"f","type":"u8"},{"name":"a","type":"bits","bits":8,"when":{"field":"f","equals":"1"}}`, "/fields/1"},
		{"bits with derive", `{"name":"a","type":"bits","bits":8,"derive":{"offset_of":"a"}}`, "/fields/0"},
		{"bits expect too big", `{"name":"a","type":"bits","bits":4,"expect":"16"},{"name":"b","type":"bits","bits":4}`, "/fields/0/expect"},
		{"float expect", `{"name":"x","type":"f32","expect":"1"}`, "/fields/0"},
		{"float as a length", `{"name":"x","type":"f32"},{"name":"b","type":"bytes","length":{"field":"x"}}`, "/fields/1/length/field"},
		{"until_offset on a string", `{"name":"o","type":"u8"},{"name":"s","type":"string","encoding":"ascii","length":{"until_offset":"o"}}`, "/fields/1/length"},
		{"until_offset with expect", `{"name":"b","type":"bytes","length":{"until_offset":2},"expect":"0000"}`, "/fields/0/expect"},
		{"until_offset to a float", `{"name":"x","type":"f64"},{"name":"b","type":"bytes","length":{"until_offset":"x"}}`, "/fields/1/length/until_offset"},
		{"until_offset to a measured offset", `{"name":"o","type":"u8","derive":{"offset_of":"z"}},{"name":"b","type":"bytes","length":{"until_offset":"o"}},{"name":"z","type":"u8"}`, "/fields/0/derive/offset_of"},
		{"until_offset and size_of", `{"name":"n","type":"u8","derive":{"size_of":"b"}},{"name":"b","type":"bytes","length":{"until_offset":"n"}}`, "/fields/0/derive/size_of"},
		{"until_offset with field too", `{"name":"o","type":"u8"},{"name":"b","type":"bytes","length":{"until_offset":"o","field":"o"}}`, "/fields/1/length"},
	} {
		_, e := Compile([]byte(head+c.fields+`]}`), DefaultLimits())
		if e == nil {
			t.Fatalf("%s: accepted", c.name)
		}
		if d := diagnosticOf(t, e); d.Code != "invalid_schema" || d.Path != c.path {
			t.Fatalf("%s: %s at %q, want %q", c.name, d.Code, d.Path, c.path)
		}
	}
}

// FuzzFloatsAndBits decodes arbitrary bytes as floats of both widths and
// endians and as a bit group, requires every pattern to round-trip, and
// checks values against math and encoding/binary rather than the codec.
func FuzzFloatsAndBits(f *testing.F) {
	f.Add([]byte{0x7f, 0xc0, 0, 1, 0, 0, 0, 0})
	f.Add([]byte{0x80, 0, 0, 0, 0, 0, 0, 0})
	plans := map[string]*Plan{}
	for _, name := range []string{"f32", "f64"} {
		for _, endian := range []string{"big", "little"} {
			plans[name+endian], _ = Compile([]byte(`{"schema_version":1,"name":"t","endian":"`+endian+`","fields":[{"name":"x","type":"`+name+`"}]}`), DefaultLimits())
		}
	}
	group, _ := Compile([]byte(`{"schema_version":1,"name":"g","endian":"big","fields":[{"name":"a","type":"bits","bits":3},{"name":"b","type":"bits","bits":17},{"name":"c","type":"bits","bits":44}]}`), DefaultLimits())
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) < 8 {
			return
		}
		raw = raw[:8]
		for key, p := range plans {
			size := 4
			if strings.HasPrefix(key, "f64") {
				size = 8
			}
			input := raw[:size]
			doc, _, e := p.Decode(context.Background(), bytes.NewReader(input))
			if e != nil {
				t.Fatalf("%s: %v", key, e)
			}
			var out bytes.Buffer
			if e := p.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), input) {
				t.Fatalf("%s %x did not round-trip: %v", key, input, e)
			}
			var bits uint64
			order := binary.ByteOrder(binary.BigEndian)
			if strings.HasSuffix(key, "little") {
				order = binary.LittleEndian
			}
			text := doc.Values["x"].(string)
			if size == 4 {
				bits = uint64(order.Uint32(input))
				if v := float64(math.Float32frombits(uint32(bits))); !math.IsNaN(v) && !math.IsInf(v, 0) {
					if got, _ := parseFloat(text, 4); got != bits {
						t.Fatalf("%s text %s", key, text)
					}
				}
			} else {
				bits = order.Uint64(input)
			}
			if math.IsNaN(math.Float64frombits(bits)) && size == 8 && !strings.HasPrefix(text, "nan:") {
				t.Fatalf("%s NaN shown as %s", key, text)
			}
		}
		doc, _, e := group.Decode(context.Background(), bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		v := binary.BigEndian.Uint64(raw)
		want := map[string]any{"a": u(v >> 61), "b": u(v >> 44 & (1<<17 - 1)), "c": u(v & (1<<44 - 1))}
		if !reflect.DeepEqual(doc.Values, want) {
			t.Fatalf("group %x: %v", raw, doc.Values)
		}
		var out bytes.Buffer
		if e := group.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), raw) {
			t.Fatalf("group round trip %v", e)
		}
	})
}

func u(v uint64) string { return strconv.FormatUint(v, 10) }
