package main

// simd.go — SRC_C_<VARIANT> macro handling for SIMD permutations.
//
// Upstream (`build/ymake.core.conf:3848-3923`) defines the family:
//
//     SRC_C_SSE2(file, flags...)   → compile `file` with $SSE2_CFLAGS  + `.sse2`  suffix
//     SRC_C_SSE3(file, flags...)   → ...                $SSE3_CFLAGS   + `.sse3`
//     SRC_C_SSSE3(file, flags...)  → ...                $SSSE3_CFLAGS  + `.ssse3`
//     SRC_C_SSE4(file, flags...)   → ...                $SSE4_CFLAGS   + `.sse4`
//     SRC_C_SSE41(file, flags...)  → ...                $SSE41_CFLAGS  + `.sse41`
//     SRC_C_AVX(file, flags...)    → ...                $AVX_CFLAGS    + `.avx`
//     SRC_C_XOP(file, flags...)    → ...                $XOP_CFLAGS    + `.xop`
//
// Each macro emits one CC node per (source, variant) pair. The output path
// is FLAT (`<module>/<src>.<variant>.pic.o`, no `_/` infix even when the
// source contains a `/`) and the cmd_args carry the variant `-m<flag>`
// bundle plus any extra tokens (typically `-DSUFFIX=_<variant>`) at the
// per-source slot — between `macroPrefixMapFlags` and the input path.
//
// The variant flag values follow the linux-clang branch of
// `build/ymake.core.conf:3060-3082`:
//
//     SSE2_CFLAGS   = -msse2
//     SSE3_CFLAGS   = -msse3
//     SSSE3_CFLAGS  = -mssse3
//     SSE41_CFLAGS  = -msse4.1
//     AVX_CFLAGS    = -mavx -mpclmul
//     XOP_CFLAGS    = -mxop
//
// SSE4_CFLAGS expands to "$SSE41_CFLAGS $SSE42_CFLAGS $POPCNT_CFLAGS
// $CX16_FLAGS" — none of M3's reference modules use SRC_C_SSE4 with a
// per-source override, so the table below carries the empirical 4-flag
// expansion observed in the host SSE feature bundle (kept for parity with
// the macro definition but unreferenced by the M3 closure).

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
