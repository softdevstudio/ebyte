package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/softdevstudio/ebyte"
)

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestAvailableCommands(t *testing.T) {
	for _, arg := range []string{"--version", "--help", "help"} {
		var out, errOut bytes.Buffer
		if code := run([]string{arg}, &out, &errOut); code != 0 || out.Len() == 0 || errOut.Len() != 0 {
			t.Fatal(arg, code, out.String(), errOut.String())
		}
		if arg == "--version" && out.String() != "ebyte "+ebyte.Version+"\n" {
			t.Fatal(out.String())
		}
		if code := run([]string{arg}, brokenWriter{}, io.Discard); code != 1 {
			t.Fatal("lost output failure", code)
		}
	}
}
func TestInvalidArgumentsFailWithoutEcho(t *testing.T) {
	for _, args := range [][]string{nil, {"--version", "SECRET"}, {"unknown-SECRET"}, {"decode", "--input=SECRET"}, {"encode"}, {"inspect"}, {"validate-schema"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 || out.Len() != 0 || errOut.Len() == 0 || strings.Contains(errOut.String(), "SECRET") {
			t.Fatal(args, code, out.String(), errOut.String())
		}
	}
}
