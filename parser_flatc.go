package main

type FlatbuffersIncludeDirectiveParser struct{}

func (FlatbuffersIncludeDirectiveParser) id() uint32 {
	return 3
}

func (FlatbuffersIncludeDirectiveParser) parse(_ string, data []byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	data = stripComments(data)

	block := a.alloc(directiveBlockHint)
	k := 0

	eachLine(data, func(line []byte) {
		m := flatbuffersIncludeRe.FindSubmatch(line)

		if len(m) != 2 {
			return
		}

		k = addDirective(block, k, IncludeDirective{kind: includeQuoted, target: internStr(string(m[1]))})
	})

	a.commit(k)

	if k == 0 {
		return ParsedIncludeSet{}
	}

	return ParsedIncludeSet{parsedIncludesLocal: block[:k]}
}
