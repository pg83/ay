package main

import (
	"bytes"
)

type CfgProtoIncludeDirectiveParser struct {
	proto ProtoIncludeDirectiveParser
}

func (CfgProtoIncludeDirectiveParser) id() uint32 {
	return 9
}

func (p CfgProtoIncludeDirectiveParser) parse(_ string, data []byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	set := p.proto.parseDirectiveSet(data, a)
	block := a.alloc(directiveBlockHint)
	k := 0

	eachLine(data, func(line []byte) {
		target, ok := parseProtoConfigIncludeLine(line)

		if !ok {
			return
		}

		k = addDirective(block, k, IncludeDirective{kind: includeSystem, target: includeTarget(internStr(target).any())})
	})

	if k > 0 {
		set[parsedIncludesProtoConfig] = block[:k]
	}

	a.commit(k)

	return set
}

func parseProtoConfigIncludeLine(line []byte) (string, bool) {
	b := bytes.TrimSpace(line)

	if !bytes.HasPrefix(b, protoConfigIncludeKw) {
		return "", false
	}

	if idx := bytes.Index(b, protoLineComment); idx >= 0 {
		b = bytes.TrimSpace(b[:idx])
	}

	rest := bytes.TrimSpace(b[len(protoConfigIncludeKw):])

	if !bytes.HasPrefix(rest, protoConfigIncludeExt) {
		return "", false
	}

	rest = bytes.TrimSpace(rest[len(protoConfigIncludeExt):])

	if len(rest) == 0 || rest[0] != '=' {
		return "", false
	}

	q := bytes.IndexByte(rest, '"')

	if q < 0 {
		return "", false
	}

	target, _, ok := parseDelimitedIncludeTarget(string(rest[q:]))

	return target, ok
}
