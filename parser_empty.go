package main

type EmptyIncludeDirectiveParser struct{}

func (EmptyIncludeDirectiveParser) id() uint32 {
	return 8
}

func (EmptyIncludeDirectiveParser) parse(_ string, _ [][]byte, _ *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	return ParsedIncludeSet{}
}
