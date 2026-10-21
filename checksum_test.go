package ebyte

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"reflect"
	"testing"
)

// Check values for the ASCII bytes "123456789" from the published CRC
// catalogue and the zlib specification, not from this code.
func TestChecksumCheckValues(t *testing.T) {
	for algorithm, want := range map[string]uint32{
		"crc32": 0xcbf43926, "adler32": 0x091e01de,
		"crc16_arc": 0xbb3d, "crc16_modbus": 0x4b37, "crc16_xmodem": 0x31c3,
	} {
		h := newChecksum(algorithm)
		_, _ = h.Write([]byte("1234"))
		_, _ = h.Write([]byte("56789"))
		if got := h.Sum32(); got != want {
			t.Fatalf("%s: %08x, want %08x", algorithm, got, want)
		}
	}
}

// pngChunks is the PNG chunk layout: big-endian length, type, data and a
// CRC-32 over type and data.
const pngChunks = `{"schema_version":1,"name":"chunks","endian":"big","fields":[
{"name":"chunks","type":"repeat","until":"end_of_input","fields":[
 {"name":"length","type":"u32","derive":{"size_of":"data"}},
 {"name":"type","type":"string","encoding":"ascii","length":4},
 {"name":"data","type":"bytes","length":{"field":"length"}},
 {"name":"crc","type":"u32","derive":{"checksum":"crc32","of":{"from":"type","to":"data"}}}]}]}`

func TestChecksumPNGChunks(t *testing.T) {
	p := repeatPlan(t, pngChunks, DefaultLimits())
	// Every PNG ends with this 12-byte IEND chunk; its CRC ae426082 is fixed.
	iend := unhex(t, "00000000 49454e44 ae426082")
	doc, trace, e := p.Decode(context.Background(), bytes.NewReader(iend))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(doc.Values, map[string]any{"chunks": []any{map[string]any{"type": "IEND", "data": ""}}}) {
		t.Fatalf("values %#v", doc.Values)
	}
	last := trace.Entries[len(trace.Entries)-1]
	if last.Path != "/chunks/0/crc" || *last.Value != "2923585666" || last.Offset != 8 {
		t.Fatalf("crc entry %+v", last)
	}

	// A tEXt chunk in front of IEND, written by the encoder from values alone.
	text := []byte("Comment\x00made by EByte")
	doc.Values["chunks"] = append([]any{map[string]any{"type": "tEXt", "data": hex.EncodeToString(text)}}, doc.Values["chunks"].([]any)...)
	var out bytes.Buffer
	if e := p.Encode(context.Background(), doc, &out); e != nil {
		t.Fatal(e)
	}
	b := out.Bytes()
	if int(binary.BigEndian.Uint32(b)) != len(text) || binary.BigEndian.Uint32(b[8+len(text):]) != crc32.ChecksumIEEE(append([]byte("tEXt"), text...)) || !bytes.Equal(b[12+len(text):], iend) {
		t.Fatalf("encoded %x", b)
	}

	// One flipped data bit fails at that chunk's CRC, which starts after the data.
	b[10] ^= 1
	_, failed, e := p.Decode(context.Background(), bytes.NewReader(b))
	want := "checksum_mismatch /chunks/0/crc 29 29" // 4 length + 4 type + 21 data
	if got := at(diagnosticOf(t, e)); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, entry := range failed.Entries {
		if entry.Path == "/chunks/0/crc" && entry.State != "failed" {
			t.Fatalf("crc entry not marked failed")
		}
	}
}

func TestChecksumBeforeItsRangeAndAfterSizes(t *testing.T) {
	// The CRC-16 is stored before the bytes it covers.
	p := repeatPlan(t, `{"schema_version":1,"name":"m","endian":"little","fields":[
{"name":"sum","type":"u16","derive":{"checksum":"crc16_modbus","of":"payload"}},
{"name":"payload","type":"bytes","length":9}]}`, DefaultLimits())
	input := append([]byte{0x37, 0x4b}, "123456789"...)
	doc, _, e := p.Decode(context.Background(), bytes.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := p.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), input) {
		t.Fatalf("round trip %v %x", e, out.Bytes())
	}
	input[0] = 0x38
	if _, _, e := p.Decode(context.Background(), bytes.NewReader(input)); at(diagnosticOf(t, e)) != "checksum_mismatch /sum 0 0" {
		t.Fatalf("stale crc16: %v", e)
	}

	// The range holds a measured total and a driving length: both must be
	// final before the checksum is taken. Hand layout for d = "abc":
	// total 0600 (2+1+3), n 03, d 616263, crc over those six bytes.
	q := repeatPlan(t, `{"schema_version":1,"name":"o","endian":"little","fields":[
{"name":"total","type":"u16","derive":{"size_of":{"from":"total","to":"d"}}},
{"name":"n","type":"u8","derive":{"size_of":"d"}},
{"name":"d","type":"bytes","length":{"field":"n"}},
{"name":"crc","type":"u32","derive":{"checksum":"crc32","of":{"from":"total","to":"d"}}}]}`, DefaultLimits())
	out.Reset()
	if e := q.Encode(context.Background(), &Document{DocumentVersion: 1, SchemaSHA256: q.SchemaSHA256(), Values: map[string]any{"d": "616263"}}, &out); e != nil {
		t.Fatal(e)
	}
	head := unhex(t, "0600 03 616263")
	sum := make([]byte, 4)
	binary.LittleEndian.PutUint32(sum, crc32.ChecksumIEEE(head))
	if !bytes.Equal(out.Bytes(), append(head, sum...)) {
		t.Fatalf("got %x", out.Bytes())
	}
	if _, _, e := q.Decode(context.Background(), bytes.NewReader(out.Bytes())); e != nil {
		t.Fatal(e)
	}
}

