package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/softdevstudio/ebyte"
)

func supportedPublication(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("publication not enabled on this platform")
	}
}

// unixPublication skips checks of Unix permission bits, symlinks made without
// privileges and renaming a directory that has open handles, none of which
// apply on Windows.
func unixPublication(t *testing.T) {
	t.Helper()
	supportedPublication(t)
	if runtime.GOOS == "windows" {
		t.Skip("Unix file semantics")
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
	if e != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != outputFileMode) {
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
			if mode != "directory" && runtime.GOOS == "windows" {
				t.Skip("symlinks need privileges on Windows")
			}
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
	unixPublication(t)
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

func TestInjectedPublicationFailures(t *testing.T) {
	supportedPublication(t)
	for _, stage := range []string{"create", "chmod", "sync", "close", "link", "cleanup-before-link", "cleanup-after-link", "cancel-after-sync"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "result")
			ops := nativePublicationOps()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fault := errors.New("SECRET injected filesystem failure")
			linked := false
			var temporary string
			originalCreate, originalClose, originalLink := ops.create, ops.close, ops.link
			ops.create = func(root *os.Root, name string) (*os.File, error) {
				temporary = name
				if stage == "create" {
					return nil, fault
				}
				return originalCreate(root, name)
			}
			if stage == "chmod" {
				ops.chmod = func(*os.File, os.FileMode) error { return fault }
			}
			if stage == "sync" {
				ops.sync = func(*os.File) error { return fault }
			}
			if stage == "close" {
				calls := 0
				ops.close = func(file *os.File) error {
					calls++
					if calls == 1 {
						return fault
					}
					return originalClose(file)
				}
			}
			ops.link = func(root *os.Root, old, new string) error {
				if stage == "link" {
					return fault
				}
				e := originalLink(root, old, new)
				linked = e == nil
				return e
			}
			if stage == "cleanup-before-link" || stage == "cleanup-after-link" {
				ops.remove = func(*os.Root, string) error { return fault }
			}
			if stage == "cancel-after-sync" {
				original := ops.sync
				ops.sync = func(file *os.File) error { err := original(file); cancel(); return err }
			}
			err := publishOutputWith(ctx, path, func(w io.Writer) error {
				_, err := w.Write([]byte("complete"))
				if stage == "cleanup-before-link" {
					return fault
				}
				return err
			}, ops)
			d, ok := err.(*ebyte.Diagnostic)
			if !ok || strings.Contains(d.Error(), "SECRET") {
				t.Fatalf("got %#v", err)
			}
			want := "write_failed"
			if stage == "cancel-after-sync" {
				want = "cancelled"
			}
			if d.Code != want {
				t.Fatalf("got %#v", d)
			}
			_, statErr := os.Lstat(path)
			if stage == "cleanup-after-link" {
				if !linked || statErr != nil {
					t.Fatal("committed destination lost")
				}
				b, _ := os.ReadFile(path)
				if string(b) != "complete" {
					t.Fatal("partial publication")
				}
				if !strings.Contains(d.Observed, "destination") {
					t.Fatal("lost publication warning")
				}
			} else if linked || !errors.Is(statErr, os.ErrNotExist) {
				t.Fatal("failed operation published output")
			}
			if stage == "cleanup-before-link" || stage == "cleanup-after-link" {
				if !strings.Contains(d.Observed, "temporary file") {
					t.Fatal("lost cleanup warning")
				}
				if _, err := os.Lstat(filepath.Join(dir, temporary)); err != nil {
					t.Fatal("fault not exercised")
				}
				if err := os.Remove(filepath.Join(dir, temporary)); err != nil {
					t.Fatal(err)
				}
			}
			assertNoTemporary(t, dir)
		})
	}
}

func TestPublicationRejectsDirectoryStyleDestinations(t *testing.T) {
	supportedPublication(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "out")
	if e := os.Mkdir(sub, 0700); e != nil {
		t.Fatal(e)
	}
	for _, destination := range []string{sub + string(os.PathSeparator), sub + "/", sub + "/.", sub + "/..", "/", ""} {
		called := false
		err := publishOutput(context.Background(), destination, func(w io.Writer) error { called = true; return nil })
		d, ok := err.(*ebyte.Diagnostic)
		if !ok || d.Code != "write_failed" || called {
			t.Fatalf("%q: %#v", destination, err)
		}
	}
	entries, e := os.ReadDir(sub)
	if e != nil || len(entries) != 0 {
		t.Fatalf("directory destination created files: %v %v", entries, e)
	}
	assertNoTemporary(t, dir)
}
func TestPublishedFileUsesOutputMode(t *testing.T) {
	unixPublication(t)
	original := outputFileMode
	defer func() { outputFileMode = original }()
	for _, mode := range []os.FileMode{0644, 0640, 0600} {
		outputFileMode = mode
		path := filepath.Join(t.TempDir(), "result")
		if e := publishOutput(context.Background(), path, func(w io.Writer) error { _, e := w.Write([]byte("x")); return e }); e != nil {
			t.Fatal(e)
		}
		info, e := os.Stat(path)
		if e != nil || info.Mode().Perm() != mode {
			t.Fatalf("mode %v, want %v (%v)", info.Mode().Perm(), mode, e)
		}
	}
}
func TestDefaultOutputModeFollowsUmask(t *testing.T) {
	supportedPublication(t)
	if runtime.GOOS != "linux" {
		t.Skip("umask read independently from /proc on Linux only")
	}
	status, e := os.ReadFile("/proc/self/status")
	if e != nil {
		t.Skip("no /proc/self/status")
	}
	for _, line := range strings.Split(string(status), "\n") {
		if value, ok := strings.CutPrefix(line, "Umask:"); ok {
			mask, e := strconv.ParseUint(strings.TrimSpace(value), 8, 32)
			if e != nil {
				t.Fatal(e)
			}
			if got, want := defaultOutputMode(), os.FileMode(0666&^mask); got != want {
				t.Fatalf("default output mode %v, want %v", got, want)
			}
			return
		}
	}
	t.Skip("kernel does not report Umask")
}
