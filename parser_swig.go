package main

import (
	"bytes"
	"strings"
)

type SwigIncludeDirectiveParser struct{}

func (SwigIncludeDirectiveParser) id() uint32 {
	return 6
}

func (SwigIncludeDirectiveParser) parse(rel string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0
	inBlock := false

	var cChunkArr [32][]byte

	cChunks := cChunkArr[:0]

	if !strings.Contains(rel, "/swig/Lib/") {
		for _, d := range swigImplicitDirectives {
			k = addDirective(block, k, d)
		}
	}

	eachLine(data, func(line []byte) {
		trimmed := trimParserSpace(line)

		if len(trimmed) == 0 {
			return
		}

		if !inBlock && (bytes.HasPrefix(trimmed, []byte("%include")) || bytes.HasPrefix(trimmed, []byte("%import")) || bytes.HasPrefix(trimmed, []byte("%insert"))) {
			target, kind, ok := parseSwigIncludeLine(string(trimmed))

			if ok {
				k = addDirective(block, k, IncludeDirective{kind: kind, target: includeTarget(internStr(target).any())})
			}
		}

		if bytes.HasPrefix(trimmed, []byte("%{")) || bytes.HasSuffix(trimmed, []byte("%{")) {
			inBlock = true
		}

		if inBlock && trimmed[0] == '#' {
			cChunks = append(cChunks, trimmed)
		}

		if bytes.HasSuffix(trimmed, []byte("%}")) {
			inBlock = false
		}
	})

	direct := block[:k]

	a.commit(k)

	iblock := a.alloc(directiveBlockHint)
	j := 0

	for _, chunk := range cChunks {
		j = parseCIncludes(chunk, iblock, j)
	}

	induced := iblock[:j]

	a.commit(j)

	var set ParsedIncludeSet

	if len(direct) > 0 {
		set[parsedIncludesLocal] = direct
	}

	set[parsedIncludesHeader] = induced
	set[parsedIncludesCpp] = induced

	return set
}

func parseSwigIncludeLine(line string) (string, IncludeKind, bool) {
	b := strBytes(line)

	if len(b) == 0 || b[0] != '%' {
		return "", includeSystem, false
	}

	b = b[1:]

	switch {
	case bytes.HasPrefix(b, []byte("include")):
		b = b[len("include"):]
	case bytes.HasPrefix(b, []byte("import")):
		b = b[len("import"):]
	case bytes.HasPrefix(b, []byte("insert")):
		b = b[len("insert"):]

		for len(b) > 0 && isParserSpace(b[0]) {
			b = b[1:]
		}

		if len(b) == 0 || b[0] != '(' {
			return "", includeSystem, false
		}

		close := bytes.IndexByte(b[1:], ')')

		if close < 0 {
			return "", includeSystem, false
		}

		b = b[close+2:]
	default:
		return "", includeSystem, false
	}

	for len(b) > 0 && isParserSpace(b[0]) {
		b = b[1:]
	}

	return parseDelimitedIncludeTarget(bytesString(b))
}
