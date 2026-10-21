package ebyte

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Limits are hard ceilings. Start with DefaultLimits and lower individual values.
// Zero is an actual ceiling, not a request for a default.
type Limits struct {
	MaxSchemaBytes, MaxSchemaFields, MaxDepth                       int
	MaxInputBytes, MaxOutputBytes, MaxFieldBytes, MaxArrayElements  int
	MaxValueNodes, MaxTraceEntries, MaxDocumentBytes, MaxTraceBytes int
}

func DefaultLimits() Limits {
	return Limits{1 << 20, 1024, 8, 16 << 20, 16 << 20, 16 << 20, 65536, 100000, 100000, 64 << 20, 64 << 20}
}

// Diagnostic describes a failure without including input literals.
type Diagnostic struct {
	DiagnosticVersion int    `json:"diagnostic_version"`
	Stage             string `json:"stage"`
	Code              string `json:"code"`
	Path              string `json:"path"`
	Offset            *int64 `json:"offset"`
	FieldStart        *int64 `json:"field_start"`
	Expected          string `json:"expected"`
	Observed          string `json:"observed"`
}

func (d *Diagnostic) Error() string { return d.Stage + ": " + d.Code + " at " + d.Path }
func failure(code, path string) error {
	return &Diagnostic{DiagnosticVersion: 1, Stage: "schema", Code: code, Path: path, Expected: "valid bounded schema", Observed: "schema rejected"}
}

// Plan owns an immutable compiled schema. No input buffers are retained.
type Plan struct {
	digest string
	name   string
	limits Limits
	fields []field
}

func (p *Plan) SchemaSHA256() string { return p.digest }
func (p *Plan) Name() string         { return p.name }
func (p *Plan) Limits() Limits       { return p.limits }

type dimension struct {
	literal uint64
	ref     int
	dynamic bool
}
type condition struct {
	ref    int
	equals string
}
type field struct {
	name, kind, endian, encoding, expect string
	hasExpect                            bool
	length                               dimension
	children                             []field
	when                                 *condition
	// derive is the index of the sibling a driving derived integer is
	// calculated from (count_of an array, size_of the field it sizes), or -1.
	derive int
	// measure is set for a measured derived integer (size, offset or repeat
	// count of siblings); it drives nothing and is checked on decode.
	measure *measure
	// until is set for repeat fields: "end_of_input" or "tag_mismatch".
	until string
	tag   []byte
}

// measure describes a measured derived integer: the byte size of siblings
// from..to inclusive, the offset of sibling from, or the element count of
// repeat sibling from, plus add.
type measure struct {
	kind     string // "size", "offset" or "count"
	from, to int
	add      int64
}

// derived reports whether a field is calculated by the encoder and therefore
// absent from editable values.
func (f field) derived() bool { return f.derive >= 0 || f.measure != nil }

