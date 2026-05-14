package main

// simd.go — SRC_C_<VARIANT> macro handling for SIMD permutations.
//
// Upstream `build/ymake.core.conf:3848-3923` defines SRC_C_SSE2/SSE3/SSSE3/
// SSE4/SSE41/AVX/XOP. Each emits one CC node per (source, variant) pair with
// FLAT output `<module>/<src>.<variant>.pic.o` (no `_/` infix) and cmd_args
// carrying the variant `-m<flag>` bundle plus extras (typically
// `-DSUFFIX=_<variant>`) at the per-source slot.
//
// Variant flag values follow the linux-clang branch of
// `build/ymake.core.conf:3060-3082`. SSE4_CFLAGS expands to
// $SSE41+$SSE42+$POPCNT+$CX16 — carried for parity but not exercised by
// the reference closure.

// simdVariant carries the per-variant emit knobs derived from
// `_SRC_CUSTOM_C_CPP(... $FILE .<variant> $<VARIANT>_CFLAGS $FLAGS)`:
//
//   - Suffix is the lowercase token appended to the output path,
//     producing `<src>.<suffix>.pic.o`.
//   - CFlags is the variant's `-m<flag>` bundle (the `$<V>_CFLAGS`
//     resolution from ymake.core.conf), slotted at the per-source
//     position in cmd_args.
type simdVariant struct {
	Suffix string
	CFlags []string
}

// simdVariants maps each `SRC_C_<NAME>` macro name to its variant
// descriptor. The map is read-only at runtime; gen.go consults it via
// `simdVariantFor`.
var simdVariants = map[string]simdVariant{
	"SRC_C_SSE2":  {Suffix: "sse2", CFlags: []string{"-msse2"}},
	"SRC_C_SSE3":  {Suffix: "sse3", CFlags: []string{"-msse3"}},
	"SRC_C_SSSE3": {Suffix: "ssse3", CFlags: []string{"-mssse3"}},
	"SRC_C_SSE41": {Suffix: "sse41", CFlags: []string{"-msse4.1"}},
	"SRC_C_AVX":   {Suffix: "avx", CFlags: []string{"-mavx", "-mpclmul"}},
	"SRC_C_XOP":   {Suffix: "xop", CFlags: []string{"-mxop"}},
	// SRC_C_SSE4 expands to SSE41+SSE42+POPCNT+CX16 per
	// build/ymake.core.conf:3124. Carried for completeness; not exercised
	// by the M3 closure (closure uses SSE41/SSE2/SSSE3/AVX/XOP only).
	"SRC_C_SSE4": {Suffix: "sse4", CFlags: []string{"-msse4.1", "-msse4.2", "-mpopcnt", "-mcx16"}},
}

// simdVariantFor returns the variant descriptor for `macroName` plus a
// hit indicator. A nil hit means the name does not denote a SIMD-
// permutation macro and the caller must fall through to its normal
// handling.
func simdVariantFor(macroName string) (simdVariant, bool) {
	v, ok := simdVariants[macroName]
	return v, ok
}

// simdSrc captures a single `SRC_C_<V>(file flags...)` invocation in
// the order the ya.make declares them. The walker (`gen.go`) emits one
// CC node per `simdSrc` after the regular SRCS pass.
type simdSrc struct {
	Src     string   // source filename, relative to module dir (e.g. `src/blake2b.c`).
	Variant string   // lowercase variant suffix (e.g. `avx`, `sse41`).
	CFlags  []string // variant `-m<flag>` bundle followed by extra macro args.
	Line    int      // source line for error messages.
}
