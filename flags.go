package main

var ccIncludesPrefix = internArgs([]string{
	"-I$(B)",
	"-I$(S)",
})

var debugPrefixMapFlags = internArgs([]string{
	"-fdebug-prefix-map=$(B)=/-B",
	"-fdebug-prefix-map=$(S)=/-S",
	"-fdebug-prefix-map=$(TOOL_ROOT)=/-T",
})

var xclangDebugCompilationDir = internArgs([]string{
	"-Xclang",
	"-fdebug-compilation-dir",
	"-Xclang",
	"/tmp",
})

var commonCFlags = internArgs([]string{
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
})

var x86TargetCFlags = internArgs([]string{
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
})

var hostCFlags = internArgs([]string{
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
})

var warningFlags = internArgs([]string{
	"-Werror",
	"-Wall",
	"-Wextra",
	"-Wno-parentheses",
	"-Wno-implicit-const-int-float-conversion",
	"-Wno-unknown-warning-option",
})

var commonDefines = internArgs([]string{
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
})

var hostDefines = internArgs([]string{
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
})

var hostSseFeatures = internArgs([]string{
	"-msse2",
	"-msse3",
	"-mssse3",
	"-msse4.1",
	"-msse4.2",
	"-mpopcnt",
	"-mcx16",
})

var noLibcWarningSuppressions = internArgs([]string{
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
})

var catboostOpenSourceDefine = internArgs([]string{
	"-DCATBOOST_OPENSOURCE=yes",
})

var builtinMacroDateTime = internArgs([]string{
	"-Wno-builtin-macro-redefined",
	`-D__DATE__="Jan 10 2019"`,
	`-D__TIME__="00:00:00"`,
})

var macroPrefixMapFlags = internArgs([]string{
	"-fmacro-prefix-map=$(B)/=",
	"-fmacro-prefix-map=$(S)/=",
	"-fmacro-prefix-map=$(TOOL_ROOT)/=",
})

var noWarningsBundle = internArgs([]string{
	"-Wno-everything",
})

var cxxStandardWarnings = internArgs([]string{
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
})

const binPath = "/usr/bin"

func sseBaseCFlags(x8664 bool) []ARG {
	if x8664 {
		return hostSseFeatures
	}

	return nil
}

func noLibcBlock(p *Platform) []ARG {
	out := make([]ARG, 0, 3+len(noLibcWarningSuppressions))

	if p.BuildRelease {
		out = append(out, argNDEBUG)
	} else {
		out = append(out, argUNDEBUG)
	}

	if p.ISA == ISAAArch64 {
		out = append(out, argNoOutlineAtomics)
	}

	if p.PIC {
		out = append(out, argFPIC)
	}

	out = append(out, noLibcWarningSuppressions...)

	return out
}

type compileFlagBundle struct {
	ArchArgs    []ARG
	CFlags      []ARG
	Defines     []ARG
	NoLibcBlock []ARG
}

func withSandboxingDebugCompression(base []ARG, p *Platform) []ARG {
	if p == nil || p.PIC || p.Flags[envSANDBOXING] != strYes {
		return base
	}

	out := make([]ARG, 0, len(base)+1)
	inserted := false

	for _, flag := range base {
		out = append(out, flag)

		if !inserted && flag == argDashG {
			out = append(out, argGzZstd)
			inserted = true
		}
	}

	if !inserted {
		out = append(out, argGzZstd)
	}

	return out
}

func compileFlagBundleFor(p *Platform) compileFlagBundle {
	switch p.ISA {
	case ISAX8664:
		cflags := x86TargetCFlags

		if p.BuildRelease {
			cflags = hostCFlags
		}

		return compileFlagBundle{
			CFlags:      withSandboxingDebugCompression(cflags, p),
			Defines:     hostDefines,
			NoLibcBlock: noLibcBlock(p),
		}
	case ISAAArch64:
		bundle := compileFlagBundle{
			CFlags:      withSandboxingDebugCompression(commonCFlags, p),
			Defines:     commonDefines,
			NoLibcBlock: noLibcBlock(p),
		}

		if p.March != "" {
			bundle.ArchArgs = []ARG{internArg("-march=" + p.March)}
		}

		return bundle
	}

	ThrowFmt("compileFlagBundleFor: unsupported platform ISA %q", p.ISA)
	return compileFlagBundle{}
}
