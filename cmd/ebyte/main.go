package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/softdevstudio/ebyte"
)

const helpText = `EByte
Usage: ebyte COMMAND --schema FILE [--input FILE|-] [--output FILE|-]
Commands: validate-schema, decode, encode, inspect, diff
inspect, diff: --format json|text (default json)
--input is required for decode, encode and inspect. --output defaults to stdout (-).
diff: --before FILE|- --after FILE|- instead of --input; exit 5 when they differ.
Schema files cannot use stdin. Output files must not already exist.
Limits can only lower defaults: --max-schema-bytes, --max-schema-fields,
--max-depth, --max-input-bytes, --max-output-bytes, --max-field-bytes,
--max-array-elements, --max-value-nodes, --max-trace-entries,
--max-document-bytes, --max-trace-bytes.
Available: --help, --version. See README.md.
`

var errUsage = errors.New("invalid command arguments")

type options struct {
	command, schema, input, output, format string
	before, after                          string
	limits                                 ebyte.Limits
}

func main() {
	prepareSignals()
	outputFileMode = defaultOutputMode()
	ctx, stop := signal.NotifyContext(context.Background(), cancelSignals...)
	// Restore default interrupt behavior promptly, including while file I/O blocks.
	stopOnCancellation := context.AfterFunc(ctx, stop)
	code := runContext(ctx, os.Args[1:], interruptibleStdin{ctx: ctx, reader: os.Stdin}, os.Stdout, os.Stderr)
	stopOnCancellation()
	stop()
	os.Exit(code)
}

type interruptibleStdin struct {
	ctx    context.Context
	reader io.Reader
}

