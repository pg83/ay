package main

type FlatbuffersIncludeDirectiveParser struct{}

func (FlatbuffersIncludeDirectiveParser) id() uint32 {
	return 3
}

func (FlatbuffersIncludeDirectiveParser) parse(_ string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	raw := stripComments(concatChunks(data))

	block := a.alloc(directiveBlockHint)
	k := 0

	eachChunkLine(raw, func(line []byte) {
		rest, ok := cutParserKeyword(trimParserSpace(line), "include")

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

		tail := trimParserSpace(rest[end+1:])

		if len(tail) == 0 || tail[0] != ';' {
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
