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
type publicationOps struct {
	create func(*os.Root, string) (*os.File, error)
	sync   func(*os.File) error
	close  func(*os.File) error
	link   func(*os.Root, string, string) error
	remove func(*os.Root, string) error
}

func nativePublicationOps() publicationOps {
	return publicationOps{
		create: func(root *os.Root, name string) (*os.File, error) {
			return root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		},
		sync:   func(file *os.File) error { return file.Sync() },
		close:  func(file *os.File) error { return file.Close() },
		link:   func(root *os.Root, old, new string) error { return root.Link(old, new) },
		remove: func(root *os.Root, name string) error { return root.Remove(name) },
	}
}
func publishOutput(ctx context.Context, destination string, produce func(io.Writer) error) error {
	return publishOutputWith(ctx, destination, produce, nativePublicationOps())
}

// Per-call operations allow failure tests without mutable global hooks.
func publishOutputWith(ctx context.Context, destination string, produce func(io.Writer) error, ops publicationOps) (result error) {
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
		file, e = ops.create(root, temporary)
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
	closed, published := false, false
	defer func() {
		if !closed {
			if e := ops.close(file); e != nil && result == nil {
				result = publicationFailure("write_failed", published)
			}
		}
		if e := ops.remove(root, temporary); e != nil && !errors.Is(e, os.ErrNotExist) {
			observed := "staging cleanup failed; temporary file may remain"
			if published {
				observed = "staging cleanup failed; destination and temporary file may exist"
			}
			result = &ebyte.Diagnostic{DiagnosticVersion: 1, Stage: "io", Code: "write_failed", Expected: "staging file removed", Observed: observed}
		}
	}()
	if e = produce(file); e != nil {
		return e
	}
	if e = publicationCancelled(ctx); e != nil {
		return e
	}
	if e = ops.sync(file); e != nil {
		return publicationFailure("write_failed", false)
	}
	if e = ops.close(file); e != nil {
		return publicationFailure("write_failed", false)
	}
	closed = true
	if e = publicationCancelled(ctx); e != nil {
		return e
	}
	if e = ops.link(root, temporary, name); e != nil {
		if errors.Is(e, os.ErrExist) {
			return publicationFailure("destination_exists", false)
		}
		return publicationFailure("write_failed", false)
	}
	published = true
	return nil
}
