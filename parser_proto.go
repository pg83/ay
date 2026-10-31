package main

import "strings"

type ProtoIncludeDirectiveParser struct {
	induced *IntMap[STR]
}

func (p ProtoIncludeDirectiveParser) parse(_ string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	return p.parseDirectiveSet(data, a)
}

func (p ProtoIncludeDirectiveParser) inducedHeader(target STR) STR {
	if p.induced != nil {
		if v := p.induced.get(uint64(target)); v != nil {
			return *v
		}
	}

	h := protoImportInducedHeader(target.string())

	if p.induced != nil {
		p.induced.put(uint64(target), h)
	}

	return h
}

func protoImportInducedHeader(target string) STR {
	switch {
	case extIsEv(target):
		return internV(strings.TrimSuffix(target, ".ev"), ".ev.pb.h")
	case extIsCfgproto(target):
		return internV(target, ".pb.h")
	}

	if dot := strings.LastIndexByte(target, '.'); dot >= 0 {
		target = target[:dot]
	}

	return internV(target, ".pb.h")
}

func (p ProtoIncludeDirectiveParser) parseDirectiveSet(data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0

	eachLine(data, func(line []byte) {
		if d, ok := parseIncludeLineBase(line, protoImportKw, nil); ok {
			k = addDirective(block, k, d)
		}
	})

	local := block[:k]

	a.commit(k)

	hcpp := a.alloc(len(local))[:len(local):len(local)]

	for i, d := range local {
		hcpp[i] = IncludeDirective{kind: d.kind, target: includeTarget(p.inducedHeader(d.target.str()).any())}
	}

	a.commit(len(local))

	var set ParsedIncludeSet

	if len(local) > 0 {
		set[parsedIncludesLocal] = local
	}

	set[parsedIncludesHeader] = hcpp
	set[parsedIncludesCpp] = hcpp

	return set
}
