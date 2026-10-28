package main

type ScIncludeDirectiveParser struct{}

func (ScIncludeDirectiveParser) parse(_ string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0

	eachLine(data, func(line []byte) {
		line = trimParserSpace(line)

		ts, te, ok := scIncludeMatch(line)

		if !ok {
			return
		}

		k = addDirective(block, k, IncludeDirective{kind: includeQuoted, target: includeTarget(internBytes(line[ts:te]).any())})
	})

	a.commit(k)

	return ParsedIncludeSet{parsedIncludesLocal: block[:k:k]}
}
