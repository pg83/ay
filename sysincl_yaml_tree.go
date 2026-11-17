package main

import (
	"regexp/syntax"
	"slices"
	"strconv"
	"strings"
)

var (
	yamlEscapes    = map[byte]string{'0': "\x00", 'a': "\a", 'b': "\b", 't': "\t", '\t': "\t", 'n': "\n", 'v': "\v", 'f': "\f", 'r': "\r", 'e': "\x1b", ' ': " ", '"': "\"", '/': "/", '\\': "\\", 'N': "\u0085", '_': "\u00a0", 'L': "\u2028", 'P': "\u2029"}
	yamlHexEscapes = map[byte]int{'x': 2, 'u': 4, 'U': 8}
)

const (
	yamlNull   = 'n'
	yamlScalar = 's'
	yamlSeq    = 'q'
	yamlMap    = 'm'
)

type yamlNode struct {
	kind      byte
	value     string
	line, col int
	items     []*yamlNode
	keys      []*yamlNode
}

func (n *yamlNode) mark() string {
	return "line: " + strconv.Itoa(n.line) + " column: " + strconv.Itoa(n.col)
}

func (n *yamlNode) get(key string) *yamlNode {
	if n.kind != yamlMap {
		return nil
	}

	for i, k := range n.keys {
		if k.kind == yamlScalar && k.value == key {
			return n.items[i]
		}
	}

	return nil
}

type yamlLine struct {
	no, indent, start int
	text              string
}

type yamlTree struct {
	src     []byte
	lines   []yamlLine
	i       int
	failed  bool
	errLine int
	errCol  int
}

func (t *yamlTree) fail(line, col int) {
	if !t.failed {
		t.failed, t.errLine, t.errCol = true, line, col
	}
}

func (t *yamlTree) failAtEnd() {
	line, col := 0, 0

	for _, c := range t.src {
		if c == '\n' {
			line, col = line+1, 0
		} else {
			col++
		}
	}

	t.fail(line, col)
}

func stripYAMLComment(text string) string {
	quote := byte(0)

	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#' && (i == 0 || text[i-1] == ' ' || text[i-1] == '\t'):
			return strings.TrimRight(text[:i], " \t")
		}
	}

	return strings.TrimRight(text, " \t")
}

func newYAMLTree(src []byte) *yamlTree {
	t := &yamlTree{src: src}

	for no, start := 0, 0; start < len(src); no++ {
		end := len(src)

		if nl := slices.Index(src[start:], '\n'); nl >= 0 {
			end = start + nl
		}

		raw := strings.TrimRight(string(src[start:end]), "\r")
		indent := len(raw) - len(strings.TrimLeft(raw, " "))

		if text := stripYAMLComment(raw[indent:]); text != "" {
			t.lines = append(t.lines, yamlLine{no: no, indent: indent, start: start, text: text})
		}

		start = end + 1
	}

	return t
}

