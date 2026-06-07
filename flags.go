package main

var ccIncludesPrefix = []ARG{
	argIB,
	argIS,
}

var debugPrefixMapFlags = []ARG{
	argFdebugPrefixMapBB,
	argFdebugPrefixMapSS,
	argFdebugPrefixMapToolRootT,
}

var xclangDebugCompilationDir = []ARG{
	argXclang,
	argFdebugCompilationDir,
	argXclang,
	argTmp,
}

var commonCFlags = []ARG{
	argPipe,
	argDashG,
	argFdebugDefaultVersion4,
	argGgnuPubnames,
	argFnoCommon,
	argFfunctionSections,
	argFdataSections,
	argFsignedChar,
	argFsizedDeallocation,
	argFexceptions,
	argFuseInitArray,
	argFcolorDiagnostics,
	argFalignedAllocation,
	argFstackProtector,
}

var x86TargetCFlags = []ARG{
	argPipe,
	argM64,
	argDashG,
	argFdebugDefaultVersion4,
	argGgnuPubnames,
	argFnoCommon,
	argFfunctionSections,
	argFdataSections,
	argFsizedDeallocation,
	argFexceptions,
	argFuseInitArray,
	argFcolorDiagnostics,
	argFalignedAllocation,
	argFstackProtector,
}

var hostCFlags = []ARG{
	argPipe,
	argM64,
	argO3,
	argFnoCommon,
	argFfunctionSections,
	argFdataSections,
	argFsizedDeallocation,
	argFexceptions,
	argFuseInitArray,
	argFcolorDiagnostics,
	argFalignedAllocation,
}

var warningFlags = []ARG{
	argWerror,
	argWall,
	argWextra,
	argWnoParentheses,
	argWnoImplicitConstIntFloatConversion,
	argWnoUnknownWarningOption,
}

var commonDefines = []ARG{
	argDarcadiaRootS,
	argDarcadiaBuildRootB,
	argDThreadSafe,
	argDPthreads,
	argDReentrant,
	argDLargefileSource,
	argDStdcConstantMacros,
	argDStdcFormatMacros,
	argDFileOffsetBits64,
	argDGnuSource,
	argDLongLongSupported,
}

var hostDefines = []ARG{
	argDarcadiaRootS,
	argDarcadiaBuildRootB,
	argDThreadSafe,
	argDPthreads,
	argDReentrant,
	argDLargefileSource,
	argDStdcConstantMacros,
	argDStdcFormatMacros,
	argDFileOffsetBits64,
	argDGnuSource,
	argDYndxLibunwindEnableExceptionBacktrace,
	argDLongLongSupported,
}

var hostSseFeatures = []ARG{
	argMsse2,
	argMsse3,
	argMssse3,
	argMsse41,
	argMsse42,
	argMpopcnt,
	argMcx16,
}

var noLibcWarningSuppressions = []ARG{
	argWnoArrayParameter,
	argWnoDeprecateLaxVecConvAll,
	argWnoUnqualifiedStdCastCall,
	argWnoUnusedButSetParameter,
	argWnoImplicitFunctionDeclaration,
	argWnoIntConversion,
	argWnoIncompatibleFunctionPointerTypes,
	argWnoAddressOfPackedMember,
	argWnoDeprecatedThisCapture,
	argWnoMissingDesignatedFieldInitializers,
	argWnoFormat,
	argWnoVlaCxxExtension,
	argWnoInvalidOffsetof,
	argWnoAliasTemplateInDeclarationName,
	argWnoCastFunctionTypeMismatch,
	argWnoExplicitSpecializationStorageClass,
	argWnoExtraneousTemplateHead,
	argWnoMissingTemplateArgListAfterTemplateKw,
	argWnoNontrivialMemcall,
	argWnoStrictPrimaryTemplateShadow,
}

var catboostOpenSourceDefine = []ARG{
	argDcatboostOpensourceYes,
}

var builtinMacroDateTime = []ARG{
	argWnoBuiltinMacroRedefined,
	argDDateJan102019,
	argDTime000000,
}

var macroPrefixMapFlags = []ARG{
	argFmacroPrefixMapB,
	argFmacroPrefixMapS,
	argFmacroPrefixMapToolRoot,
}

var noWarningsBundle = []ARG{
	argWnoEverything,
}

var cxxStandardWarnings = []ARG{
	argWimportPreprocessorDirectivePedantic,
	argWoverloadedVirtual,
	argWnoAmbiguousReversedOperator,
	argWnoDefaultedFunctionDeleted,
	argWnoDeprecatedAnonEnumEnumConversion,
	argWnoDeprecatedEnumEnumConversion,
	argWnoDeprecatedEnumFloatConversion,
	argWnoDeprecatedVolatile,
	argWnoPessimizingMove,
	argWnoUndefinedVarTemplate,
}

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
