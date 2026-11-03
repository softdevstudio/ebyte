package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/softdevstudio/ebyte"
)

func TestImportKSYCommand(t *testing.T) {
	ksy, _ := filepath.Abs(filepath.Join("..", "..", "kaitai", "testdata", "gallery", "stl.ksy"))
	var out, errOut bytes.Buffer
	if code := run([]string{"import-ksy", "--input", ksy}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if _, e := ebyte.Compile(out.Bytes(), ebyte.DefaultLimits()); e != nil {
		t.Fatal(e)
	}
	var notes struct{ Notes []string }
	if e := json.Unmarshal(errOut.Bytes(), &notes); e != nil || len(notes.Notes) == 0 {
		t.Fatalf("notes %q", errOut.String())
	}
	// To a new file, which must not exist yet.
	dest := filepath.Join(t.TempDir(), "stl.schema.json")
	out.Reset()
	errOut.Reset()
	if code := run([]string{"import-ksy", "--input=" + ksy, "--output", dest}, &out, &errOut); code != 0 || out.Len() != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if b, _ := os.ReadFile(dest); !strings.Contains(string(b), `"name": "stl"`) {
		t.Fatalf("published %s", b)
	}
	if code := run([]string{"import-ksy", "--input", ksy, "--output", dest}, &out, &errOut); code != 1 {
		t.Fatalf("existing destination: exit %d", code)
	}
	// Refusals list every reason on stderr, exit 2, nothing on stdout.
	bad := filepath.Join(t.TempDir(), "bad.ksy")
	os.WriteFile(bad, []byte("meta: {id: b, endian: le}\nseq:\n  - {id: a, type: strz, encoding: ASCII}\n  - {id: c, size-eos: true}\n"), 0o644)
	out.Reset()
	errOut.Reset()
	if code := run([]string{"import-ksy", "--input", bad}, &out, &errOut); code != 2 || out.Len() != 0 {
		t.Fatalf("refusal exit %d, stdout %q", code, out.String())
	}
	var refused struct {
		Code    string
		Reasons []struct{ Path, Reason string }
	}
	if e := json.Unmarshal(errOut.Bytes(), &refused); e != nil || refused.Code != "unsupported_construct" || len(refused.Reasons) != 2 {
		t.Fatalf("refusal %q", errOut.String())
	}
	for _, args := range [][]string{{"import-ksy"}, {"import-ksy", "--schema", ksy}, {"import-ksy", "--input", ksy, "--input", ksy}, {"import-ksy", "--input", ksy, "extra"}} {
		if code := run(args, &out, &errOut); code != 2 {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
	// stdin works too.
	src, _ := os.ReadFile(ksy)
	out.Reset()
	if code := runContext(t.Context(), []string{"import-ksy", "--input", "-"}, bytes.NewReader(src), &out, &errOut); code != 0 || out.Len() == 0 {
		t.Fatalf("stdin exit %d", code)
	}
}