func yamlSeqEntry(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

func yamlMapColon(text string) int {
	if text == "" || text[0] == '[' || text[0] == '{' {
		return -1
	}

	i := 0

	if text[0] == '"' || text[0] == '\'' {
		if end := strings.IndexByte(text[1:], text[0]); end >= 0 {
			i = end + 2
		}
	}

	for ; i < len(text); i++ {
		if text[i] == ':' && (i+1 == len(text) || text[i+1] == ' ') {
			return i
		}
	}

	return -1
}

func yamlPlainNode(text string, line, col int) *yamlNode {
	if text == "~" || text == "null" || text == "Null" || text == "NULL" {
		return &yamlNode{kind: yamlNull, line: line, col: col}
	}

	return &yamlNode{kind: yamlScalar, value: text, line: line, col: col}
}

func yamlQuoted(text string) (value string, length, badEscape int) {
	var out strings.Builder

	quote := text[0]

	for i := 1; i < len(text); i++ {
		c := text[i]

		switch {
		case quote == '\'' && c == '\'':
			if i+1 < len(text) && text[i+1] == '\'' {
				out.WriteByte('\'')
				i++

				continue
			}

			return out.String(), i + 1, -1
		case quote == '"' && c == '"':
			return out.String(), i + 1, -1
		case quote == '"' && c == '\\':
			if i+1 == len(text) {
				return "", -1, -1
			}

			i++

			if escaped, ok := yamlEscapes[text[i]]; ok {
				out.WriteString(escaped)
			} else if n := yamlHexEscapes[text[i]]; n != 0 && i+1+n <= len(text) {
				code, err := strconv.ParseUint(text[i+1:i+1+n], 16, 32)

				if err != nil {
					return "", 0, i - 1
				}

				out.WriteRune(rune(code))
				i += n
			} else {
				return "", 0, i - 1
			}
		default:
			out.WriteByte(c)
		}
	}

	return "", -1, -1
}

func (t *yamlTree) scalar(text string, line, col int) *yamlNode {
	if text[0] != '"' && text[0] != '\'' {
		return yamlPlainNode(text, line, col)
	}

	value, length, bad := yamlQuoted(text)

	switch {
	case length < 0 && bad < 0:
		t.failAtEnd()
	case length <= 0:
		t.fail(line, col+bad)
	case strings.TrimSpace(text[length:]) != "":
		t.fail(line, col+length)
	}

	return &yamlNode{kind: yamlScalar, value: value, line: line, col: col}
}

func (t *yamlTree) node(li, col int, text string) *yamlNode {
	switch {
	case yamlSeqEntry(text):
		return t.sequence(li, col, text)
	case yamlMapColon(text) >= 0:
		return t.mapping(li, col, text)
	}

	return t.inline(li, col, text)
}

func (t *yamlTree) below(col int, compact bool, line int) *yamlNode {
	if t.i < len(t.lines) {
		if l := t.lines[t.i]; l.indent > col || compact && l.indent == col && yamlSeqEntry(l.text) {
			return t.node(t.i, l.indent, l.text)
		}
	}

	return &yamlNode{kind: yamlNull, line: line, col: col}
}

func (t *yamlTree) sequence(li, col int, text string) *yamlNode {
	seq := &yamlNode{kind: yamlSeq, line: t.lines[li].no, col: col}

	for {
		rest := strings.TrimLeft(text[1:], " ")

		if rest == "" {
			t.i = li + 1
			seq.items = append(seq.items, t.below(col, false, t.lines[li].no))
		} else {
			seq.items = append(seq.items, t.node(li, col+len(text)-len(rest), rest))
		}

		if t.failed || t.i >= len(t.lines) || t.lines[t.i].indent != col || !yamlSeqEntry(t.lines[t.i].text) {
			return seq
		}

		li, text = t.i, t.lines[t.i].text
	}
}

func (t *yamlTree) mapping(li, col int, text string) *yamlNode {
	m := &yamlNode{kind: yamlMap, line: t.lines[li].no, col: col}

	for {
		colon := yamlMapColon(text)
		line := t.lines[li].no

		m.keys = append(m.keys, t.scalar(strings.TrimRight(text[:colon], " "), line, col))

		if value := strings.TrimLeft(text[colon+1:], " "); value == "" {
			t.i = li + 1
			m.items = append(m.items, t.below(col, true, line))
		} else {
			m.items = append(m.items, t.inline(li, col+len(text)-len(value), value))
		}

		if t.failed || t.i >= len(t.lines) {
			return m
		}

		next := t.lines[t.i]

		if next.indent != col || yamlSeqEntry(next.text) || yamlMapColon(next.text) < 0 {
			if next.indent > col {
				t.fail(next.no, next.indent)
			}

			return m
		}

		li, text = t.i, next.text
	}
}

func (t *yamlTree) inline(li, col int, text string) *yamlNode {
	if text[0] == '[' || text[0] == '{' {
		return t.flow(li, col)
	}

	t.i = li + 1

	return t.scalar(text, t.lines[li].no, col)
}

func (t *yamlTree) flow(li, col int) *yamlNode {
	pos := t.lines[li].start + col
	line, lineStart := t.lines[li].no, t.lines[li].start

	var parse func() *yamlNode

	skip := func() {
		for pos < len(t.src) {
			switch c := t.src[pos]; c {
			case ' ', '\t', '\r':
				pos++
			case '\n':
				pos++
				line, lineStart = line+1, pos
			case '#':
				for pos < len(t.src) && t.src[pos] != '\n' {
					pos++
				}
			default:
				return
			}
		}
	}

	scalar := func() *yamlNode {
		start, startCol := pos, pos-lineStart

		if c := t.src[pos]; c == '"' || c == '\'' {
			value, length, bad := yamlQuoted(string(t.src[pos:lineEnd(t.src, pos)]))

			if length <= 0 {
				if bad >= 0 {
					t.fail(line, startCol+bad)
				}

				pos = len(t.src)

				return nil
			}

			pos += length

			return &yamlNode{kind: yamlScalar, value: value, line: line, col: startCol}
		}

		for pos < len(t.src) && !strings.ContainsRune(",[]{}\n", rune(t.src[pos])) && !(t.src[pos] == ':' && pos+1 < len(t.src) && t.src[pos+1] == ' ') {
			pos++
		}

		return yamlPlainNode(strings.TrimRight(string(t.src[start:pos]), " \t\r"), line, startCol)
	}

	parse = func() *yamlNode {
		open, startCol, startLine := t.src[pos], pos-lineStart, line
		node := &yamlNode{kind: yamlSeq, line: startLine, col: startCol}
		closing := byte(']')

		if open == '{' {
			node.kind, closing = yamlMap, '}'
		}

		pos++

		for {
			skip()

			if pos >= len(t.src) {
				return nil
			}

			if t.src[pos] == closing {
				pos++

				return node
			}

			var item *yamlNode

			at := pos

			if c := t.src[pos]; c == '[' || c == '{' {
				item = parse()
			} else {
				item = scalar()
			}

			if item == nil || pos == at && t.src[pos] != ',' {
				return nil
			}

			if node.kind == yamlMap {
				skip()

				var value *yamlNode

				if pos < len(t.src) && t.src[pos] == ':' {
					pos++
					skip()

					if pos >= len(t.src) {
						return nil
					}

					if c := t.src[pos]; c == '[' || c == '{' {
						value = parse()
					} else {
						value = scalar()
					}

					if value == nil {
						return nil
					}
				} else {
					value = &yamlNode{kind: yamlNull, line: line, col: pos - lineStart}
				}

				node.keys, node.items = append(node.keys, item), append(node.items, value)
			} else {
				node.items = append(node.items, item)
			}

			skip()

			if pos < len(t.src) && t.src[pos] == ',' {
				pos++
			}
		}
	}

	node := parse()

	if node == nil {
		t.failAtEnd()

		return &yamlNode{kind: yamlNull}
	}

	for t.i = li; t.i < len(t.lines) && t.lines[t.i].no <= line; t.i++ {
	}

	return node
}

func parseYAMLTree(src []byte) (*yamlNode, int, int, bool) {
	t := newYAMLTree(src)

	if len(t.lines) == 0 {
		return &yamlNode{kind: yamlNull}, 0, 0, true
	}

	root := t.node(0, t.lines[0].indent, t.lines[0].text)

	if !t.failed && t.i < len(t.lines) {
		t.fail(t.lines[t.i].no, t.lines[t.i].indent)
	}

	return root, t.errLine, t.errCol, !t.failed
}

func yamlBool(s string) (bool, bool) {
	flexible := s == strings.ToLower(s) || s == strings.ToUpper(s) || len(s) > 0 && s[0] >= 'A' && s[0] <= 'Z' && s[1:] == strings.ToLower(s[1:])

	if !flexible {
		return false, false
	}

	switch strings.ToLower(s) {
	case "y", "yes", "true", "on":
		return true, true
	case "n", "no", "false", "off":
		return false, true
	}

	return false, false
}

type sysinclTreeReader struct {
	name, path string
	onWarn     func(Warn)
	records    []SysIncl
}

func (r *sysinclTreeReader) report(msg string) {
	r.onWarn(Warn{Kind: WarnMisconfiguration, Where: "At top level", Error: msg})
}

func sysinclSection(n *yamlNode) bool {
	includes := n.get("includes")

	return includes != nil && includes.kind == yamlSeq
}

func (r *sysinclTreeReader) section(n *yamlNode, context string) *yamlNode {
	if !sysinclSection(n) {
		r.report("Sysincl section at " + n.mark() + " in " + context + " is invalid.")

		return nil
	}

	for _, k := range n.keys {
		if k.kind != yamlScalar {
			return k
		}

		if k.value != "case_sensitive" && k.value != "source_filter" && k.value != "includes" {
			r.report("Sysincl include section with unsupported key '" + k.value + "': " + n.mark() + " in " + context + " is invalid.")

			break
		}
	}

	filter := ""

	if f := n.get("source_filter"); f != nil {
		if f.kind != yamlScalar {
			r.report("Sysincl source_filter " + f.mark() + " in " + context + " is invalid. Must be regex as yaml scalar.")

			return nil
		}

		filter = f.value

		if msg := sysinclRegexError(filter); msg != "" {
			r.report("Error while parsing sysincl section at " + n.mark() + " in " + context + ": library/cpp/regex/pcre/regexp.cpp:165: RegExp " + filter + ": Error: regular expression " + filter + " is wrong: " + msg)

			return nil
		}
	}

	caseSensitive := true

	if c := n.get("case_sensitive"); c != nil && c.kind == yamlScalar {
		value, ok := yamlBool(c.value)

		if !ok {
			return c
		}

		caseSensitive = value
	}

	r.list(n.get("includes"), caseSensitive, filter, "'"+filter+"' in "+context, n.line)

	return nil
}

func (r *sysinclTreeReader) list(n *yamlNode, caseSensitive bool, filter, context string, line int) {
	if n.kind != yamlSeq {
		r.report("Sysincl include list in " + context + " is invalid. Must be yaml sequence.")

		return
	}

	rec := SysIncl{CaseInsensitive: !caseSensitive, Filter: compileSourceFilter(r.name, line+1, filter, r.onWarn)}

	for _, entry := range n.items {
		switch entry.kind {
		case yamlScalar:
			rec.setMapping([]byte(entry.value), nil)
		case yamlMap:
			for i, k := range entry.keys {
				if k.kind != yamlScalar {
					r.report("Sysincl item at " + k.mark() + " in " + context + " must have scalar as key.")

					continue
				}

				switch v := entry.items[i]; v.kind {
				case yamlNull:
					rec.setMapping([]byte(k.value), nil)
				case yamlScalar:
					var paths []VFS

					if v.value != "" {
						paths = []VFS{source(v.value)}
					}

					rec.setMapping([]byte(k.value), paths)
				case yamlSeq:
					var paths []VFS

					for _, target := range v.items {
						if target.kind != yamlScalar {
							r.report("Sysincl list target for " + k.value + " in " + context + " has invalid non-scalar target at " + target.mark())

							continue
						}

						paths = append(paths, source(target.value))
					}

					rec.setMapping([]byte(k.value), paths)
				}
			}
		default:
			r.report("Sysincl item at " + entry.mark() + " in " + context + " is invalid.")
		}
	}

	r.records = append(r.records, rec)
}

func parseSysinclTree(name, path string, data []byte, onWarn func(Warn)) []SysIncl {
	r := &sysinclTreeReader{name: name, path: path, onWarn: onWarn}
	root, line, col, ok := parseYAMLTree(data)

	invalid := func(at string) {
		r.report("Sysincl config " + path + " is invalid. Yaml syntax error at " + at)
	}

	switch {
	case !ok:
		invalid("line: " + strconv.Itoa(line) + " column: " + strconv.Itoa(col))
	case sysinclSection(root):
		if bad := r.section(root, path); bad != nil {
			invalid(bad.mark())
		}
	case root.kind == yamlSeq && len(root.items) > 0 && sysinclSection(root.items[0]):
		for _, n := range root.items {
			if bad := r.section(n, path); bad != nil {
				invalid(bad.mark())

				break
			}
		}
	default:
		r.list(root, true, "", path, 0)
	}

	return r.records
}

func lineEnd(src []byte, pos int) int {
	if nl := slices.Index(src[pos:], '\n'); nl >= 0 {
		return pos + nl
	}

	return len(src)
}

func sysinclRegexError(pat string) string {
	if _, err := syntax.Parse(pat, syntax.Perl); err != nil {
		if e, ok := err.(*syntax.Error); ok && e.Code == syntax.ErrMissingBracket {
			return "unbalanced [] at offset " + strconv.Itoa(len(pat))
		}
	}

	return ""
}
