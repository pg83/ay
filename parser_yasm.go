package main

type YasmIncludeDirectiveParser struct{}

func (YasmIncludeDirectiveParser) id() uint32 {
	return 7
}

func (YasmIncludeDirectiveParser) parse(_ string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := parseYasmIncludes(data, block, 0)

	a.commit(k)

	if k == 0 {
		return ParsedIncludeSet{}
	}

	return ParsedIncludeSet{parsedIncludesLocal: block[:k]}
}

func parseYasmIncludes(data [][]byte, block []IncludeDirective, k int) int {
	eachLine(data, func(line []byte) {
		line = trimParserSpace(line)

		if len(line) == 0 || line[0] != '%' {
			return
		}

		line = line[1:]

		for len(line) > 0 && isParserSpace(line[0]) {
			line = line[1:]
		}

		const keyword = "include"

		if len(line) < len(keyword) || !equalFoldASCII(line[:len(keyword)], keyword) {
			return
		}

		line = line[len(keyword):]

		for len(line) > 0 && isParserSpace(line[0]) {
			line = line[1:]
		}

		if len(line) < 3 || (line[0] != '<' && line[0] != '"') {
			return
		}

		end := 1

		for end < len(line) && line[end] != '>' && line[end] != '"' {
			end++
		}

		if end == 1 || end == len(line) {
			return
		}

		kind := includeSystem

		if line[0] == '"' {
			kind = includeQuoted
		}

		k = addDirective(block, k, IncludeDirective{kind: kind, target: includeTarget(internBytes(line[1:end]).any())})
	})

	return k
}

func equalFoldASCII(b []byte, s string) bool {
	if len(b) != len(s) {
		return false
	}

	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}

		if c != s[i] {
			return false
		}
	}

	return true
}
