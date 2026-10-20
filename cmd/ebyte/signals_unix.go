//go:build unix

package main

import (
	"os/signal"
	"syscall"
)

// Let stdout/stderr writes report EPIPE through the normal diagnostic path.
func prepareSignals() { signal.Ignore(syscall.SIGPIPE) }
