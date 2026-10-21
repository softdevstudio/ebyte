package ebyte

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// describe flattens a change list into lines that can be written out by hand.
func describe(r *DiffResult) []string {
	side := func(s *Span) string {
		if s == nil {
			return "-"
		}
		v := ""
		if s.Value != nil {
			v = "=" + *s.Value
		}
		return fmt.Sprintf("%d+%d%s", s.Offset, s.Length, v)
	}
	out := []string{}
	for _, c := range r.Changes {
		line := fmt.Sprintf("%s %s %s %s %s", c.Change, c.Path, c.Kind, side(c.Before), side(c.After))
		if c.FirstDifference != nil {
			line += fmt.Sprintf(" first=%d", *c.FirstDifference)
		}
		if c.Derived {
			line += " derived"
		}
		out = append(out, line)
	}
	return out
}

func TestDiffGolden(t *testing.T) {
	p := repeatPlan(t, boxSchema, DefaultLimits())
	before := unhex(t, boxHex)
	// The edited box from TestDeriveGolden: name "abcd" and a third item.
	after := unhex(t, "4258 1c00 04 61626364 1000 0a00 03 1c00 490101 49020203 4901ff 454e")
	r, e := p.Diff(context.Background(), bytes.NewReader(before), bytes.NewReader(after))
	if e != nil {
		t.Fatal(e)
	}
	// Written from the two hand layouts: every derived header value moves,
	// the name differs after its 2-byte common prefix, a third item appears,
	// and the unchanged tail is not reported although it moved.
	want := []string{
		"changed /total u16 2+2=23 2+2=28 derived",
		"changed /name_len u8 4+1=2 4+1=4 derived",
		"changed /name string 5+2 5+4 first=2",
		"changed /items_off u16 7+2=14 9+2=16 derived",
		"changed /items_len u16 9+2=7 11+2=10 derived",
		"changed /count u8 11+1=2 13+1=3 derived",
		"changed /body_size u16 12+2=23 14+2=28 derived",
		"added /items/2 struct - 23+3",
	}
	if got := describe(r); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if r.State != "different" || r.SizeBefore != 23 || r.SizeAfter != 28 || r.DiffVersion != 1 || r.SchemaSHA256 != p.SchemaSHA256() {
		t.Fatalf("envelope %+v", r)
	}
	// The same comparison the other way round removes the item.
	r, _ = p.Diff(context.Background(), bytes.NewReader(after), bytes.NewReader(before))
	if got := describe(r); got[len(got)-1] != "removed /items/2 struct 23+3 -" || len(got) != len(want) {
		t.Fatalf("reversed %q", got)
	}
	// An input compared with itself has no changes.
	r, _ = p.Diff(context.Background(), bytes.NewReader(before), bytes.NewReader(before))
	if r.State != "same" || len(r.Changes) != 0 || r.Changes == nil {
		t.Fatalf("self %+v", r)
	}
}

func TestDiffConditionsAndBytes(t *testing.T) {
	p := repeatPlan(t, `{"schema_version":1,"name":"c","endian":"little","fields":[
{"name":"flag","type":"u8"},
{"name":"opt","type":"bytes","length":1,"when":{"field":"flag","equals":"1"}},
{"name":"data","type":"bytes","length":4}]}`, DefaultLimits())
	r, e := p.Diff(context.Background(), bytes.NewReader(unhex(t, "00 01020304")), bytes.NewReader(unhex(t, "01 aa 01020904")))
	if e != nil {
		t.Fatal(e)
	}
	want := "changed /flag u8 0+1=0 0+1=1|added /opt bytes - 1+1|changed /data bytes 1+4 2+4 first=2"
	if got := strings.Join(describe(r), "|"); got != want {
		t.Fatalf("got %s", got)
	}
}

func TestDiffFailures(t *testing.T) {
	p := repeatPlan(t, boxSchema, DefaultLimits())
	good := unhex(t, boxHex)
	_, e := p.Diff(context.Background(), bytes.NewReader(good), bytes.NewReader(good[:10]))
	var failed *DiffError
	if !errors.As(e, &failed) || failed.Input != "after" || failed.Diagnostic.Code != "truncated_input" {
		t.Fatalf("truncated after: %v", e)
	}
	var d *Diagnostic
	if !errors.As(e, &d) || d.Stage != "decode" {
		t.Fatalf("diagnostic not reachable through errors.As: %v", e)
	}
	// Both broken: before is reported, because it is decoded first.
	_, e = p.Diff(context.Background(), bytes.NewReader(good[:3]), bytes.NewReader(good[:10]))
	if !errors.As(e, &failed) || failed.Input != "before" {
		t.Fatalf("both broken: %v", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = p.Diff(ctx, bytes.NewReader(good), bytes.NewReader(good))
	if !errors.As(e, &failed) || failed.Input != "before" || failed.Diagnostic.Code != "cancelled" {
		t.Fatalf("cancelled: %v", e)
	}
}

// FuzzDiff checks the contract's central property on arbitrary pairs: two
// accepted inputs have no changes exactly when their bytes are equal, and
// every reported span lies inside its own input.
func FuzzDiff(f *testing.F) {
	f.Add(unhexF(boxHex), unhexF("4258 1c00 04 61626364 1000 0a00 03 1c00 490101 49020203 4901ff 454e"))
	f.Add(unhexF(boxHex), unhexF(boxHex))
	p, e := Compile([]byte(boxSchema), DefaultLimits())
	if e != nil {
		f.Fatal(e)
	}
	f.Fuzz(func(t *testing.T, a, b []byte) {
		if len(a) > 2048 || len(b) > 2048 {
			return
		}
		r, e := p.Diff(context.Background(), bytes.NewReader(a), bytes.NewReader(b))
		if e != nil {
			var failed *DiffError
			if !errors.As(e, &failed) {
				t.Fatalf("not a DiffError: %v", e)
			}
			return
		}
		if (r.State == "same") != bytes.Equal(a, b) || (r.State == "same") != (len(r.Changes) == 0) {
			t.Fatalf("state %s for equal=%v with %d changes", r.State, bytes.Equal(a, b), len(r.Changes))
		}
		for _, c := range r.Changes {
			for _, s := range []struct {
				span *Span
				size int
			}{{c.Before, len(a)}, {c.After, len(b)}} {
				if s.span != nil && (s.span.Offset < 0 || s.span.Offset+s.span.Length > int64(s.size)) {
					t.Fatalf("%s span outside input", c.Path)
				}
			}
		}
	})
}

func unhexF(s string) []byte {
	b, _ := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	return b
}
