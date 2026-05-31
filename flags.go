package main

var ccIncludesPrefix = []string{
	"-I$(B)",
	"-I$(S)",
}

var ccIncludesSuffix = []string{
	"-I$(S)/contrib/libs/linux-headers",
	"-I$(S)/contrib/libs/linux-headers/_nf",
}

var googleapisCommonProtosAddIncl = Intern("$(B)/contrib/libs/googleapis-common-protos")

var debugPrefixMapFlags = []string{
	"-fdebug-prefix-map=$(B)=/-B",
	"-fdebug-prefix-map=$(S)=/-S",
	"-fdebug-prefix-map=$(TOOL_ROOT)=/-T",
}

var xclangDebugCompilationDir = []string{
	"-Xclang",
	"-fdebug-compilation-dir",
	"-Xclang",
	"/tmp",
}

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

var warningFlags = []string{
	"-Werror",
	"-Wall",
	"-Wextra",
	"-Wno-parentheses",
	"-Wno-implicit-const-int-float-conversion",
	"-Wno-unknown-warning-option",
}

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

var hostSseFeatures = []string{
	"-msse2",
	"-msse3",
	"-mssse3",
	"-msse4.1",
	"-msse4.2",
	"-mpopcnt",
	"-mcx16",
}

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

var catboostOpenSourceDefine = []string{
	"-DCATBOOST_OPENSOURCE=yes",
}

var builtinMacroDateTime = []string{
	"-Wno-builtin-macro-redefined",
	`-D__DATE__="Jan 10 2019"`,
	`-D__TIME__="00:00:00"`,
}

var macroPrefixMapFlags = []string{
	"-fmacro-prefix-map=$(B)/=",
	"-fmacro-prefix-map=$(S)/=",
	"-fmacro-prefix-map=$(TOOL_ROOT)/=",
}

var noWarningsBundle = []string{
	"-Wno-everything",
}

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

const cxxStandardFlag = "-std=c++20"

const binPath = "/usr/bin"

func sseBaseCFlags(x8664 bool) []string {
	if x8664 {
		return hostSseFeatures
	}

	return nil
}

func noLibcBlock(p *Platform) []string {
	out := make([]string, 0, 3+len(noLibcWarningSuppressions))

	if p.BuildRelease {
		out = append(out, "-DNDEBUG")
	} else {
		out = append(out, "-UNDEBUG")
	}

	if p.ISA == ISAAArch64 {
		out = append(out, "-mno-outline-atomics")
	}

	if p.PIC {
		out = append(out, "-fPIC")
	}

	out = append(out, noLibcWarningSuppressions...)

	return out
}

type compileFlagBundle struct {
	ArchArgs    []string
	CFlags      []string
	Defines     []string
	NoLibcBlock []string
}

func withSandboxingDebugCompression(base []string, p *Platform) []string {
	if p == nil || p.PIC || p.Flags["SANDBOXING"] != "yes" {
		return base
	}

	out := make([]string, 0, len(base)+1)
	inserted := false

	for _, flag := range base {
		out = append(out, flag)

		if !inserted && flag == "-g" {
			out = append(out, "-gz=zstd")
			inserted = true
		}
	}

	if !inserted {
		out = append(out, "-gz=zstd")
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
			bundle.ArchArgs = []string{"-march=" + p.March}
		}

		return bundle
	}

	ThrowFmt("compileFlagBundleFor: unsupported platform ISA %q", p.ISA)
	return compileFlagBundle{}
}
