package kaitai

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// node is one value of the YAML subset that .ksy files use: a mapping with
// keys in file order, a sequence, or a scalar. Quoted scalars are always
// strings; plain scalars are interpreted by the importer.
type node struct {
	kind   byte // 'm' mapping, 's' sequence, 'v' scalar
	keys   []string
	values []*node
	items  []*node
	text   string
	quoted bool
	line   int
}

func (n *node) get(key string) *node {
	if n == nil || n.kind != 'm' {
		return nil
	}
	for i, k := range n.keys {
		if k == key {
			return n.values[i]
		}
	}
	return nil
}

// yamlError is a reading failure at a line of the .ksy file.
type yamlError struct {
	line int
	msg  string
}

func (e *yamlError) Error() string { return fmt.Sprintf("yaml: line %d: %s", e.line, e.msg) }

type line struct {
	indent int
	text   string // without indentation; comments are removed later
	number int
}

type reader struct {
	lines []line
	pos   int
	depth int
}

const maxYAMLDepth = 64

// parseYAML reads the block-structured YAML subset: block mappings and
// sequences, flow sequences and mappings, plain, single- and double-quoted
// scalars, literal and folded block scalars and comments. Anchors, aliases,
// tags, complex keys, directives and multiple documents are refused.
func parseYAML(src []byte) (*node, error) {
	if !utf8.Valid(src) {
		return nil, &yamlError{1, "not UTF-8"}
	}
	s := strings.TrimPrefix(string(src), "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	r := &reader{}
	for i, raw := range strings.Split(s, "\n") {
		indent := 0
		for indent < len(raw) && raw[indent] == ' ' {
			indent++
		}
		if indent < len(raw) && raw[indent] == '\t' {
			if strings.TrimSpace(raw) != "" {
				return nil, &yamlError{i + 1, "tab in indentation"}
			}
		}
		r.lines = append(r.lines, line{indent, raw[indent:], i + 1})
	}
	// One optional document start marker; nothing else of the kind.
	r.skipBlank()
	if r.pos < len(r.lines) && strings.TrimRight(stripComment(r.lines[r.pos].text), " ") == "---" && r.lines[r.pos].indent == 0 {
		r.pos++
	}
	r.skipBlank()
	if r.pos >= len(r.lines) {
		return nil, &yamlError{1, "empty document"}
	}
	root, e := r.block(r.lines[r.pos].indent)
	if e != nil {
		return nil, e
	}
	r.skipBlank()
	if r.pos < len(r.lines) {
		return nil, &yamlError{r.lines[r.pos].number, "unexpected content"}
	}
	return root, nil
}

// skipBlank moves past empty and comment-only lines.
func (r *reader) skipBlank() {
	for r.pos < len(r.lines) && strings.TrimSpace(stripComment(r.lines[r.pos].text)) == "" {
		r.pos++
	}
}

// stripComment removes a comment that starts with # at the line start or
// after a space, outside quotes.
func stripComment(s string) string {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '\'' && c == '\'':
			if i+1 < len(s) && s[i+1] == '\'' {
				i++
			} else {
				quote = 0
			}
		case quote == '"' && c == '\\':
			i++
		case quote == '"' && c == '"':
			quote = 0
		case quote != 0:
		case (c == '\'' || c == '"') && (i == 0 || strings.ContainsRune(" [{,:-", rune(s[i-1]))):
			quote = c
		case c == '#' && (i == 0 || s[i-1] == ' '):
			return strings.TrimRight(s[:i], " ")
		}
	}
	return strings.TrimRight(s, " ")
}

// block reads the mapping, sequence or scalar starting at the current line,
// whose indentation is indent.
func (r *reader) block(indent int) (*node, error) {
	r.depth++
	defer func() { r.depth-- }()
	if r.pos >= len(r.lines) {
		return &node{kind: 'v', line: r.lines[len(r.lines)-1].number}, nil
	}
	if r.depth > maxYAMLDepth {
		return nil, &yamlError{r.lines[r.pos].number, "nesting too deep"}
	}
	r.skipBlank()
	if r.pos >= len(r.lines) {
		return &node{kind: 'v', line: r.lines[len(r.lines)-1].number}, nil
	}
	l := r.lines[r.pos]
	if strings.ContainsRune(l.text, '\t') && strings.TrimLeft(l.text, " \t") != l.text {
		return nil, &yamlError{l.number, "tab in indentation"}
	}
	text := stripComment(l.text)
	if text == "-" || strings.HasPrefix(text, "- ") {
		return r.sequence(indent)
	}
	if _, _, ok := splitKey(text); ok {
		return r.mapping(indent)
	}
	// A lone scalar or flow collection, possibly spanning deeper lines.
	r.pos++
	return r.value(text, l.number, indent-1)
}

