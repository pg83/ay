package main

var simdVariants = map[string]SimdVariant{
	"SRC_C_SSE2":  {Suffix: internStr("sse2"), CFlags: simdFlags("-msse2")},
	"SRC_C_SSE3":  {Suffix: internStr("sse3"), CFlags: simdFlags("-msse3")},
	"SRC_C_SSSE3": {Suffix: internStr("ssse3"), CFlags: simdFlags("-mssse3")},
	"SRC_C_SSE41": {Suffix: internStr("sse41"), CFlags: simdFlags("-msse4.1")},
	"SRC_C_AVX":   {Suffix: internStr("avx"), CFlags: simdFlags("-mavx", "-mpclmul")},
	"SRC_C_AVX2":  {Suffix: internStr("avx2"), CFlags: simdFlags("-mavx2", "-mfma", "-mbmi", "-mbmi2")},
	"SRC_C_AVX512": {Suffix: internStr("avx512"), CFlags: simdFlags(
		"-mavx512f", "-mavx512cd", "-mavx512bw", "-mavx512dq", "-mavx512vl",
	)},
	"SRC_C_AMX": {Suffix: internStr("amx"), CFlags: simdFlags(
		"-mamx-tile", "-mamx-int8",
		"-mavx512f", "-mavx512cd", "-mavx512bw", "-mavx512dq", "-mavx512vl",
	)},
	"SRC_C_XOP":  {Suffix: internStr("xop"), CFlags: simdFlags("-mxop")},
	"SRC_C_SSE4": {Suffix: internStr("sse4"), CFlags: simdFlags("-msse4.1", "-msse4.2", "-mpopcnt", "-mcx16")},
}

func simdFlags(flags ...string) []ANY {
	return internAnys(flags)
}

type SimdVariant struct {
	Suffix STR
	CFlags []ANY
}

func simdVariantFor(macroName TOK) (SimdVariant, bool) {
	v, ok := simdVariants[macroName.string()]

	return v, ok
}
