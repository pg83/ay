package main

import (
	"bytes"
)

type YasmIncludeDirectiveParser struct{}

func (YasmIncludeDirectiveParser) id() uint32 {
	return 7
}

func (YasmIncludeDirectiveParser) parse(_ string, data []byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := parseYasmIncludes(data, block, 0)
	a.commit(k)

	if k == 0 {
		return ParsedIncludeSet{}
	}

	return ParsedIncludeSet{parsedIncludesLocal: block[:k:k]}
}

func parseYasmIncludes(data []byte, block []IncludeDirective, k int) int {
	eachLine(data, func(line []byte) {
		if bytes.IndexByte(line, '%') < 0 {
			return
		}

		m := yasmIncludeRe.FindSubmatchIndex(line)

		if m == nil {
			return
		}

		kind := includeSystem

		idx := indexOfAngleOrQuote(line)

		if idx >= 0 && line[idx] == '"' {
			kind = includeQuoted
		}

		k = addDirective(block, k, IncludeDirective{kind: kind, target: internStr(string(line[m[2]:m[3]]))})
	})

	return k
}