func (r *reader) sequence(indent int) (*node, error) {
	n := &node{kind: 's', line: r.lines[r.pos].number}
	for {
		r.skipBlank()
		if r.pos >= len(r.lines) || r.lines[r.pos].indent != indent {
			break
		}
		l := r.lines[r.pos]
		text := stripComment(l.text)
		if text != "-" && !strings.HasPrefix(text, "- ") {
			break
		}
		if text == "-" {
			r.pos++
			r.skipBlank()
			if r.pos >= len(r.lines) || r.lines[r.pos].indent <= indent {
				n.items = append(n.items, &node{kind: 'v', line: l.number})
				continue
			}
			item, e := r.block(r.lines[r.pos].indent)
			if e != nil {
				return nil, e
			}
			n.items = append(n.items, item)
			continue
		}
		// "- rest": rest is read as if it started its own line two columns in.
		rest := l.text[2:]
		extra := 0
		for extra < len(rest) && rest[extra] == ' ' {
			extra++
		}
		r.lines[r.pos] = line{indent + 2 + extra, rest[extra:], l.number}
		item, e := r.block(indent + 2 + extra)
		if e != nil {
			return nil, e
		}
		n.items = append(n.items, item)
	}
	return n, nil
}

func (r *reader) mapping(indent int) (*node, error) {
	n := &node{kind: 'm', line: r.lines[r.pos].number}
	seen := map[string]bool{}
	for {
		r.skipBlank()
		if r.pos >= len(r.lines) || r.lines[r.pos].indent != indent {
			break
		}
		l := r.lines[r.pos]
		text := stripComment(l.text)
		if strings.HasPrefix(text, "- ") || text == "-" {
			break
		}
		key, rest, ok := splitKey(text)
		if !ok {
			return nil, &yamlError{l.number, "expected a key"}
		}
		if seen[key] {
			return nil, &yamlError{l.number, "duplicate key"}
		}
		seen[key] = true
		r.pos++
		value, e := r.value(rest, l.number, indent)
		if e != nil {
			return nil, e
		}
		n.keys = append(n.keys, key)
		n.values = append(n.values, value)
	}
	return n, nil
}

// value reads what follows "key:" on a line at indent: nothing (a nested
// block, or null), a block scalar, a flow collection or a scalar.
func (r *reader) value(rest string, number, indent int) (*node, error) {
	rest = strings.TrimSpace(rest)
	if e := refused(rest, number); e != nil {
		return nil, e
	}
	switch {
	case rest == "":
		r.skipBlank()
		if r.pos < len(r.lines) {
			next := r.lines[r.pos]
			t := stripComment(next.text)
			if next.indent > indent || (next.indent == indent && (t == "-" || strings.HasPrefix(t, "- "))) {
				return r.block(next.indent)
			}
		}
		return &node{kind: 'v', line: number}, nil
	case rest[0] == '|' || rest[0] == '>':
		return r.blockScalar(rest, number, indent)
	case rest[0] == '[' || rest[0] == '{':
		// A flow collection may continue on deeper lines until it closes.
		var b strings.Builder
		var scan flowScan
		b.WriteString(rest)
		scan.feed(rest)
		for !scan.balanced() {
			if r.pos >= len(r.lines) || (r.lines[r.pos].indent <= indent && strings.TrimSpace(r.lines[r.pos].text) != "") {
				return nil, &yamlError{number, "unclosed flow collection"}
			}
			more := stripComment(strings.TrimSpace(r.lines[r.pos].text))
			b.WriteString(" ")
			b.WriteString(more)
			scan.feed(" " + more)
			r.pos++
		}
		text := b.String()
		f := &flow{s: text, line: number}
		v, e := f.value()
		if e != nil {
			return nil, e
		}
		f.space()
		if f.i != len(f.s) {
			return nil, &yamlError{number, "text after flow collection"}
		}
		return v, nil
	default:
		return r.scalarLines(rest, number, indent)
	}
}

