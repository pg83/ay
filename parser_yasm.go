package main

import "bytes"

var (
	includeCommentSign     = []byte("//")
	yasmIncludePrefix      = []byte("%include")
	yasmIncludePrefixUpper = []byte("%INCLUDE")
)

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
		if d, ok := parseIncludeLineBase(line, yasmIncludePrefix, yasmIncludePrefixUpper); ok {
			k = addDirective(block, k, d)
		}
	})

	return k
}

func parseIncludeLineBase(line, prefix, alt []byte) (IncludeDirective, bool) {
	line = bytes.TrimLeft(line, " \t")

	if len(line) == 0 || line[0] != prefix[0] {
		return IncludeDirective{}, false
	}

	if comment := bytes.Index(line, includeCommentSign); comment >= 0 {
		line = line[:comment]
	}

	word, rest := includeLineWord(line)

	if !bytes.Equal(word, prefix) && (alt == nil || !bytes.Equal(word, alt)) {
		return IncludeDirective{}, false
	}

	word, rest = includeLineWord(rest)

	target, ok := validIncludeTarget(word)

	if !ok {
		word, _ = includeLineWord(rest)

		if target, ok = validIncludeTarget(word); !ok {
			return IncludeDirective{}, false
		}
	}

	kind := includeQuoted

	if target[0] == '<' {
		kind = includeSystem
	}

	return IncludeDirective{kind: kind, target: includeTarget(internBytes(target[1 : len(target)-1]).any())}, true
}

func includeLineWord(b []byte) (word, rest []byte) {
	b = bytes.TrimLeft(b, " \t")

	if i := bytes.IndexAny(b, " \t"); i >= 0 {
		return b[:i], b[i:]
	}

	return b, nil
}

func validIncludeTarget(inc []byte) ([]byte, bool) {
	inc = bytes.TrimSuffix(inc, []byte(";"))

	if len(inc) < 3 || inc[0] != '"' && inc[0] != '\'' && inc[0] != '<' {
		return nil, false
	}

	return inc, inc[0] == inc[len(inc)-1] || inc[0] == '<' && inc[len(inc)-1] == '>'
}
