package main

import "strings"

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

	switch p.ifEnv.string(envBUILD_TYPE) {
	case "RELEASE", "MINSIZEREL", "VALGRIND-RELEASE", "PROFILE", "GPROF", "DEBUGNOASSERTS":
		out = append(out, argNDEBUG.any())
	default:
		out = append(out, argUNDEBUG.any())
	}

	if p.ISA == ISAAArch64 || p.ISA == ISAArm64 {
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

func composeCompileCFlags(env Environment, flags map[string]string, debugInfo []ANY) []ANY {
	return concat(expandCompilerConfFlags(env, flags["_C_FLAGS"]), debugInfo, expandCompilerConfFlags(env, flags["_C_FOPTIONS"]))
}

func appendBaseUnitSanitizerCFlags(flags []ANY, env Environment) []ANY {
	sanitizer := env.string(envSANITIZER_TYPE)

	if sanitizer == "" || sanitizer == "no" || !env.bool(envEnvBaseUnit) {
		return flags
	}

	return concat(flags, []ANY{internAny("-fsanitize=" + sanitizer), internAny("-D" + sanitizer + "_sanitizer_enabled"), strSanitizerCflags.any(), strFnoOmitFramePointer.any()})
}

func expandCompilerConfFlags(env Environment, value string) []ANY {
	words := splitShellWords(value)
	out := make([]ANY, 0, len(words))

	for _, w := range words {
		switch w {
		case "$CL_DEBUG_INFO", "$CL_DEBUG_INFO_DISABLE_CACHE__NO_UID__":
		case "$OPTIMIZE":
			out = append(out, optimizeFlags(env)...)
		case "$FSTACK":
			out = append(out, argFstackProtector.any())
		default:
			if strings.HasPrefix(w, "$") {
				throwFmt("ymake conf: compiler flag %s is not modelled", w)
			}

			out = append(out, internAny(w))
		}
	}

	return out
}

func optimizeFlags(env Environment) []ANY {
	switch {
	case env.string(envBuildRelease) != "yes":
		return nil
	case env.string(envBuildSizeOptimized) != "yes":
		return []ANY{argO3.any()}
	case env.string(envCLANG) == "yes":
		return []ANY{argOz.any()}
	}

	return []ANY{argOs.any()}
}

func compileFlagBundleFor(p *Platform) CompileFlagBundle {
	switch p.ISA {
	case ISAX8664:
		return CompileFlagBundle{
			CFlags:      p.CompileCFlags,
			Defines:     hostDefines,
			NoLibcBlock: p.NoLibcBlock,
		}
	case ISAAArch64, ISAArm64:
		bundle := CompileFlagBundle{
			CFlags:      p.CompileCFlags,
			Defines:     commonDefines,
			NoLibcBlock: p.NoLibcBlock,
		}

		bundle.ArchArgs = p.MarchArgs

		return bundle
	}

	panic(exceptionf("compileFlagBundleFor: unsupported platform ISA %q", p.ISA))
}
