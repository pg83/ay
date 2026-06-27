package main

import (
	"strings"
	"testing"
)

func oldIsCxxSource(s string) bool {
	return strings.HasSuffix(s, ".cpp") || strings.HasSuffix(s, ".cc") || strings.HasSuffix(s, ".cxx") || strings.HasSuffix(s, ".C")
}

func oldIsCCSourceExt(p string) bool {
	return strings.HasSuffix(p, ".cpp") || strings.HasSuffix(p, ".cc") || strings.HasSuffix(p, ".cxx") || strings.HasSuffix(p, ".c")
}

func oldIsAsmSourceExt(p string) bool {
	return strings.HasSuffix(p, ".asm") || strings.HasSuffix(p, ".s") || strings.HasSuffix(p, ".S")
}

func oldIsHeaderSource(s string) bool {
	switch {
	case strings.HasSuffix(s, ".h"), strings.HasSuffix(s, ".hh"), strings.HasSuffix(s, ".hpp"),
		strings.HasSuffix(s, ".cuh"), strings.HasSuffix(s, ".H"), strings.HasSuffix(s, ".hxx"),
		strings.HasSuffix(s, ".xh"), strings.HasSuffix(s, ".ipp"), strings.HasSuffix(s, ".ixx"),
		strings.HasSuffix(s, ".inl"):
		return true
	}

	return false
}

func oldCarriesIncl(p string) bool {
	return oldIsCCSourceExt(p) || oldIsHeaderSource(p) || strings.HasSuffix(p, ".inc")
}

func oldIsCodegenProducingSrc(s string) bool {
	return strings.HasSuffix(s, ".proto") || strings.HasSuffix(s, ".gztproto") || strings.HasSuffix(s, ".fbs64") ||
		strings.HasSuffix(s, ".fbs") || strings.HasSuffix(s, ".ev") || strings.HasSuffix(s, ".cfgproto") ||
		strings.HasSuffix(s, ".rl6") || strings.HasSuffix(s, ".rl") || strings.HasSuffix(s, ".y") ||
		strings.HasSuffix(s, ".ypp") || strings.HasSuffix(s, ".cpp.in") || strings.HasSuffix(s, ".c.in") ||
		strings.HasSuffix(s, ".sc") || strings.HasSuffix(s, ".gperf") || strings.HasSuffix(s, ".lpp") ||
		strings.HasSuffix(s, ".lex") || strings.HasSuffix(s, ".l")
}

func oldCopyAuto(s string) bool {
	return oldIsHeaderSource(s) || strings.HasSuffix(s, ".c") || strings.HasSuffix(s, ".cpp") ||
		strings.HasSuffix(s, ".cc") || strings.HasSuffix(s, ".cxx") || strings.HasSuffix(s, ".proto") ||
		strings.HasSuffix(s, ".ev") || strings.HasSuffix(s, ".g4") || strings.HasSuffix(s, ".y") ||
		strings.HasSuffix(s, ".ypp") || strings.HasSuffix(s, ".rl") || strings.HasSuffix(s, ".rl6") ||
		strings.HasSuffix(s, ".h.in") || strings.HasSuffix(s, ".c.in") || strings.HasSuffix(s, ".cpp.in")
}

func oldFlatc(p string) *flatcVariant {
	switch {
	case strings.HasSuffix(p, ".fbs64"):
		return &flatcVariantFL64
	case strings.HasSuffix(p, ".fbs"):
		return &flatcVariantFL
	}

	return nil
}

func oldClassifySrcExt(s string) SrcExtClass {
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
	case strings.HasSuffix(s, ".lpp"), strings.HasSuffix(s, ".lex"), strings.HasSuffix(s, ".l"):
		return srcExtFlex
	default:
		return srcExtRegular
	}
}

func TestExtClassMatchesLegacy(t *testing.T) {
	exts := []string{
		"", ".txt", ".in", ".inc", ".c", ".C", ".cpp", ".cc", ".cxx", ".s", ".S", ".asm",
		".h", ".hh", ".hpp", ".cuh", ".H", ".hxx", ".xh", ".ipp", ".ixx", ".inl",
		".proto", ".gztproto", ".fbs", ".fbs64", ".ev", ".cfgproto", ".rl", ".rl6",
		".y", ".ypp", ".cpp.in", ".c.in", ".h.in", ".sc", ".gperf", ".lpp", ".lex", ".l", ".g4",
		".cpp.tar", ".tar.cpp", ".PROTO", ".Fbs",
	}

	prefixes := []string{"foo", "a/b/c/foo", "x", "dir.with.dots/name", "lib.cpp.backup"}

	for _, pre := range prefixes {
		for _, e := range exts {
			p := pre + e

			if got, want := isCxxSource(p), oldIsCxxSource(p); got != want {
				t.Errorf("isCxxSource(%q)=%v want %v", p, got, want)
			}

			if got, want := isCCSourceExt(p), oldIsCCSourceExt(p); got != want {
				t.Errorf("isCCSourceExt(%q)=%v want %v", p, got, want)
			}

			if got, want := isAsmSourceExt(p), oldIsAsmSourceExt(p); got != want {
				t.Errorf("isAsmSourceExt(%q)=%v want %v", p, got, want)
			}

			if got, want := isHeaderSource(p), oldIsHeaderSource(p); got != want {
				t.Errorf("isHeaderSource(%q)=%v want %v", p, got, want)
			}

			if got, want := generatedOutputCarriesIncludes(p), oldCarriesIncl(p); got != want {
				t.Errorf("generatedOutputCarriesIncludes(%q)=%v want %v", p, got, want)
			}

			if got, want := isCodegenProducingSrc(p), oldIsCodegenProducingSrc(p); got != want {
				t.Errorf("isCodegenProducingSrc(%q)=%v want %v", p, got, want)
			}

			if got, want := isSourceEligibleForCopyAuto(p), oldCopyAuto(p); got != want {
				t.Errorf("isSourceEligibleForCopyAuto(%q)=%v want %v", p, got, want)
			}

			if got, want := flatcVariantForExt(p), oldFlatc(p); got != want {
				t.Errorf("flatcVariantForExt(%q)=%v want %v", p, got, want)
			}

			if got, want := classifySrcExt(p), oldClassifySrcExt(p); got != want {
				t.Errorf("classifySrcExt(%q)=%v want %v", p, got, want)
			}
		}
	}
}
