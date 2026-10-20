package main

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/softdevstudio/ebyte"
)

func publicationFailure(code string, published bool) error {
	observed := "file publication failed"
	if published {
		observed = "publication cleanup failed; destination may already exist"
	}
	return &ebyte.Diagnostic{DiagnosticVersion: 1, Stage: "io", Code: code, Expected: "complete output at a new destination", Observed: observed}
}
func publicationCancelled(ctx context.Context) error {
	if ctx.Err() != nil {
		return &ebyte.Diagnostic{DiagnosticVersion: 1, Stage: "io", Code: "cancelled", Expected: "active operation", Observed: "context cancelled"}
	}
	return nil
}

// publishOutput stages in the destination directory, syncs/closes the file, then
// atomically creates a new hard link. It never replaces a destination, including
// a symlink. This helper is gated to native-tested Linux/macOS; unsupported
// filesystems fail closed. It does not promise directory/power-loss durability.
// The opened parent directory must be trusted against concurrent modification of
// its entries. Directory renames are pinned by os.Root rather than followed.
func publishOutput(ctx context.Context, destination string, produce func(io.Writer) error) error {
	if e := publicationCancelled(ctx); e != nil {
		return e
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return publicationFailure("write_failed", false)
	}
	root, e := os.OpenRoot(filepath.Dir(destination))
	if e != nil {
		return publicationFailure("write_failed", false)
	}
	defer root.Close()
	name := filepath.Base(destination)
	if _, e = root.Lstat(name); e == nil {
		return publicationFailure("destination_exists", false)
	} else if !errors.Is(e, os.ErrNotExist) {
		return publicationFailure("write_failed", false)
	}
	var file *os.File
	var temporary string
	for attempts := 0; attempts < 64; attempts++ {
		temporary = ".ebyte-" + rand.Text() + ".tmp"
		file, e = root.OpenFile(temporary, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if e == nil {
			break
		}
		if !errors.Is(e, os.ErrExist) {
			return publicationFailure("write_failed", false)
		}
	}
	if e != nil {
		return publicationFailure("write_failed", false)
	}
	defer file.Close()
	defer root.Remove(temporary)
	if e = produce(file); e != nil {
		return e
	}
	if e = publicationCancelled(ctx); e != nil {
		return e
	}
	if e = file.Sync(); e != nil {
		return publicationFailure("write_failed", false)
	}
	if e = file.Close(); e != nil {
		return publicationFailure("write_failed", false)
	}
	if e = publicationCancelled(ctx); e != nil {
		return e
	}
	if e = root.Link(temporary, name); e != nil {
		if errors.Is(e, os.ErrExist) {
			return publicationFailure("destination_exists", false)
		}
		return publicationFailure("write_failed", false)
	}
	if e = root.Remove(temporary); e != nil {
		return publicationFailure("write_failed", true)
	}
	return nil
}
