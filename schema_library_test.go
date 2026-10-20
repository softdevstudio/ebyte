package ebyte

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// Every schema in schemas/ must compile, and its real sample file must
// decode and encode back to identical bytes.
func TestSchemaLibrarySamples(t *testing.T) {
	cases := map[string]string{
		"pcm-wav":             "tone.wav",
		"bmp-24bit":           "stripes.bmp",
		"zip-one-stored-file": "one.zip",
		"riff-wave":           "tagged.wav",
		"zip":                 "three.zip",
	}
	entries, e := os.ReadDir("schemas")
	if e != nil {
		t.Fatal(e)
	}
	seen := 0
	for _, entry := range entries {
		name, ok := bytes.CutSuffix([]byte(entry.Name()), []byte(".schema.json"))
		if !ok {
			continue
		}
		seen++
		sample, listed := cases[string(name)]
		if !listed {
			t.Fatalf("schemas/%s has no sample in this test", entry.Name())
		}
		raw, e := os.ReadFile("schemas/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		plan, e := Compile(raw, DefaultLimits())
		if e != nil {
			t.Fatalf("%s: %v", entry.Name(), e)
		}
		input, e := os.ReadFile("schemas/samples/" + sample)
		if e != nil {
			t.Fatal(e)
		}
		doc, trace, e := plan.Decode(context.Background(), bytes.NewReader(input))
		if e != nil || trace.State != "complete" {
			t.Fatalf("%s: decode %v", sample, e)
		}
		var out bytes.Buffer
		if e := plan.Encode(context.Background(), doc, &out); e != nil {
			t.Fatalf("%s: encode %v", sample, e)
		}
		if !bytes.Equal(out.Bytes(), input) {
			t.Fatalf("%s: round trip changed bytes", sample)
		}
	}
	if seen != len(cases) {
		t.Fatalf("found %d schemas, want %d", seen, len(cases))
	}
}
