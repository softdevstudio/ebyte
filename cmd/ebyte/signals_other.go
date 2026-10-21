//go:build !unix

package main

import "os"

var cancelSignals = []os.Signal{os.Interrupt}

func prepareSignals() {}

// Windows has no Unix permission bits; Chmod with this mode only keeps the
// published file writable.
func defaultOutputMode() os.FileMode { return 0644 }
