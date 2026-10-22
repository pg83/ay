package main

import "regexp"

var scIncludePattern = regexp.MustCompile(`^include[[:space:]]+"([^"]+)"[[:space:]]*;`)

type ScIncludeDirectiveParser struct{}

func (ScIncludeDirectiveParser) parse(_ string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0

	// TScIncludesParser matches stripped individual lines, including lines
	// inside block comments. It does not apply C++ comment preprocessing.
	eachLine(data, func(line []byte) {
		match := scIncludePattern.FindSubmatch(trimParserSpace(line))

		if match == nil {
			return
		}

		k = addDirective(block, k, IncludeDirective{kind: includeQuoted, target: includeTarget(internBytes(match[1]).any())})
	})

	a.commit(k)

	return ParsedIncludeSet{parsedIncludesLocal: block[:k:k]}
}