// Repeat stop rules. A tag_mismatch repeat's first child is a fixed-length
// bytes field with expect; elements continue while the next bytes equal it.
const (
	untilEnd = "end_of_input"
	untilTag = "tag_mismatch"
	maxTag   = 16
)

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
var decimal = regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`)
var natural = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// Compile validates exact schema bytes and resolves all sibling references.
// It does not read binary input. Limits may only lower DefaultLimits ceilings.
func Compile(raw []byte, limits Limits) (*Plan, error) {
	caps := DefaultLimits()
	a := []int{limits.MaxSchemaBytes, limits.MaxSchemaFields, limits.MaxDepth, limits.MaxInputBytes, limits.MaxOutputBytes, limits.MaxFieldBytes, limits.MaxArrayElements, limits.MaxValueNodes, limits.MaxTraceEntries, limits.MaxDocumentBytes, limits.MaxTraceBytes}
	b := []int{caps.MaxSchemaBytes, caps.MaxSchemaFields, caps.MaxDepth, caps.MaxInputBytes, caps.MaxOutputBytes, caps.MaxFieldBytes, caps.MaxArrayElements, caps.MaxValueNodes, caps.MaxTraceEntries, caps.MaxDocumentBytes, caps.MaxTraceBytes}
	for i, v := range a {
		if v < 0 || v > b[i] {
			return nil, failure("limit_exceeded", "")
		}
	}
	if len(raw) > limits.MaxSchemaBytes {
		return nil, failure("limit_exceeded", "")
	}
	if !utf8.Valid(raw) || !validEscapes(raw) {
		return nil, failure("invalid_schema", "")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	value, err := parseJSON(dec, "", 0)
	if err != nil {
		return nil, err
	}
	if _, err = dec.Token(); err != io.EOF {
		return nil, failure("invalid_schema", "")
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, failure("invalid_schema", "")
	}
	if err = members(root, "", []string{"schema_version", "name", "endian", "fields"}, nil); err != nil {
		return nil, err
	}
	if n, ok := root["schema_version"].(json.Number); !ok || n.String() != "1" {
		return nil, failure("invalid_schema", "/schema_version")
	}
	name, ok := root["name"].(string)
	if !ok || !identifier.MatchString(name) {
		return nil, failure("invalid_schema", "/name")
	}
	endian, ok := root["endian"].(string)
	if !ok || (endian != "little" && endian != "big") {
		return nil, failure("invalid_schema", "/endian")
	}
	count := 0
	fields, err := compileFields(root["fields"], "/fields", 0, endian, limits, &count)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return &Plan{hex.EncodeToString(sum[:]), name, limits, fields}, nil
}

// encoding/json replaces unpaired UTF-16 escapes. Reject them before decoding.
func validEscapes(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		i++
		for ; i < len(raw) && raw[i] != '"'; i++ {
			if raw[i] != '\\' {
				continue
			}
			i++
			if i >= len(raw) {
				return false
			}
			if raw[i] != 'u' {
				continue
			}
			if i+4 >= len(raw) {
				return false
			}
			n, e := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
			if e != nil {
				return false
			}
			i += 4
			if n >= 0xdc00 && n <= 0xdfff {
				return false
			}
			if n >= 0xd800 && n <= 0xdbff {
				if i+6 >= len(raw) || string(raw[i+1:i+3]) != "\\u" {
					return false
				}
				m, e := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
				if e != nil || m < 0xdc00 || m > 0xdfff {
					return false
				}
				i += 6
			}
		}
	}
	return true
}
func safePath(path, key string) string {
	// Only grammar member names enter diagnostics; arbitrary unknown keys stay private.
	switch key {
	case "schema_version", "name", "endian", "fields", "type", "length", "count", "encoding", "expect", "when", "derive", "field", "equals", "count_of", "size_of", "offset_of", "from", "to", "add", "until":
		return path + "/" + key
	}
	return path
}
func parseJSON(d *json.Decoder, path string, depth int) (any, error) {
	if depth > 40 {
		return nil, failure("limit_exceeded", path)
	}
	t, e := d.Token()
	if e != nil || t == nil {
		return nil, failure("invalid_schema", path)
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
	switch delim {
	case '{':
		m := map[string]any{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return nil, failure("invalid_schema", path)
			}
			key, ok := k.(string)
			if !ok {
				return nil, failure("invalid_schema", path)
			}
			p := safePath(path, key)
			if _, exists := m[key]; exists {
				return nil, failure("invalid_schema", p)
			}
			v, e := parseJSON(d, p, depth+1)
			if e != nil {
				return nil, e
			}
			m[key] = v
		}
		end, e := d.Token()
		if e != nil || end != json.Delim('}') {
			return nil, failure("invalid_schema", path)
		}
		return m, nil
	case '[':
		a := []any{}
		for d.More() {
			v, e := parseJSON(d, fmt.Sprintf("%s/%d", path, len(a)), depth+1)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		end, e := d.Token()
		if e != nil || end != json.Delim(']') {
			return nil, failure("invalid_schema", path)
		}
		return a, nil
	}
	return nil, failure("invalid_schema", path)
}
func members(m map[string]any, path string, required, optional []string) error {
	allowed := map[string]bool{}
	for _, k := range required {
		allowed[k] = true
		if _, ok := m[k]; !ok {
			return failure("invalid_schema", path+"/"+k)
		}
	}
	for _, k := range optional {
		allowed[k] = true
	}
	for k := range m {
		if !allowed[k] {
			return failure("invalid_schema", path)
		}
	}
	return nil
}
func integer(kind string) (int, bool, bool) {
	if len(kind) < 2 {
		return 0, false, false
	}
	if kind[0] != 'u' && kind[0] != 'i' {
		return 0, false, false
	}
	n, e := strconv.Atoi(kind[1:])
	return n, kind[0] == 'u', e == nil && (n == 8 || n == 16 || n == 32 || n == 64) && kind == string(kind[0])+strconv.Itoa(n)
}
func fits(s string, kind string) bool {
	n, u, ok := integer(kind)
	if !ok || !decimal.MatchString(s) {
		return false
	}
	if u {
		_, e := strconv.ParseUint(s, 10, n)
		return e == nil
	}
	_, e := strconv.ParseInt(s, 10, n)
	return e == nil
}
func ref(v any, prior []field, path string) (int, error) {
	s, ok := v.(string)
	if ok {
		for i, f := range prior {
			_, _, integer := integer(f.kind)
			if f.name == s && integer && f.when == nil {
				return i, nil
			}
		}
	}
	return 0, failure("invalid_schema", path)
}
func dim(v any, prior []field, path string, cap int) (dimension, error) {
	if n, ok := v.(json.Number); ok {
		if !natural.MatchString(n.String()) {
			return dimension{}, failure("invalid_schema", path)
		}
		u, e := strconv.ParseUint(n.String(), 10, 64)
		if e != nil || u > uint64(cap) {
			return dimension{}, failure("limit_exceeded", path)
		}
		return dimension{literal: u}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return dimension{}, failure("invalid_schema", path)
	}
	if e := members(m, path, []string{"field"}, nil); e != nil {
		return dimension{}, e
	}
	i, e := ref(m["field"], prior, path+"/field")
	return dimension{ref: i, dynamic: true}, e
}
func compileFields(v any, path string, depth int, endian string, limits Limits, total *int) ([]field, error) {
	a, ok := v.([]any)
	if !ok {
		return nil, failure("invalid_schema", path)
	}
	if len(a) > limits.MaxSchemaFields-*total {
		return nil, failure("limit_exceeded", path)
	}
	out := make([]field, 0, len(a))
	names := map[string]bool{}
	for i, item := range a {
		p := fmt.Sprintf("%s/%d", path, i)
		m, ok := item.(map[string]any)
		if !ok {
			return nil, failure("invalid_schema", p)
		}
		name, ok := m["name"].(string)
		if !ok || !identifier.MatchString(name) || names[name] {
			return nil, failure("invalid_schema", p+"/name")
		}
		names[name] = true
		kind, ok := m["type"].(string)
		if !ok {
			return nil, failure("invalid_schema", p+"/type")
		}
		_, unsigned, isInt := integer(kind)
		required := []string{"name", "type"}
		optional := []string{"when"}
		switch {
		case isInt:
			optional = append(optional, "endian", "expect", "derive")
		case kind == "bytes":
			required = append(required, "length")
			optional = append(optional, "expect")
		case kind == "string":
			required = append(required, "length", "encoding")
		case kind == "array":
			required = append(required, "count", "fields")
		case kind == "repeat":
			required = append(required, "until", "fields")
		default:
			return nil, failure("invalid_schema", p+"/type")
		}
		if e := members(m, p, required, optional); e != nil {
			return nil, e
		}
		*total++
		if *total > limits.MaxSchemaFields {
			return nil, failure("limit_exceeded", p)
		}
		f := field{name: name, kind: kind, endian: endian, derive: -1}
		if e, exists := m["endian"]; exists {
			s, ok := e.(string)
			if !ok || (s != "little" && s != "big") {
				return nil, failure("invalid_schema", p+"/endian")
			}
			f.endian = s
		}
		if w, exists := m["when"]; exists {
			obj, ok := w.(map[string]any)
			if !ok {
				return nil, failure("invalid_schema", p+"/when")
			}
			if e := members(obj, p+"/when", []string{"field", "equals"}, nil); e != nil {
				return nil, e
			}
			idx, e := ref(obj["field"], out, p+"/when/field")
			if e != nil {
				return nil, e
			}
			s, ok := obj["equals"].(string)
			if !ok || !fits(s, out[idx].kind) {
				return nil, failure("invalid_schema", p+"/when/equals")
			}
			f.when = &condition{idx, s}
		}
		if kind == "bytes" || kind == "string" || kind == "array" {
			key := "length"
			cap := limits.MaxFieldBytes
			if kind == "array" {
				key = "count"
				cap = limits.MaxArrayElements
			}
			d, e := dim(m[key], out, p+"/"+key, cap)
			if e != nil {
				return nil, e
			}
			f.length = d
		}
		if kind == "string" {
			s, ok := m["encoding"].(string)
			if !ok || (s != "ascii" && s != "utf8") {
				return nil, failure("invalid_schema", p+"/encoding")
			}
			f.encoding = s
		}
		if x, exists := m["expect"]; exists {
			s, ok := x.(string)
			valid := ok
			if isInt {
				valid = valid && fits(s, kind)
			} else {
				valid = valid && !f.length.dynamic && uint64(len(s)) == 2*f.length.literal && strings.ToLower(s) == s
				if valid {
					_, e := hex.DecodeString(s)
					valid = e == nil
				}
			}
			if !valid {
				return nil, failure("invalid_schema", p+"/expect")
			}
			f.expect = s
			f.hasExpect = true
		}
		if kind == "array" || kind == "repeat" {
			if depth >= limits.MaxDepth {
				return nil, failure("limit_exceeded", p+"/fields")
			}
			children, e := compileFields(m["fields"], p+"/fields", depth+1, endian, limits, total)
			if e != nil {
				return nil, e
			}
			f.children = children
		}
		if kind == "repeat" {
			until, ok := m["until"].(string)
			switch {
			case ok && until == untilEnd:
				// End of input only means something for the last top-level field.
				if depth != 0 || i != len(a)-1 || f.when != nil {
					return nil, failure("invalid_schema", p+"/until")
				}
			case ok && until == untilTag:
				if len(f.children) == 0 {
					return nil, failure("invalid_schema", p+"/fields")
				}
				tag := f.children[0]
				if tag.kind != "bytes" || tag.when != nil || !tag.hasExpect || tag.length.dynamic || tag.length.literal == 0 || tag.length.literal > maxTag {
					return nil, failure("invalid_schema", p+"/fields/0")
				}
			default:
				return nil, failure("invalid_schema", p+"/until")
			}
			f.until = until
			if until == untilTag {
				f.tag, _ = hex.DecodeString(f.children[0].expect)
			}
		}
		if _, exists := m["derive"]; exists {
			if !unsigned || f.when != nil || f.hasExpect {
				return nil, failure("invalid_schema", p+"/derive")
			}
			obj, ok := m["derive"].(map[string]any)
			if !ok {
				return nil, failure("invalid_schema", p+"/derive")
			}
			forms := 0
			for _, k := range []string{"count_of", "size_of", "offset_of"} {
				if _, ok := obj[k]; ok {
					forms++
				}
			}
			if forms != 1 {
				return nil, failure("invalid_schema", p+"/derive")
			}
			if e := members(obj, p+"/derive", nil, []string{"count_of", "size_of", "offset_of", "add"}); e != nil {
				return nil, e
			}
			// Resolved after all siblings are known; -2 marks it pending.
			f.derive = -2
		}
		out = append(out, f)
	}
	for i := range out {
		if out[i].derive == -2 {
			if e := resolveDerive(out, i, a[i].(map[string]any)["derive"].(map[string]any), fmt.Sprintf("%s/%d/derive", path, i)); e != nil {
				return nil, e
			}
		}
	}
	return out, nil
}

// resolveDerive decides whether a derived integer drives one later sibling
// (count_of an array, size_of a field whose length it is) or measures
// siblings, and checks how other fields use it.
func resolveDerive(out []field, i int, obj map[string]any, path string) error {
	sibling := func(v any, key string) (int, error) {
		name, ok := v.(string)
		if ok {
			for j, f := range out {
				if f.name == name {
					return j, nil
				}
			}
		}
		return 0, failure("invalid_schema", path+"/"+key)
	}
	lengthUses, whenUses := 0, 0
	for _, f := range out {
		if f.length.dynamic && (f.kind == "array" || f.kind == "bytes" || f.kind == "string") && f.length.ref == i {
			lengthUses++
		}
		if f.when != nil && f.when.ref == i {
			whenUses++
		}
	}
	drives := func(j int, kinds ...string) bool {
		f := out[j]
		for _, k := range kinds {
			if f.kind == k && j > i && f.when == nil && f.length.dynamic && f.length.ref == i {
				return true
			}
		}
		return false
	}
	var m measure
	key := ""
	switch {
	case obj["count_of"] != nil:
		key = "count_of"
		j, e := sibling(obj[key], key)
		if e != nil {
			return e
		}
		if out[j].kind == "repeat" {
			m = measure{kind: "count", from: j, to: j}
			break
		}
		if !drives(j, "array") || lengthUses != 1 {
			return failure("invalid_schema", path+"/"+key)
		}
		if _, ok := obj["add"]; ok {
			return failure("invalid_schema", path+"/add")
		}
		out[i].derive = j
		return nil
	case obj["offset_of"] != nil:
		key = "offset_of"
		j, e := sibling(obj[key], key)
		if e != nil {
			return e
		}
		m = measure{kind: "offset", from: j, to: j}
	default:
		key = "size_of"
		if r, ok := obj[key].(map[string]any); ok {
			if e := members(r, path+"/"+key, []string{"from", "to"}, nil); e != nil {
				return e
			}
			from, e := sibling(r["from"], key+"/from")
			if e != nil {
				return e
			}
			to, e := sibling(r["to"], key+"/to")
			if e != nil {
				return e
			}
			if from > to {
				return failure("invalid_schema", path+"/"+key)
			}
			m = measure{kind: "size", from: from, to: to}
			break
		}
		j, e := sibling(obj[key], key)
		if e != nil {
			return e
		}
		if out[j].length.dynamic && out[j].length.ref == i {
			if !drives(j, "bytes", "string") || lengthUses != 1 {
				return failure("invalid_schema", path+"/"+key)
			}
			if _, ok := obj["add"]; ok {
				return failure("invalid_schema", path+"/add")
			}
			out[i].derive = j
			return nil
		}
		m = measure{kind: "size", from: j, to: j}
	}
	// A measured value is only known once its object is complete, so nothing
	// may read it to decide how to read input.
	if lengthUses != 0 || whenUses != 0 {
		return failure("invalid_schema", path+"/"+key)
	}
	if v, ok := obj["add"]; ok {
		s, ok := v.(string)
		n, e := strconv.ParseInt(s, 10, 64)
		if !ok || e != nil || !decimal.MatchString(s) {
			return failure("invalid_schema", path+"/add")
		}
		m.add = n
	}
	out[i].derive = -1
	out[i].measure = &m
	return nil
}
