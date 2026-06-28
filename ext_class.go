package main

var extClassMatcher = buildExtClassMatcher()

const (
	extCxxSource extFlags = 1 << iota
	extCCSource
	extAsmSource
	extHeader
	extCarriesIncl
	extCodegen
	extCopyAuto
)

type extFlags uint8

type extClass struct {
	flags  extFlags
	flatc  *flatcVariant
	srcExt SrcExtClass
}

func buildExtClassMatcher() *ExtMatcher[extClass] {
	m := map[string]*extClass{}

	get := func(ext string) *extClass {
		c := m[ext]

		if c == nil {
			c = &extClass{srcExt: srcExtRegular}
			m[ext] = c
		}

		return c
	}

	set := func(flag extFlags, exts ...string) {
		for _, e := range exts {
			get(e).flags |= flag
		}
	}

	set(extCxxSource, ".cpp", ".cc", ".cxx", ".C")
	set(extCCSource, ".cpp", ".cc", ".cxx", ".c")
	set(extAsmSource, ".asm", ".s", ".S")
	set(extHeader, ".h", ".hh", ".hpp", ".cuh", ".H", ".hxx", ".xh", ".ipp", ".ixx", ".inl")
	set(extCopyAuto, ".c", ".cpp", ".cc", ".cxx", ".proto", ".ev", ".g4", ".y", ".ypp", ".rl", ".rl6", ".h.in", ".c.in", ".cpp.in")
	set(extCodegen, ".proto", ".gztproto", ".fbs64", ".fbs", ".ev", ".cfgproto", ".rl6", ".rl", ".y", ".ypp", ".cpp.in", ".c.in", ".sc", ".gperf", ".lpp", ".lex", ".l")

	for ext, cls := range map[string]SrcExtClass{
		".gztproto": srcExtGztProto,
		".proto":    srcExtProto,
		".fbs64":    srcExtFbs64,
		".fbs":      srcExtFbs,
		".ev":       srcExtEv,
		".rl6":      srcExtRl6,
		".rl":       srcExtRl,
		".y":        srcExtY,
		".ypp":      srcExtY,
		".cpp.in":   srcExtCppIn,
		".c.in":     srcExtCIn,
		".h.in":     srcExtHIn,
		".sc":       srcExtSc,
		".cfgproto": srcExtCfgProto,
		".gperf":    srcExtGperf,
		".lpp":      srcExtFlex,
		".lex":      srcExtFlex,
		".l":        srcExtFlex,
	} {
		get(ext).srcExt = cls
	}

	get(".fbs64").flatc = &flatcVariantFL64
	get(".fbs").flatc = &flatcVariantFL
	get(".inc")

	entries := make([]ExtEntry[extClass], 0, len(m))

	for ext, c := range m {
		if c.flags&extHeader != 0 {
			c.flags |= extCopyAuto
		}

		if c.flags&(extCCSource|extHeader) != 0 || ext == ".inc" {
			c.flags |= extCarriesIncl
		}

		entries = append(entries, ExtEntry[extClass]{Ext: ext, Val: *c})
	}

	return NewExtMatcher(entries)
}

func extHas(p string, flag extFlags) bool {
	c, _ := extClassMatcher.match(p)

	return c.flags&flag != 0
}

func isCxxSource(srcRel string) bool {
	return extHas(srcRel, extCxxSource)
}

func isCCSourceExt(p string) bool {
	return extHas(p, extCCSource)
}

func isAsmSourceExt(p string) bool {
	return extHas(p, extAsmSource)
}

func isHeaderSource(srcRel string) bool {
	return extHas(srcRel, extHeader)
}

func generatedOutputCarriesIncludes(p string) bool {
	return extHas(p, extCarriesIncl)
}

func isCodegenProducingSrc(srcRel string) bool {
	return extHas(srcRel, extCodegen)
}

func isSourceEligibleForCopyAuto(srcRel string) bool {
	return extHas(srcRel, extCopyAuto)
}

func flatcVariantForExt(p string) *flatcVariant {
	c, _ := extClassMatcher.match(p)

	return c.flatc
}

func classifySrcExt(s string) SrcExtClass {
	c, ok := extClassMatcher.match(s)

	if !ok {
		return srcExtRegular
	}

	return c.srcExt
}
