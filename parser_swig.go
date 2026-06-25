package main

import (
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
		trimmed := strings.TrimSpace(string(line))

		if trimmed == "" {
			return
		}

		if !inBlock && (strings.HasPrefix(trimmed, "%include") || strings.HasPrefix(trimmed, "%import") || strings.HasPrefix(trimmed, "%insert")) {
			target, kind, ok := parseSwigIncludeLine(trimmed)

			if ok {
				k = addDirective(block, k, IncludeDirective{kind: kind, target: internStr(target)})
			}
		}

		if strings.HasPrefix(trimmed, "%{") || strings.HasSuffix(trimmed, "%{") {
			inBlock = true
		}

		if inBlock && strings.HasPrefix(trimmed, "#") {
			cChunks = append(cChunks, []byte(trimmed))
		}

		if strings.HasSuffix(trimmed, "%}") {
			inBlock = false
		}
	})

	direct := block[:k:k]
	a.commit(k)

	iblock := a.alloc(directiveBlockHint)
	j := 0

	for _, chunk := range cChunks {
		j = parseCIncludes(chunk, iblock, j)
	}

	induced := iblock[:j:j]
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