func (r interruptibleStdin) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}
	type readResult struct {
		data []byte
		n    int
		err  error
	}
	done := make(chan readResult, 1)
	// Own the buffer so a cancelled read cannot later modify a caller's memory.
	go func() { data := make([]byte, len(p)); n, err := r.reader.Read(data); done <- readResult{data, n, err} }()
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	case result := <-done:
		if result.n >= 0 && result.n <= len(result.data) {
			copy(p, result.data[:result.n])
		}
		return result.n, result.err
	}
}
func reportContext(ctx context.Context, w io.Writer, err error) int {
	if ctx.Err() != nil {
		var d *ebyte.Diagnostic
		if errors.As(err, &d) && d.Code == "cancelled" {
			return report(w, err)
		}
		if d != nil && d.Code == "write_failed" {
			observed := d.Observed
			if utf8.RuneCountInString("context cancelled; "+observed) <= 96 {
				observed = "context cancelled; " + observed
			}
			cancelled := commandFailure("cancelled", observed).(*ebyte.Diagnostic)
			cancelled.Path = d.Path
			cancelled.Offset = d.Offset
			cancelled.FieldStart = d.FieldStart
			err = cancelled
		} else {
			cancelled := commandCancelled(ctx).(*ebyte.Diagnostic)
			if d != nil {
				cancelled.Path = d.Path
				cancelled.Offset = d.Offset
				cancelled.FieldStart = d.FieldStart
			}
			err = cancelled
		}
	}
	return report(w, err)
}
func run(args []string, out, errOut io.Writer) int {
	return runContext(context.Background(), args, os.Stdin, out, errOut)
}
func runContext(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help" || args[0] == "--version") {
		payload := helpText
		if args[0] == "--version" {
			payload = "ebyte " + ebyte.Version + "\n"
		}
		if e := writePayload(ctx, out, []byte(payload)); e != nil {
			return reportContext(ctx, errOut, e)
		}
		return 0
	}
	if len(args) == 2 && validCommand(args[0]) && args[1] == "--help" {
		if e := writePayload(ctx, out, []byte(helpText)); e != nil {
			return reportContext(ctx, errOut, e)
		}
		return 0
	}
	opt, e := parseOptions(args)
	if e != nil {
		if errors.Is(e, errUsage) {
			if e = writePayload(ctx, errOut, []byte("usage: ebyte COMMAND --schema FILE [--input FILE|-] [--output FILE|-]; see --help\n")); e != nil {
				return 1
			}
			return 2
		}
		return reportContext(ctx, errOut, e)
	}
	if e = commandCancelled(ctx); e != nil {
		return reportContext(ctx, errOut, e)
	}
	raw, e := readPath(ctx, opt.schema, nil, opt.limits.MaxSchemaBytes)
	if e != nil {
		return reportContext(ctx, errOut, e)
	}
	plan, e := ebyte.Compile(raw, opt.limits)
	if e != nil {
		return reportContext(ctx, errOut, e)
	}
	var payload []byte
	var operationError error
	if opt.command == "validate-schema" {
		payload, e = jsonOutput(struct {
			SchemaVersion int    `json:"schema_version"`
			Name          string `json:"name"`
			SchemaSHA256  string `json:"schema_sha256"`
			Valid         bool   `json:"valid"`
		}{1, plan.Name(), plan.SchemaSHA256(), true}, opt.limits.MaxDocumentBytes)
	} else if opt.command == "diff" {
		var result *ebyte.DiffResult
		result, e = runDiff(ctx, plan, opt, in)
		var failed *ebyte.DiffError
		if errors.As(e, &failed) {
			return reportDiff(ctx, errOut, failed)
		}
		if e == nil {
			if opt.format == "text" {
				payload, e = textDiff(result, opt.limits.MaxTraceBytes)
			} else {
				payload, e = jsonOutput(result, opt.limits.MaxTraceBytes)
			}
		}
		if e == nil && result.State == "different" {
			operationError = errDifferent
		}
	} else {
		reader, closeInput, openError := openInput(opt.input, in)
		if openError != nil {
			return reportContext(ctx, errOut, openError)
		}
		switch opt.command {
		case "decode":
			var document *ebyte.Document
			document, _, e = plan.Decode(ctx, reader)
			if e == nil {
				payload, e = jsonOutput(document, opt.limits.MaxDocumentBytes)
			}
		case "inspect":
			var trace *ebyte.Trace
			trace, operationError = plan.Inspect(ctx, reader)
			payload, e = renderInspect(trace, operationError, func(t *ebyte.Trace) ([]byte, error) {
				if opt.format == "text" {
					return textTrace(t, opt.limits.MaxTraceBytes)
				}
				return jsonOutput(t, opt.limits.MaxTraceBytes)
			})
		case "encode":
			var documentBytes []byte
			documentBytes, e = readBounded(ctx, reader, opt.limits.MaxDocumentBytes)
			if e == nil {
				var document *ebyte.Document
				document, e = plan.LoadDocument(ctx, documentBytes)
				if e == nil {
					buffer := limitedBuffer{limit: opt.limits.MaxOutputBytes}
					e = plan.Encode(ctx, document, &buffer)
					if e == nil {
						payload = buffer.data
					}
				}
			}
		}
		closeError := closeInput()
		if e == nil && closeError != nil {
			e = commandFailure("read_failed", "input close failed")
		}
	}
	if e != nil {
		return reportContext(ctx, errOut, e)
	}
	if e = commandCancelled(ctx); e != nil {
		return reportContext(ctx, errOut, e)
	}
	if opt.output == "-" {
		e = writePayload(ctx, out, payload)
	} else {
		e = publishOutput(ctx, opt.output, func(w io.Writer) error { return writePayload(ctx, w, payload) })
	}
	if e != nil {
		return reportContext(ctx, errOut, e)
	}
	if operationError == errDifferent {
		return 5
	}
	if operationError != nil {
		return reportContext(ctx, errOut, operationError)
	}
	return 0
}
func validCommand(s string) bool {
	return s == "validate-schema" || s == "decode" || s == "encode" || s == "inspect" || s == "diff"
}
func parseOptions(args []string) (options, error) {
	opt := options{output: "-", format: "json", limits: ebyte.DefaultLimits()}
	if len(args) == 0 || !validCommand(args[0]) {
		return opt, errUsage
	}
	opt.command = args[0]
	settings := map[string]*int{"max-schema-bytes": &opt.limits.MaxSchemaBytes, "max-schema-fields": &opt.limits.MaxSchemaFields, "max-depth": &opt.limits.MaxDepth, "max-input-bytes": &opt.limits.MaxInputBytes, "max-output-bytes": &opt.limits.MaxOutputBytes, "max-field-bytes": &opt.limits.MaxFieldBytes, "max-array-elements": &opt.limits.MaxArrayElements, "max-value-nodes": &opt.limits.MaxValueNodes, "max-trace-entries": &opt.limits.MaxTraceEntries, "max-document-bytes": &opt.limits.MaxDocumentBytes, "max-trace-bytes": &opt.limits.MaxTraceBytes}
	defaults := map[string]int{}
	for key, ptr := range settings {
		defaults[key] = *ptr
	}
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			return opt, errUsage
		}
		key, value, hasValue := strings.Cut(arg[2:], "=")
		if seen[key] {
			return opt, errUsage
		}
		seen[key] = true
		if !hasValue {
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "--") {
				return opt, errUsage
			}
			value = args[i]
		}
		if value == "" {
			return opt, errUsage
		}
		switch key {
		case "schema":
			opt.schema = value
		case "input":
			if opt.command == "diff" {
				return opt, errUsage
			}
			opt.input = value
		case "before", "after":
			if opt.command != "diff" {
				return opt, errUsage
			}
			if key == "before" {
				opt.before = value
			} else {
				opt.after = value
			}
		case "output":
			opt.output = value
		case "format":
			if (opt.command != "inspect" && opt.command != "diff") || (value != "json" && value != "text") {
				return opt, errUsage
			}
			opt.format = value
		default:
			ptr, ok := settings[key]
			if !ok {
				return opt, errUsage
			}
			for j := range value {
				if value[j] < '0' || value[j] > '9' {
					return opt, errUsage
				}
			}
			n, e := strconv.ParseUint(value, 10, 64)
			if e != nil || n > uint64(defaults[key]) {
				return opt, commandFailure("limit_exceeded", "requested ceiling exceeds hard limit")
			}
			*ptr = int(n)
		}
	}
	if opt.schema == "" || opt.schema == "-" {
		return opt, errUsage
	}
	if opt.output != "-" && !validDestination(opt.output) {
		return opt, errUsage
	}
	if opt.command == "validate-schema" {
		if seen["input"] {
			return opt, errUsage
		}
	} else if opt.command == "diff" {
		if opt.before == "" || opt.after == "" || (opt.before == "-" && opt.after == "-") {
			return opt, errUsage
		}
	} else if opt.input == "" {
		return opt, errUsage
	}
	return opt, nil
}
func commandFailure(code, observed string) error {
	return &ebyte.Diagnostic{DiagnosticVersion: 1, Stage: "io", Code: code, Expected: "complete bounded command", Observed: observed}
}
func commandCancelled(ctx context.Context) error {
	if ctx.Err() != nil {
		return commandFailure("cancelled", "context cancelled")
	}
	return nil
}
func report(w io.Writer, err error) int {
	var d *ebyte.Diagnostic
	if !errors.As(err, &d) {
		d = commandFailure("read_failed", "operation failed").(*ebyte.Diagnostic)
	}
	raw, _ := json.Marshal(d)
	raw = append(raw, '\n')
	if e := writePayload(context.Background(), w, raw); e != nil {
		return 1
	}
	switch d.Code {
	case "cancelled":
		return 3
	case "limit_exceeded":
		return 4
	case "read_failed", "write_failed", "destination_exists":
		return 1
	default:
		return 2
	}
}
func writePayload(ctx context.Context, w io.Writer, p []byte) error {
	if e := commandCancelled(ctx); e != nil {
		return e
	}
	if len(p) > 0 {
		n, e := w.Write(p)
		if e != nil || n != len(p) {
			d := commandFailure("write_failed", "write failed; partial output may remain").(*ebyte.Diagnostic)
			if n >= 0 && n <= len(p) {
				at := int64(n)
				d.Offset = &at
			}
			return d
		}
	}
	if ctx.Err() != nil {
		return commandFailure("cancelled", "context cancelled; output may already be written")
	}
	return nil
}
func openInput(path string, stdin io.Reader) (io.Reader, func() error, error) {
	if path == "-" {
		return stdin, func() error { return nil }, nil
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, nil, commandFailure("read_failed", "input open failed")
	}
	return f, f.Close, nil
}
func readPath(ctx context.Context, path string, stdin io.Reader, limit int) ([]byte, error) {
	r, closeInput, e := openInput(path, stdin)
	if e != nil {
		return nil, e
	}
	raw, e := readBounded(ctx, r, limit)
	closed := closeInput()
	if e == nil && closed != nil {
		e = commandFailure("read_failed", "input close failed")
	}
	return raw, e
}
func readBounded(ctx context.Context, r io.Reader, limit int) ([]byte, error) {
	data := []byte{}
	var chunk [32768]byte
	empty := 0
	for {
		if e := commandCancelled(ctx); e != nil {
			return nil, e
		}
		request := limit + 1 - len(data)
		if request > len(chunk) {
			request = len(chunk)
		}
		n, e := r.Read(chunk[:request])
		if n < 0 || n > request {
			return nil, commandFailure("read_failed", "invalid reader count")
		}
		// A reader reporting unexpected EOF failed mid-read: that's an I/O failure
		// (exit 1), not a truncated binary field.
		if e != nil && e != io.EOF {
			return nil, commandFailure("read_failed", "input read failed")
		}
		data = append(data, chunk[:n]...)
		if len(data) > limit {
			return nil, commandFailure("limit_exceeded", "input exceeds configured byte ceiling")
		}
		if e == io.EOF {
			if e := commandCancelled(ctx); e != nil {
				return nil, e
			}
			return data, nil
		}
		if n == 0 {
			empty++
			if empty >= 100 {
				return nil, commandFailure("read_failed", "reader made no progress")
			}
		} else {
			empty = 0
		}
	}
}
func jsonOutput(v any, limit int) ([]byte, error) {
	raw, e := json.Marshal(v)
	if e != nil {
		return nil, commandFailure("write_failed", "JSON serialization failed")
	}
	if len(raw) > limit {
		return nil, commandFailure("limit_exceeded", "serialized JSON exceeds byte ceiling")
	}
	return raw, nil
}

