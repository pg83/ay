package main

import (
	"strings"
)

func isCxxSource(srcRel string) bool {
	return strings.HasSuffix(srcRel, ".cpp") ||
		strings.HasSuffix(srcRel, ".cc") ||
		strings.HasSuffix(srcRel, ".cxx") ||

		strings.HasSuffix(srcRel, ".C")
}

func isCCSourceExt(p string) bool {
	return strings.HasSuffix(p, ".cpp") ||
		strings.HasSuffix(p, ".cc") ||
		strings.HasSuffix(p, ".cxx") ||
		strings.HasSuffix(p, ".c")
}

func isAsmSourceExt(p string) bool {
	return strings.HasSuffix(p, ".asm") ||
		strings.HasSuffix(p, ".s") ||
		strings.HasSuffix(p, ".S")
}

func isHeaderSource(srcRel string) bool {
	switch {
	case strings.HasSuffix(srcRel, ".h"),
		strings.HasSuffix(srcRel, ".hh"),
		strings.HasSuffix(srcRel, ".hpp"),
		strings.HasSuffix(srcRel, ".cuh"),
		strings.HasSuffix(srcRel, ".H"),
		strings.HasSuffix(srcRel, ".hxx"),
		strings.HasSuffix(srcRel, ".xh"),
		strings.HasSuffix(srcRel, ".ipp"),
		strings.HasSuffix(srcRel, ".ixx"),
		strings.HasSuffix(srcRel, ".inl"):
		return true
	}

	return false
}

func generatedOutputCarriesIncludes(p string) bool {
	return isCCSourceExt(p) || isHeaderSource(p) || strings.HasSuffix(p, ".inc")
}

func isCodegenProducingSrc(srcRel string) bool {
	return strings.HasSuffix(srcRel, ".proto") ||
		strings.HasSuffix(srcRel, ".gztproto") ||
		strings.HasSuffix(srcRel, ".fbs64") ||
		strings.HasSuffix(srcRel, ".fbs") ||
		strings.HasSuffix(srcRel, ".ev") ||
		strings.HasSuffix(srcRel, ".cfgproto") ||
		strings.HasSuffix(srcRel, ".rl6") ||
		strings.HasSuffix(srcRel, ".rl") ||
		strings.HasSuffix(srcRel, ".y") ||
		strings.HasSuffix(srcRel, ".ypp") ||
		strings.HasSuffix(srcRel, ".cpp.in") ||
		strings.HasSuffix(srcRel, ".c.in") ||
		strings.HasSuffix(srcRel, ".sc") ||
		strings.HasSuffix(srcRel, ".gperf") ||
		strings.HasSuffix(srcRel, ".lpp") ||
		strings.HasSuffix(srcRel, ".lex") ||
		strings.HasSuffix(srcRel, ".l")
}

func isSourceEligibleForCopyAuto(srcRel string) bool {
	return isHeaderSource(srcRel) ||
		strings.HasSuffix(srcRel, ".c") ||
		strings.HasSuffix(srcRel, ".cpp") ||
		strings.HasSuffix(srcRel, ".cc") ||
		strings.HasSuffix(srcRel, ".cxx") ||
		strings.HasSuffix(srcRel, ".proto") ||
		strings.HasSuffix(srcRel, ".ev") ||
		strings.HasSuffix(srcRel, ".g4") ||
		strings.HasSuffix(srcRel, ".y") ||
		strings.HasSuffix(srcRel, ".ypp") ||
		strings.HasSuffix(srcRel, ".rl") ||
		strings.HasSuffix(srcRel, ".rl6") ||
		strings.HasSuffix(srcRel, ".h.in") ||
		strings.HasSuffix(srcRel, ".c.in") ||
		strings.HasSuffix(srcRel, ".cpp.in")
}

func flatcVariantForExt(p string) *flatcVariant {
	switch {
	case strings.HasSuffix(p, ".fbs64"):
		return &flatcVariantFL64
	case strings.HasSuffix(p, ".fbs"):
		return &flatcVariantFL
	}

	return nil
}

func classifySrcExt(s string) SrcExtClass {
	switch {
	case strings.HasSuffix(s, ".gztproto"):
		return srcExtGztProto
	case strings.HasSuffix(s, ".proto"):
		return srcExtProto
	case strings.HasSuffix(s, ".fbs64"):
		return srcExtFbs64
	case strings.HasSuffix(s, ".fbs"):
		return srcExtFbs
	case strings.HasSuffix(s, ".ev"):
		return srcExtEv
	case strings.HasSuffix(s, ".rl6"):
		return srcExtRl6
	case strings.HasSuffix(s, ".rl"):
		return srcExtRl
	case strings.HasSuffix(s, ".y"), strings.HasSuffix(s, ".ypp"):
		return srcExtY
	case strings.HasSuffix(s, ".cpp.in"):
		return srcExtCppIn
	case strings.HasSuffix(s, ".c.in"):
		return srcExtCIn
	case strings.HasSuffix(s, ".h.in"):
		return srcExtHIn
	case strings.HasSuffix(s, ".sc"):
		return srcExtSc
	case strings.HasSuffix(s, ".cfgproto"):
		return srcExtCfgProto
	case strings.HasSuffix(s, ".gperf"):
		return srcExtGperf
	case strings.HasSuffix(s, ".lpp"),
		strings.HasSuffix(s, ".lex"),
		strings.HasSuffix(s, ".l"):
		return srcExtFlex
	default:
		return srcExtRegular
	}
}
