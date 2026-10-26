package main

type FlatbuffersIncludeDirectiveParser struct{}

func (FlatbuffersIncludeDirectiveParser) parse(_ string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0

	eachLine(data, func(line []byte) {
		line = trimParserSpace(line)

		rest, ok := cutParserKeyword(line, "include")

		if !ok {
			rest, ok = cutParserKeyword(line, "native_include")
		}

		if !ok || rest[0] != '"' {
			return
		}

		end := 1

		for end < len(rest) && rest[end] != '"' {
			end++
		}

		if end == 1 || end == len(rest) {
			return
		}

		k = addDirective(block, k, IncludeDirective{kind: includeQuoted, target: includeTarget(internBytes(rest[1:end]).any())})
	})

	a.commit(k)

	if k == 0 {
		return ParsedIncludeSet{}
	}

	return ParsedIncludeSet{parsedIncludesLocal: block[:k]}
}
