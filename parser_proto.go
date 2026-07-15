package main

import (
	"bytes"
	"strings"
)

type ProtoIncludeDirectiveParser struct {
	induced *IntMap[STR]
}

func (ProtoIncludeDirectiveParser) id() uint32 {
	return 4
}

func (p ProtoIncludeDirectiveParser) parse(_ string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	return p.parseDirectiveSet(data, a)
}

func (p ProtoIncludeDirectiveParser) inducedHeader(target STR) (STR, bool) {
	if p.induced != nil {
		if v := p.induced.get(uint64(target)); v != nil {
			return *v, *v != 0
		}
	}

	var h STR

	if pbH, ok := protoImportInducedHeader(target.string()); ok {
		h = pbH
	}

	if p.induced != nil {
		p.induced.put(uint64(target), h)
	}

	return h, h != 0
}

func protoImportInducedHeader(target string) (STR, bool) {
	switch {
	case extIsEv(target):
		return internV(strings.TrimSuffix(target, ".ev"), ".ev.pb.h"), true
	case extIsCfgproto(target):

		return internV(target, ".pb.h"), true
	case extIsGztproto(target):

		return internV(strings.TrimSuffix(target, ".gztproto"), ".pb.h"), true
	case extIsProto(target):
		return internV(strings.TrimSuffix(target, ".proto"), ".pb.h"), true
	}

	return 0, false
}

func (p ProtoIncludeDirectiveParser) parseDirectiveSet(data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0

	eachLine(data, func(line []byte) {
		target, kind, ok := parseProtoImportLine(line)

		if !ok {
			return
		}

		k = addDirective(block, k, IncludeDirective{kind: kind, target: includeTarget(target.any())})
	})

	local := block[:k]

	a.commit(k)

	hblock := a.alloc(directiveBlockHint)
	j := 0

	for _, d := range local {
		if pbH, ok := p.inducedHeader(d.target.str()); ok {
			j = addDirective(hblock, j, IncludeDirective{kind: d.kind, target: includeTarget(pbH.any())})
		}
	}

	hcpp := hblock[:j]

	a.commit(j)

	var set ParsedIncludeSet

	if len(local) > 0 {
		set[parsedIncludesLocal] = local
	}

	set[parsedIncludesHeader] = hcpp
	set[parsedIncludesCpp] = hcpp

	return set
}

func parseProtoImportLine(line []byte) (STR, IncludeKind, bool) {
	b := line

	for len(b) > 0 && isParserSpace(b[0]) {
		b = b[1:]
	}

	if len(b) == 0 {
		return 0, includeSystem, false
	}

	if b[0] != 'i' || !bytes.HasPrefix(b, protoImportKw) {
		return 0, includeSystem, false
	}

	if isParserIdentContinuation(bytesString(b), len("import")) {
		return 0, includeSystem, false
	}

	rest := b[len("import"):]

	for len(rest) > 0 && isParserSpace(rest[0]) {
		rest = rest[1:]
	}

	if bytes.HasPrefix(rest, []byte("public")) && !isParserIdentContinuation(bytesString(rest), len("public")) {
		rest = rest[len("public"):]
	} else if bytes.HasPrefix(rest, []byte("weak")) && !isParserIdentContinuation(bytesString(rest), len("weak")) {
		rest = rest[len("weak"):]
	}

	target, kind, ok := parseDelimitedIncludeTarget(rest)

	if !ok {
		return 0, kind, false
	}

	if bytes.Contains(target, protoLineComment) {
		return 0, kind, false
	}

	return internBytes(target), kind, true
}
