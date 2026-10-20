//go:build !unix

package main

import "os"

var cancelSignals = []os.Signal{os.Interrupt}

func prepareSignals() {}

// File publication is disabled on these platforms; the mode is unused.
func defaultOutputMode() os.FileMode { return 0644 }