// scalarLines reads a scalar that may continue on following lines indented
// more than indent (a multi-line plain or quoted scalar).
func (r *reader) scalarLines(first string, number, indent int) (*node, error) {
	var b strings.Builder
	b.WriteString(first)
	// A plain scalar may run on; a quoted one runs on until it closes.
	quoted := first != "" && (first[0] == '"' || first[0] == '\'')
	var q quoteScan
	if quoted {
		q.q = first[0]
		q.feed(first[1:])
	}
	afterBreak := false
	for r.pos < len(r.lines) {
		l := r.lines[r.pos]
		t := stripComment(l.text)
		if strings.TrimSpace(t) == "" {
			// A blank line inside a multi-line scalar is a line break.
			j := r.pos
			for j < len(r.lines) && strings.TrimSpace(stripComment(r.lines[j].text)) == "" {
				j++
			}
			if j < len(r.lines) && r.lines[j].indent > indent && !looksStructural(r.lines[j], indent) && (!quoted || q.open()) {
				b.WriteString(strings.Repeat("\n", j-r.pos))
				afterBreak = true
				r.pos = j
				continue
			}
			break
		}
		if l.indent <= indent || looksStructural(l, indent) {
			break
		}
		next := strings.TrimSpace(t)
		if !afterBreak {
			b.WriteString(" ")
			if quoted {
				q.feed(" ")
			}
		}
		afterBreak = false
		b.WriteString(next)
		if quoted {
			q.feed(next)
		}
		r.pos++
	}
	return scalar(b.String(), number)
}

// looksStructural reports whether a deeper line starts a key or an item,
// which would end a plain scalar.
func looksStructural(l line, indent int) bool {
	t := stripComment(l.text)
	_, _, ok := splitKey(t)
	return ok || t == "-" || strings.HasPrefix(t, "- ")
}

func (r *reader) blockScalar(header string, number, indent int) (*node, error) {
	style, chomp := header[0], byte(0)
	for _, c := range header[1:] {
		switch {
		case (c == '-' || c == '+') && chomp == 0:
			chomp = byte(c)
		case c >= '1' && c <= '9':
			return nil, &yamlError{number, "explicit block indentation"}
		case c == ' ':
		case c == '#':
		default:
			return nil, &yamlError{number, "bad block scalar header"}
		}
		if c == '#' {
			break
		}
	}
	var lines []string
	blockIndent := -1
	for r.pos < len(r.lines) {
		l := r.lines[r.pos]
		if strings.TrimSpace(l.text) == "" {
			lines = append(lines, "")
			r.pos++
			continue
		}
		if l.indent <= indent {
			break
		}
		if blockIndent < 0 {
			blockIndent = l.indent
		}
		if l.indent < blockIndent {
			break
		}
		lines = append(lines, strings.Repeat(" ", l.indent-blockIndent)+l.text)
		r.pos++
	}
	// Trailing blank lines belong to chomping, not to following content.
	trailing := 0
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
		trailing++
	}
	var text string
	if style == '|' {
		text = strings.Join(lines, "\n")
	} else {
		// Folding: a break between two plain lines becomes a space; blank
		// lines and lines indented more than the block keep their breaks.
		var b strings.Builder
		prev := -1
		for i, s := range lines {
			if s == "" {
				continue
			}
			if prev >= 0 {
				blanks := i - prev - 1
				more := strings.HasPrefix(s, " ") || strings.HasPrefix(lines[prev], " ")
				switch {
				case blanks == 0 && !more:
					b.WriteString(" ")
				case more:
					b.WriteString(strings.Repeat("\n", blanks+1))
				default:
					b.WriteString(strings.Repeat("\n", blanks))
				}
			} else {
				b.WriteString(strings.Repeat("\n", i))
			}
			b.WriteString(s)
			prev = i
		}
		text = b.String()
	}
	switch {
	case chomp == '+':
		text += strings.Repeat("\n", trailing+1)
	case chomp == 0 && len(lines) > 0:
		text += "\n"
	}
	return &node{kind: 'v', text: text, quoted: true, line: number}, nil
}

