package main

// flags.go — CC compile flag bundles.
//
// Three flavours of CC bundle are pinned byte-exact:
//
//   - `build/cow/on` TARGET (M1 leaf, PR-08): 101-element cmd_args
//     against `default-linux-aarch64`.
//   - `build/cow/on` HOST (PR-23): 105-element cmd_args against
//     `default-linux-x86_64` (PIC build, `lib.c.pic.o`).
//   - `contrib/libs/musl/...` (M2 wave 2, PR-14): 111-element
//     cmd_args. Chosen by EmitCC when the instance's path is
//     `contrib/libs/musl[/...]`.
//
// Rather than dump each bundle as one giant literal, the flags are
// partitioned into named bundles that EmitCC `append`-chains
// together. The grouping is the natural one observed in the
// reference output:
//
//   prologue (clang+target+output)        — varies by flavor
//   ccIncludes / muslCcIncludes           — 4 / 10 args
//   debugPrefixMapFlags                   — 3 args
//   xclangDebugCompilationDir             — 4 args
//   commonCFlags / hostCFlags             — 14 / 11 args (host drops -g/-fno-stack-protector...)
//   warningFlags / muslWarningFlags       — 6 / 1 arg
//   commonDefines / hostDefines           — 11 / 13 args (host adds two cpu/feature)
//   noLibcUndebugBlock                    — 22 args (target uses, host substitutes
//                                                    its own ndebugPicBlock)
//   ndebugPicBlock                        — 22 args (host counterpart of
//                                                    noLibcUndebugBlock)
//   catboostOpenSourceDefine              — 1 arg
//   builtinMacroDateTime                  — 3 args
//   macroPrefixMapFlags                   — 3 args
//
// Plus the input source path appended last (1 arg) — composed in
// EmitCC, not here.
//
// Why the duplicated `noLibcUndebugBlock`/`ndebugPicBlock`? The
// reference graph repeats the suppression bundle on either side of
// `-DCATBOOST_OPENSOURCE=yes` and (for host) inserts an
// SSE/feature-flag bundle between them. That is what ymake itself
// emits; reproducing the duplication via two references to the
// same slice keeps the source readable and the intent explicit.
//
// $(B) / $(S) / $(TOOL_ROOT) are LITERAL strings —
// the build system substitutes them at execution time. They are not
// Go template variables; do not interpolate them at emit time.

// cxxStandardFlag is the C++ language-standard switch the reference
// graph applies to every C++ compilation (PR-29-D05).
const cxxStandardFlag = "-std=c++20"

// targetTriple is the --target= argument for the M1 aarch64 platform.
const targetTriple = "aarch64-linux-gnu"

// hostTriple is the --target= argument for the host x86_64 platform.
const hostTriple = "x86_64-linux-gnu"

// archFlag is the -march= argument for the M1 aarch64 platform.
const archFlag = "armv8-a"

// binPath is the value of -B (assembler/linker driver search path)
// used in the reference graph (identical for target and host).
const binPath = "/usr/bin"

// ccIncludesPrefix and ccIncludesSuffix are the two halves of the
// non-musl include set. Per PR-29-D03, per-module ADDINCL paths slot
// BETWEEN the baseline pair (BUILD_ROOT + SOURCE_ROOT) and the
// linux-headers pair. Verified against the builtins fp_mode.c.o
// reference (cmd_args[7..14]: prefix → 4 own ADDINCL → suffix).
//
// $(B) and $(S) are literal placeholders the
// build system substitutes at execution time; they are not Go
// variables.
var ccIncludesPrefix = []string{
	"-I$(B)",
	"-I$(S)",
}

var ccIncludesSuffix = []string{
	"-I$(S)/contrib/libs/linux-headers",
	"-I$(S)/contrib/libs/linux-headers/_nf",
}

// ccIncludes is the original 4-arg flat composition retained for the
// musl-target case where own ADDINCL slots inside muslCcIncludes
// (which interleaves musl arch paths between the prefix and suffix
// halves). Composed at init time from prefix + suffix to keep the
// declaration single-source.
var ccIncludes = func() []string {
	out := make([]string, 0, len(ccIncludesPrefix)+len(ccIncludesSuffix))
	out = append(out, ccIncludesPrefix...)
	out = append(out, ccIncludesSuffix...)

	return out
}()

