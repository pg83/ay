package main

// flags.go — CC compile flag bundles.
//
// Bundles emit byte-exact against the reference graph in the natural order
// observed there:
//   prologue → include-prefix/own-ADDINCL/suffix → debugPrefixMapFlags →
//   xclangDebugCompilationDir → commonCFlags / hostCFlags →
//   warningFlags / muslWarningFlags → commonDefines / hostDefines →
//   noLibcUndebugBlock / ndebugPicBlock → catboostOpenSourceDefine →
//   builtinMacroDateTime → macroPrefixMapFlags → input source path.
//
// The reference repeats the suppression bundle on either side of
// `-DCATBOOST_OPENSOURCE=yes` (host inserts `hostSseFeatures` between the
// two copies); reusing one slice keeps the duplication explicit.
//
// $(B) / $(S) / $(TOOL_ROOT) are LITERAL strings the build system
// substitutes at execution time — not Go template variables.

// cxxStandardFlag is the C++ language-standard switch the reference
// graph applies to every C++ compilation (PR-29-D05).
const cxxStandardFlag = "-std=c++20"

// binPath is the value of -B (assembler/linker driver search path)
// used in the reference graph (identical for target and host).
const binPath = "/usr/bin"

// ccIncludesPrefix and ccIncludesSuffix are the two halves of the non-musl
// include set. Per-module ADDINCL paths slot BETWEEN the baseline pair
// ($(B)+$(S)) and the linux-headers pair. Verified against fp_mode.c.o
// cmd_args[7..14]: prefix → own ADDINCL → suffix.
var ccIncludesPrefix = []string{
	"-I$(B)",
	"-I$(S)",
}

var ccIncludesSuffix = []string{
	"-I$(S)/contrib/libs/linux-headers",
	"-I$(S)/contrib/libs/linux-headers/_nf",
}

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

// x86TargetCFlags are the target-build x86_64 counterpart of
// commonCFlags: still debug/non-PIC, but with the x86_64 ABI switch
// used by the reference graph.
var x86TargetCFlags = []string{
	"-pipe",
	"-m64",
	"-g",
	"-fdebug-default-version=4",
	"-ggnu-pubnames",
	"-fno-common",
	"-ffunction-sections",
	"-fdata-sections",
	"-fsized-deallocation",
	"-fexceptions",
	"-fuse-init-array",
	"-fcolor-diagnostics",
	"-faligned-allocation",
	"-fstack-protector",
}

// hostCFlags is the host-build counterpart of commonCFlags (release: -m64
// replaces -g/-fdebug-default-version/-ggnu-pubnames, +-O3, drops
// -fsigned-char/-fstack-protector). Verified against
// build/cow/on/lib.c.pic.o cmd_args[17..27]; 11 args.
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

// warningFlags is `-Werror/-Wall/-Wextra` plus the three baseline `-Wno-*`
// suppressions accompanying them in the reference (without these clang
// refuses to compile parts of the tree). Target AND host.
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

// hostDefines is the host counterpart of commonDefines: adds
// `-D_YNDX_LIBUNWIND_ENABLE_EXCEPTION_BACKTRACE` (host-only libunwind shim)
// between -D_GNU_SOURCE and -D__LONG_LONG_SUPPORTED. 12 args.
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

// hostSseFeatures is the SSE/CPU-feature bundle inserted between the two
// halves of the host PIC build's ndebug block. 7 args; observed at
// cmd_args[71..77] of build/cow/on/lib.c.pic.o.
var hostSseFeatures = []string{
	"-msse2",
	"-msse3",
	"-mssse3",
	"-msse4.1",
	"-msse4.2",
	"-mpopcnt",
	"-mcx16",
}

// noLibcWarningSuppressions is the -Wno-* tail accompanying the no-libc/
// no-runtime/no-util module flavour. Shared between target's
// noLibcUndebugBlock and host's ndebugPicBlock — both wrap the same
// suppressions in different prologues (-UNDEBUG vs -DNDEBUG -fPIC).
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

var x86NoLibcUndebugBlock = func() []string {
	out := make([]string, 0, 1+len(noLibcWarningSuppressions))
	out = append(out, "-UNDEBUG")
	out = append(out, noLibcWarningSuppressions...)

	return out
}()

type compileFlagBundle struct {
	ArchArgs               []string
	CFlags                 []string
	Defines                []string
	NoLibcBlock            []string
	CPUFeatures            []string
	SplitAutoPeerAroundCPU bool
}

func compileFlagBundleFor(p *Platform) compileFlagBundle {
	if p.PIC {
		return compileFlagBundle{
			CFlags:                 hostCFlags,
			Defines:                hostDefines,
			NoLibcBlock:            ndebugPicBlock,
			CPUFeatures:            hostSseFeatures,
			SplitAutoPeerAroundCPU: true,
		}
	}

	switch p.ISA {
	case ISAX8664:
		return compileFlagBundle{
			CFlags:                 x86TargetCFlags,
			Defines:                hostDefines,
			NoLibcBlock:            x86NoLibcUndebugBlock,
			CPUFeatures:            hostSseFeatures,
			SplitAutoPeerAroundCPU: true,
		}
	case ISAAArch64:
		bundle := compileFlagBundle{
			CFlags:      commonCFlags,
			Defines:     commonDefines,
			NoLibcBlock: noLibcUndebugBlock,
		}
		if p.March != "" {
			bundle.ArchArgs = []string{"-march=" + p.March}
		}

		return bundle
	}

	ThrowFmt("compileFlagBundleFor: unsupported platform ISA %q", p.ISA)
	return compileFlagBundle{}
}

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

// muslWarningFlags is the single-flag warning bundle the reference
// graph applies to musl CC nodes in place of the 6-arg
// `-Werror`/`-Wall`/`-Wextra` + 3× `-Wno-*` set used elsewhere.
// musl silences everything because it's a vendored upstream codebase
// the tree refuses to patch into clang's diagnostic style.
var muslWarningFlags = []string{
	"-Wno-everything",
}

// cxxStandardWarnings mirrors upstream ymake_conf.py:1624-1636 — clang C++
// standard-warning-extension bundle (10 args) emitted unconditionally for
// every C++ compile that does NOT set NO_COMPILER_WARNINGS. Slotted
// immediately AFTER -std=c++20 and BEFORE the module's own non-GLOBAL
// CXX/CONLYFLAGS. NO_COMPILER_WARNINGS modules (libcxx, libcxxrt,
// abseil-cpp, tcmalloc, …) substitute `-Wno-everything`.
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

// muslExtraDefines is the 9-arg block injected between commonDefines and
// the no-libc bundle for musl CC nodes:
//
//	-D_XOPEN_SOURCE=700: POSIX.1-2008 XSI feature gates.
//	-U_GNU_SOURCE: undo commonDefines' -D_GNU_SOURCE; musl rejects glibc-style.
//	-nostdinc / -ffreestanding: strip host toolchain's <stdlib.h> family.
//	-fno-stack-protector: musl startup runs before stack canary init.
//	-D__libc_calloc=calloc / __libc_malloc / __libc_free: route musl's
//	internal allocator references to public symbols.
//	-D_musl_=1: project-wide musl-build sentinel.
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
