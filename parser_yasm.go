package main

import "bytes"

var yasmCommentSign = []byte("//")

type YasmIncludeDirectiveParser struct{}

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
		if comment := bytes.Index(line, yasmCommentSign); comment >= 0 {
			line = line[:comment]
		}

		parts := bytes.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' })

		if len(parts) < 2 || string(parts[0]) != "%include" && string(parts[0]) != "%INCLUDE" {
			return
		}

		target, ok := yasmIncludeTarget(parts[1])

		if !ok && len(parts) > 2 {
			target, ok = yasmIncludeTarget(parts[2])
		}

		if !ok {
			return
		}

		kind := includeQuoted

		if target[0] == '<' {
			kind = includeSystem
		}

		k = addDirective(block, k, IncludeDirective{kind: kind, target: includeTarget(internBytes(target[1 : len(target)-1]).any())})
	})

	return k
}

func yasmIncludeTarget(inc []byte) ([]byte, bool) {
	inc = bytes.TrimSuffix(inc, []byte(";"))

	if len(inc) < 3 || inc[0] != '"' && inc[0] != '\'' && inc[0] != '<' {
		return nil, false
	}

	return inc, inc[0] == inc[len(inc)-1] || inc[0] == '<' && inc[len(inc)-1] == '>'
}
