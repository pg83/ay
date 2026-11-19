package main

const (
	mmapedParsedPresent = 1
	mmapedNilSpan       = ^uint32(0)
)

type MmapedSpan struct {
	off uint32
	n   uint32
}

type MmapedParsed struct {
	local  MmapedSpan
	extra  uint32
	dir    STR
	parser IncludeParserKind
	bucket ParsedIncludeBucket
	flags  uint8
}

type MmapedExtra [parsedIncludeBucketCount]MmapedSpan

type MmapedFiles struct {
	fileNo     []uint32
	hashes     []uint64
	parsed     []MmapedParsed
	directives []IncludeDirective
	extras     []MmapedExtra
}

func (f *MmapedFiles) no(rel STR) uint32 {
	if int(rel) < len(f.fileNo) {
		return f.fileNo[rel]
	}

	return 0
}

func (f *MmapedFiles) span(s MmapedSpan) []IncludeDirective {
	if s.off == mmapedNilSpan {
		return nil
	}

	return f.directives[s.off : s.off+s.n : s.off+s.n]
}

func (f *MmapedFiles) parsedSource(no uint32) (ParsedSource, bool) {
	rec := &f.parsed[no]

	if rec.flags&mmapedParsedPresent == 0 {
		return ParsedSource{}, false
	}

	p := ParsedSource{local: f.span(rec.local), dir: rec.dir, parser: rec.parser, bucket: rec.bucket}

	if rec.extra != 0 {
		p.extra = parsedExtraInImage | rec.extra
	}

	return p, true
}

func (f *MmapedFiles) extraSet(extra uint32) ParsedIncludeSet {
	var set ParsedIncludeSet

	for bucket, s := range f.extras[extra-1] {
		set[bucket] = f.span(s)
	}

	return set
}