type limitedBuffer struct {
	data  []byte
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-len(b.data) {
		return 0, commandFailure("limit_exceeded", "serialized output exceeds byte ceiling")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func textTrace(trace *ebyte.Trace, limit int) ([]byte, error) {
	b := limitedBuffer{limit: limit}
	if _, e := fmt.Fprintf(&b, "EByte trace state=%s schema_sha256=%s\n", trace.State, trace.SchemaSHA256); e != nil {
		return nil, e
	}
	for _, entry := range trace.Entries {
		line := fmt.Sprintf("%s %s offset=%d length=%d state=%s", strconv.Quote(entry.Path), entry.Kind, entry.Offset, entry.Length, entry.State)
		if entry.Value != nil {
			line += " value=" + *entry.Value
		}
		line += "\n"
		if _, e := b.Write([]byte(line)); e != nil {
			return nil, e
		}
	}
	if trace.Diagnostic != nil {
		raw, _ := json.Marshal(trace.Diagnostic)
		if _, e := fmt.Fprintf(&b, "diagnostic=%s\n", raw); e != nil {
			return nil, e
		}
	}
	return b.data, nil
}

// renderInspect renders a trace. When the decode itself failed and its trace
// can't be rendered within the ceiling, the decode failure is reported rather
// than hidden behind the rendering limit.
func renderInspect(trace *ebyte.Trace, operationError error, render func(*ebyte.Trace) ([]byte, error)) ([]byte, error) {
	if trace == nil {
		return nil, operationError
	}
	payload, e := render(trace)
	if e != nil && operationError != nil {
		return nil, operationError
	}
	return payload, e
}

// errDifferent marks a completed diff that found differences (exit 5).
var errDifferent = errors.New("inputs differ")

// runDiff opens both inputs, compares them and closes them again.
func runDiff(ctx context.Context, plan *ebyte.Plan, opt options, stdin io.Reader) (*ebyte.DiffResult, error) {
	before, closeBefore, e := openInput(opt.before, stdin)
	if e != nil {
		return nil, &ebyte.DiffError{Input: "before", Diagnostic: e.(*ebyte.Diagnostic)}
	}
	after, closeAfter, e := openInput(opt.after, stdin)
	if e != nil {
		closeBefore()
		return nil, &ebyte.DiffError{Input: "after", Diagnostic: e.(*ebyte.Diagnostic)}
	}
	result, e := plan.Diff(ctx, before, after)
	beforeClosed, afterClosed := closeBefore(), closeAfter()
	if e == nil && beforeClosed != nil {
		e = &ebyte.DiffError{Input: "before", Diagnostic: commandFailure("read_failed", "input close failed").(*ebyte.Diagnostic)}
	} else if e == nil && afterClosed != nil {
		e = &ebyte.DiffError{Input: "after", Diagnostic: commandFailure("read_failed", "input close failed").(*ebyte.Diagnostic)}
	}
	return result, e
}

// reportDiff writes the failed input and its diagnostic as one JSON line and
// returns the diagnostic's usual exit code.
func reportDiff(ctx context.Context, w io.Writer, failed *ebyte.DiffError) int {
	// After Ctrl-C an interrupted stdin read surfaces as a read failure; report
	// it as cancellation at the same location, as the other commands do.
	if ctx.Err() != nil && failed.Diagnostic.Code != "cancelled" {
		cancelled := commandCancelled(ctx).(*ebyte.Diagnostic)
		cancelled.Path, cancelled.Offset, cancelled.FieldStart = failed.Diagnostic.Path, failed.Diagnostic.Offset, failed.Diagnostic.FieldStart
		failed = &ebyte.DiffError{Input: failed.Input, Diagnostic: cancelled}
	}
	code := report(io.Discard, failed.Diagnostic)
	raw, _ := json.Marshal(struct {
		DiffVersion int               `json:"diff_version"`
		Input       string            `json:"input"`
		Diagnostic  *ebyte.Diagnostic `json:"diagnostic"`
	}{1, failed.Input, failed.Diagnostic})
	if e := writePayload(context.Background(), w, append(raw, '\n')); e != nil {
		return 1
	}
	return code
}

func textDiff(result *ebyte.DiffResult, limit int) ([]byte, error) {
	counts := map[string]int{}
	for _, c := range result.Changes {
		counts[c.Change]++
	}
	b := limitedBuffer{limit: limit}
	if _, e := fmt.Fprintf(&b, "EByte diff state=%s schema_sha256=%s size_before=%d size_after=%d changed=%d added=%d removed=%d\n", result.State, result.SchemaSHA256, result.SizeBefore, result.SizeAfter, counts["changed"], counts["added"], counts["removed"]); e != nil {
		return nil, e
	}
	side := func(name string, s *ebyte.Span) string {
		if s == nil {
			return name + "=-"
		}
		return fmt.Sprintf("%s=%d+%d", name, s.Offset, s.Length)
	}
	for _, c := range result.Changes {
		line := fmt.Sprintf("%s %s %s %s %s", c.Change, strconv.Quote(c.Path), c.Kind, side("a", c.Before), side("b", c.After))
		switch {
		case c.Before != nil && c.After != nil && c.Before.Value != nil && c.After.Value != nil:
			line += " value=" + *c.Before.Value + "->" + *c.After.Value
		case c.Before != nil && c.Before.Value != nil:
			line += " value=" + *c.Before.Value
		case c.After != nil && c.After.Value != nil:
			line += " value=" + *c.After.Value
		}
		if c.FirstDifference != nil {
			line += " first_difference=" + strconv.FormatInt(*c.FirstDifference, 10)
		}
		if c.Derived {
			line += " derived"
		}
		if _, e := b.Write([]byte(line + "\n")); e != nil {
			return nil, e
		}
	}
	return b.data, nil
}
