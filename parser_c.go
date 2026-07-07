package main

import (
	"bytes"
)

type CIncludeDirectiveParser struct{}

func (CIncludeDirectiveParser) id() uint32 {
	return 1
}

func (CIncludeDirectiveParser) parse(rel string, data []byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := parseCIncludes(data, block, 0)

	a.commit(k)

	if k == 0 {
		return ParsedIncludeSet{}
	}

	return ParsedIncludeSet{parsedIncludesLocal: block[:k]}
}

func parseCIncludes(data []byte, block []IncludeDirective, k int) int {
	n := len(data)
	p := 0
	clean := 0

	for p < n {
		rel := bytes.IndexByte(data[p:], '#')

		if rel < 0 {
			break
		}

		hi := p + rel
		ls := hi

		for ls > 0 && data[ls-1] != '\n' {
			ls--
		}

		if q0 := skipWSAndBlockComments(data, ls); q0 != hi {
			p = nextLineStart(data, q0)

			continue
		}

		if end, covered := leadingBlockCoversHi(data, clean, hi); covered {
			p, clean = end, end

			continue
		}

		d, ok, next := parseDirectiveInline(data, hi)

		if ok {
			k = addDirective(block, k, d)
		}

		p, clean = next, next
	}

	return k
}

func parseDirectiveInline(data []byte, hashPos int) (IncludeDirective, bool, int) {
	n := len(data)
	q := skipWSAndBlockComments(data, hashPos+1)

	switch {
	case bytesHasPrefixAt(data, q, "include") && !identByteAt(data, q+len("include")):
		q += len("include")
	case bytesHasPrefixAt(data, q, "import") && !identByteAt(data, q+len("import")):
		q += len("import")
	default:
		return IncludeDirective{}, false, nextLineStart(data, q)
	}

	q = skipWSAndBlockComments(data, q)

	if q >= n {
		return IncludeDirective{}, false, n
	}

	var closeCh byte

	switch data[q] {
	case '<':
		closeCh = '>'
	case '"':
		closeCh = '"'
	default:
		start := q

		for q < n {
			if c := data[q]; c == ' ' || c == '\t' || c == '\v' || c == '\f' || c == '\r' || c == '\n' {
				break
			}

			q++
		}

		if q > start && data[start] != '$' && !hasYIgnoreComment(data, q) && !bytes.ContainsAny(data[start:q], "[]") {
			if data[start] == 'B' && bytes.Equal(data[start:q], backtraceHeaderInclude) {
				return IncludeDirective{}, false, nextLineStart(data, q)
			}

			if data[start] == 'O' && bytes.Equal(data[start:q], opensslUnistdInclude) {
				return IncludeDirective{kind: includeSystem, target: includeTarget(opensslUnistdTarget)}, true, nextLineStart(data, q)
			}

			return IncludeDirective{kind: includeQuoted, target: includeTarget(internBytes(data[start:q]))}, true, nextLineStart(data, q)
		}

		return IncludeDirective{}, false, nextLineStart(data, q)
	}

	q++

	start := q

	for q < n && data[q] != closeCh && data[q] != '\n' && data[q] != '$' {
		q++
	}

	if q >= n || data[q] != closeCh {
		return IncludeDirective{}, false, nextLineStart(data, q)
	}

	targetBytes := data[start:q]

	q++

	kind := includeSystem

	if closeCh == '"' {
		kind = includeQuoted
	}

	if !hasYIgnoreComment(data, q) && !bytes.ContainsAny(targetBytes, "[]") {
		return IncludeDirective{kind: kind, target: includeTarget(internBytes(targetBytes))}, true, nextLineStart(data, q)
	}

	return IncludeDirective{}, false, nextLineStart(data, q)
}

func isCWSByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\v' || c == '\f' || c == '\r'
}

func leadingBlockCoversHi(data []byte, from, hi int) (int, bool) {
	for i := from; i < hi; {
		rel := bytes.Index(data[i:hi], blockCommentOpen)

		if rel < 0 {
			return 0, false
		}

		open := i + rel
		lineLeading := true

		for k := open; k > 0 && data[k-1] != '\n'; k-- {
			if !isCWSByte(data[k-1]) {
				lineLeading = false

				break
			}
		}

		end := len(data)

		if cl := bytes.Index(data[open+2:], blockCommentClose); cl >= 0 {
			end = open + 2 + cl + 2
		}

		if lineLeading {
			if end > hi {
				return end, true
			}

			i = end
		} else {
			i = open + 2
		}
	}

	return 0, false
}

func skipWSAndBlockComments(data []byte, i int) int {
	n := len(data)

	for i < n {
		switch data[i] {
		case ' ', '\t', '\v', '\f', '\r':
			i++
		case '/':
			if i+1 < n && data[i+1] == '*' {
				i += 2

				for i+1 < n && !(data[i] == '*' && data[i+1] == '/') {
					i++
				}

				if i+1 < n {
					i += 2
				} else {
					i = n
				}

				continue
			}

			return i
		default:
			return i
		}
	}

	return i
}

func nextLineStart(data []byte, i int) int {
	if i >= len(data) {
		return len(data)
	}

	nl := bytes.IndexByte(data[i:], '\n')

	if nl < 0 {
		return len(data)
	}

	return i + nl + 1
}

