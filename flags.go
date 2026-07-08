package main

var cxxStandardFlagChunk = []ANY{cxxStandardFlag.any()}

var ccIncludesPrefix = []ANY{
	argIB.any(),
	argIS.any(),
}

var debugPrefixMapFlags = []ANY{
	argFdebugPrefixMapBB.any(),
	argFdebugPrefixMapSS.any(),
	argFdebugPrefixMapToolRootT.any(),
}

var xclangDebugCompilationDir = []ANY{
	argXclang.any(),
	argFdebugCompilationDir.any(),
	argXclang.any(),
	argTmp.any(),
}

var commonCFlagsPre = []ANY{
	argPipe.any(),
}

var commonCFlagsPost = []ANY{
	argFnoCommon.any(),
	argFfunctionSections.any(),
	argFdataSections.any(),
	argFsignedChar.any(),
	argFsizedDeallocation.any(),
	argFexceptions.any(),
	argFuseInitArray.any(),
	argFcolorDiagnostics.any(),
	argFalignedAllocation.any(),
	argFstackProtector.any(),
}

var x86TargetCFlagsPre = []ANY{
	argPipe.any(),
	argM64.any(),
}

var x86TargetCFlagsPost = []ANY{
	argFnoCommon.any(),
	argFfunctionSections.any(),
	argFdataSections.any(),
	argFsizedDeallocation.any(),
	argFexceptions.any(),
	argFuseInitArray.any(),
	argFcolorDiagnostics.any(),
	argFalignedAllocation.any(),
	argFstackProtector.any(),
}

var hostCFlags = []ANY{
	argPipe.any(),
	argM64.any(),
	argO3.any(),
	argFnoCommon.any(),
	argFfunctionSections.any(),
	argFdataSections.any(),
	argFsizedDeallocation.any(),
	argFexceptions.any(),
	argFuseInitArray.any(),
	argFcolorDiagnostics.any(),
	argFalignedAllocation.any(),
}

var warningFlags = []ANY{
	argWerror.any(),
	argWall.any(),
	argWextra.any(),
	argWnoParentheses.any(),
	argWnoImplicitConstIntFloatConversion.any(),
	argWnoUnknownWarningOption.any(),
}

var commonDefines = []ANY{
	argDarcadiaRootS.any(),
	argDarcadiaBuildRootB.any(),
	argDThreadSafe.any(),
	argDPthreads.any(),
	argDReentrant.any(),
	argDLargefileSource.any(),
	argDStdcConstantMacros.any(),
	argDStdcFormatMacros.any(),
	argDFileOffsetBits64.any(),
	argDGnuSource.any(),
	argDLongLongSupported.any(),
}

var hostDefines = []ANY{
	argDarcadiaRootS.any(),
	argDarcadiaBuildRootB.any(),
	argDThreadSafe.any(),
	argDPthreads.any(),
	argDReentrant.any(),
	argDLargefileSource.any(),
	argDStdcConstantMacros.any(),
	argDStdcFormatMacros.any(),
	argDFileOffsetBits64.any(),
	argDGnuSource.any(),
	argDYndxLibunwindEnableExceptionBacktrace.any(),
	argDLongLongSupported.any(),
}

var hostSseFeatures = []ANY{
	argMsse2.any(),
	argMsse3.any(),
	argMssse3.any(),
	argMsse41.any(),
	argMsse42.any(),
	argMpopcnt.any(),
	argMcx16.any(),
}

