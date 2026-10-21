package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/softdevstudio/ebyte"
)

func TestDiffCommand(t *testing.T) {
	schema := fixturePath(t, "packet.schema.json")
	before := packetInput(t)
	after := append([]byte(nil), before...)
	after[17] = 0x2d // records/1/temperature 300 -> 301 (2c01 -> 2d01), from the fixture's hand layout
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.bin")
	if os.WriteFile(a, before, 0o600) != nil || os.WriteFile(b, after, 0o600) != nil {
		t.Fatal("write fixtures")
	}

	code, out, errOut := invoke(t, []string{"diff", "--schema", schema, "--before", a, "--after", b, "--format", "text"}, nil)
	want := "changed \"/records/1/temperature\" i16 a=17+2 b=17+2 value=300->301\n"
	if code != 5 || len(errOut) != 0 || !strings.HasPrefix(string(out), "EByte diff state=different ") || !strings.Contains(string(out), " size_before=21 size_after=21 changed=1 added=0 removed=0\n") || !strings.HasSuffix(string(out), want) {
		t.Fatalf("text diff %d %q %q", code, out, errOut)
	}

	// JSON, with the after side on stdin.
	code, out, _ = invoke(t, []string{"diff", "--schema", schema, "--before", a, "--after", "-"}, after)
	var r ebyte.DiffResult
	if code != 5 || json.Unmarshal(out, &r) != nil || r.State != "different" || len(r.Changes) != 1 || r.Changes[0].Path != "/records/1/temperature" || *r.Changes[0].After.Value != "301" {
		t.Fatalf("json diff %d %s", code, out)
	}

	code, out, _ = invoke(t, []string{"diff", "--schema", schema, "--before", a, "--after", a}, nil)
	if code != 0 || json.Unmarshal(out, &r) != nil || r.State != "same" || len(r.Changes) != 0 {
		t.Fatalf("same %d %s", code, out)
	}

	// A broken after input names the side and keeps the usual exit code.
	code, out, errOut = invoke(t, []string{"diff", "--schema", schema, "--before", a, "--after", "-"}, before[:16])
	var failed struct {
		DiffVersion int              `json:"diff_version"`
		Input       string           `json:"input"`
		Diagnostic  ebyte.Diagnostic `json:"diagnostic"`
	}
	if code != 2 || len(out) != 0 || json.Unmarshal(errOut, &failed) != nil || failed.Input != "after" || failed.Diagnostic.Code != "truncated_input" || failed.Diagnostic.Path != "/records/1/id" {
		t.Fatalf("broken after %d %s", code, errOut)
	}
	code, _, errOut = invoke(t, []string{"diff", "--schema", schema, "--before", filepath.Join(dir, "missing"), "--after", a}, nil)
	if code != 1 || json.Unmarshal(errOut, &failed) != nil || failed.Input != "before" || failed.Diagnostic.Code != "read_failed" {
		t.Fatalf("missing before %d %s", code, errOut)
	}

	for _, args := range [][]string{
		{"diff", "--schema", schema, "--before", a},
		{"diff", "--schema", schema, "--after", a},
		{"diff", "--schema", schema, "--before", "-", "--after", "-"},
		{"diff", "--schema", schema, "--input", a, "--before", a, "--after", b},
		{"decode", "--schema", schema, "--input", a, "--before", a},
		{"diff", "--schema", schema, "--before", a, "--after", b, "--format", "xml"},
	} {
		if code, _, _ := invoke(t, args, nil); code != 2 {
			t.Fatalf("%v: exit %d, want usage error", args, code)
		}
	}
}

// cancelOnRead cancels the command on its first read, like Ctrl-C while
// waiting on stdin.
type cancelOnRead struct{ cancel func() }

func (r cancelOnRead) Read([]byte) (int, error) {
	r.cancel()
	return 0, context.Canceled
}

func TestDiffCancelledStdin(t *testing.T) {
	schema := fixturePath(t, "packet.schema.json")
	a := filepath.Join(t.TempDir(), "a.bin")
	if os.WriteFile(a, packetInput(t), 0o600) != nil {
		t.Fatal("write fixture")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out, errOut bytes.Buffer
	code := runContext(ctx, []string{"diff", "--schema", schema, "--before", "-", "--after", a}, cancelOnRead{cancel}, &out, &errOut)
	var failed struct {
		Input      string           `json:"input"`
		Diagnostic ebyte.Diagnostic `json:"diagnostic"`
	}
	if code != 3 || json.Unmarshal(errOut.Bytes(), &failed) != nil || failed.Input != "before" || failed.Diagnostic.Code != "cancelled" {
		t.Fatalf("exit %d %s", code, errOut.Bytes())
	}
}
