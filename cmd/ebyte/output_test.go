package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/softdevstudio/ebyte"
)

func supportedPublication(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("publication not enabled on this platform")
	}
}
func assertNoTemporary(t *testing.T, dir string) {
	t.Helper()
	paths, e := filepath.Glob(filepath.Join(dir, ".ebyte-*.tmp"))
	if e != nil || len(paths) != 0 {
		t.Fatalf("staging files left: %v %v", paths, e)
	}
}
func TestPublishOutput(t *testing.T) {
	supportedPublication(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "result.bin")
	e := publishOutput(context.Background(), path, func(w io.Writer) error { _, e := w.Write([]byte("complete")); return e })
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(path)
	if e != nil || string(b) != "complete" {
		t.Fatal("publication")
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("output permissions")
	}
	assertNoTemporary(t, dir)
	called := false
	e = publishOutput(context.Background(), path, func(io.Writer) error { called = true; return nil })
	if e.(*ebyte.Diagnostic).Code != "destination_exists" || called {
		t.Fatal("existing destination")
	}
	b, _ = os.ReadFile(path)
	if string(b) != "complete" {
		t.Fatal("existing content replaced")
	}
}
func TestPublicationFailuresLeaveNoDestination(t *testing.T) {
	supportedPublication(t)
	for _, mode := range []string{"producer", "cancel", "closed-file"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "result")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("producer failure")
			e := publishOutput(ctx, path, func(w io.Writer) error {
				_, _ = w.Write([]byte("partial"))
				switch mode {
				case "producer":
					return sentinel
				case "cancel":
					cancel()
				case "closed-file":
					return w.(*os.File).Close()
				}
				return nil
			})
			if e == nil {
				t.Fatal("failure accepted")
			}
			if mode == "producer" && !errors.Is(e, sentinel) {
				t.Fatal("producer error lost")
			}
			if _, e = os.Lstat(path); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("failed output published")
			}
			assertNoTemporary(t, dir)
		})
	}
}
func TestPublicationRefusesSymlinksAndDirectories(t *testing.T) {
	supportedPublication(t)
	for _, mode := range []string{"symlink", "dangling", "directory"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target")
			path := filepath.Join(dir, "result")
			if mode == "directory" {
				if e := os.Mkdir(path, 0700); e != nil {
					t.Fatal(e)
				}
			} else {
				if mode == "symlink" {
					if e := os.WriteFile(target, []byte("untouched"), 0600); e != nil {
						t.Fatal(e)
					}
				}
				if e := os.Symlink(target, path); e != nil {
					t.Fatal(e)
				}
			}
			e := publishOutput(context.Background(), path, func(io.Writer) error { t.Fatal("producer called"); return nil })
			if e.(*ebyte.Diagnostic).Code != "destination_exists" {
				t.Fatal(e)
			}
			if mode == "symlink" {
				b, _ := os.ReadFile(target)
				if string(b) != "untouched" {
					t.Fatal("symlink target modified")
				}
			}
			assertNoTemporary(t, dir)
		})
	}
}
func TestDestinationCreatedDuringStaging(t *testing.T) {
	supportedPublication(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "result")
	e := publishOutput(context.Background(), path, func(w io.Writer) error {
		if _, e := w.Write([]byte("new")); e != nil {
			return e
		}
		return os.WriteFile(path, []byte("race winner"), 0600)
	})
	if e.(*ebyte.Diagnostic).Code != "destination_exists" {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "race winner" {
		t.Fatal("concurrent destination replaced")
	}
	assertNoTemporary(t, dir)
}
func TestPublicationPinsRenamedParent(t *testing.T) {
	supportedPublication(t)
	base := t.TempDir()
	dir := filepath.Join(base, "parent")
	moved := filepath.Join(base, "moved")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	e := publishOutput(context.Background(), filepath.Join(dir, "result"), func(w io.Writer) error {
		if e := os.Rename(dir, moved); e != nil {
			return e
		}
		if e := os.Mkdir(dir, 0700); e != nil {
			return e
		}
		if e := os.WriteFile(filepath.Join(dir, "result"), []byte("untouched"), 0600); e != nil {
			return e
		}
		_, e := w.Write([]byte("original directory"))
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	for path, want := range map[string]string{filepath.Join(dir, "result"): "untouched", filepath.Join(moved, "result"): "original directory"} {
		b, e := os.ReadFile(path)
		if e != nil || string(b) != want {
			t.Fatalf("pinned directory: %v", e)
		}
	}
	assertNoTemporary(t, moved)
}