func TestChecksumSchemaRules(t *testing.T) {
	head := `{"schema_version":1,"name":"s","endian":"little","fields":[`
	for _, c := range []struct{ name, fields, path string }{
		{"unknown algorithm", `{"name":"c","type":"u32","derive":{"checksum":"md5","of":"x"}},{"name":"x","type":"u8"}`, "/fields/0/derive/checksum"},
		{"crc32 in a u16", `{"name":"c","type":"u16","derive":{"checksum":"crc32","of":"x"}},{"name":"x","type":"u8"}`, "/fields/0/derive/checksum"},
		{"crc16 in a u32", `{"name":"c","type":"u32","derive":{"checksum":"crc16_arc","of":"x"}},{"name":"x","type":"u8"}`, "/fields/0/derive/checksum"},
		{"checksum without of", `{"name":"c","type":"u32","derive":{"checksum":"crc32"}},{"name":"x","type":"u8"}`, "/fields/0/derive/of"},
		{"of without checksum", `{"name":"c","type":"u32","derive":{"size_of":"x","of":"x"}},{"name":"x","type":"u8"}`, "/fields/0/derive/of"},
		{"checksum with add", `{"name":"c","type":"u32","derive":{"checksum":"crc32","of":"x","add":"1"}},{"name":"x","type":"u8"}`, "/fields/0/derive/add"},
		{"unknown target", `{"name":"c","type":"u32","derive":{"checksum":"crc32","of":"zz"}}`, "/fields/0/derive/of"},
		{"range covers itself", `{"name":"x","type":"u8"},{"name":"c","type":"u32","derive":{"checksum":"crc32","of":{"from":"x","to":"c"}}}`, "/fields/1/derive/of"},
		{"range covers another checksum", `{"name":"x","type":"u8"},{"name":"a","type":"u16","derive":{"checksum":"crc16_arc","of":"x"}},{"name":"c","type":"u32","derive":{"checksum":"crc32","of":{"from":"x","to":"a"}}}`, "/fields/2/derive/of"},
		{"checksum used as a length", `{"name":"c","type":"u16","derive":{"checksum":"crc16_arc","of":"x"}},{"name":"x","type":"u8"},{"name":"b","type":"bytes","length":{"field":"c"}}`, "/fields/0/derive/checksum"},
		{"checksum with size_of", `{"name":"c","type":"u32","derive":{"checksum":"crc32","size_of":"x","of":"x"}},{"name":"x","type":"u8"}`, "/fields/0/derive"},
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

// FuzzChecksums encodes chunk lists built from fuzz data and checks each
// chunk's length and CRC with encoding/binary and hash/crc32, then decodes
// the output back to the same values; it also feeds raw fuzz bytes to the
// decoder and requires accepted input to re-encode unchanged.
func FuzzChecksums(f *testing.F) {
	f.Add([]byte("IHDR\x00\x01tEXt"), []byte{})
	f.Add([]byte{}, []byte{0, 0, 0, 0, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82})
	p, e := Compile([]byte(pngChunks), DefaultLimits())
	if e != nil {
		f.Fatal(e)
	}
	f.Fuzz(func(t *testing.T, data, raw []byte) {
		if len(data) > 2000 || len(raw) > 4096 {
			return
		}
		if doc, _, e := p.Decode(context.Background(), bytes.NewReader(raw)); e == nil {
			var out bytes.Buffer
			if e := p.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), raw) {
				t.Fatalf("accepted input did not round-trip: %v", e)
			}
		}
		chunks := []any{}
		var bodies [][]byte
		for len(data) > 0 {
			n := int(data[0] % 16)
			if n > len(data)-1 {
				n = len(data) - 1
			}
			body := data[1 : 1+n]
			bodies = append(bodies, body)
			chunks = append(chunks, map[string]any{"type": "abcd", "data": hex.EncodeToString(body)})
			data = data[1+n:]
		}
		values := map[string]any{"chunks": chunks}
		var out bytes.Buffer
		if e := p.Encode(context.Background(), &Document{DocumentVersion: 1, SchemaSHA256: p.SchemaSHA256(), Values: values}, &out); e != nil {
			t.Fatal(e)
		}
		b := out.Bytes()
		for _, body := range bodies {
			if int(binary.BigEndian.Uint32(b)) != len(body) || binary.BigEndian.Uint32(b[8+len(body):]) != crc32.ChecksumIEEE(append([]byte("abcd"), body...)) {
				t.Fatalf("chunk disagrees with hash/crc32: %x", b)
			}
			b = b[12+len(body):]
		}
		doc, _, e := p.Decode(context.Background(), bytes.NewReader(out.Bytes()))
		if e != nil || !reflect.DeepEqual(doc.Values, values) {
			t.Fatalf("decode back: %v", e)
		}
	})
}