// splitKey splits "key: rest" (or "key:" at the end of the line). Keys are
// plain words or quoted strings.
func splitKey(text string) (string, string, bool) {
	if text == "" {
		return "", "", false
	}
	if text[0] == '"' || text[0] == '\'' {
		k, n, e := quotedScalar(text, 0)
		for e == nil && n >= 0 && n < len(text) && text[n] == ' ' {
			n++
		}
		if e != nil || n < 0 || n >= len(text) || text[n] != ':' || (n+1 < len(text) && text[n+1] != ' ') {
			return "", "", false
		}
		return k, text[n+1:], true
	}
	if strings.ContainsRune("[{&*!|>?%@`", rune(text[0])) {
		return "", "", false
	}
	for i := 0; i < len(text); i++ {
		if text[i] == ':' && (i+1 == len(text) || text[i+1] == ' ') {
			key := strings.TrimRight(text[:i], " ")
			if key == "" {
				return "", "", false
			}
			return key, text[i+1:], true
		}
	}
	return "", "", false
}

// refused rejects YAML features outside the subset at the start of a value.
func refused(rest string, number int) error {
	if rest == "" {
		return nil
	}
	switch rest[0] {
	case '&':
		return &yamlError{number, "anchors are not supported"}
	case '*':
		return &yamlError{number, "aliases are not supported"}
	case '!':
		return &yamlError{number, "tags are not supported"}
	case '?':
		return &yamlError{number, "complex keys are not supported"}
	case '%', '@', '`':
		return &yamlError{number, "reserved indicator"}
	}
	return nil
}

// flowScan tracks bracket depth and quoting across the lines of a flow
// collection, so each line is scanned once.
type flowScan struct {
	depth int
	quote byte
	esc   bool
}

func (f *flowScan) feed(s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case f.esc:
			f.esc = false
		case f.quote == '\'' && c == '\'':
			f.quote = 0
		case f.quote == '"' && c == '\\':
			f.esc = true
		case f.quote == '"' && c == '"':
			f.quote = 0
		case f.quote != 0:
		case c == '\'' || c == '"':
			f.quote = c
		case c == '[' || c == '{':
			f.depth++
		case c == ']' || c == '}':
			f.depth--
		}
	}
}

func (f *flowScan) balanced() bool { return f.depth <= 0 && f.quote == 0 }

func balanced(s string) bool {
	var f flowScan
	f.feed(s)
	return f.balanced()
}

// quoteScan tracks whether a quoted scalar spread over lines has closed.
type quoteScan struct {
	q                  byte
	closed, esc, quote bool // quote: a ' that may be the first of ''
}

func (q *quoteScan) feed(s string) {
	for i := 0; i < len(s) && !q.closed; i++ {
		c := s[i]
		if q.quote {
			q.quote = false
			if c == '\'' {
				continue
			}
			q.closed = true
			return
		}
		switch {
		case q.q == '"' && q.esc:
			q.esc = false
		case q.q == '"' && c == '\\':
			q.esc = true
		case q.q == '"' && c == '"':
			q.closed = true
		case q.q == '\'' && c == '\'':
			q.quote = true
		}
	}
}

func (q *quoteScan) open() bool { return !q.closed && !q.quote }

// scalar turns the text of a block-context scalar into a node.
func scalar(text string, number int) (*node, error) {
	text = strings.TrimSpace(text)
	if e := refused(text, number); e != nil {
		return nil, e
	}
	if text != "" && (text[0] == '"' || text[0] == '\'') {
		s, n, e := quotedScalar(text, 0)
		if e != nil {
			return nil, &yamlError{number, e.Error()}
		}
		if n != len(text) {
			return nil, &yamlError{number, "text after quoted scalar"}
		}
		return &node{kind: 'v', text: s, quoted: true, line: number}, nil
	}
	if text == "~" || text == "null" {
		text = ""
	}
	return &node{kind: 'v', text: text, line: number}, nil
}

