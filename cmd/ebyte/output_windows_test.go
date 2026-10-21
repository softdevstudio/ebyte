//go:build windows

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/softdevstudio/ebyte"
)

// Windows names that are not plain new files must fail without touching
// anything: an alternate data stream of an existing file, a device name and
// a directory junction (which, unlike a symlink, needs no privilege).
func TestWindowsPublicationRefusesSpecialNames(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.txt")
	if e := os.WriteFile(existing, []byte("untouched"), 0o600); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(dir, "target")
	if e := os.Mkdir(target, 0o700); e != nil {
		t.Fatal(e)
	}
	junction := filepath.Join(dir, "junction")
	if out, e := exec.Command("cmd", "/c", "mklink", "/J", junction, target).CombinedOutput(); e != nil {
		t.Fatalf("mklink: %v %s", e, out)
	}
	for _, destination := range []string{existing + ":x", filepath.Join(dir, "NUL"), filepath.Join(dir, "CON"), junction} {
		called := false
		err := publishOutput(context.Background(), destination, func(w io.Writer) error { called = true; _, e := w.Write([]byte("new")); return e })
		d, ok := err.(*ebyte.Diagnostic)
		if !ok || (d.Code != "write_failed" && d.Code != "destination_exists") {
			t.Fatalf("%q: %#v", destination, err)
		}
		_ = called
		if b, _ := os.ReadFile(existing); string(b) != "untouched" {
			t.Fatalf("%q changed the existing file", destination)
		}
		entries, _ := os.ReadDir(target)
		if len(entries) != 0 {
			t.Fatalf("%q wrote through the junction", destination)
		}
		assertNoTemporary(t, dir)
	}
	if validDestination(existing + ":x") {
		t.Fatal("stream name accepted as a destination")
	}
}
