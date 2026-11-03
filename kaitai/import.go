// Package kaitai imports Kaitai Struct .ksy files into EByte schemas, for the
// subset of Kaitai that an EByte schema can express. A file outside that
// subset is refused with every reason found, never imported approximately.
package kaitai

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/softdevstudio/ebyte"
)

// MaxSourceBytes bounds a .ksy file. The largest file in the Kaitai gallery
// is well under 300 KiB.
const MaxSourceBytes = 1 << 20

// Result is a successful import: the schema JSON, already checked with
// ebyte.Compile, and notes on what was left out.
type Result struct {
	Schema []byte
	Notes  []string
}

// Reason is one construct the import could not express, at a path in the
// .ksy file such as /types/record/seq/2/type.
type Reason struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Refusal lists every reason a file was not imported.
type Refusal struct {
	Reasons []Reason
}

func (r *Refusal) Error() string {
	parts := make([]string, len(r.Reasons))
	for i, x := range r.Reasons {
		parts[i] = x.Path + ": " + x.Reason
	}
	return "kaitai: not imported: " + strings.Join(parts, "; ")
}

// Import converts one .ksy file. It returns a *Refusal when the file uses
// anything outside the supported subset, or when the result does not compile.
func Import(src []byte) (*Result, error) {
	if len(src) > MaxSourceBytes {
		return nil, &Refusal{[]Reason{{"/", "file larger than 1 MiB"}}}
	}
	root, e := parseYAML(src)
	if e != nil {
		return nil, &Refusal{[]Reason{{"/", e.Error()}}}
	}
	if root.kind != 'm' {
		return nil, &Refusal{[]Reason{{"/", "top level is not a mapping"}}}
	}
	im := &importer{notes: map[string]bool{}}
	schema := im.run(root)
	if len(im.reasons) > 0 {
		return nil, &Refusal{im.reasons}
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if e := enc.Encode(schema); e != nil {
		return nil, &Refusal{[]Reason{{"/", "cannot write schema"}}}
	}
	if _, e := ebyte.Compile(out.Bytes(), ebyte.DefaultLimits()); e != nil {
		reason := e.Error()
		if d, ok := e.(*ebyte.Diagnostic); ok {
			reason = "EByte refuses the result: " + d.Code + " at " + d.Path
		}
		return nil, &Refusal{[]Reason{{"/", reason}}}
	}
	notes := make([]string, 0, len(im.notes))
	for n := range im.notes {
		notes = append(notes, n)
	}
	sort.Strings(notes)
	return &Result{Schema: out.Bytes(), Notes: notes}, nil
}

// object is a JSON object that keeps its keys in insertion order.
type object struct {
	keys   []string
	values []any
}

func (o *object) set(k string, v any) *object {
	for i, key := range o.keys {
		if key == k {
			o.values[i] = v
			return o
		}
	}
	o.keys = append(o.keys, k)
	o.values = append(o.values, v)
	return o
}

func (o *object) get(k string) any {
	for i, key := range o.keys {
		if key == k {
			return o.values[i]
		}
	}
	return nil
}

func (o *object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, e := json.Marshal(o.values[i])
		if e != nil {
			return nil, e
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

type importer struct {
	reasons  []Reason
	notes    map[string]bool
	endian   string // "little", "big" or "" when meta has none
	encoding string // meta encoding, normalised, or ""
	needsEnd bool   // a field needed the default endian
	count    int    // fields produced so far, user types inlined
}

// maxFields is EByte's limit on schema field declarations; inlining user
// types stops once it is passed, so a small file cannot expand without bound.
const maxFields = 1024

func (im *importer) refuse(path, format string, args ...any) {
	if len(im.reasons) < 100 {
		im.reasons = append(im.reasons, Reason{path, fmt.Sprintf(format, args...)})
	}
}

// scope is a type namespace: a type's own nested types and enums, then those
// of the scope the type is defined in. Names resolve where a type is defined,
// not where it is used, as in Kaitai.
type scope struct {
	types, enums *node
	path         string
	parent       *scope
}

// lookup finds a type and the scope that defines it.
func (s *scope) lookup(name string) (*node, string, *scope) {
	for ; s != nil; s = s.parent {
		if t := s.types.get(name); t != nil {
			return t, s.path + "/" + name, s
		}
	}
	return nil, "", nil
}

var (
	identifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
	integerRe  = regexp.MustCompile(`^(u|s)([1248])(le|be)?$`)
	floatRe    = regexp.MustCompile(`^f([48])(le|be)?$`)
	bitsRe     = regexp.MustCompile(`^b([0-9]+)(le|be)?$`)
	ifRe       = regexp.MustCompile(`^([a-z_][a-z0-9_]*)\s*==\s*(\S+)$`)
)

func (im *importer) run(root *node) *object {
	for _, k := range root.keys {
		switch k {
		case "meta", "seq", "types", "enums", "instances", "doc", "doc-ref", "params":
		default:
			if !strings.HasPrefix(k, "-") {
				im.refuse("/"+k, "unknown top-level key")
			}
		}
	}
	meta := root.get("meta")
	name := ""
	if meta == nil || meta.kind != 'm' {
		im.refuse("/meta", "meta with an id is required")
	} else {
		for i, k := range meta.keys {
			v := meta.values[i]
			switch k {
			case "id":
				name = v.text
				if v.kind != 'v' || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`).MatchString(name) {
					im.refuse("/meta/id", "id is not an EByte name")
				}
			case "endian":
				if v.kind != 'v' || (v.text != "le" && v.text != "be") {
					im.refuse("/meta/endian", "endian chosen at run time (switch-on) is not supported")
				} else if v.text == "le" {
					im.endian = "little"
				} else {
					im.endian = "big"
				}
			case "bit-endian":
				if v.kind != 'v' || v.text != "be" {
					im.refuse("/meta/bit-endian", "little-endian bit order is not supported")
				}
			case "encoding":
				enc, ok := normaliseEncoding(v.text)
				if v.kind != 'v' || !ok {
					im.refuse("/meta/encoding", "only ASCII and UTF-8 strings are supported")
				}
				im.encoding = enc
			case "imports":
				im.refuse("/meta/imports", "imports are not supported")
			case "ks-opaque-types":
				im.refuse("/meta/ks-opaque-types", "opaque types are not supported")
			default:
				im.notes["meta "+k+" dropped"] = true
			}
		}
	}
	if root.get("params") != nil {
		im.refuse("/params", "parameters are not supported")
	}
	if root.get("doc") != nil || root.get("doc-ref") != nil {
		im.notes["doc dropped"] = true
	}
	top := &scope{types: root.get("types"), enums: root.get("enums"), path: "/types"}
	fields := im.fields(root, "", top, 0, map[string]bool{}, true)
	im.instances(root.get("instances"), "/instances")
	if root.get("seq") == nil {
		im.refuse("/seq", "no seq: nothing to read")
	}
	endian := im.endian
	if endian == "" {
		endian = "little"
		if im.needsEnd {
			im.refuse("/meta/endian", "multi-byte fields without le or be need meta endian")
		}
	}
	return (&object{}).set("schema_version", 1).set("name", name).set("endian", endian).set("fields", fields)
}

func normaliseEncoding(s string) (string, bool) {
	switch strings.ToUpper(strings.ReplaceAll(s, "_", "-")) {
	case "ASCII", "US-ASCII":
		return "ascii", true
	case "UTF-8", "UTF8":
		return "utf8", true
	}
	return "", false
}

// instances drops value instances with a note and refuses the rest.
func (im *importer) instances(n *node, path string) {
	if n == nil {
		return
	}
	if n.kind != 'm' {
		im.refuse(path, "instances is not a mapping")
		return
	}
	for i, k := range n.keys {
		v := n.values[i]
		if v.kind == 'm' && v.get("value") != nil {
			im.notes["value instances dropped (computed, not stored)"] = true
			continue
		}
		im.refuse(path+"/"+k, "instances read at a position (pos, io) are not supported")
	}
}

// fields converts a type's seq. typePath is the type's own path ("" for the
// top level); top marks the top-level seq, where repeat eos may end it.
func (im *importer) fields(t *node, typePath string, sc *scope, depth int, stack map[string]bool, top bool) []any {
	if depth > 8 {
		im.refuse(typePath, "types nest deeper than EByte's 8 levels")
		return nil
	}
	if t.kind != 'm' {
		im.refuse(typePath, "type is not a mapping")
		return nil
	}
	if t.get("params") != nil {
		im.refuse(typePath+"/params", "parameters are not supported")
	}
	if typePath != "" {
		for _, k := range t.keys {
			switch k {
			case "seq", "types", "enums", "instances", "doc", "doc-ref":
			case "meta":
				im.refuse(typePath+"/meta", "per-type meta is not supported")
			default:
				if !strings.HasPrefix(k, "-") {
					im.refuse(typePath+"/"+k, "unknown type key")
				}
			}
		}
		im.instances(t.get("instances"), typePath+"/instances")
	}
	seq := t.get("seq")
	if seq == nil {
		return []any{}
	}
	if seq.kind != 's' {
		im.refuse(typePath+"/seq", "seq is not a list")
		return nil
	}
	var out []*object
	for i, a := range seq.items {
		im.count++
		if im.count > maxFields {
			im.refuse(typePath+"/seq", "more than %d fields once user types are inlined", maxFields)
			return nil
		}
		p := fmt.Sprintf("%s/seq/%d", typePath, i)
		last := top && i == len(seq.items)-1
		if f := im.attribute(a, p, out, sc, depth, stack, last); f != nil {
			out = append(out, f)
		}
	}
	drive(out)
	list := make([]any, len(out))
	for i, f := range out {
		list[i] = f
	}
	return list
}

// drive makes an earlier unsigned integer that is used exactly once, as the
// length or count of a later sibling, a driving derive of that sibling, so an
// edited document keeps it right.
func drive(fields []*object) {
	uses := map[string]int{}
	for _, f := range fields {
		for _, key := range []string{"length", "count"} {
			if r, ok := f.get(key).(*object); ok {
				if name, ok := r.get("field").(string); ok {
					uses[name]++
				}
			}
		}
		if w, ok := f.get("when").(*object); ok {
			uses[w.get("field").(string)] += 2
		}
	}
	for i, f := range fields {
		kind, _ := f.get("type").(string)
		if !strings.HasPrefix(kind, "u") || f.get("expect") != nil || f.get("when") != nil || uses[f.get("name").(string)] != 1 {
			continue
		}
		for _, g := range fields[i+1:] {
			if g.get("when") != nil {
				continue
			}
			for _, key := range []string{"length", "count"} {
				r, ok := g.get(key).(*object)
				if !ok || r.get("field") != f.get("name") {
					continue
				}
				form := "size_of"
				if key == "count" {
					form = "count_of"
				}
				f.set("derive", (&object{}).set(form, g.get("name")))
			}
		}
	}
}

// attribute converts one seq entry.
func (im *importer) attribute(a *node, p string, prior []*object, sc *scope, depth int, stack map[string]bool, last bool) *object {
	if a.kind != 'm' {
		im.refuse(p, "seq entry is not a mapping")
		return nil
	}
	start := len(im.reasons)
	for _, k := range a.keys {
		switch k {
		case "id", "type", "size", "contents", "repeat", "repeat-expr", "if", "encoding", "enum", "valid", "doc", "doc-ref":
		default:
			if strings.HasPrefix(k, "-") {
				continue
			}
			switch k {
			case "size-eos":
				im.refuse(p+"/size-eos", "size-eos (read to the end) is not supported")
			case "terminator", "consume", "include", "eos-error":
				im.refuse(p+"/"+k, "terminated strings and bytes are not supported")
			case "pad-right":
				im.refuse(p+"/pad-right", "pad-right is not supported")
			case "process":
				im.refuse(p+"/process", "process (decompression, XOR and the like) is not supported")
			case "repeat-until":
				im.refuse(p+"/repeat-until", "repeat until is not supported")
			default:
				im.refuse(p+"/"+k, "attribute is not supported")
			}
		}
	}
	idNode := a.get("id")
	if idNode == nil || idNode.kind != 'v' || !identifier.MatchString(idNode.text) || len(idNode.text) > 64 {
		im.refuse(p+"/id", "every seq entry needs an id that is an EByte name")
		return nil
	}
	id := idNode.text
	for _, f := range prior {
		if f.get("name") == id {
			im.refuse(p+"/id", "duplicate id")
		}
	}
	if a.get("enum") != nil {
		im.notes["enums dropped (values stay numbers)"] = true
	}
	if a.get("doc") != nil || a.get("doc-ref") != nil {
		im.notes["doc dropped"] = true
	}
	if len(im.reasons) > start {
		return nil
	}
	// The element: what one occurrence of the field reads.
	elem, children := im.element(a, p, id, prior, sc, depth, stack)
	if len(im.reasons) > start {
		return nil
	}
	if v := a.get("valid"); v != nil {
		lit := v
		if v.kind == 'm' {
			if len(v.keys) != 1 || v.keys[0] != "eq" {
				im.refuse(p+"/valid", "only valid eq is supported")
				return nil
			}
			lit = v.values[0]
		}
		n, ok := literal(lit)
		if !ok && lit.kind == 'v' && !lit.quoted {
			n, ok = sc.enumValue(lit.text)
		}
		kind, _ := elem.get("type").(string)
		if !ok || !(strings.HasPrefix(kind, "u") || strings.HasPrefix(kind, "i") || kind == "bits") || a.get("repeat") != nil {
			im.refuse(p+"/valid", "valid is supported for one integer only")
			return nil
		}
		elem.set("expect", n)
	}
	var f *object
	repeat := a.get("repeat")
	if repeat == nil {
		if a.get("repeat-expr") != nil {
			im.refuse(p+"/repeat-expr", "repeat-expr without repeat")
			return nil
		}
		if children != nil {
			// A user type becomes a single-element array.
			f = (&object{}).set("name", id).set("type", "array").set("count", 1).set("fields", children)
			im.notes["user types become arrays of one element"] = true
		} else {
			f = elem
		}
	} else {
		if children == nil {
			// A repeated primitive: one element per value.
			elem.set("name", "value")
			children = []any{elem}
		}
		switch repeat.text {
		case "expr":
			count, ok := im.dimension(a.get("repeat-expr"), p+"/repeat-expr", prior)
			if !ok {
				return nil
			}
			f = (&object{}).set("name", id).set("type", "array").set("count", count).set("fields", children)
		case "eos":
			if !last {
				im.refuse(p+"/repeat", "repeat eos is supported only as the last top-level field")
				return nil
			}
			f = (&object{}).set("name", id).set("type", "repeat").set("until", "end_of_input").set("fields", children)
		default:
			im.refuse(p+"/repeat", "this kind of repeat is not supported")
			return nil
		}
	}
	if c := a.get("if"); c != nil {
		m := ifRe.FindStringSubmatch(strings.TrimSpace(c.text))
		var ref *object
		for _, g := range prior {
			if m != nil && g.get("name") == m[1] {
				ref = g
			}
		}
		n, ok := "", false
		if m != nil {
			n, ok = literal(&node{kind: 'v', text: m[2]})
			if !ok {
				n, ok = sc.enumValue(m[2])
			}
		}
		if c.kind != 'v' || c.quoted || m == nil || !ok || ref == nil || !isInteger(ref) {
			im.refuse(p+"/if", "only if: field == integer, on an earlier integer field, is supported")
			return nil
		}
		if repeat != nil && repeat.text == "eos" {
			im.refuse(p+"/if", "repeat eos with if is not supported")
			return nil
		}
		f.set("when", (&object{}).set("field", m[1]).set("equals", n))
	}
	return f
}

func isInteger(f *object) bool {
	k, _ := f.get("type").(string)
	return (strings.HasPrefix(k, "u") || strings.HasPrefix(k, "i")) && k != "" || k == "bits"
}

// element converts what one occurrence of a seq entry reads: a field object,
// and for a user type also the type's fields as children (a size given with
// a user type must equal the type's fixed size).
func (im *importer) element(a *node, p, id string, prior []*object, sc *scope, depth int, stack map[string]bool) (*object, []any) {
	f := (&object{}).set("name", id)
	typ, size, contents := a.get("type"), a.get("size"), a.get("contents")
	if contents != nil {
		if typ != nil || size != nil {
			im.refuse(p+"/contents", "contents with type or size is not supported")
			return f, nil
		}
		b, ok := contentBytes(contents)
		if !ok {
			im.refuse(p+"/contents", "contents must be bytes or ASCII/UTF-8 text")
			return f, nil
		}
		return f.set("type", "bytes").set("length", len(b)).set("expect", hex.EncodeToString(b)), nil
	}
	if typ == nil {
		if size == nil {
			im.refuse(p, "neither type nor size")
			return f, nil
		}
		length, ok := im.dimension(size, p+"/size", prior)
		if !ok {
			return f, nil
		}
		if a.get("encoding") != nil {
			im.refuse(p+"/encoding", "encoding without type str")
		}
		return f.set("type", "bytes").set("length", length), nil
	}
	if typ.kind != 'v' {
		im.refuse(p+"/type", "switch-on types are not supported")
		return f, nil
	}
	name := typ.text
	if m := integerRe.FindStringSubmatch(name); m != nil {
		if size != nil {
			im.refuse(p+"/size", "size on an integer is not supported")
		}
		w, _ := strconv.Atoi(m[2])
		kind := map[string]string{"u": "u", "s": "i"}[m[1]] + strconv.Itoa(w*8)
		f.set("type", kind)
		im.byteOrder(f, m[3], w)
		return f, nil
	}
	if m := floatRe.FindStringSubmatch(name); m != nil {
		if size != nil {
			im.refuse(p+"/size", "size on a float is not supported")
		}
		w, _ := strconv.Atoi(m[1])
		f.set("type", map[string]string{"4": "f32", "8": "f64"}[m[1]])
		im.byteOrder(f, m[2], w)
		return f, nil
	}
	if m := bitsRe.FindStringSubmatch(name); m != nil {
		w, _ := strconv.Atoi(m[1])
		if m[2] == "le" {
			im.refuse(p+"/type", "little-endian bit fields are not supported")
		}
		if w < 1 || w > 64 || size != nil {
			im.refuse(p+"/type", "bit fields of 1 to 64 bits without size only")
		}
		return f.set("type", "bits").set("bits", w), nil
	}
	switch name {
	case "str":
		if size == nil {
			im.refuse(p, "str needs a size (terminated strings are not supported)")
			return f, nil
		}
		length, ok := im.dimension(size, p+"/size", prior)
		enc := im.encoding
		if e := a.get("encoding"); e != nil {
			var good bool
			enc, good = normaliseEncoding(e.text)
			if !good {
				im.refuse(p+"/encoding", "only ASCII and UTF-8 strings are supported")
				return f, nil
			}
		}
		if enc == "" {
			im.refuse(p+"/encoding", "str without an encoding")
			return f, nil
		}
		if !ok {
			return f, nil
		}
		if enc == "utf8" {
			im.notes["UTF-8 strings must be valid UTF-8 (Kaitai replaces bad bytes; EByte refuses the file)"] = true
		} else {
			im.notes["ASCII strings must be 7-bit (EByte refuses other bytes)"] = true
		}
		return f.set("type", "string").set("length", length).set("encoding", enc), nil
	case "strz":
		im.refuse(p+"/type", "strz (terminated strings) is not supported")
		return f, nil
	}
	if strings.ContainsAny(name, "(:.") {
		im.refuse(p+"/type", "parameterized types, enum types and type paths are not supported")
		return f, nil
	}
	t, tp, defined := sc.lookup(name)
	if t == nil {
		im.refuse(p+"/type", "unknown type")
		return f, nil
	}
	if stack[tp] {
		im.refuse(p+"/type", "recursive types are not supported")
		return f, nil
	}
	stack[tp] = true
	defer delete(stack, tp)
	inner := &scope{types: t.get("types"), enums: t.get("enums"), path: tp + "/types", parent: defined}
	children := im.fields(t, tp, inner, depth+1, stack, false)
	if children == nil {
		children = []any{}
	}
	if size != nil {
		want, ok := literal(size)
		got, static := staticSize(children)
		if !ok || !static || want != strconv.FormatInt(got, 10) {
			im.refuse(p+"/size", "size with a user type is supported only when it equals the type's fixed size")
		}
	}
	return f, children
}

// byteOrder sets a field's endian from a suffix, or notes that it needs the
// schema default.
func (im *importer) byteOrder(f *object, suffix string, width int) {
	if width == 1 {
		return
	}
	switch suffix {
	case "":
		im.needsEnd = true
	case "le":
		if im.endian != "little" {
			f.set("endian", "little")
		}
	case "be":
		if im.endian != "big" {
			f.set("endian", "big")
		}
	}
}

// dimension converts a size or repeat-expr: an integer literal, or the id of
// an earlier integer sibling.
func (im *importer) dimension(n *node, p string, prior []*object) (any, bool) {
	if n == nil || n.kind != 'v' {
		im.refuse(p, "missing or not a scalar")
		return nil, false
	}
	if s, ok := literal(n); ok {
		v, e := strconv.ParseInt(s, 10, 64)
		if e != nil || v < 0 {
			im.refuse(p, "negative or too large")
			return nil, false
		}
		return v, true
	}
	if !n.quoted && identifier.MatchString(n.text) {
		for _, g := range prior {
			if g.get("name") == n.text {
				if !isInteger(g) {
					break
				}
				return (&object{}).set("field", n.text), true
			}
		}
	}
	im.refuse(p, "expression is not supported (only a number or an earlier integer field)")
	return nil, false
}

// enumValue resolves enum::member from the current type outward.
func (sc *scope) enumValue(s string) (string, bool) {
	name, member, found := strings.Cut(s, "::")
	if !found || strings.Contains(member, "::") {
		return "", false
	}
	var e *node
	for x := sc; x != nil && e == nil; x = x.parent {
		e = x.enums.get(name)
	}
	if e == nil || e.kind != 'm' {
		return "", false
	}
	for i, k := range e.keys {
		v := e.values[i]
		id := v.text
		if v.kind == 'm' {
			if x := v.get("id"); x != nil {
				id = x.text
			}
		}
		if id == member {
			return literal(&node{kind: 'v', text: k})
		}
	}
	return "", false
}

// literal reads a Kaitai integer literal (decimal, 0x, 0o or 0b, with
// underscores) as a canonical decimal string.
func literal(n *node) (string, bool) {
	if n == nil || n.kind != 'v' || n.quoted {
		return "", false
	}
	s := strings.ReplaceAll(n.text, "_", "")
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	base := 10
	switch {
	case strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X"):
		base, s = 16, s[2:]
	case strings.HasPrefix(s, "0o"):
		base, s = 8, s[2:]
	case strings.HasPrefix(s, "0b"):
		base, s = 2, s[2:]
	}
	if s == "" {
		return "", false
	}
	v, e := strconv.ParseUint(s, base, 64)
	if e != nil {
		return "", false
	}
	if neg {
		if v > 1<<63 {
			return "", false
		}
		if v == 0 {
			return "0", true
		}
		return "-" + strconv.FormatUint(v, 10), true
	}
	return strconv.FormatUint(v, 10), true
}

// contentBytes reads contents: a string, or a list of byte numbers and strings.
func contentBytes(n *node) ([]byte, bool) {
	if n.kind == 'v' {
		// Kaitai refuses a bare number here; text must be a word or quoted.
		if _, number := literal(n); n.text == "" || number {
			return nil, false
		}
		return []byte(n.text), true
	}
	if n.kind != 's' || len(n.items) == 0 {
		return nil, false
	}
	var b []byte
	quotedNumber := regexp.MustCompile(`^-?[0-9]+$`)
	quotedHex := regexp.MustCompile(`^0x[0-9a-fA-F]+$`)
	notText := regexp.MustCompile(`^(true|false|True|False|TRUE|FALSE|null|Null|NULL|~|[-+]?(\.[0-9]+|[0-9][0-9_]*\.[0-9_]*)([eE][-+]?[0-9]+)?|[-+]?\.(inf|Inf|INF|nan|NaN|NAN))$`)
	for _, it := range n.items {
		if it.kind != 'v' {
			return nil, false
		}
		// As Kaitai's compiler: a quoted item that is a decimal or 0x number
		// is one byte, other quoted items are UTF-8 text; an unquoted item
		// YAML reads as an integer is one byte, other words are text.
		number, ok := "", false
		switch {
		case it.quoted && quotedNumber.MatchString(it.text):
			number, ok = literal(&node{kind: 'v', text: it.text})
		case it.quoted && quotedHex.MatchString(it.text):
			number, ok = literal(&node{kind: 'v', text: it.text})
		case it.quoted:
		case strings.HasPrefix(it.text, "0X"):
		case notText.MatchString(it.text):
			return nil, false
		default:
			number, ok = literal(it)
		}
		if ok {
			v, e := strconv.Atoi(number)
			if e != nil || v < -128 || v > 255 {
				return nil, false
			}
			b = append(b, byte(v)) // -1 is 0xff, as in Kaitai
			continue
		}
		b = append(b, it.text...)
	}
	return b, len(b) > 0
}

// staticSize is the fixed byte size of a field list, if it has one.
func staticSize(fields []any) (int64, bool) {
	var total, bits int64
	for _, x := range fields {
		f := x.(*object)
		if f.get("when") != nil {
			return 0, false
		}
		kind, _ := f.get("type").(string)
		if kind == "bits" {
			bits += int64(f.get("bits").(int))
			continue
		}
		if bits%8 != 0 {
			return 0, false
		}
		total += bits / 8
		bits = 0
		switch {
		case kind == "bytes" || kind == "string":
			n, ok := f.get("length").(int64)
			if !ok {
				if m, ok2 := f.get("length").(int); ok2 {
					n, ok = int64(m), true
				}
			}
			if !ok {
				return 0, false
			}
			total += n
		case kind == "array":
			var count int64
			switch c := f.get("count").(type) {
			case int:
				count = int64(c)
			case int64:
				count = c
			default:
				return 0, false
			}
			n, ok := staticSize(f.get("fields").([]any))
			if !ok {
				return 0, false
			}
			total += count * n
		case kind == "repeat":
			return 0, false
		default:
			w := map[string]int64{"u8": 1, "i8": 1, "u16": 2, "i16": 2, "u32": 4, "i32": 4, "f32": 4, "u64": 8, "i64": 8, "f64": 8}[kind]
			if w == 0 {
				return 0, false
			}
			total += w
		}
	}
	if bits%8 != 0 {
		return 0, false
	}
	return total + bits/8, true
}
