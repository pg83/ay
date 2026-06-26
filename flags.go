package main

var ccIncludesPrefix = []ARG{
	argIB,
	argIS,
}

var (
	ccIncludesPrefixStr          = argSTRs(ccIncludesPrefix)
	debugPrefixMapFlagsStr       = argSTRs(debugPrefixMapFlags)
	xclangDebugCompilationDirStr = argSTRs(xclangDebugCompilationDir)
	builtinMacroDateTimeStr      = argSTRs(builtinMacroDateTime)
	macroPrefixMapFlagsStr       = argSTRs(macroPrefixMapFlags)
	noWarningsBundleStr          = argSTRs(noWarningsBundle)
	cxxStandardWarningsStr       = argSTRs(cxxStandardWarnings)
	warningFlagsStr              = argSTRs(warningFlags)
	catboostOpenSourceDefineStr  = argSTRs(catboostOpenSourceDefine)
	cxxStandardFlagStr           = []STR{cxxStandardFlag.str()}
)

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

type CompileFlagBundle struct {
	ArchArgs    []ARG
	CFlags      []ARG
	Defines     []ARG
	NoLibcBlock []ARG
}

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

func composeCompileCFlags(isa ISA, release bool, debugInfo []ARG) []ARG {
	switch isa {
	case ISAX8664:
		if release {
			return hostCFlags
		}

		return concat(x86TargetCFlagsPre, debugInfo, x86TargetCFlagsPost)
	case ISAAArch64:
		return concat(commonCFlagsPre, debugInfo, commonCFlagsPost)
	}

	return nil
}

func compileFlagBundleFor(p *Platform) CompileFlagBundle {
	switch p.ISA {
	case ISAX8664:
		return CompileFlagBundle{
			CFlags:      p.CompileCFlags,
			Defines:     hostDefines,
			NoLibcBlock: noLibcBlock(p),
		}
	case ISAAArch64:
		bundle := CompileFlagBundle{
			CFlags:      p.CompileCFlags,
			Defines:     commonDefines,
			NoLibcBlock: noLibcBlock(p),
		}

		bundle.ArchArgs = p.MarchArgs

		return bundle
	}

	throwFmt("compileFlagBundleFor: unsupported platform ISA %q", p.ISA)

	return CompileFlagBundle{}
}
