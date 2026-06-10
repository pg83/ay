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

// commonCFlags / x86TargetCFlags are the debug-build C flag vectors with the
// debug-info group factored out: the bundle is composed as Pre + Platform.
// DebugInfoFlags + Post (see composeCompileCFlags), so the debug-info flags
// (-g, optional -gz=zstd, …) land in their natural slot by construction rather
// than being spliced in afterward.
var commonCFlagsPre = []ARG{
	argPipe,
}

var commonCFlagsPost = []ARG{
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

var x86TargetCFlagsPre = []ARG{
	argPipe,
	argM64,
}

var x86TargetCFlagsPost = []ARG{
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

// catboostOpenSourceDefineFor returns the -DCATBOOST_OPENSOURCE=yes define only
// for opensource builds (OPENSOURCE=yes in the repo ya.conf [flags]). Upstream's
// build/conf/opensource.conf gates it on OPENSOURCE, so internal-contour builds
// (where OPENSOURCE is unset) must omit it.
func catboostOpenSourceDefineFor(p *Platform) []ARG {
	if p.Flags[envOPENSOURCE] == strYes {
		return catboostOpenSourceDefine
	}

	return nil
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

// buildDebugInfoFlags mirrors ymake_conf.py's GnuCompiler.debug_info_flags: -g,
// then -gz=zstd for a non-release Linux target IF the source repo's
// build/ymake_conf.py carries that rule (compress; see confCompressesDebug —
// yatool's conf omits it, ydb's has it), then -fdebug-default-version=4 (clang>=14,
// always here) and -ggnu-pubnames (clang && linux). The release-only
// -fdebug-info-for-profiling is not modelled: release targets use hostCFlags, which
// carry no debug-info group.
func buildDebugInfoFlags(os OS, release, compress bool) []ARG {
	out := make([]ARG, 0, 4)
	out = append(out, argDashG)

	if compress && !release && os == OSLinux {
		out = append(out, argGzZstd)
	}

	out = append(out, argFdebugDefaultVersion4)

	if os == OSLinux {
		out = append(out, argGgnuPubnames)
	}

	return out
}

// composeCompileCFlags builds the platform's compile C flag vector once, splicing
// the debug-info group into its natural slot (Pre + debugInfo + Post). Release x86
// uses hostCFlags, which has no debug-info group.
func composeCompileCFlags(isa ISA, release bool, debugInfo []ARG) []ARG {
	switch isa {
	case ISAX8664:
		if release {
			return hostCFlags
		}

		return concatARG(x86TargetCFlagsPre, debugInfo, x86TargetCFlagsPost)
	case ISAAArch64:
		return concatARG(commonCFlagsPre, debugInfo, commonCFlagsPost)
	}

	return nil
}

func concatARG(parts ...[]ARG) []ARG {
	n := 0

	for _, p := range parts {
		n += len(p)
	}

	out := make([]ARG, 0, n)

	for _, p := range parts {
		out = append(out, p...)
	}

	return out
}

func compileFlagBundleFor(p *Platform) compileFlagBundle {
	switch p.ISA {
	case ISAX8664:
		return compileFlagBundle{
			CFlags:      p.CompileCFlags,
			Defines:     hostDefines,
			NoLibcBlock: noLibcBlock(p),
		}
	case ISAAArch64:
		bundle := compileFlagBundle{
			CFlags:      p.CompileCFlags,
			Defines:     commonDefines,
			NoLibcBlock: noLibcBlock(p),
		}

		bundle.ArchArgs = p.MarchArgs

		return bundle
	}

	ThrowFmt("compileFlagBundleFor: unsupported platform ISA %q", p.ISA)
	return compileFlagBundle{}
}