func bytesHasPrefixAt(data []byte, i int, s string) bool {
	if i+len(s) > len(data) {
		return false
	}

	for k := 0; k < len(s); k++ {
		if data[i+k] != s[k] {
			return false
		}
	}

	return true
}

func hasYIgnoreComment(data []byte, i int) bool {
	n := len(data)

	i = skipWSAndBlockComments(data, i)

	if i+2 > n || data[i] != '/' || data[i+1] != '/' {
		return false
	}

	i += 2

	for i < n && data[i] == ' ' {
		i++
	}

	return bytesHasPrefixAt(data, i, "Y_IGNORE")
}

func indexOfAngleOrQuote(b []byte) int {
	for i := 0; i < len(b); i++ {
		c := b[i]

		if c == '<' || c == '"' {
			return i
		}
	}

	return -1
}

func stripComments(data []byte) []byte {
	hasTrigger := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if c == '/' || c == '"' || c == '\'' {
			hasTrigger = true

			break
		}
	}

	if !hasTrigger {
		return data
	}

	n := len(data)
	i := 0
	atLineStart := true

	for i < n {
		c := data[i]

		if atLineStart {
			if next, ok := scanIncludeDirectiveTarget(data, i); ok {
				i = next
				atLineStart = false

				continue
			}
		}

		if c == '/' && i+1 < n && data[i+1] == '/' {
			data[i] = ' '
			data[i+1] = ' '
			i += 2

			for i < n && data[i] != '\n' {
				data[i] = ' '
				i++
			}

			atLineStart = true

			continue
		}

		if c == '/' && i+1 < n && data[i+1] == '*' {
			data[i] = ' '
			data[i+1] = ' '
			i += 2

			for i < n {
				if i+1 < n && data[i] == '*' && data[i+1] == '/' {
					data[i] = ' '
					data[i+1] = ' '
					i += 2

					break
				}

				if data[i] != '\n' {
					data[i] = ' '
				}

				i++
			}

			atLineStart = true

			continue
		}

		if c == 'R' && i+1 < n && data[i+1] == '"' && !isIdentByte(prevByte(data, i)) {
			delimStart := i + 2
			j := delimStart

			for j < n && data[j] != '(' && data[j] != '\n' && j-delimStart < 16 {
				j++
			}

			if j >= n || data[j] != '(' {
				i++

				continue
			}

			delim := make([]byte, j-delimStart)

			copy(delim, data[delimStart:j])

			i = j + 1

			for i < n {
				if data[i] == ')' && i+1+len(delim)+1 <= n {
					match := true

					for k, b := range delim {
						if data[i+1+k] != b {
							match = false

							break
						}
					}

					if match && data[i+1+len(delim)] == '"' {
						for k := 0; k <= len(delim); k++ {
							data[i+k] = ' '
						}

						data[i+1+len(delim)] = ' '
						i += 1 + len(delim) + 1

						break
					}
				}

				if data[i] != '\n' {
					data[i] = ' '
				}

				i++
			}

			continue
		}

		if c == '"' {
			i++

			for i < n {
				if data[i] == '\\' && i+1 < n && data[i+1] != '\n' {
					i += 2

					continue
				}

				if data[i] == '"' {
					i++

					break
				}

				if data[i] == '\n' {
					break
				}

				i++
			}

			continue
		}

		if c == '\'' {
			i++

			for i < n {
				if data[i] == '\\' && i+1 < n && data[i+1] != '\n' {
					i += 2

					continue
				}

				if data[i] == '\'' {
					i++

					break
				}

				if data[i] == '\n' {
					break
				}

				i++
			}

			continue
		}

		i++
		atLineStart = c == '\n'
	}

	return data
}

func scanIncludeDirectiveTarget(data []byte, i int) (int, bool) {
	n := len(data)
	j := i

	for j < n {
		switch data[j] {
		case ' ', '\t':
			j++
		default:
			goto nonSpace
		}
	}

	return 0, false

nonSpace:
	if data[j] != '#' {
		return 0, false
	}

	j++

	for j < n {
		switch data[j] {
		case ' ', '\t':
			j++
		default:
			goto directive
		}
	}

	return 0, false

directive:
	switch {
	case bytes.HasPrefix(data[j:], []byte("include_next")):
		j += len("include_next")
	case bytes.HasPrefix(data[j:], []byte("include")):
		j += len("include")
	default:
		return 0, false
	}

	for j < n {
		switch data[j] {
		case ' ', '\t':
			j++
		default:
			goto target
		}
	}

	return 0, false

target:
	if j >= n {
		return 0, false
	}

	var close byte

	switch data[j] {
	case '<':
		close = '>'
	case '"':
		close = '"'
	default:
		return 0, false
	}

	j++

	for j < n {
		if data[j] == '\\' && close == '"' && j+1 < n && data[j+1] != '\n' {
			j += 2

			continue
		}

		if data[j] == close {
			return j + 1, true
		}

		if data[j] == '\n' {
			return 0, false
		}

		j++
	}

	return 0, false
}

func prevByte(data []byte, i int) byte {
	if i == 0 {
		return 0
	}

	return data[i-1]
}

func isIdentByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}

func identByteAt(data []byte, i int) bool {
	return i < len(data) && isIdentByte(data[i])
}
