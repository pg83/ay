package main

import (
	"bytes"
)

type CIncludeDirectiveParser struct{}

func (CIncludeDirectiveParser) parse(rel string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	local := countedDirectives(a, func(block []IncludeDirective) int { return parseCIncludesChunks(data, block, 0) })

	if len(local) == 0 {
		return ParsedIncludeSet{}
	}

	return ParsedIncludeSet{parsedIncludesLocal: local}
}

type cChunkState struct {
	inBlockComment bool
}

func parseCIncludesChunks(chunks [][]byte, block []IncludeDirective, k int) int {
	if len(chunks) == 1 {
		return parseCIncludes(chunks[0], block, k)
	}

	var state cChunkState
	var pending []byte

	for i, chunk := range chunks {
		last := i == len(chunks)-1

		if len(pending) > 0 {
			if nl := bytes.IndexByte(chunk, '\n'); nl >= 0 {
				pending = append(pending, chunk[:nl+1]...)
				k = state.parseBlock(pending, block, k)
				pending = pending[:0]
				chunk = chunk[nl+1:]
			} else {
				pending = append(pending, chunk...)

				if last {
					k = state.parseBlock(pending, block, k)
				}

				continue
			}
		}

		if last {
			if len(chunk) > 0 {
				k = state.parseBlock(chunk, block, k)
			}

			continue
		}

		lastNL := bytes.LastIndexByte(chunk, '\n')

		if lastNL < 0 {
			pending = append(pending[:0], chunk...)

			continue
		}

		k = state.parseBlock(chunk[:lastNL+1], block, k)
		pending = append(pending[:0], chunk[lastNL+1:]...)
	}

	return k
}

func (s *cChunkState) parseBlock(data []byte, block []IncludeDirective, k int) int {
	if s.inBlockComment {
		end := bytes.Index(data, blockCommentClose)

		if end < 0 {
			return k
		}

		data = data[end+len(blockCommentClose):]
		s.inBlockComment = false
	}

	k, s.inBlockComment = parseCIncludesBlock(data, block, k, true)

	return k
}

func parseCIncludes(data []byte, block []IncludeDirective, k int) int {
	k, _ = parseCIncludesBlock(data, block, k, false)

	return k
}

func parseCIncludesBlock(data []byte, block []IncludeDirective, k int, trackTrailingComment bool) (int, bool) {
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

		if end, covered, open := leadingBlockCoversHi(data, clean, hi); covered {
			if open {
				return k, true
			}

			p, clean = end, end

			continue
		}

		clean = hi

		d, ok, next := parseDirectiveInline(data, hi)

		if ok {
			k = countDirective(block, k, d)
		}

		p, clean = next, next
	}

	if trackTrailingComment {
		_, _, open := leadingBlockCoversHi(data, clean, n)

		return k, open
	}

	return k, false
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
				return IncludeDirective{kind: includeSystem, target: opensslUnistdTarget.any()}, true, nextLineStart(data, q)
			}

			return IncludeDirective{kind: includeMacro, target: internBytes(data[start:q]).any()}, true, nextLineStart(data, q)
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
		return IncludeDirective{kind: kind, target: internBytes(targetBytes).any()}, true, nextLineStart(data, q)
	}

	return IncludeDirective{}, false, nextLineStart(data, q)
}

func isCWSByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\v' || c == '\f' || c == '\r'
}

func leadingBlockCoversHi(data []byte, from, hi int) (int, bool, bool) {
	for i := from; i < hi; {
		rel := bytes.Index(data[i:hi], blockCommentOpen)

		if rel < 0 {
			return 0, false, false
		}

		open := i + rel
		lineLeading := true

		for k := open; k > 0 && data[k-1] != '\n'; k-- {
			if !isCWSByte(data[k-1]) {
				lineLeading = false

				break
			}
		}

		if lineLeading {
			cl := bytes.Index(data[open+len(blockCommentOpen):], blockCommentClose)

			if cl < 0 {
				return len(data), true, true
			}

			end := open + len(blockCommentOpen) + cl + len(blockCommentClose)

			if end > hi {
				return end, true, false
			}

			i = end
		} else {
			i = open + len(blockCommentOpen)
		}
	}

	return 0, false, false
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

func isIdentByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}

func identByteAt(data []byte, i int) bool {
	return i < len(data) && isIdentByte(data[i])
}
