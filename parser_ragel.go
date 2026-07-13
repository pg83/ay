package main

import (
	"bytes"
	"strings"
)

type RagelIncludeDirectiveParser struct{}

func (RagelIncludeDirectiveParser) id() uint32 {
	return 5
}

func (RagelIncludeDirectiveParser) parse(rel string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	local := block[:parseCIncludesChunks(data, block, 0)]

	local = local[:len(local)]
	a.commit(len(local))

	nblock := a.alloc(directiveBlockHint)
	nk := 0
	inSpecification := false

	eachLine(data, func(line []byte) {
		trimmed := bytes.TrimLeft(line, " \t")

		if len(trimmed) == 0 {
			return
		}

		switch {
		case bytes.HasPrefix(trimmed, []byte("%%{")):
			inSpecification = true

			return
		case bytes.HasPrefix(trimmed, []byte("}%%")):
			inSpecification = false

			return
		}

		if !inSpecification {
			return
		}

		target, ok := parseRagelNativeIncludeLine(bytesString(trimmed))

		if !ok {
			return
		}

		wrapped := includeTarget(internStr(target).any())

		for _, d := range nblock[:nk] {
			if d.target == wrapped {
				return
			}
		}

		nk = addDirective(nblock, nk, IncludeDirective{kind: includeQuoted, target: wrapped})
	})

	native := nblock[:nk]

	a.commit(nk)

	iblock := a.alloc(directiveBlockHint)
	j := addDirective(iblock, 0, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(rel).any())})

	for _, d := range local {
		j = addDirective(iblock, j, d)
	}

	induced := iblock[:j]

	a.commit(j)

	var set ParsedIncludeSet

	if len(local) > 0 {
		set[parsedIncludesLocal] = local
	}

	if len(native) > 0 {
		set[parsedIncludesRagelNative] = native
	}

	set[parsedIncludesHeader] = induced
	set[parsedIncludesCpp] = induced

	return set
}

func parseRagelNativeIncludeLine(line string) (string, bool) {
	if idx := strings.IndexByte(line, '#'); idx >= 0 {
		line = line[:idx]
	}

	line = strings.TrimSpace(line)

	if !strings.HasPrefix(line, "include") || isParserIdentContinuation(line, len("include")) {
		return "", false
	}

	rest := strings.TrimSpace(line[len("include"):])
	firstQuote := strings.IndexAny(rest, `"'`)

	if firstQuote < 0 {
		return "", false
	}

	target, _, ok := parseDelimitedIncludeTarget(strBytes(rest[firstQuote:]))

	return bytesString(target), ok
}
