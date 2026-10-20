package main

import (
	"fmt"
	"io"
	"os"

	"github.com/softdevstudio/ebyte"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, out, errOut io.Writer) int {
	if len(args) == 1 {
		switch args[0] {
		case "--version":
			if _, e := fmt.Fprintln(out, "ebyte "+ebyte.Version); e != nil {
				return 1
			}
			return 0
		case "--help", "help":
			if _, e := fmt.Fprintln(out, "EByte development build\nAvailable: --version, --help\nPlanned: validate-schema, decode, encode, inspect\nCodec operations are not implemented. No release is available. See README.md."); e != nil {
				return 1
			}
			return 0
		}
	}
	message := "usage: ebyte --version | --help"
	if len(args) > 0 {
		switch args[0] {
		case "validate-schema", "decode", "encode", "inspect":
			message = "codec operations are not available in this development build"
		}
	}
	_, _ = fmt.Fprintln(errOut, message)
	return 2
}