var noLibcWarningSuppressions = []ANY{
	argWnoArrayParameter.any(),
	argWnoDeprecateLaxVecConvAll.any(),
	argWnoUnqualifiedStdCastCall.any(),
	argWnoUnusedButSetParameter.any(),
	argWnoImplicitFunctionDeclaration.any(),
	argWnoIntConversion.any(),
	argWnoIncompatibleFunctionPointerTypes.any(),
	argWnoAddressOfPackedMember.any(),
	argWnoDeprecatedThisCapture.any(),
	argWnoMissingDesignatedFieldInitializers.any(),
	argWnoFormat.any(),
	argWnoVlaCxxExtension.any(),
	argWnoInvalidOffsetof.any(),
	argWnoAliasTemplateInDeclarationName.any(),
	argWnoCastFunctionTypeMismatch.any(),
	argWnoExplicitSpecializationStorageClass.any(),
	argWnoExtraneousTemplateHead.any(),
	argWnoMissingTemplateArgListAfterTemplateKw.any(),
	argWnoNontrivialMemcall.any(),
	argWnoStrictPrimaryTemplateShadow.any(),
}

var catboostOpenSourceDefine = []ANY{
	argDcatboostOpensourceYes.any(),
}

var builtinMacroDateTime = []ANY{
	argWnoBuiltinMacroRedefined.any(),
	argDDateJan102019.any(),
	argDTime000000.any(),
}

var macroPrefixMapFlags = []ANY{
	argFmacroPrefixMapB.any(),
	argFmacroPrefixMapS.any(),
	argFmacroPrefixMapToolRoot.any(),
}

var noWarningsBundle = []ANY{
	argWnoEverything.any(),
}

var cxxStandardWarnings = []ANY{
	argWimportPreprocessorDirectivePedantic.any(),
	argWoverloadedVirtual.any(),
	argWnoAmbiguousReversedOperator.any(),
	argWnoDefaultedFunctionDeleted.any(),
	argWnoDeprecatedAnonEnumEnumConversion.any(),
	argWnoDeprecatedEnumEnumConversion.any(),
	argWnoDeprecatedEnumFloatConversion.any(),
	argWnoDeprecatedVolatile.any(),
	argWnoPessimizingMove.any(),
	argWnoUndefinedVarTemplate.any(),
}

const binPath = "/usr/bin"

func catboostOpenSourceDefineFor(p *Platform) []ANY {
	if p.Flags[envOPENSOURCE] == strYes {
		return catboostOpenSourceDefine
	}

	return nil
}

func sseBaseCFlags(x8664 bool) []ANY {
	if x8664 {
		return hostSseFeatures
	}

	return nil
}

func noLibcBlock(p *Platform) []ANY {
	out := make([]ANY, 0, 3+len(noLibcWarningSuppressions))

	if p.BuildRelease {
		out = append(out, argNDEBUG.any())
	} else {
		out = append(out, argUNDEBUG.any())
	}

	if p.ISA == ISAAArch64 {
		out = append(out, argNoOutlineAtomics.any())
	}

	if p.PIC {
		out = append(out, argFPIC.any())
	}

	out = append(out, noLibcWarningSuppressions...)

	return out
}

type CompileFlagBundle struct {
	ArchArgs    []ANY
	CFlags      []ANY
	Defines     []ANY
	NoLibcBlock []ANY
}

func buildDebugInfoFlags(os OS, release, compress bool) []ANY {
	out := make([]ANY, 0, 4)

	out = append(out, argDashG.any())

	if compress && !release && os == OSLinux {
		out = append(out, argGzZstd.any())
	}

	out = append(out, argFdebugDefaultVersion4.any())

	if os == OSLinux {
		out = append(out, argGgnuPubnames.any())
	}

	return out
}

func composeCompileCFlags(isa ISA, release bool, debugInfo []ANY) []ANY {
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
			NoLibcBlock: p.NoLibcBlock,
		}
	case ISAAArch64:
		bundle := CompileFlagBundle{
			CFlags:      p.CompileCFlags,
			Defines:     commonDefines,
			NoLibcBlock: p.NoLibcBlock,
		}

		bundle.ArchArgs = p.MarchArgs

		return bundle
	}

	throwFmt("compileFlagBundleFor: unsupported platform ISA %q", p.ISA)

	return CompileFlagBundle{}
}
