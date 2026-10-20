//go:build unix

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// Ctrl-C and SIGTERM both cancel cooperatively, so staging files are cleaned up.
var cancelSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// Let stdout/stderr writes report EPIPE through the normal diagnostic path.
func prepareSignals() { signal.Ignore(syscall.SIGPIPE) }

// defaultOutputMode returns 0666 minus the process umask, the mode a plain
// file created by this user would get. It reads the umask once at startup,
// before any file is created, and restores it immediately.
func defaultOutputMode() os.FileMode {
	mask := syscall.Umask(0)
	syscall.Umask(mask)
	return os.FileMode(0666 &^ mask)
}
