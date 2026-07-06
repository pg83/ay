package main

import "strings"

var (
	extClassMatcher = buildExtClassMatcher()
	srcExtClasses   []uint8
)

const (
	extCxxSource ExtFlags = 1 << iota
	extCCSource
	extAsmSource
	extHeader
	extCarriesIncl
	extCodegen
	extCopyAuto
)

const (
	srcExtUnseen SrcExtClass = iota
	srcExtRegular
	srcExtProto
	srcExtGztProto
	srcExtFbs
	srcExtFbs64
	srcExtEv
	srcExtRl6
	srcExtRl
	srcExtY
	srcExtCppIn
	srcExtCIn
	srcExtHIn
	srcExtSc
	srcExtCfgProto
	srcExtGperf
	srcExtFlex
	srcExtHeader
	srcExtCSource
	srcExtAsm
	srcExtGo
	srcExtYasm
	srcExtCuda
	srcExtRodata
)

type ExtFlags uint8

type ExtClass struct {
	flags  ExtFlags
	flatc  *FlatcVariant
	srcExt SrcExtClass
}

func buildExtClassMatcher() *ExtMatcher[ExtClass] {
	m := map[string]*ExtClass{}

	get := func(ext string) *ExtClass {
		c := m[ext]

		if c == nil {
			c = &ExtClass{srcExt: srcExtRegular}
			m[ext] = c
		}

		return c
	}

	set := func(flag ExtFlags, exts ...string) {
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
		".h":        srcExtHeader,
		".hh":       srcExtHeader,
		".hpp":      srcExtHeader,
		".cuh":      srcExtHeader,
		".H":        srcExtHeader,
		".hxx":      srcExtHeader,
		".xh":       srcExtHeader,
		".ipp":      srcExtHeader,
		".ixx":      srcExtHeader,
		".inl":      srcExtHeader,
		".c":        srcExtCSource,
		".cpp":      srcExtCSource,
		".cc":       srcExtCSource,
		".cxx":      srcExtCSource,
		".C":        srcExtCSource,
		".auxcpp":   srcExtCSource,
		".S":        srcExtAsm,
		".go":       srcExtGo,
		".s":        srcExtAsm,
		".asm":      srcExtYasm,
		".cu":       srcExtCuda,
		".rodata":   srcExtRodata,
	} {
		get(ext).srcExt = cls
	}

	get(".fbs64").flatc = &flatcVariantFL64
	get(".fbs").flatc = &flatcVariantFL
	get(".inc")

	entries := make([]ExtEntry[ExtClass], 0, len(m))

	for ext, c := range m {
		if c.flags&extHeader != 0 {
			c.flags |= extCopyAuto
		}

		if c.flags&(extCCSource|extHeader) != 0 || ext == ".inc" {
			c.flags |= extCarriesIncl
		}

		entries = append(entries, ExtEntry[ExtClass]{Ext: ext, Val: *c})
	}

	return newExtMatcher(entries)
}

func extHas(p string, flag ExtFlags) bool {
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

func generatedOutputAutoCompiles(p string) bool {
	return isCCSourceExt(p) || isAsmSourceExt(p) || flatcVariantForExt(p) != nil
}

func isCodegenProducingSrc(srcRel string) bool {
	return extHas(srcRel, extCodegen)
}

func isSourceEligibleForCopyAuto(srcRel string) bool {
	return extHas(srcRel, extCopyAuto)
}

func flatcVariantForExt(p string) *FlatcVariant {
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

func extIsProto(p string) bool {
	return strings.HasSuffix(p, ".proto")
}

func extIsEv(p string) bool {
	return strings.HasSuffix(p, ".ev")
}

func extIsGztproto(p string) bool {
	return strings.HasSuffix(p, ".gztproto")
}

func extIsCfgproto(p string) bool {
	return strings.HasSuffix(p, ".cfgproto")
}

func extIsPbH(p string) bool {
	return strings.HasSuffix(p, ".pb.h")
}

func extIsAsm(p string) bool {
	return strings.HasSuffix(p, ".asm")
}

func extIsFlexL(p string) bool {
	return strings.HasSuffix(p, ".l")
}

func extIsPy(p string) bool {
	return strings.HasSuffix(p, ".py")
}

func extIsPyi(p string) bool {
	return strings.HasSuffix(p, ".pyi")
}

func extIsPyx(p string) bool {
	return strings.HasSuffix(p, ".pyx")
}

func extIsSwg(p string) bool {
	return strings.HasSuffix(p, ".swg")
}

func extIsLua(p string) bool {
	return strings.HasSuffix(p, ".lua")
}

func extIsTemplateIn(p string) bool {
	return strings.HasSuffix(p, ".in")
}

func extIsPicObject(p string) bool {
	return strings.HasSuffix(p, ".pic.o")
}

func extIsArchiveMember(p string) bool {
	return strings.HasSuffix(p, ".a") || strings.HasSuffix(p, ".o")
}

func extIsRefOnlyArtifact(p string) bool {
	return strings.HasSuffix(p, ".o") || strings.HasSuffix(p, ".a") ||
		strings.HasSuffix(p, ".pyplugin") || strings.HasSuffix(p, ".exports")
}

func extIsCOrHeaderSource(p string) bool {
	return strings.HasSuffix(p, ".cpp") || strings.HasSuffix(p, ".cc") ||
		strings.HasSuffix(p, ".cxx") || strings.HasSuffix(p, ".c") ||
		strings.HasSuffix(p, ".h") || strings.HasSuffix(p, ".hpp") ||
		strings.HasSuffix(p, ".hxx")
}

func extIsEnumSerialized(p string) bool {
	return strings.HasSuffix(p, "_serialized.cpp") || strings.HasSuffix(p, "_serialized.h")
}

func extIsProtoGeneratedHeader(p string) bool {
	return extIsPbH(p) || strings.HasSuffix(p, ".sproto.h")
}

type SrcExtClass uint8

func srcExtClassOf(id STR) SrcExtClass {
	if int(id) < len(srcExtClasses) {
		if c := SrcExtClass(srcExtClasses[id]); c != srcExtUnseen {
			return c
		}
	}

	c := classifySrcExt(id.string())

	for int(id) >= len(srcExtClasses) {
		grown := len(srcExtClasses) * 2

		if grown <= int(id) {
			grown = int(id) + 1
		}

		next := make([]uint8, grown)

		copy(next, srcExtClasses)
		srcExtClasses = next
	}

	srcExtClasses[id] = uint8(c)

	return c
}

func isCodegenProducingSrcID(id STR) bool {
	switch srcExtClassOf(id) {
	case srcExtProto, srcExtGztProto, srcExtFbs, srcExtFbs64, srcExtEv, srcExtCfgProto, srcExtRl6, srcExtRl, srcExtY, srcExtCppIn, srcExtCIn, srcExtSc, srcExtGperf, srcExtFlex:
		return true
	}

	return false
}
