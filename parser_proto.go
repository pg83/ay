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

func (p ProtoIncludeDirectiveParser) parse(_ string, data []byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
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
		h = internStr(pbH)
	}

	if p.induced != nil {
		p.induced.put(uint64(target), h)
	}

	return h, h != 0
}

func protoImportInducedHeader(target string) (string, bool) {
	switch {
	case extIsEv(target):
		return strings.TrimSuffix(target, ".ev") + ".ev.pb.h", true
	case extIsCfgproto(target):

		return target + ".pb.h", true
	case extIsGztproto(target):

		return strings.TrimSuffix(target, ".gztproto") + ".pb.h", true
	case extIsProto(target):
		return strings.TrimSuffix(target, ".proto") + ".pb.h", true
	}

	return "", false
}

func (p ProtoIncludeDirectiveParser) parseDirectiveSet(data []byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0

	eachLine(data, func(line []byte) {
		target, kind, ok := parseProtoImportLine(line)

		if !ok {
			return
		}

		k = addDirective(block, k, IncludeDirective{kind: kind, target: includeTarget(internStr(target).any())})
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

func parseProtoImportLine(line []byte) (string, IncludeKind, bool) {
	b := bytes.TrimSpace(line)

	if len(b) == 0 {
		return "", includeSystem, false
	}

	if idx := bytes.Index(b, protoLineComment); idx >= 0 {
		b = bytes.TrimSpace(b[:idx])
	}

	if !bytes.HasPrefix(b, protoImportKw) {
		return "", includeSystem, false
	}

	trimmed := string(b)

	if isParserIdentContinuation(trimmed, len("import")) {
		return "", includeSystem, false
	}

	rest := strings.TrimSpace(trimmed[len("import"):])

	if strings.HasPrefix(rest, "public") && !isParserIdentContinuation(rest, len("public")) {
		rest = strings.TrimSpace(rest[len("public"):])
	} else if strings.HasPrefix(rest, "weak") && !isParserIdentContinuation(rest, len("weak")) {
		rest = strings.TrimSpace(rest[len("weak"):])
	}

	target, kind, ok := parseDelimitedIncludeTarget(rest)

	return target, kind, ok
}