// quotedScalar reads a single- or double-quoted scalar starting at s[i] and
// returns its value and the index just past the closing quote.
func quotedScalar(s string, i int) (string, int, error) {
	q := s[i]
	var b strings.Builder
	for j := i + 1; j < len(s); j++ {
		c := s[j]
		if q == '\'' {
			if c == '\'' {
				if j+1 < len(s) && s[j+1] == '\'' {
					b.WriteByte('\'')
					j++
					continue
				}
				return b.String(), j + 1, nil
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '"':
			return b.String(), j + 1, nil
		case '\\':
			if j+1 >= len(s) {
				return "", -1, fmt.Errorf("unfinished escape")
			}
			j++
			switch e := s[j]; e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '0':
				b.WriteByte(0)
			case '\\', '"', '/', ' ':
				b.WriteByte(e)
			case 'x', 'u', 'U':
				width := map[byte]int{'x': 2, 'u': 4, 'U': 8}[e]
				if j+1+width > len(s) {
					return "", -1, fmt.Errorf("short escape")
				}
				v, err := strconv.ParseUint(s[j+1:j+1+width], 16, 32)
				if err != nil || !utf8.ValidRune(rune(v)) {
					return "", -1, fmt.Errorf("bad escape")
				}
				b.WriteRune(rune(v))
				j += width
			default:
				return "", -1, fmt.Errorf("unsupported escape")
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", -1, fmt.Errorf("unclosed quote")
}

// flow reads a flow collection on one logical line.
type flow struct {
	s     string
	i     int
	line  int
	depth int
}

func (f *flow) space() {
	for f.i < len(f.s) && f.s[f.i] == ' ' {
		f.i++
	}
}

func (f *flow) fail(msg string) error { return &yamlError{f.line, msg} }

func (f *flow) value() (*node, error) {
	f.depth++
	defer func() { f.depth-- }()
	if f.depth > maxYAMLDepth {
		return nil, f.fail("nesting too deep")
	}
	f.space()
	if f.i >= len(f.s) {
		return nil, f.fail("missing flow value")
	}
	switch c := f.s[f.i]; c {
	case '[':
		f.i++
		n := &node{kind: 's', line: f.line}
		f.space()
		if f.i < len(f.s) && f.s[f.i] == ']' {
			f.i++
			return n, nil
		}
		for {
			v, e := f.value()
			if e != nil {
				return nil, e
			}
			n.items = append(n.items, v)
			f.space()
			if f.i < len(f.s) && f.s[f.i] == ',' {
				f.i++
				f.space()
				if f.i < len(f.s) && f.s[f.i] == ']' {
					f.i++
					return n, nil
				}
				continue
			}
			if f.i < len(f.s) && f.s[f.i] == ']' {
				f.i++
				return n, nil
			}
			return nil, f.fail("expected , or ]")
		}
	case '{':
		f.i++
		n := &node{kind: 'm', line: f.line}
		seen := map[string]bool{}
		f.space()
		if f.i < len(f.s) && f.s[f.i] == '}' {
			f.i++
			return n, nil
		}
		for {
			k, e := f.value()
			if e != nil {
				return nil, e
			}
			if k.kind != 'v' || seen[k.text] {
				return nil, f.fail("bad or duplicate flow key")
			}
			seen[k.text] = true
			f.space()
			if f.i >= len(f.s) || f.s[f.i] != ':' {
				return nil, f.fail("expected :")
			}
			f.i++
			v, e := f.value()
			if e != nil {
				return nil, e
			}
			n.keys = append(n.keys, k.text)
			n.values = append(n.values, v)
			f.space()
			if f.i < len(f.s) && f.s[f.i] == ',' {
				f.i++
				continue
			}
			if f.i < len(f.s) && f.s[f.i] == '}' {
				f.i++
				return n, nil
			}
			return nil, f.fail("expected , or }")
		}
	case '"', '\'':
		s, n, e := quotedScalar(f.s, f.i)
		if e != nil {
			return nil, f.fail(e.Error())
		}
		f.i = n
		return &node{kind: 'v', text: s, quoted: true, line: f.line}, nil
	default:
		if e := refused(f.s[f.i:], f.line); e != nil {
			return nil, e
		}
		start := f.i
		for f.i < len(f.s) && !strings.ContainsRune(",]}", rune(f.s[f.i])) && !(f.s[f.i] == ':' && (f.i+1 == len(f.s) || f.s[f.i+1] == ' ')) {
			f.i++
		}
		return &node{kind: 'v', text: strings.TrimSpace(f.s[start:f.i]), line: f.line}, nil
	}
}
