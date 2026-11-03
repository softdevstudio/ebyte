package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/softdevstudio/ebyte/kaitai"
)

// runImport converts one Kaitai Struct .ksy file into an EByte schema. The
// schema goes to stdout or a new file; notes on what was left out go to
// stderr as one JSON object. A file outside the supported subset is refused
// with every reason, exit 2.
func runImport(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	input, output := "", "-"
	seen := map[string]bool{}
	usage := func() int {
		if e := writePayload(ctx, errOut, []byte("usage: ebyte import-ksy --input FILE.ksy|- [--output FILE|-]; see --help\n")); e != nil {
			return 1
		}
		return 2
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			return usage()
		}
		key, value, hasValue := strings.Cut(arg[2:], "=")
		if seen[key] || (key != "input" && key != "output") {
			return usage()
		}
		seen[key] = true
		if !hasValue {
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "--") {
				return usage()
			}
			value = args[i]
		}
		if value == "" {
			return usage()
		}
		if key == "input" {
			input = value
		} else {
			output = value
		}
	}
	if input == "" || (output != "-" && !validDestination(output)) {
		return usage()
	}
	if e := commandCancelled(ctx); e != nil {
		return reportContext(ctx, errOut, e)
	}
	src, e := readPath(ctx, input, in, kaitai.MaxSourceBytes)
	if e != nil {
		return reportContext(ctx, errOut, e)
	}
	result, e := kaitai.Import(src)
	var refused *kaitai.Refusal
	if errors.As(e, &refused) {
		raw, _ := json.Marshal(struct {
			DiagnosticVersion int             `json:"diagnostic_version"`
			Stage             string          `json:"stage"`
			Code              string          `json:"code"`
			Reasons           []kaitai.Reason `json:"reasons"`
		}{1, "import", "unsupported_construct", refused.Reasons})
		if writePayload(ctx, errOut, append(raw, '\n')) != nil {
			return 1
		}
		return 2
	}
	if e != nil {
		return reportContext(ctx, errOut, e)
	}
	if output == "-" {
		e = writePayload(ctx, out, result.Schema)
	} else {
		e = publishOutput(ctx, output, func(w io.Writer) error { return writePayload(ctx, w, result.Schema) })
	}
	if e != nil {
		return reportContext(ctx, errOut, e)
	}
	if len(result.Notes) > 0 {
		raw, _ := json.Marshal(struct {
			Notes []string `json:"notes"`
		}{result.Notes})
		if writePayload(ctx, errOut, append(raw, '\n')) != nil {
			return 1
		}
	}
	return 0
}
