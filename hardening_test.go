package ebyte

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"unicode/utf8"
)

func TestAllASCIIEscapeAccounting(t *testing.T) {
	input := make([]byte, 128)
	for i := range input {
		input[i] = byte(i)
	}
	fields := `[{"name":"text","type":"string","length":128,"encoding":"ascii"}]`
	p := planFor(t, fields, DefaultLimits())
	doc, trace, e := p.Decode(context.Background(), bytes.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(doc)
	traceRaw, _ := json.Marshal(trace)
	l := DefaultLimits()
	l.MaxDocumentBytes = len(raw)
	l.MaxTraceBytes = len(traceRaw)
	p = planFor(t, fields, l)
	doc, _, e = p.Decode(context.Background(), bytes.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e = p.Encode(context.Background(), doc, &out); e != nil || !bytes.Equal(out.Bytes(), input) {
		t.Fatal(e)
	}
	if _, e = p.LoadDocument(context.Background(), raw); e != nil {
		t.Fatal(e)
	}
	l.MaxDocumentBytes--
	p = planFor(t, fields, l)
	if _, _, e = p.Decode(context.Background(), bytes.NewReader(input)); e == nil {
		t.Fatal("escape budget bypass")
	}
}
func TestRepeatedDerivedCountBudgetIndependence(t *testing.T) {
	fields := `[{"name":"outer","type":"array","count":2,"fields":[{"name":"n","type":"u8","derive":{"count_of":"inner"}},{"name":"inner","type":"array","count":{"field":"n"},"fields":[]}]}]`
	for _, tc := range []struct {
		nodes, entries int
		ok             bool
	}{{6, 8, true}, {5, 8, false}, {6, 7, false}} {
		l := DefaultLimits()
		l.MaxValueNodes = tc.nodes
		l.MaxTraceEntries = tc.entries
		p := planFor(t, fields, l)
		doc, trace, e := p.Decode(context.Background(), bytes.NewReader([]byte{0, 0}))
		if tc.ok {
			if e != nil || doc == nil || len(trace.Entries) != 8 {
				t.Fatal(e)
			}
		} else if e == nil || e.(*Diagnostic).Code != "limit_exceeded" || doc != nil {
			t.Fatal("pooled independent budgets")
		}
	}
}
func TestHugeDimensionsDoNotReadPayload(t *testing.T) {
	for _, kind := range []string{"bytes", "string", "array"} {
		fields := `[{"name":"n","type":"u64"},{"name":"body","type":"` + kind + `"`
		if kind == "array" {
			fields += `,"count":{"field":"n"},"fields":[]}]`
		} else {
			fields += `,"length":{"field":"n"}`
			if kind == "string" {
				fields += `,"encoding":"utf8"`
			}
			fields += `}]`
		}
		p := planFor(t, fields, DefaultLimits())
		for _, n := range []uint64{1 << 40, ^uint64(0)} {
			var raw [8]byte
			binary.LittleEndian.PutUint64(raw[:], n)
			r := &countReader{Reader: bytes.NewReader(raw[:])}
			doc, trace, e := p.Decode(context.Background(), r)
			if doc != nil || e == nil || e.(*Diagnostic).Code != "limit_exceeded" || r.bytes != 8 || r.calls != 1 {
				t.Fatal("dimension read payload", e)
			}
			if trace.Entries[len(trace.Entries)-1].Length != 0 {
				t.Fatal("unread field span")
			}
		}
	}
}
func TestUTF8BoundaryFailures(t *testing.T) {
	for _, input := range [][]byte{{0xc0, 0x80}, {0xed, 0xa0, 0x80}, {0xf4, 0x90, 0x80, 0x80}, {0x80}, {0xe2, 0x82}} {
		fields := fmt.Sprintf(`[{"name":"text","type":"string","length":%d,"encoding":"utf8"}]`, len(input))
		p := planFor(t, fields, DefaultLimits())
		doc, _, e := p.Decode(context.Background(), bytes.NewReader(input))
		if doc != nil || e == nil || e.(*Diagnostic).Code != "invalid_encoding" || *e.(*Diagnostic).Offset != 0 {
			t.Fatal("invalid UTF8 accepted", e)
		}
	}
}

// Shapes are selected independently of payload contents. Every valid generated
// representation must survive JSON loading and re-encoding byte for byte.
func FuzzLosslessShapes(f *testing.F) {
	for mode := byte(0); mode < 6; mode++ {
		f.Add(mode, []byte{0, 1, 127, 128, 255, '<', 0, '"'})
	}
	f.Fuzz(func(t *testing.T, selector byte, payload []byte) {
		if len(payload) > 4096 {
			payload = payload[:4096]
		}
		if len(payload) == 0 {
			payload = []byte{0}
		}
		at := func(i int) byte { return payload[i%len(payload)] }
		endian := "little"
		if selector&8 != 0 {
			endian = "big"
		}
		var fields string
		var raw []byte
		invalidEncoding := false
		switch selector % 6 {
		case 0:
			fields = `[{"name":"u8","type":"u8"},{"name":"i8","type":"i8"},{"name":"u16","type":"u16"},{"name":"i16","type":"i16"},{"name":"u32","type":"u32"},{"name":"i32","type":"i32"},{"name":"u64","type":"u64"},{"name":"i64","type":"i64"}]`
			raw = make([]byte, 30)
			for i := range raw {
				raw[i] = at(i)
			}
		case 1:
			count := int(at(0) % 9)
			fields = `[{"name":"n","type":"u8"},{"name":"items","type":"array","count":{"field":"n"},"fields":[{"name":"v","type":"i16"}]}]`
			raw = make([]byte, 1+2*count)
			raw[0] = byte(count)
			for i := 1; i < len(raw); i++ {
				raw[i] = at(i)
			}
		case 2:
			count := int(at(0) % 9)
			fields = `[{"name":"n","type":"u16","derive":{"count_of":"items"}},{"name":"items","type":"array","count":{"field":"n"},"fields":[{"name":"length","type":"u8"},{"name":"body","type":"bytes","length":{"field":"length"}}]},{"name":"tail","type":"bytes","length":2,"when":{"field":"n","equals":"0"}}]`
			raw = make([]byte, 2)
			if endian == "big" {
				binary.BigEndian.PutUint16(raw, uint16(count))
			} else {
				binary.LittleEndian.PutUint16(raw, uint16(count))
			}
			for i := 0; i < count; i++ {
				width := int(at(i+1) % 5)
				raw = append(raw, byte(width))
				for j := 0; j < width; j++ {
					raw = append(raw, at(i+j+2))
				}
			}
			if count == 0 {
				raw = append(raw, at(1), at(2))
			}
		case 3:
			raw = append([]byte{}, payload...)
			encoding := "utf8"
			if selector&16 != 0 {
				encoding = "ascii"
				for i := range raw {
					raw[i] &= 127
				}
			}
			fields = fmt.Sprintf(`[{"name":"text","type":"string","length":%d,"encoding":"%s"}]`, len(raw), encoding)
			invalidEncoding = encoding == "utf8" && !utf8.Valid(raw)
		case 4:
			fields = `[{"name":"outer","type":"array","count":3,"fields":[{"name":"inner","type":"array","count":2,"fields":[{"name":"raw","type":"bytes","length":2},{"name":"empty","type":"string","length":0,"encoding":"ascii"}]}]}]`
			raw = make([]byte, 12)
			for i := range raw {
				raw[i] = at(i)
			}
		case 5:
			fields = `[{"name":"magic","type":"bytes","length":2,"expect":"4ebb"},{"name":"version","type":"u8","expect":"7"},{"name":"body","type":"bytes","length":` + strconv.Itoa(len(payload)) + `}]`
			raw = append([]byte{0x4e, 0xbb, 7}, payload...)
		}
		schemaBytes := []byte(`{"schema_version":1,"name":"shape","endian":"` + endian + `","fields":` + fields + `}`)
		p, e := Compile(schemaBytes, DefaultLimits())
		if e != nil {
			t.Fatal(e)
		}
		doc, trace, e := p.Decode(context.Background(), bytes.NewReader(raw))
		if invalidEncoding {
			if e == nil || doc != nil || e.(*Diagnostic).Code != "invalid_encoding" {
				t.Fatal("invalid encoding result")
			}
			return
		}
		if e != nil {
			t.Fatal(e)
		}
		if trace.State != "complete" {
			t.Fatal("failed successful trace")
		}
		if selector%6 == 0 {
			var order binary.ByteOrder = binary.LittleEndian
			if endian == "big" {
				order = binary.BigEndian
			}
			expected := map[string]any{
				"u8": strconv.FormatUint(uint64(raw[0]), 10), "i8": strconv.FormatInt(int64(int8(raw[1])), 10),
				"u16": strconv.FormatUint(uint64(order.Uint16(raw[2:4])), 10), "i16": strconv.FormatInt(int64(int16(order.Uint16(raw[4:6]))), 10),
				"u32": strconv.FormatUint(uint64(order.Uint32(raw[6:10])), 10), "i32": strconv.FormatInt(int64(int32(order.Uint32(raw[10:14]))), 10),
				"u64": strconv.FormatUint(order.Uint64(raw[14:22]), 10), "i64": strconv.FormatInt(int64(order.Uint64(raw[22:30])), 10),
			}
			if !reflect.DeepEqual(doc.Values, expected) {
				t.Fatal("independent integer oracle mismatch")
			}
		}

		serialized, e := json.Marshal(doc)
		if e != nil {
			t.Fatal(e)
		}
		loaded, e := p.LoadDocument(context.Background(), serialized)
		if e != nil {
			t.Fatal(e)
		}
		var out bytes.Buffer
		if e = p.Encode(context.Background(), loaded, &out); e != nil || !bytes.Equal(out.Bytes(), raw) {
			t.Fatalf("shape %d endian %s round trip failed: %v", selector%6, endian, e)
		}
	})
}
