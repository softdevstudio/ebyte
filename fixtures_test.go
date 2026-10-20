package ebyte

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, e := os.ReadFile(filepath.Join("testdata", name+".hex"))
	if e != nil {
		t.Fatal(e)
	}
	b, e := hex.DecodeString(strings.TrimSpace(string(data)))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestFixtureIdentityAndDocumentShape(t *testing.T) {
	for _, name := range []string{"packet", "wide", "stored", "empty"} {
		t.Run(name, func(t *testing.T) {
			data, e := os.ReadFile(filepath.Join("testdata", name+".schema.json"))
			if e != nil {
				t.Fatal(e)
			}
			var schema map[string]json.RawMessage
			if e = json.Unmarshal(data, &schema); e != nil {
				t.Fatal(e)
			}
			if len(schema) != 4 {
				t.Fatal("unexpected fixture schema shape")
			}
			sum := sha256.Sum256(data)
			raw, e := os.ReadFile(filepath.Join("testdata", name+".document.json"))
			if e != nil {
				t.Fatal(e)
			}
			var doc struct {
				Version  int            `json:"document_version"`
				Identity string         `json:"schema_sha256"`
				Values   map[string]any `json:"values"`
			}
			if e = json.Unmarshal(raw, &doc); e != nil || doc.Version != 1 || doc.Identity != hex.EncodeToString(sum[:]) || doc.Values == nil {
				t.Fatal("bad fixture identity/document", e)
			}
			if name == "packet" {
				if _, ok := doc.Values["record_count"]; ok {
					t.Fatal("derived count must not be editable")
				}
				if doc.Values["label"] != "A\x00B " || doc.Values["reserved"] != "aa00" {
					t.Fatal("lossy fixture")
				}
			}
			fixtureBytes(t, name)
		})
	}
}

// A manual layout oracle independent of the future schema interpreter.
func TestPacketFixtureManualOracle(t *testing.T) {
	b := fixtureBytes(t, "packet")
	if len(b) != 21 || string(b[:2]) != "EB" || b[2] != 1 || b[3] != 2 || b[4] != 1 || string(b[5:9]) != "A\x00B " || hex.EncodeToString(b[9:11]) != "aa00" || hex.EncodeToString(b[19:]) != "dead" {
		t.Fatal("invalid hand-specified packet")
	}
	if binary.LittleEndian.Uint16(b[11:13]) != 1 || int16(binary.LittleEndian.Uint16(b[13:15])) != -2 || binary.LittleEndian.Uint16(b[15:17]) != 2 || int16(binary.LittleEndian.Uint16(b[17:19])) != 300 {
		t.Fatal("record oracle differs")
	}
	raw, e := os.ReadFile("testdata/packet.trace.json")
	if e != nil {
		t.Fatal(e)
	}
	var trace struct {
		Version int    `json:"trace_version"`
		State   string `json:"state"`
		Entries []struct {
			Path, Kind, State string
			Offset, Length    int
			Value             *string
		}
	}
	if e = json.Unmarshal(raw, &trace); e != nil || trace.Version != 1 || trace.State != "complete" || len(trace.Entries) != 15 {
		t.Fatal("bad golden trace", e)
	}
	expected := [][2]int{{0, 21}, {0, 2}, {2, 1}, {3, 1}, {4, 1}, {5, 4}, {9, 2}, {11, 8}, {11, 4}, {11, 2}, {13, 2}, {15, 4}, {15, 2}, {17, 2}, {19, 2}}
	for i, entry := range trace.Entries {
		if entry.Offset != expected[i][0] || entry.Length != expected[i][1] || entry.State != "complete" {
			t.Fatal("bad independent span", i, entry)
		}
		if (entry.Kind == "bytes" || entry.Kind == "string") && entry.Value != nil {
			t.Fatal("fixture leaks opaque value")
		}
	}
}
func TestWideAndEmptyManualOracle(t *testing.T) {
	b := fixtureBytes(t, "wide")
	if len(b) != 20 || binary.BigEndian.Uint64(b[:8]) != math.MaxUint64 || binary.BigEndian.Uint64(b[8:16]) != uint64(1)<<63 || string(b[16:]) != "é\x00 " {
		t.Fatal("bad scalar oracle")
	}
	if len(fixtureBytes(t, "empty")) != 0 {
		t.Fatal("empty fixture has data")
	}
	b = fixtureBytes(t, "stored")
	if len(b) != 3 || b[0] != 1 || binary.LittleEndian.Uint16(b[1:]) != 4660 {
		t.Fatal("stored count oracle")
	}
}
func TestAcceptanceVectorManualBoundaries(t *testing.T) {
	data, e := os.ReadFile("testdata/cases.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Name, Operation, Hex string
		Error                *struct {
			Code, Path string
			Offset     *int64 `json:"offset"`
			FieldStart *int64 `json:"field_start"`
		}
	}
	// Use raw members for the nullable snake-case locations.
	var rawCases []map[string]json.RawMessage
	if e = json.Unmarshal(data, &cases); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(data, &rawCases); e != nil {
		t.Fatal(e)
	}
	if len(cases) != 12 {
		t.Fatal("missing contract examples")
	}
	for i, c := range cases {
		if c.Hex != "" {
			if _, e = hex.DecodeString(c.Hex); e != nil {
				t.Fatal(c.Name, e)
			}
		}
		if c.Error != nil {
			var fields map[string]json.RawMessage
			json.Unmarshal(rawCases[i]["error"], &fields)
			if len(fields) != 4 || fields["offset"] == nil || fields["field_start"] == nil {
				t.Fatal("missing explicit unknown location", c.Name)
			}
		}
		switch c.Name {
		case "truncated note":
			if len(c.Hex)/2 != 20 {
				t.Fatal("truncation oracle")
			}
		case "derived count edit":
			b, _ := hex.DecodeString(c.Hex)
			if len(b) != 25 || b[3] != 3 || binary.LittleEndian.Uint16(b[19:21]) != 3 || int16(binary.LittleEndian.Uint16(b[21:23])) != -1 || hex.EncodeToString(b[23:]) != "dead" {
				t.Fatal("edit oracle")
			}
		case "condition absent":
			b, _ := hex.DecodeString(c.Hex)
			if len(b) != 19 || b[4] != 0 {
				t.Fatal("condition oracle")
			}
		}
	}
}