// debugPrefixMapFlags rewrite source paths in DWARF info so the
// debug output is reproducible across build hosts. Identical for
// target and host.
var debugPrefixMapFlags = []string{
	"-fdebug-prefix-map=$(B)=/-B",
	"-fdebug-prefix-map=$(S)=/-S",
	"-fdebug-prefix-map=$(TOOL_ROOT)=/-T",
}

// xclangDebugCompilationDir pins the DW_AT_comp_dir DWARF attribute
// to /tmp so the same source compiled in different working
// directories yields bit-identical .o files. Identical target/host.
var xclangDebugCompilationDir = []string{
	"-Xclang",
	"-fdebug-compilation-dir",
	"-Xclang",
	"/tmp",
}

// commonCFlags are the architecture-agnostic compile flags applied
// to every TARGET CC compilation: pipe, debug, codegen, exception
// model, color diagnostics, stack protection.
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

// hostCFlags is the host-build counterpart of commonCFlags. Differs
// from the target bundle in five places (verified against
// `build/cow/on/lib.c.pic.o` cmd_args[17..27]):
//
//   - `-pipe` retained
//   - `-m64` REPLACES `-g`/`-fdebug-default-version=4`/`-ggnu-pubnames`
//     (host build is release, no DWARF tuning)
//   - `-O3` injected (host is release)
//   - `-fsigned-char`/`-fstack-protector` dropped (no need on host)
//   - everything else preserved
//
// Total 11 args.
var hostCFlags = []string{
	"-pipe",
	"-m64",
	"-O3",
	"-fno-common",
	"-ffunction-sections",
	"-fdata-sections",
	"-fsized-deallocation",
	"-fexceptions",
	"-fuse-init-array",
	"-fcolor-diagnostics",
	"-faligned-allocation",
}

// warningFlags is `-Werror/-Wall/-Wextra` plus the three baseline
// `-Wno-*` suppressions that always accompany them in the reference
// graph (without these clang refuses to compile parts of the tree
// because of new diagnostics promoted in recent versions). Used for
// target AND host.
var warningFlags = []string{
	"-Werror",
	"-Wall",
	"-Wextra",
	"-Wno-parentheses",
	"-Wno-implicit-const-int-float-conversion",
	"-Wno-unknown-warning-option",
}

