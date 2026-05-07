package main

// flags.go — CC compile flag bundles.
//
// The CC node for `build/cow/on/lib.c` in the reference graph
// (`/home/pg/monorepo/yatool_orig/g.json`) has a 101-element `cmd_args`
// list. Rather than dump the whole 101-element slice into one giant
// literal, the flags are partitioned into named bundles that EmitCC
// `append`-chains together. The grouping is the natural one observed in
// the reference output:
//
//   prologue (clang+target+output)        — 7 args  (positionally [0-6])
//   ccIncludes                            — 4 args  ([7-10])
//   debugPrefixMapFlags                   — 3 args  ([11-13])
//   xclangDebugCompilationDir             — 4 args  ([14-17])
//   commonCFlags (-pipe, -g, -fno-common, -ffunction-sections, ...)
//                                          — 14 args ([18-31])
//   warningFlags (-Werror/-Wall/-Wextra and the three "tip-of-tree"
//                 -Wno- workarounds that always travel with them)
//                                          — 6 args  ([32-37])
//   commonDefines (-DARCADIA_*, -D_THREAD_SAFE, -D_GNU_SOURCE, ...)
//                                          — 11 args ([38-48])
//   noLibcUndebugBlock (-UNDEBUG plus the 21-flag aarch64 / cxx warning
//                       suppression set that appears twice — once before
//                       and once after -DCATBOOST_OPENSOURCE=yes)
//                                          — 22 args ([49-70] and [72-93])
//   catboostOpenSourceDefine               — 1 arg   ([71])
//   builtinMacroDateTime (-Wno-builtin-macro-redefined plus the pinned
//                         __DATE__/__TIME__ that make builds bit-identical
//                         across days)     — 3 args  ([94-96])
//   macroPrefixMapFlags                    — 3 args  ([97-99])
//
// Plus the input source path appended last (1 arg, [100]) — composed in
// EmitCC, not here.
//
// Why the duplicated noLibcUndebugBlock? The reference graph repeats the
// `-UNDEBUG` + `-mno-outline-atomics` + 20× `-Wno-*` bundle on either side
// of `-DCATBOOST_OPENSOURCE=yes`. That is what ymake itself emits — the
// duplication is part of the byte-exact target. Reproducing it via two
// references to the same slice (instead of inlining the 22 strings twice)
// keeps the source readable and the intent explicit: this is one bundle,
// applied twice, by deliberate ymake design.
//
// $(BUILD_ROOT) / $(SOURCE_ROOT) / $(TOOL_ROOT) are LITERAL strings — the
// build system substitutes them at execution time. They are not Go
// template variables; do not interpolate them at emit time.

// ccCompilerPath is the absolute path to clang as it appears in the
// reference graph. M5's toolchain refactor will lift this onto
// PlatformConfig; for M1 it is hardcoded.
const ccCompilerPath = "/ix/realm/boot/bin/clang"

// targetTriple is the --target= argument for the M1 aarch64 platform.
const targetTriple = "aarch64-linux-gnu"

// archFlag is the -march= argument for the M1 aarch64 platform.
const archFlag = "armv8-a"

// binPath is the value of -B (assembler/linker driver search path) used
// in the reference graph.
const binPath = "/usr/bin"

// ccIncludes are the -I include directories applied to every CC
// compilation in the reference graph for the build/cow/on module.
//
// $(BUILD_ROOT) and $(SOURCE_ROOT) are literal placeholders the build
// system substitutes at execution time; they are not Go variables.
var ccIncludes = []string{
	"-I$(BUILD_ROOT)",
	"-I$(SOURCE_ROOT)",
	"-I$(SOURCE_ROOT)/contrib/libs/linux-headers",
	"-I$(SOURCE_ROOT)/contrib/libs/linux-headers/_nf",
}

// debugPrefixMapFlags rewrite source paths in DWARF info so the debug
// output is reproducible across build hosts (the on-disk source/build
// roots vary between machines but `/-S`, `/-B`, `/-T` do not).
var debugPrefixMapFlags = []string{
	"-fdebug-prefix-map=$(BUILD_ROOT)=/-B",
	"-fdebug-prefix-map=$(SOURCE_ROOT)=/-S",
	"-fdebug-prefix-map=$(TOOL_ROOT)=/-T",
}

// xclangDebugCompilationDir pins the DW_AT_comp_dir DWARF attribute to
// `/tmp` so the same source compiled in different working directories
// yields bit-identical .o files.
var xclangDebugCompilationDir = []string{
	"-Xclang",
	"-fdebug-compilation-dir",
	"-Xclang",
	"/tmp",
}

// commonCFlags are the architecture-agnostic compile flags applied to
// every CC compilation: pipe, debug, codegen, exception model, color
// diagnostics, stack protection.
var commonCFlags = []string{
	"-pipe",
	"-g",
	"-fdebug-default-version=4",
	"-ggnu-pubnames",
	"-fno-common",
	"-ffunction-sections",
	"-fdata-sections",
	"-fsigned-char",
	"-fsized-deallocation",
	"-fexceptions",
	"-fuse-init-array",
	"-fcolor-diagnostics",
	"-faligned-allocation",
	"-fstack-protector",
}

