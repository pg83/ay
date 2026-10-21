package main

type EmptyIncludeDirectiveParser struct{}

func (EmptyIncludeDirectiveParser) parse(_ string, _ [][]byte, _ *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	return ParsedIncludeSet{}
}
