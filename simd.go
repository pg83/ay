package main

type simdVariant struct {
	Suffix string
	CFlags []string
}

var simdVariants = map[string]simdVariant{
	"SRC_C_SSE2":  {Suffix: "sse2", CFlags: []string{"-msse2"}},
	"SRC_C_SSE3":  {Suffix: "sse3", CFlags: []string{"-msse3"}},
	"SRC_C_SSSE3": {Suffix: "ssse3", CFlags: []string{"-mssse3"}},
	"SRC_C_SSE41": {Suffix: "sse41", CFlags: []string{"-msse4.1"}},
	"SRC_C_AVX":   {Suffix: "avx", CFlags: []string{"-mavx", "-mpclmul"}},
	"SRC_C_AVX2":  {Suffix: "avx2", CFlags: []string{"-mavx2", "-mfma", "-mbmi", "-mbmi2"}},
	"SRC_C_AVX512": {Suffix: "avx512", CFlags: []string{
		"-mavx512f", "-mavx512cd", "-mavx512bw", "-mavx512dq", "-mavx512vl",
	}},
	"SRC_C_AMX": {Suffix: "amx", CFlags: []string{
		"-mamx-tile", "-mamx-int8",
		"-mavx512f", "-mavx512cd", "-mavx512bw", "-mavx512dq", "-mavx512vl",
	}},
	"SRC_C_XOP":  {Suffix: "xop", CFlags: []string{"-mxop"}},
	"SRC_C_SSE4": {Suffix: "sse4", CFlags: []string{"-msse4.1", "-msse4.2", "-mpopcnt", "-mcx16"}},
}

func simdVariantFor(macroName string) (simdVariant, bool) {
	v, ok := simdVariants[macroName]
	return v, ok
}

type simdSrc struct {
	Src     string
	Variant string
	CFlags  []string
	Line    int
}
