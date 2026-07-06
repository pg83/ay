package main

import (
	"bytes"
)

type LexIncludeDirectiveParser struct{}

func (LexIncludeDirectiveParser) id() uint32 {
	return 11
}

func (LexIncludeDirectiveParser) parse(rel string, data []byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0

	for len(data) > 0 {
		line := data

		if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
			line = data[:nl]
			data = data[nl+1:]
		} else {
			data = nil
		}

		if c := bytes.Index(line, lexLineComment); c >= 0 {
			line = line[:c]
		}

		parts := bytes.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' })

		if len(parts) == 0 || !bytes.Equal(parts[0], lexIncludePrefix) {
			continue
		}

		inc := lexValidInclude(parts[1:])

		if inc == nil {
			continue
		}

		kind := includeQuoted

		if inc[0] == '<' {
			kind = includeSystem
		}

		block[k] = IncludeDirective{kind: kind, target: internBytes(inc[1 : len(inc)-1])}
		k++
	}

	a.commit(k)

	if k == 0 {
		return ParsedIncludeSet{}
	}

	return ParsedIncludeSet{parsedIncludesLocal: block[:k]}
}

var (
	lexLineComment   = []byte("//")
	lexIncludePrefix = []byte("#include")
)

func lexValidInclude(cands [][]byte) []byte {
	for i, s := range cands {
		if i >= 2 {
			return nil
		}

		if len(s) > 0 && s[len(s)-1] == ';' {
			s = s[:len(s)-1]
		}

		if len(s) < 3 || (s[0] != '"' && s[0] != '\'' && s[0] != '<') {
			continue
		}

		if s[0] == s[len(s)-1] || (s[0] == '<' && s[len(s)-1] == '>') {
			return s
		}
	}

	return nil
}
