package main

import (
	"bytes"
	"fmt"
	"strings"
)

func parseSysInclYAML(name string, data []byte, onWarn func(Warn)) []SysIncl {
	var (
		out  []SysIncl
		rec  SysIncl
		open bool

		pendingKey    []byte
		pendingPaths  []VFS
		pendingIndent int
		pendingActive bool

		lineNo int
	)

	flushPending := func() {
		if pendingActive {
			rec.setMapping(pendingKey, pendingPaths)
			pendingActive = false
			pendingPaths = nil
		}
	}

	flushRecord := func() {
		flushPending()

		if open {
			out = append(out, rec)
			rec = SysIncl{}
			open = false
		}
	}

	recordKey := func(b []byte) {
		colon := bytes.IndexByte(b, ':')

		if colon < 0 {
			throwFmt("%s:%d: expected `key: value`, got %q", name, lineNo, b)
		}

		val := trimYSpace(b[colon+1:])

		switch string(b[:colon]) {
		case "source_filter":
			s := string(unquoteYScalar(name, lineNo, val))
			rec.Filter = compileSourceFilter(name, lineNo, s, onWarn)
			rec.KeyBySource = strings.Contains(s, "(?!")
		case "case_sensitive":
			rec.CaseInsensitive = string(val) == "false"
		case "includes":

			if len(val) != 0 && string(val) != "[]" {
				throwFmt("%s:%d: inline includes value is not part of the sysincl subset", name, lineNo)
			}
		default:
			onWarn(Warn{
				Kind:    WarnSysIncl,
				Message: fmt.Sprintf("%s:%d: unrecognised record key %q — record disabled", name, lineNo, b[:colon]),
			})
			rec.Filter = &SourceFilter{unsupported: true}
		}
	}

	includeItem := func(b []byte, indent int) {
		colon := yScalarColon(b)

		if colon < 0 {
			rec.setMapping(unquoteYScalar(name, lineNo, b), nil)

			return
		}

		key := unquoteYScalar(name, lineNo, trimYSpace(b[:colon]))
		val := trimYSpace(b[colon+1:])

		if len(val) == 0 {
			pendingKey = key
			pendingIndent = indent
			pendingActive = true

			return
		}

		if v := unquoteYScalar(name, lineNo, val); len(v) != 0 {
			rec.setMapping(key, []VFS{sourceBytes(v)})
		} else {
			rec.setMapping(key, nil)
		}
	}

	for start := 0; start < len(data); {
		lineNo++
		line := data[start:]

		if nl := bytes.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
			start += nl + 1
		} else {
			start = len(data)
		}

		indent := 0

		for indent < len(line) && line[indent] == ' ' {
			indent++
		}

		rest := stripYComment(line[indent:])

		if len(rest) == 0 {
			continue
		}

		if rest[0] == '\t' {
			j := 0

			for j < len(rest) && (rest[j] == '\t' || rest[j] == ' ') {
				j++
			}

			if j == len(rest) || rest[j] == '#' {
				continue
			}

			throwFmt("%s:%d: tab indentation is not part of the sysincl subset", name, lineNo)
		}

		if rest[0] == '-' && (len(rest) == 1 || rest[1] == ' ') {
			body := trimYSpace(rest[1:])

			if len(body) == 0 {
				throwFmt("%s:%d: empty sequence item", name, lineNo)
			}

			if indent == 0 {
				flushRecord()
				open = true
				recordKey(body)

				continue
			}

			if pendingActive {
				if indent > pendingIndent {
					if v := unquoteYScalar(name, lineNo, body); len(v) != 0 {
						pendingPaths = append(pendingPaths, sourceBytes(v))
					}

					continue
				}

				flushPending()
			}

			if !open {
				throwFmt("%s:%d: sequence item outside a record", name, lineNo)
			}

			includeItem(body, indent)

			continue
		}

		flushPending()

		if indent == 0 {
			open = true
			recordKey(rest)

			continue
		}

		if !open {
			throwFmt("%s:%d: unsupported top-level line %q (expected a record sequence)", name, lineNo, rest)
		}

		recordKey(rest)
	}

	flushRecord()

	return out
}

func stripYComment(b []byte) []byte {
	if len(b) > 0 && b[0] == '#' {
		return nil
	}

	inQuote := false

	for i := 0; i < len(b); i++ {
		switch b[i] {
		case '\\':
			if inQuote {
				i++
			}
		case '"':
			inQuote = !inQuote
		case '#':
			if !inQuote && i > 0 && (b[i-1] == ' ' || b[i-1] == '\t') {
				return trimYSpace(b[:i])
			}
		}
	}

	return trimYSpace(b)
}

func trimYSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\r') {
		b = b[1:]
	}

	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}

	return b
}

func yScalarColon(b []byte) int {
	i := 0

	if len(b) > 0 && b[0] == '"' {
		for i = 1; i < len(b); i++ {
			if b[i] == '\\' {
				i++

				continue
			}

			if b[i] == '"' {
				i++

				break
			}
		}
	}

	for ; i < len(b); i++ {
		if b[i] == ':' && (i+1 == len(b) || b[i+1] == ' ') {
			return i
		}
	}

	return -1
}

func unquoteYScalar(name string, lineNo int, b []byte) []byte {
	if len(b) == 0 || b[0] != '"' {
		return b
	}

	if len(b) < 2 || b[len(b)-1] != '"' {
		throwFmt("%s:%d: unterminated quoted scalar %q", name, lineNo, b)
	}

	inner := b[1 : len(b)-1]

	if !bytes.ContainsRune(inner, '\\') {
		return inner
	}

	out := inner[:0]

	for i := 0; i < len(inner); i++ {
		if inner[i] != '\\' {
			out = append(out, inner[i])

			continue
		}

		i++

		if i == len(inner) {
			throwFmt("%s:%d: dangling escape in %q", name, lineNo, b)
		}

		switch inner[i] {
		case '\\', '"':
			out = append(out, inner[i])
		default:
			throwFmt("%s:%d: escape \\%c is not part of the sysincl subset", name, lineNo, inner[i])
		}
	}

	return out
}