// warningFlags is `-Werror/-Wall/-Wextra` plus the three baseline
// `-Wno-*` suppressions that always accompany them in the reference
// graph (without these clang refuses to compile parts of the tree
// because of new diagnostics promoted in recent versions).
var warningFlags = []string{
	"-Werror",
	"-Wall",
	"-Wextra",
	"-Wno-parentheses",
	"-Wno-implicit-const-int-float-conversion",
	"-Wno-unknown-warning-option",
}

// commonDefines is the baseline `-D` set the reference graph applies to
// every CC compilation: ARCADIA roots, _GNU_SOURCE / _LARGEFILE_SOURCE
// glibc-feature gates, and the C99 stdint format-macro toggles.
var commonDefines = []string{
	"-DARCADIA_ROOT=$(SOURCE_ROOT)",
	"-DARCADIA_BUILD_ROOT=$(BUILD_ROOT)",
	"-D_THREAD_SAFE",
	"-D_PTHREADS",
	"-D_REENTRANT",
	"-D_LARGEFILE_SOURCE",
	"-D__STDC_CONSTANT_MACROS",
	"-D__STDC_FORMAT_MACROS",
	"-D_FILE_OFFSET_BITS=64",
	"-D_GNU_SOURCE",
	"-D__LONG_LONG_SUPPORTED",
}

// noLibcUndebugBlock is the bundle that travels with the no-libc /
// no-runtime / no-util module flavor used by `build/cow/on` (LIBRARY()
// with NO_UTIL/NO_LIBC/NO_RUNTIME). It contains:
//
//   - `-UNDEBUG`: ensure assert() is live regardless of NDEBUG state.
//   - `-mno-outline-atomics`: aarch64 codegen knob.
//   - 20× `-Wno-*`: the long tail of warnings clang has added that the
//     tree predates and must not break on.
//
// The reference graph emits this bundle TWICE — once before and once
// after `-DCATBOOST_OPENSOURCE=yes` — by ymake design (each "module
// macro" appends its own flag set, and several modules attach the same
// suppression set). EmitCC composes it twice to match.
//
// TODO(future-PR): once a non-NO_LIBC leaf module exists, this bundle
// becomes conditional. For M1 the only target is build/cow/on, which
// always wants the no-libc set, so EmitCC applies it unconditionally.
var noLibcUndebugBlock = []string{
	"-UNDEBUG",
	"-mno-outline-atomics",
	"-Wno-array-parameter",
	"-Wno-deprecate-lax-vec-conv-all",
	"-Wno-unqualified-std-cast-call",
	"-Wno-unused-but-set-parameter",
	"-Wno-implicit-function-declaration",
	"-Wno-int-conversion",
	"-Wno-incompatible-function-pointer-types",
	"-Wno-address-of-packed-member",
	"-Wno-deprecated-this-capture",
	"-Wno-missing-designated-field-initializers",
	"-Wno-format",
	"-Wno-vla-cxx-extension",
	"-Wno-invalid-offsetof",
	"-Wno-alias-template-in-declaration-name",
	"-Wno-cast-function-type-mismatch",
	"-Wno-explicit-specialization-storage-class",
	"-Wno-extraneous-template-head",
	"-Wno-missing-template-arg-list-after-template-kw",
	"-Wno-nontrivial-memcall",
	"-Wno-strict-primary-template-shadow",
}

// catboostOpenSourceDefine is the single sentinel flag that sits between
// the two copies of noLibcUndebugBlock in the reference output.
var catboostOpenSourceDefine = []string{
	"-DCATBOOST_OPENSOURCE=yes",
}

// builtinMacroDateTime suppresses the "redefined" warning and pins the
// preprocessor `__DATE__`/`__TIME__` macros to a fixed value so the
// resulting .o is reproducible regardless of when it was compiled.
var builtinMacroDateTime = []string{
	"-Wno-builtin-macro-redefined",
	`-D__DATE__="Jan 10 2019"`,
	`-D__TIME__="00:00:00"`,
}

// macroPrefixMapFlags rewrite the `__FILE__` macro the same way
// debugPrefixMapFlags rewrite DWARF source paths: so the preprocessor
// expansion of `__FILE__` is identical across build hosts. Note the
// trailing `/=` (collapsing the prefix to empty), which differs from
// the `/-S`/`/-B`/`/-T` shape of debugPrefixMapFlags.
var macroPrefixMapFlags = []string{
	"-fmacro-prefix-map=$(BUILD_ROOT)/=",
	"-fmacro-prefix-map=$(SOURCE_ROOT)/=",
	"-fmacro-prefix-map=$(TOOL_ROOT)/=",
}
