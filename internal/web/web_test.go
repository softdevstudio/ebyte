package web

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T) (string, []byte) {
	t.Helper()
	schema, e := os.ReadFile("../../testdata/packet.schema.json")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../testdata/packet.hex")
	if e != nil {
		t.Fatal(e)
	}
	b, e := hex.DecodeString(strings.TrimSpace(string(raw)))
	if e != nil {
		t.Fatal(e)
	}
	return string(schema), b
}

type envelope struct {
	OK         bool            `json:"ok"`
	Schema     *SchemaInfo     `json:"schema"`
	Result     json.RawMessage `json:"result"`
	Input      string          `json:"input"`
	Diagnostic *struct {
		Code string `json:"code"`
		Path string `json:"path"`
	} `json:"diagnostic"`
}

func parse(t *testing.T, raw string) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return e
}

func TestWebEnvelopes(t *testing.T) {
	ctx := context.Background()
	schema, packet := fixture(t)
	if e := parse(t, Validate(schema)); !e.OK || e.Schema.Name != "ebyte_packet" || len(e.Schema.SHA256) != 64 || e.Diagnostic != nil {
		t.Fatalf("validate %+v", e)
	}
	if e := parse(t, Validate("{")); e.OK || e.Diagnostic.Code != "invalid_schema" || e.Schema != nil || string(e.Result) != "null" {
		t.Fatalf("bad schema %+v", e)
	}
	if e := parse(t, Inspect(ctx, schema, packet)); !e.OK || !strings.Contains(string(e.Result), `"state":"complete"`) {
		t.Fatalf("inspect %+v", e)
	}
	failed := parse(t, Inspect(ctx, schema, packet[:16]))
	if failed.OK || failed.Diagnostic.Code != "truncated_input" || !strings.Contains(string(failed.Result), `"state":"failed"`) {
		t.Fatalf("failed inspect %+v", failed)
	}
	decoded := parse(t, Decode(ctx, schema, packet))
	if !decoded.OK {
		t.Fatalf("decode %+v", decoded)
	}
	out, raw := Encode(ctx, schema, string(decoded.Result))
	if e := parse(t, raw); !e.OK || string(out) != string(packet) || string(e.Result) != `{"bytes":21}` {
		t.Fatalf("encode %+v %x", e, out)
	}
	if out, raw := Encode(ctx, schema, "{}"); out != nil || parse(t, raw).Diagnostic.Code != "invalid_document" {
		t.Fatalf("bad document %s", raw)
	}
	if e := parse(t, Decode(ctx, schema, packet[:16])); e.OK || e.Diagnostic.Path != "/records/1/id" || e.Result != nil && string(e.Result) != "null" {
		t.Fatalf("bad decode %+v", e)
	}
	changed := append([]byte(nil), packet...)
	changed[17] = 0x2d
	if e := parse(t, Diff(ctx, schema, packet, changed)); !e.OK || !strings.Contains(string(e.Result), `"/records/1/temperature"`) {
		t.Fatalf("diff %+v", e)
	}
	if e := parse(t, Diff(ctx, schema, packet, packet[:16])); e.OK || e.Input != "after" || e.Diagnostic.Code != "truncated_input" {
		t.Fatalf("diff failure %+v", e)
	}
	if e := parse(t, Usage("a string argument")); e.OK || e.Diagnostic.Code != "invalid_argument" || e.Schema != nil {
		t.Fatalf("usage %+v", e)
	}
	if e := parse(t, TooLarge()); e.OK || e.Diagnostic.Code != "limit_exceeded" {
		t.Fatalf("too large %+v", e)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if e := parse(t, Decode(cancelled, schema, packet)); e.OK || e.Diagnostic.Code != "cancelled" {
		t.Fatalf("cancelled %+v", e)
	}
}
