package main

import (
	"bytes"
	"strings"
)

type SwigIncludeDirectiveParser struct{}

func (SwigIncludeDirectiveParser) id() uint32 {
	return 6
}

func (SwigIncludeDirectiveParser) parse(rel string, data []byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
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
		trimmed := bytes.TrimSpace(line)

		if len(trimmed) == 0 {
			return
		}

		if !inBlock && (bytes.HasPrefix(trimmed, []byte("%include")) || bytes.HasPrefix(trimmed, []byte("%import")) || bytes.HasPrefix(trimmed, []byte("%insert"))) {
			target, kind, ok := parseSwigIncludeLine(string(trimmed))

			if ok {
				k = addDirective(block, k, IncludeDirective{kind: kind, target: internStr(target)})
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
	m := swigIncludeRe.FindStringSubmatch(line)

	if len(m) != 3 {
		return "", includeSystem, false
	}

	return parseDelimitedIncludeTarget(m[2])
}