// commonDefines is the baseline `-D` set the reference graph applies
// to every TARGET CC compilation. 11 args.
var commonDefines = []string{
	"-DARCADIA_ROOT=$(S)",
	"-DARCADIA_BUILD_ROOT=$(B)",
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

// hostDefines is the host counterpart of commonDefines. The
// reference graph for `build/cow/on/lib.c.pic.o` adds
// `-D_YNDX_LIBUNWIND_ENABLE_EXCEPTION_BACKTRACE` (host-only
// libunwind shim) AFTER `-D_GNU_SOURCE` and BEFORE
// `-D__LONG_LONG_SUPPORTED`. Otherwise identical to commonDefines.
// 12 args.
var hostDefines = []string{
	"-DARCADIA_ROOT=$(S)",
	"-DARCADIA_BUILD_ROOT=$(B)",
	"-D_THREAD_SAFE",
	"-D_PTHREADS",
	"-D_REENTRANT",
	"-D_LARGEFILE_SOURCE",
	"-D__STDC_CONSTANT_MACROS",
	"-D__STDC_FORMAT_MACROS",
	"-D_FILE_OFFSET_BITS=64",
	"-D_GNU_SOURCE",
	"-D_YNDX_LIBUNWIND_ENABLE_EXCEPTION_BACKTRACE",
	"-D__LONG_LONG_SUPPORTED",
}

// hostSseFeatures is the SSE/CPU-feature bundle the reference graph
// inserts between the two halves of the host PIC build's
// `noLibcUndebugBlock`-equivalent. 7 args, observed at cmd_args
// positions [71..77] of `build/cow/on/lib.c.pic.o`.
var hostSseFeatures = []string{
	"-msse2",
	"-msse3",
	"-mssse3",
	"-msse4.1",
	"-msse4.2",
	"-mpopcnt",
	"-mcx16",
}

// noLibcWarningSuppressions is the long tail of -Wno-* flags that
// travels with the no-libc / no-runtime / no-util module flavour.
// Shared between target's `noLibcUndebugBlock` and host's
// `ndebugPicBlock` — both wrap the same warning suppressions in
// different `-UNDEBUG`/`-DNDEBUG -fPIC` prologues.
var noLibcWarningSuppressions = []string{
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

// noLibcUndebugBlock is the TARGET-build counterpart used by build/cow/on,
// musl, and similar no-libc modules. Begins with `-UNDEBUG -mno-outline-atomics`,
// then the 20 -Wno-* flags. 22 entries total. Emitted twice in the
// reference cmd_args (once before, once after `-DCATBOOST_OPENSOURCE=yes`).
var noLibcUndebugBlock = func() []string {
	out := make([]string, 0, 2+len(noLibcWarningSuppressions))
	out = append(out, "-UNDEBUG", "-mno-outline-atomics")
	out = append(out, noLibcWarningSuppressions...)

	return out
}()

// ndebugPicBlock is the HOST-build counterpart of noLibcUndebugBlock.
// Replaces `-UNDEBUG -mno-outline-atomics` with `-DNDEBUG -fPIC` (host
// is release + position-independent), keeping the same 20-flag
// suppression tail. 22 entries total. Emitted twice in the host
// reference cmd_args, with `hostSseFeatures` between the two copies
// instead of just `catboostOpenSourceDefine`.
var ndebugPicBlock = func() []string {
	out := make([]string, 0, 2+len(noLibcWarningSuppressions))
	out = append(out, "-DNDEBUG", "-fPIC")
	out = append(out, noLibcWarningSuppressions...)

	return out
}()

// catboostOpenSourceDefine is the single sentinel flag that sits
// between the two copies of noLibcUndebugBlock (target) or, for host,
// before the `hostSseFeatures` block.
var catboostOpenSourceDefine = []string{
	"-DCATBOOST_OPENSOURCE=yes",
}

// builtinMacroDateTime suppresses the "redefined" warning and pins
// the preprocessor `__DATE__`/`__TIME__` macros to a fixed value so
// the resulting .o is reproducible regardless of when it was
// compiled. Identical target/host.
var builtinMacroDateTime = []string{
	"-Wno-builtin-macro-redefined",
	`-D__DATE__="Jan 10 2019"`,
	`-D__TIME__="00:00:00"`,
}

// macroPrefixMapFlags rewrite the `__FILE__` macro the same way
// debugPrefixMapFlags rewrite DWARF source paths. Identical
// target/host.
var macroPrefixMapFlags = []string{
	"-fmacro-prefix-map=$(B)/=",
	"-fmacro-prefix-map=$(S)/=",
	"-fmacro-prefix-map=$(TOOL_ROOT)/=",
}

// muslCcIncludes is the include set for `contrib/libs/musl` CC
// nodes (TARGET, aarch64). Differs from `ccIncludes` by inserting
// eight musl-specific `-I` paths between `$(S)` and the
// linux-headers pair. Order matches the reference graph exactly —
// musl's own headers must shadow the global linux-headers in
// resolution.
var muslCcIncludes = []string{
	"-I$(B)",
	"-I$(S)",
	"-I$(S)/contrib/libs/musl/arch/aarch64",
	"-I$(S)/contrib/libs/musl/arch/generic",
	"-I$(S)/contrib/libs/musl/src/include",
	"-I$(S)/contrib/libs/musl/src/internal",
	"-I$(S)/contrib/libs/musl/include",
	"-I$(S)/contrib/libs/musl/extra",
	"-I$(S)/contrib/libs/linux-headers",
	"-I$(S)/contrib/libs/linux-headers/_nf",
}

// muslCcIncludesX8664 is the host-musl (x86_64) include set used by
// `composeMuslHostCC` (PR-29-D01). Replaces `arch/aarch64` with
// `arch/x86_64` — the only delta from `muslCcIncludes`. Verified
// against `$(B)/contrib/libs/musl/_/src/string/strlen.c.pic.o`
// cmd_args[6..15] in the reference graph.
var muslCcIncludesX8664 = []string{
	"-I$(B)",
	"-I$(S)",
	"-I$(S)/contrib/libs/musl/arch/x86_64",
	"-I$(S)/contrib/libs/musl/arch/generic",
	"-I$(S)/contrib/libs/musl/src/include",
	"-I$(S)/contrib/libs/musl/src/internal",
	"-I$(S)/contrib/libs/musl/include",
	"-I$(S)/contrib/libs/musl/extra",
	"-I$(S)/contrib/libs/linux-headers",
	"-I$(S)/contrib/libs/linux-headers/_nf",
}

// muslWarningFlags is the single-flag warning bundle the reference
// graph applies to musl CC nodes in place of the 6-arg
// `-Werror`/`-Wall`/`-Wextra` + 3× `-Wno-*` set used elsewhere.
// musl silences everything because it's a vendored upstream codebase
// the tree refuses to patch into clang's diagnostic style.
var muslWarningFlags = []string{
	"-Wno-everything",
}

// cxxStandardWarnings is the clang C++ standard-warning-extension
// bundle the reference graph emits unconditionally for every clang C++
// compile that does NOT set NO_COMPILER_WARNINGS (PR-33 D04 — mirror
// of `ymake_conf.py:1624-1636`). 10 args. Slotted in cmd_args
// immediately AFTER `-std=c++20` (which itself is emitted by
// `appendCxxStdAndOwn`) and BEFORE the module's own non-GLOBAL
// CXXFLAGS / CONLYFLAGS. Empirically observed at
// util/charset/all_charset.cpp.o cmd_args[102..111] and on every
// non-NoCompilerWarnings C++ CC node in the reference graph.
//
// For modules with `NO_COMPILER_WARNINGS()` (libcxx, libcxxrt,
// abseil-cpp, tcmalloc, ...) this bundle is replaced by the
// single-arg `-Wno-everything` (already handled by the existing
// `appendCxxStdAndOwn` muslWarningFlags branch when
// `injectCxxWarningBundle && noCompilerWarnings`).
var cxxStandardWarnings = []string{
	"-Wimport-preprocessor-directive-pedantic",
	"-Woverloaded-virtual",
	"-Wno-ambiguous-reversed-operator",
	"-Wno-defaulted-function-deleted",
	"-Wno-deprecated-anon-enum-enum-conversion",
	"-Wno-deprecated-enum-enum-conversion",
	"-Wno-deprecated-enum-float-conversion",
	"-Wno-deprecated-volatile",
	"-Wno-pessimizing-move",
	"-Wno-undefined-var-template",
}

// muslExtraDefines is the 9-arg block the reference graph injects
// between `commonDefines` and the no-libc bundle for musl CC nodes.
// Each flag captures a musl-specific compile-time invariant:
//
//   - `-D_XOPEN_SOURCE=700` enables POSIX.1-2008 (XSI) feature gates.
//   - `-U_GNU_SOURCE` undoes the global `-D_GNU_SOURCE` from
//     `commonDefines`; musl ships its own GNU compatibility shim and
//     refuses the glibc-style flag.
//   - `-nostdinc` / `-ffreestanding` strip the host toolchain's
//     `<stdlib.h>` family so musl's own headers (added via
//     `muslCcIncludes`) win unambiguously.
//   - `-fno-stack-protector` is required because musl's startup code
//     runs before the stack canary is initialised.
//   - `-D__libc_calloc=calloc` / `-D__libc_malloc=malloc` /
//     `-D__libc_free=free` are macro renamings that route musl's
//     internal allocator references to the public symbols.
//   - `-D_musl_=1` is the project-wide sentinel that conditional
//     code uses to detect a musl build.
var muslExtraDefines = []string{
	"-D_XOPEN_SOURCE=700",
	"-U_GNU_SOURCE",
	"-nostdinc",
	"-ffreestanding",
	"-fno-stack-protector",
	"-D__libc_calloc=calloc",
	"-D__libc_malloc=malloc",
	"-D__libc_free=free",
	"-D_musl_=1",
}
