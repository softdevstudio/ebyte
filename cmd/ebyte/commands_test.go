package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/softdevstudio/ebyte"
)

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	p, e := filepath.Abs(filepath.Join("..", "..", "testdata", name))
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func fixtureRaw(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile(fixturePath(t, name))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func packetInput(t *testing.T) []byte {
	t.Helper()
	b, e := hex.DecodeString(strings.TrimSpace(string(fixtureRaw(t, "packet.hex"))))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func invoke(t *testing.T, args []string, input []byte) (int, []byte, []byte) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), args, bytes.NewReader(input), &out, &errOut)
	return code, out.Bytes(), errOut.Bytes()
}
func errorDiagnostic(t *testing.T, raw []byte, code string) ebyte.Diagnostic {
	t.Helper()
	var d ebyte.Diagnostic
	if e := json.Unmarshal(raw, &d); e != nil || d.Code != code {
		t.Fatalf("diagnostic %s: %v", raw, e)
	}
	return d
}
func TestOperationalCommands(t *testing.T) {
	schema := fixturePath(t, "packet.schema.json")
	code, out, errOut := invoke(t, []string{"validate-schema", "--schema", schema}, nil)
	if code != 0 || len(errOut) != 0 {
		t.Fatalf("validate %d %s", code, errOut)
	}
	var valid map[string]any
	if e := json.Unmarshal(out, &valid); e != nil || valid["valid"] != true || valid["name"] != "ebyte_packet" {
		t.Fatal("validation output")
	}
	for _, name := range []string{"packet", "wide", "empty", "stored"} {
		t.Run(name, func(t *testing.T) {
			schema := fixturePath(t, name+".schema.json")
			raw, e := hex.DecodeString(strings.TrimSpace(string(fixtureRaw(t, name+".hex"))))
			if e != nil {
				t.Fatal(e)
			}
			code, out, errOut := invoke(t, []string{"decode", "--schema=" + schema, "--input=-"}, raw)
			if code != 0 || len(errOut) != 0 {
				t.Fatalf("decode %d %s", code, errOut)
			}
			var got, want any
			_ = json.Unmarshal(out, &got)
			_ = json.Unmarshal(fixtureRaw(t, name+".document.json"), &want)
			if !reflect.DeepEqual(got, want) {
				t.Fatal("document differs from golden")
			}
			code, encoded, errOut := invoke(t, []string{"encode", "--schema", schema, "--input", "-"}, out)
			if code != 0 || len(errOut) != 0 || !bytes.Equal(encoded, raw) {
				t.Fatalf("encode %d %x %s", code, encoded, errOut)
			}
		})
	}
	code, out, errOut = invoke(t, []string{"inspect", "--schema", schema, "--input", "-"}, packetInput(t))
	if code != 0 || len(errOut) != 0 {
		t.Fatalf("inspect %d %s", code, errOut)
	}
	var got, want any
	_ = json.Unmarshal(out, &got)
	_ = json.Unmarshal(fixtureRaw(t, "packet.trace.json"), &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("trace differs from golden")
	}
	code, out, errOut = invoke(t, []string{"inspect", "--schema", schema, "--input", "-", "--format", "text"}, packetInput(t))
	if code != 0 || len(errOut) != 0 || !bytes.Contains(out, []byte(`"/record_count" u8 offset=3 length=1 state=complete value=2`)) || bytes.Contains(out, []byte("4542")) {
		t.Fatalf("text trace %d %s %s", code, out, errOut)
	}
}
func TestFailedDecodeAndInspect(t *testing.T) {
	schema := fixturePath(t, "packet.schema.json")
	input := packetInput(t)
	input = input[:len(input)-1]
	for _, command := range []string{"decode", "inspect"} {
		for _, format := range []string{"json", "text"} {
			if command == "decode" && format == "text" {
				continue
			}
			args := []string{command, "--schema", schema, "--input", "-"}
			if command == "inspect" {
				args = append(args, "--format", format)
			}
			code, out, errOut := invoke(t, args, input)
			if code != 2 {
				t.Fatalf("%s failed code %d", command, code)
			}
			d := errorDiagnostic(t, errOut, "truncated_input")
			if d.Path != "/note" || *d.Offset != 20 {
				t.Fatal("failed position")
			}
			if command == "decode" {
				if len(out) != 0 {
					t.Fatal("partial editable stdout")
				}
			} else if format == "json" {
				var trace ebyte.Trace
				if e := json.Unmarshal(out, &trace); e != nil || trace.State != "failed" || trace.Diagnostic.Code != "truncated_input" {
					t.Fatalf("failed trace %s", out)
				}
			} else if !bytes.HasPrefix(out, []byte("EByte trace state=failed")) || !bytes.Contains(out, []byte("diagnostic=")) {
				t.Fatal("unlabelled text failure")
			}
		}
	}
	// A budget too small for a failed trace emits only the diagnostic.
	code, out, errOut := invoke(t, []string{"inspect", "--schema", schema, "--input", "-", "--max-trace-bytes", "0"}, input)
	if code != 4 || len(out) != 0 {
		t.Fatal("failed trace budget")
	}
	errorDiagnostic(t, errOut, "limit_exceeded")
}
func TestCLIFilePublication(t *testing.T) {
	supportedPublication(t)
	dir := t.TempDir()
	input := filepath.Join(dir, "opaque.bin")
	output := filepath.Join(dir, "editable.json")
	schema := fixturePath(t, "packet.schema.json")
	if e := os.WriteFile(input, packetInput(t), 0600); e != nil {
		t.Fatal(e)
	}
	args := []string{"decode", "--schema", schema, "--input", input, "--output", output}
	code, out, errOut := invoke(t, args, nil)
	if code != 0 || len(out) != 0 || len(errOut) != 0 {
		t.Fatalf("file decode %d %s", code, errOut)
	}
	doc, e := os.ReadFile(output)
	if e != nil {
		t.Fatal(e)
	}
	restored := filepath.Join(dir, "restored.bin")
	code, out, errOut = invoke(t, []string{"encode", "--schema", schema, "--input", output, "--output", restored}, nil)
	b, e := os.ReadFile(restored)
	if code != 0 || len(out) != 0 || len(errOut) != 0 || e != nil || !bytes.Equal(b, packetInput(t)) {
		t.Fatal("file round trip")
	}
	code, out, errOut = invoke(t, args, nil)
	if code != 1 || len(out) != 0 {
		t.Fatal("existing output accepted")
	}
	errorDiagnostic(t, errOut, "destination_exists")
	b, _ = os.ReadFile(output)
	if !bytes.Equal(b, doc) {
		t.Fatal("existing output replaced")
	}
	// A failed decode never stages or publishes its editable output.
	failed := filepath.Join(dir, "failed.json")
	code, out, _ = invoke(t, []string{"decode", "--schema", schema, "--input", "-", "--output", failed}, []byte{0})
	if code != 2 || len(out) != 0 {
		t.Fatal("failure result")
	}
	if _, e = os.Lstat(failed); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("failed output exists")
	}
	assertNoTemporary(t, dir)
	// Inspect deliberately publishes a labelled failed trace and exits nonzero.
	tracePath := filepath.Join(dir, "failed-trace.json")
	code, out, errOut = invoke(t, []string{"inspect", "--schema", schema, "--input", "-", "--output", tracePath}, packetInput(t)[:20])
	traceRaw, e := os.ReadFile(tracePath)
	if code != 2 || len(out) != 0 || len(errOut) == 0 || e != nil || !bytes.Contains(traceRaw, []byte(`"state":"failed"`)) {
		t.Fatal("failed trace file")
	}
}
func TestCLIArgumentAndIOFailures(t *testing.T) {
	schema := fixturePath(t, "empty.schema.json")
	for _, args := range [][]string{
		{"decode", "--schema", schema},
		{"decode", "--schema", schema, "--input", "-", "--input=SECRET"},
		{"decode", "--schema=-", "--input=-"},
		{"validate-schema", "--schema", schema, "--input=SECRET"},
		{"decode", "--schema", schema, "--input=-", "--format=text"},
		{"inspect", "--schema", schema, "--input=-", "--format=SECRET"},
		{"decode", "--schema", schema, "--input=-", "--unknown=SECRET"},
		{"decode", "--schema", schema, "--input=-", "--max-depth=-1"},
	} {
		code, out, errOut := invoke(t, args, nil)
		if code != 2 || len(out) != 0 || bytes.Contains(errOut, []byte("SECRET")) {
			t.Fatalf("arguments %v: %d %s", args, code, errOut)
		}
	}
	code, out, errOut := invoke(t, []string{"decode", "--schema=missing-SECRET", "--input=-"}, nil)
	if code != 1 || len(out) != 0 || bytes.Contains(errOut, []byte("SECRET")) {
		t.Fatal("open failure privacy")
	}
	errorDiagnostic(t, errOut, "read_failed")
	// Invalid schema is rejected before a binary input is read.
	bad := filepath.Join(t.TempDir(), "bad.json")
	if e := os.WriteFile(bad, []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	var stdout, stderr bytes.Buffer
	r := &countingReader{Reader: strings.NewReader("SECRET")}
	code = runContext(context.Background(), []string{"decode", "--schema", bad, "--input=-"}, r, &stdout, &stderr)
	if code != 2 || r.calls != 0 || stdout.Len() != 0 {
		t.Fatal("read before schema validation")
	}
	errorDiagnostic(t, stderr.Bytes(), "invalid_schema")
}

type countingReader struct {
	io.Reader
	calls, bytes int
}

func (r *countingReader) Read(p []byte) (int, error) {
	r.calls++
	n, e := r.Reader.Read(p)
	r.bytes += n
	return n, e
}

type zeroReader struct{}

func (zeroReader) Read([]byte) (int, error) { return 0, nil }

type badCountReader struct{}

func (badCountReader) Read([]byte) (int, error) { return -1, nil }

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }
func TestCLILimitsCancellationAndWriters(t *testing.T) {
	schema := fixturePath(t, "packet.schema.json")
	for _, limit := range []struct{ flag, value string }{{"max-input-bytes", "1"}, {"max-schema-bytes", "1"}, {"max-array-elements", "1"}, {"max-document-bytes", "0"}, {"max-depth", "9"}} {
		code, out, errOut := invoke(t, []string{"decode", "--schema", schema, "--input=-", "--" + limit.flag, limit.value}, packetInput(t))
		if code != 4 || len(out) != 0 {
			t.Fatalf("limit %s %d", limit.flag, code)
		}
		errorDiagnostic(t, errOut, "limit_exceeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	r := &countingReader{Reader: bytes.NewReader(packetInput(t))}
	code := runContext(ctx, []string{"decode", "--schema", schema, "--input=-"}, r, &out, &errOut)
	if code != 3 || r.calls != 0 || out.Len() != 0 {
		t.Fatal("cancelled command")
	}
	errorDiagnostic(t, errOut.Bytes(), "cancelled")
	for _, writer := range []io.Writer{shortWriter{}, brokenWriter{}} {
		errOut.Reset()
		code = runContext(context.Background(), []string{"decode", "--schema", schema, "--input=-"}, bytes.NewReader(packetInput(t)), writer, &errOut)
		if code != 1 {
			t.Fatal("stdout failure")
		}
		errorDiagnostic(t, errOut.Bytes(), "write_failed")
	}
	if code = runContext(context.Background(), []string{"decode", "--schema=missing"}, strings.NewReader(""), io.Discard, shortWriter{}); code != 1 {
		t.Fatal("stderr failure")
	}
}
func TestBoundedJSONReads(t *testing.T) {
	r := &countingReader{Reader: strings.NewReader(strings.Repeat("x", 10000))}
	if _, e := readBounded(context.Background(), r, 10); e.(*ebyte.Diagnostic).Code != "limit_exceeded" || r.bytes != 11 {
		t.Fatal("unbounded JSON read")
	}
	if raw, e := readBounded(context.Background(), strings.NewReader("abcd"), 4); e != nil || string(raw) != "abcd" {
		t.Fatal("exact read limit")
	}
	for _, r := range []io.Reader{zeroReader{}, badCountReader{}} {
		if _, e := readBounded(context.Background(), r, 4); e == nil || e.(*ebyte.Diagnostic).Code != "read_failed" {
			t.Fatal("bad reader")
		}
	}
}
func TestTextTraceLimitAndHelpShortWrite(t *testing.T) {
	trace := &ebyte.Trace{TraceVersion: 1, SchemaSHA256: strings.Repeat("0", 64), State: "complete", Entries: []ebyte.TraceEntry{}}
	payload, e := textTrace(trace, 4096)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = textTrace(trace, len(payload)); e != nil {
		t.Fatal(e)
	}
	if _, e = textTrace(trace, len(payload)-1); e == nil {
		t.Fatal("text size ignored")
	}
	for _, args := range [][]string{{"--help"}, {"--version"}, {"decode", "--help"}} {
		var stderr bytes.Buffer
		if code := runContext(context.Background(), args, strings.NewReader(""), shortWriter{}, &stderr); code != 1 {
			t.Fatal("help output error")
		}
	}
}

type cancelOnWrite struct {
	cancel context.CancelFunc
	fail   bool
}

func (w cancelOnWrite) Write(p []byte) (int, error) {
	w.cancel()
	if w.fail {
		return 1, io.ErrClosedPipe
	}
	return len(p), nil
}
func TestCancellationRetainsOutputWarning(t *testing.T) {
	for _, fail := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		var stderr bytes.Buffer
		code := runContext(ctx, []string{"--help"}, strings.NewReader(""), cancelOnWrite{cancel: cancel, fail: fail}, &stderr)
		cancel()
		if code != 3 {
			t.Fatal("cancelled write exit", code)
		}
		d := errorDiagnostic(t, stderr.Bytes(), "cancelled")
		if !strings.Contains(d.Observed, "output may") {
			t.Fatalf("lost output warning: %#v", d)
		}
	}
}
