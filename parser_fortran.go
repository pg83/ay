package main

import "bytes"

type FortranIncludeDirectiveParser struct{}

func (FortranIncludeDirectiveParser) parse(_ string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0

	eachLine(data, func(line []byte) {
		line = trimParserSpace(line)

		tail, ok := cutParserKeyword(line, "include")

		if !ok || tail[0] != '\'' {
			return
		}

		end := bytes.IndexByte(tail[1:], '\'')

		if end <= 0 {
			return
		}

		k = addDirective(block, k, IncludeDirective{kind: includeQuoted, target: includeTarget(internBytes(tail[1 : 1+end]).any())})
	})

	a.commit(k)

	return ParsedIncludeSet{parsedIncludesLocal: block[:k:k]}
}
