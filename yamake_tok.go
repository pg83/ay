package main

var tokName = [...]STR{
	tokInvalid:                            0,
	tokAddInclSelf:                        internStr("ADDINCLSELF"),
	tokAliceCapability:                    internStr("ALICE_CAPABILITY"),
	tokAliceTypedCallback:                 internStr("ALICE_TYPED_CALLBACK"),
	tokAllocator:                          internStr("ALLOCATOR"),
	tokAllocatorImpl:                      internStr("ALLOCATOR_IMPL"),
	tokAllPySrcs:                          internStr("ALL_PY_SRCS"),
	tokApphost:                            internStr("APPHOST"),
	tokArchive:                            internStr("ARCHIVE"),
	tokArchiveAsm:                         internStr("ARCHIVE_ASM"),
	tokArPlugin:                           internStr("AR_PLUGIN"),
	tokBaseCodegen:                        internStr("BASE_CODEGEN"),
	tokBisonGenC:                          internStr("BISON_GEN_C"),
	tokBisonGenCpp:                        internStr("BISON_GEN_CPP"),
	tokBuildwithCythonC:                   internStr("BUILDWITH_CYTHON_C"),
	tokBuildwithCythonCpp:                 internStr("BUILDWITH_CYTHON_CPP"),
	tokBuildOnlyIf:                        internStr("BUILD_ONLY_IF"),
	tokBuildMn:                            internStr("BUILD_MN"),
	tokCheckConfigH:                       internStr("CHECK_CONFIG_H"),
	tokCheckDependentDirs:                 internStr("CHECK_DEPENDENT_DIRS"),
	tokClangWarnings:                      internStr("CLANG_WARNINGS"),
	tokCopy:                               internStr("COPY"),
	tokCopyFile:                           internStr("COPY_FILE"),
	tokCopyFileWithContext:                internStr("COPY_FILE_WITH_CONTEXT"),
	tokCppEvlog:                           internStr("CPP_EVLOG"),
	tokCppProtoPlugin:                     internStr("CPP_PROTO_PLUGIN"),
	tokCppProtoPlugin0:                    internStr("CPP_PROTO_PLUGIN0"),
	tokCppProtoPlugin2:                    internStr("CPP_PROTO_PLUGIN2"),
	tokCudaNvccFlags:                      internStr("CUDA_NVCC_FLAGS"),
	tokSetAppendWithGlobal:                internStr("SET_APPEND_WITH_GLOBAL"),
	tokData:                               internStr("DATA"),
	tokDeclareInDirs:                      internStr("DECLARE_IN_DIRS"),
	tokDeclareExternalResource:            kwDECLARE_EXTERNAL_RESOURCE,
	tokDeclareExternalHostResourcesBundle: kwDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE,
	tokDeclareExternalHostResourcesBundleByJson: kwDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON,
	tokDefault:                         kwDEFAULT,
	tokDefineVariable:                  internStr("DEFINE_VARIABLE"),
	tokDisable:                         internStr("DISABLE"),
	tokDisableDataValidation:           internStr("DISABLE_DATA_VALIDATION"),
	tokDll:                             kwDLL,
	tokDllTool:                         internStr("DLL_TOOL"),
	tokDynamicLibrary:                  kwDYNAMIC_LIBRARY,
	tokDynamicLibraryFrom:              internStr("DYNAMIC_LIBRARY_FROM"),
	tokEnable:                          internStr("ENABLE"),
	tokEnv:                             kwENV,
	tokExcludeTags:                     internStr("EXCLUDE_TAGS"),
	tokExportsScript:                   internStr("EXPORTS_SCRIPT"),
	tokExtralibs:                       internStr("EXTRALIBS"),
	tokExtralibsStatic:                 internStr("EXTRALIBS_STATIC"),
	tokFatal:                           internStr("FATAL"),
	tokFbsLibrary:                      internStr("FBS_LIBRARY"),
	tokFiles:                           kwFILES,
	tokFlatcFlags:                      internStr("FLATC_FLAGS"),
	tokForkSubtests:                    internStr("FORK_SUBTESTS"),
	tokForkTests:                       internStr("FORK_TESTS"),
	tokFromSandbox:                     internStr("FROM_SANDBOX"),
	tokGlobalCflags:                    internStr("GLOBAL_CFLAGS"),
	tokGoProtoPlugin:                   internStr("GO_PROTO_PLUGIN"),
	tokGrpc:                            internStr("GRPC"),
	tokHeaders:                         internStr("HEADERS"),
	tokIdeFolder:                       internStr("IDE_FOLDER"),
	tokIncludeTags:                     internStr("INCLUDE_TAGS"),
	tokInducedDeps:                     kwINDUCED_DEPS,
	tokJavaClasspathIgnoreConflictz:    internStr("JAVA_CLASSPATH_IGNORE_CONFLICTZ"),
	tokJavaProtoPlugin:                 internStr("JAVA_PROTO_PLUGIN"),
	tokJavaSrcs:                        internStr("JAVA_SRCS"),
	tokLdPlugin:                        internStr("LD_PLUGIN"),
	tokLibrary:                         kwLIBRARY,
	tokLicense:                         internStr("LICENSE"),
	tokLicenseRestriction:              internStr("LICENSE_RESTRICTION"),
	tokLicenseRestrictionExceptions:    internStr("LICENSE_RESTRICTION_EXCEPTIONS"),
	tokLicenseTexts:                    internStr("LICENSE_TEXTS"),
	tokLint:                            internStr("LINT"),
	tokListProto:                       internStr("LIST_PROTO"),
	tokLlvmBc:                          internStr("LLVM_BC"),
	tokManualGeneration:                internStr("MANUAL_GENERATION"),
	tokMasmflags:                       internStr("MASMFLAGS"),
	tokMavenGroupId:                    internStr("MAVEN_GROUP_ID"),
	tokMessage:                         internStr("MESSAGE"),
	tokNeedCheck:                       internStr("NEED_CHECK"),
	tokNeedReview:                      internStr("NEED_REVIEW"),
	tokNoBuildIf:                       internStr("NO_BUILD_IF"),
	tokNoCheckImports:                  internStr("NO_CHECK_IMPORTS"),
	tokNoClangCoverage:                 internStr("NO_CLANG_COVERAGE"),
	tokNoClangMcdcCoverage:             internStr("NO_CLANG_MCDC_COVERAGE"),
	tokNoClangTidy:                     internStr("NO_CLANG_TIDY"),
	tokNoCompilerWarnings:              internStr("NO_COMPILER_WARNINGS"),
	tokNoExportDynamicSymbols:          internStr("NO_EXPORT_DYNAMIC_SYMBOLS"),
	tokNoExtendedSourceSearch:          internStr("NO_EXTENDED_SOURCE_SEARCH"),
	tokNoImportTracing:                 internStr("NO_IMPORT_TRACING"),
	tokNoJoinSrc:                       internStr("NO_JOIN_SRC"),
	tokNoLibc:                          internStr("NO_LIBC"),
	tokNoLint:                          internStr("NO_LINT"),
	tokNoLto:                           internStr("NO_LTO"),
	tokNoMypy:                          internStr("NO_MYPY"),
	tokNoOptimize:                      internStr("NO_OPTIMIZE"),
	tokNoOptimizePyProtos:              internStr("NO_OPTIMIZE_PY_PROTOS"),
	tokNoPlatform:                      internStr("NO_PLATFORM"),
	tokNoPlatformResources:             internStr("NO_PLATFORM_RESOURCES"),
	tokNoProfileRuntime:                internStr("NO_PROFILE_RUNTIME"),
	tokNoPython2:                       internStr("NO_PYTHON2"),
	tokNoPythonCoverage:                internStr("NO_PYTHON_COVERAGE"),
	tokNoPythonIncludes:                internStr("NO_PYTHON_INCLUDES"),
	tokNoRuntime:                       internStr("NO_RUNTIME"),
	tokNoSanitize:                      internStr("NO_SANITIZE"),
	tokNoSanitizeCoverage:              internStr("NO_SANITIZE_COVERAGE"),
	tokNoSplitDwarf:                    internStr("NO_SPLIT_DWARF"),
	tokNoUtil:                          internStr("NO_UTIL"),
	tokNoWshadow:                       internStr("NO_WSHADOW"),
	tokNoYmakePython:                   internStr("NO_YMAKE_PYTHON"),
	tokNoYmakePython3:                  internStr("NO_YMAKE_PYTHON3"),
	tokOnlyTags:                        internStr("ONLY_TAGS"),
	tokOpensourceExportReplacement:     internStr("OPENSOURCE_EXPORT_REPLACEMENT"),
	tokOpensourceExportReplacementByOs: internStr("OPENSOURCE_EXPORT_REPLACEMENT_BY_OS"),
	tokOpensourceProject:               internStr("OPENSOURCE_PROJECT"),
	tokOptimizePyProtos:                internStr("OPTIMIZE_PY_PROTOS"),
	tokOriginalSource:                  internStr("ORIGINAL_SOURCE"),
	tokOwner:                           internStr("OWNER"),
	tokPackage:                         kwPACKAGE,
	tokPrebuiltProgram:                 internStr("PREBUILT_PROGRAM"),
	tokPrimaryOutput:                   internStr("PRIMARY_OUTPUT"),
	tokProgram:                         kwPROGRAM,
	tokProtocFatalWarnings:             internStr("PROTOC_FATAL_WARNINGS"),
	tokProtoLibrary:                    kwPROTO_LIBRARY,
	tokProtoNamespace:                  internStr("PROTO_NAMESPACE"),
	tokProvides:                        internStr("PROVIDES"),
	tokPy23Library:                     kwPY23_LIBRARY,
	tokPy23NativeLibrary:               kwPY23_NATIVE_LIBRARY,
	tokPy2Library:                      kwPY2_LIBRARY,
	tokPy2Program:                      kwPY2_PROGRAM,
	tokPy3Library:                      kwPY3_LIBRARY,
	tokPy3Program:                      kwPY3_PROGRAM,
	tokPy3ProgramBin:                   kwPY3_PROGRAM_BIN,
	tokPython3:                         internStr("PYTHON3"),
	tokPython3Addincl:                  internStr("PYTHON3_ADDINCL"),
	tokPyConstructor:                   internStr("PY_CONSTRUCTOR"),
	tokPyMain:                          internStr("PY_MAIN"),
	tokPyNamespace:                     internStr("PY_NAMESPACE"),
	tokPyRegister:                      internStr("PY_REGISTER"),
	tokPySrcs:                          internStr("PY_SRCS"),
	tokRecurse:                         internStr("RECURSE"),
	tokRecurseForTests:                 internStr("RECURSE_FOR_TESTS"),
	tokRecurseRootRelative:             internStr("RECURSE_ROOT_RELATIVE"),
	tokRequirements:                    internStr("REQUIREMENTS"),
	tokRunProtoShaperStub:              internStr("RUN_PROTO_SHAPER_STUB"),
	tokSourceGroup:                     internStr("SOURCE_GROUP"),
	tokTsProtoOpt:                      internStr("TS_PROTO_OPT"),
	tokResourcesLibrary:                kwRESOURCES_LIBRARY,
	tokRestrictPath:                    internStr("RESTRICT_PATH"),
	tokSetAppend:                       internStr("SET_APPEND"),
	tokSetResourceUriFromJson:          internStr("SET_RESOURCE_URI_FROM_JSON"),
	tokSize:                            internStr("SIZE"),
	tokSoProgram:                       kwSO_PROGRAM,
	tokSplitDwarf:                      internStr("SPLIT_DWARF"),
	tokSplitFactor:                     internStr("SPLIT_FACTOR"),
	tokSrc:                             internStr("SRC"),
	tokSrcCAmx:                         internStr("SRC_C_AMX"),
	tokSrcCAvx:                         internStr("SRC_C_AVX"),
	tokSrcCAvx2:                        internStr("SRC_C_AVX2"),
	tokSrcCAvx512:                      internStr("SRC_C_AVX512"),
	tokSrcCNoLto:                       internStr("SRC_C_NO_LTO"),
	tokSrcCSse2:                        internStr("SRC_C_SSE2"),
	tokSrcCSse3:                        internStr("SRC_C_SSE3"),
	tokSrcCSse4:                        internStr("SRC_C_SSE4"),
	tokSrcCSse41:                       internStr("SRC_C_SSE41"),
	tokSrcCSsse3:                       internStr("SRC_C_SSSE3"),
	tokSrcCXop:                         internStr("SRC_C_XOP"),
	tokStrip:                           kwSTRIP,
	tokStyleCpp:                        internStr("STYLE_CPP"),
	tokStyleCppYt:                      internStr("STYLE_CPP_YT"),
	tokStylePython:                     internStr("STYLE_PYTHON"),
	tokStyleRuff:                       internStr("STYLE_RUFF"),
	tokSubscriber:                      internStr("SUBSCRIBER"),
	tokSuppressions:                    internStr("SUPPRESSIONS"),
	tokTag:                             internStr("TAG"),
	tokTasklet:                         internStr("TASKLET"),
	tokTaskletsupport:                  internStr("TASKLETSUPPORT"),
	tokTestSrcs:                        internStr("TEST_SRCS"),
	tokTimeout:                         internStr("TIMEOUT"),
	tokToolchain:                       internStr("TOOLCHAIN"),
	tokUnion:                           kwUNION,
	tokUnittestFor:                     kwUNITTEST_FOR,
	tokUseCommonGoogleApis:             internStr("USE_COMMON_GOOGLE_APIS"),
	tokUseCxx:                          internStr("USE_CXX"),
	tokUseLightPy2cc:                   internStr("USE_LIGHT_PY2CC"),
	tokUseLlvmBc16:                     internStr("USE_LLVM_BC16"),
	tokUseLlvmBc18:                     internStr("USE_LLVM_BC18"),
	tokUseLlvmBc20:                     internStr("USE_LLVM_BC20"),
	tokUsePython3:                      internStr("USE_PYTHON3"),
	tokVersion:                         internStr("VERSION"),
	tokWindowsLongPathManifest:         internStr("WINDOWS_LONG_PATH_MANIFEST"),
	tokWithoutLicenseTexts:             internStr("WITHOUT_LICENSE_TEXTS"),
	tokWithoutVersion:                  internStr("WITHOUT_VERSION"),
	tokWithKotlinGrpc:                  internStr("WITH_KOTLIN_GRPC"),
	tokYaff:                            internStr("YAFF"),
	tokYaffSchema:                      internStr("YAFF_SCHEMA"),
	tokYaConfJson:                      internStr("YA_CONF_JSON"),
	tokYqlAbiVersion:                   internStr("YQL_ABI_VERSION"),
	tokYqlLastAbiVersion:               internStr("YQL_LAST_ABI_VERSION"),
	tokYqlUdfContrib:                   kwYQL_UDF_CONTRIB,
	tokYqlUdfYdb:                       kwYQL_UDF_YDB,
	tokGoLibrary:                       internStr("GO_LIBRARY"),
	tokGoPackageName:                   internStr("GO_PACKAGE_NAME"),
	tokGoGrpcGatewayV2OpenapiSrcs:      internStr("GO_GRPC_GATEWAY_V2_OPENAPI_SRCS"),
	tokGoProgram:                       internStr("GO_PROGRAM"),
	tokGoTestSrcs:                      internStr("GO_TEST_SRCS"),
	tokGoXtestSrcs:                     internStr("GO_XTEST_SRCS"),
	tokGoSkipTests:                     internStr("GO_SKIP_TESTS"),
	tokGoEmbedPattern:                  internStr("GO_EMBED_PATTERN"),
	tokCgoSrcs:                         internStr("CGO_SRCS"),
	tokCgoLdflags:                      internStr("CGO_LDFLAGS"),
	tokCgoCflags:                       internStr("CGO_CFLAGS"),

	tokAllResourceFiles:      internStr("ALL_RESOURCE_FILES"),
	tokArchiveByKeys:         internStr("ARCHIVE_BY_KEYS"),
	tokBisonFlags:            internStr("BISON_FLAGS"),
	tokBundle:                internStr("BUNDLE"),
	tokCppEnumsSerialization: internStr("CPP_ENUMS_SERIALIZATION"),
	tokDecimalMd5Lower32Bits: internStr("DECIMAL_MD5_LOWER_32_BITS"),
	tokDefaultJdkVersion:     internStr("DEFAULT_JDK_VERSION"),
	tokExportYmapsProto:      internStr("EXPORT_YMAPS_PROTO"),
	tokLj21Archive:           internStr("LJ_21_ARCHIVE"),
	tokProtoDescriptions:     internStr("PROTO_DESCRIPTIONS"),
	tokSplitCodegen:          internStr("SPLIT_CODEGEN"),
	tokStructCodegen:         internStr("STRUCT_CODEGEN"),
	tokStyleDetekt:           internStr("STYLE_DETEKT"),
	tokYmapsSproto:           internStr("YMAPS_SPROTO"),
}

var tokByName = func() map[string]TOK {
	m := make(map[string]TOK, len(tokName))

	for t, s := range tokName {
		if s != 0 {
			m[s.string()] = TOK(t)
		}
	}

	return m
}()

var tokBySTR = func() []TOK {
	out := make([]TOK, strBound())

	for t, s := range tokName {
		if s != 0 {
			out[s] = TOK(t)
		}
	}

	return out
}()

const (
	tokInvalid TOK = iota
	tokAddInclSelf
	tokAliceCapability
	tokAliceTypedCallback
	tokAllocator
	tokAllocatorImpl
	tokAllPySrcs
	tokApphost
	tokArchive
	tokArchiveAsm
	tokArPlugin
	tokBaseCodegen
	tokBisonGenC
	tokBisonGenCpp
	tokBuildwithCythonC
	tokBuildwithCythonCpp
	tokBuildOnlyIf
	tokBuildMn
	tokCheckConfigH
	tokCheckDependentDirs
	tokClangWarnings
	tokCopy
	tokCopyFile
	tokCopyFileWithContext
	tokCppEvlog
	tokCppProtoPlugin
	tokCppProtoPlugin0
	tokCppProtoPlugin2
	tokData
	tokDeclareInDirs
	tokDeclareExternalResource
	tokDeclareExternalHostResourcesBundle
	tokDeclareExternalHostResourcesBundleByJson
	tokDefault
	tokDefineVariable
	tokDisable
	tokDisableDataValidation
	tokDll
	tokDynamicLibrary
	tokDynamicLibraryFrom
	tokEnable
	tokEnv
	tokExcludeTags
	tokExportsScript
	tokExtralibs
	tokExtralibsStatic
	tokFatal
	tokFbsLibrary
	tokFiles
	tokFlatcFlags
	tokForkSubtests
	tokForkTests
	tokFromSandbox
	tokGlobalCflags
	tokGoProtoPlugin
	tokGrpc
	tokHeaders
	tokIdeFolder
	tokIncludeTags
	tokInducedDeps
	tokJavaClasspathIgnoreConflictz
	tokJavaProtoPlugin
	tokJavaSrcs
	tokLdPlugin
	tokLibrary
	tokLicense
	tokLicenseRestriction
	tokLicenseRestrictionExceptions
	tokLicenseTexts
	tokLint
	tokListProto
	tokLlvmBc
	tokManualGeneration
	tokMasmflags
	tokMavenGroupId
	tokMessage
	tokNeedCheck
	tokNeedReview
	tokNoBuildIf
	tokNoCheckImports
	tokNoClangCoverage
	tokNoClangMcdcCoverage
	tokNoClangTidy
	tokNoCompilerWarnings
	tokNoExportDynamicSymbols
	tokNoExtendedSourceSearch
	tokNoImportTracing
	tokNoJoinSrc
	tokNoLibc
	tokNoLint
	tokNoLto
	tokNoMypy
	tokNoOptimize
	tokNoOptimizePyProtos
	tokNoPlatform
	tokNoPlatformResources
	tokNoProfileRuntime
	tokNoPython2
	tokNoPythonCoverage
	tokNoPythonIncludes
	tokNoRuntime
	tokNoSanitize
	tokNoSanitizeCoverage
	tokNoSplitDwarf
	tokNoUtil
	tokNoWshadow
	tokNoYmakePython
	tokNoYmakePython3
	tokOnlyTags
	tokOpensourceExportReplacement
	tokOpensourceExportReplacementByOs
	tokOpensourceProject
	tokOptimizePyProtos
	tokOriginalSource
	tokOwner
	tokPackage
	tokPrebuiltProgram
	tokPrimaryOutput
	tokProgram
	tokProtocFatalWarnings
	tokProtoLibrary
	tokProtoNamespace
	tokProvides
	tokPy23Library
	tokPy23NativeLibrary
	tokPy2Library
	tokPy2Program
	tokPy3Library
	tokPy3Program
	tokPy3ProgramBin
	tokPython3
	tokPython3Addincl
	tokPyConstructor
	tokPyMain
	tokPyNamespace
	tokPyRegister
	tokPySrcs
	tokRecurse
	tokRecurseForTests
	tokRecurseRootRelative
	tokRequirements
	tokResourcesLibrary
	tokRestrictPath
	tokRunProtoShaperStub
	tokSourceGroup
	tokTsProtoOpt
	tokSetAppend
	tokSetResourceUriFromJson
	tokSize
	tokSoProgram
	tokSplitDwarf
	tokSplitFactor
	tokSrc
	tokSrcCAmx
	tokSrcCAvx
	tokSrcCAvx2
	tokSrcCAvx512
	tokSrcCNoLto
	tokSrcCSse2
	tokSrcCSse3
	tokSrcCSse4
	tokSrcCSse41
	tokSrcCSsse3
	tokSrcCXop
	tokStrip
	tokStyleCpp
	tokStyleCppYt
	tokStylePython
	tokStyleRuff
	tokSubscriber
	tokSuppressions
	tokTag
	tokTasklet
	tokTaskletsupport
	tokTestSrcs
	tokTimeout
	tokToolchain
	tokUnion
	tokUnittestFor
	tokUseCommonGoogleApis
	tokUseCxx
	tokUseLightPy2cc
	tokUseLlvmBc16
	tokUseLlvmBc18
	tokUseLlvmBc20
	tokUsePython3
	tokVersion
	tokWindowsLongPathManifest
	tokWithoutLicenseTexts
	tokWithoutVersion
	tokWithKotlinGrpc
	tokYaff
	tokYaffSchema
	tokYaConfJson
	tokYqlAbiVersion
	tokYqlLastAbiVersion
	tokYqlUdfContrib
	tokYqlUdfYdb
	tokGoLibrary
	tokGoPackageName
	tokGoGrpcGatewayV2OpenapiSrcs
	tokGoProgram
	tokGoTestSrcs
	tokGoXtestSrcs
	tokGoSkipTests
	tokGoEmbedPattern
	tokCgoSrcs
	tokCgoLdflags
	tokCgoCflags

	tokAllResourceFiles
	tokArchiveByKeys
	tokBisonFlags
	tokBundle
	tokCppEnumsSerialization
	tokDecimalMd5Lower32Bits
	tokDefaultJdkVersion
	tokExportYmapsProto
	tokLj21Archive
	tokProtoDescriptions
	tokSplitCodegen
	tokStructCodegen
	tokStyleDetekt
	tokYmapsSproto
	tokCudaNvccFlags
	tokSetAppendWithGlobal
	tokDllTool
)

type TOK uint16

func internTok(s string) TOK {
	if t, ok := tokByName[s]; ok {
		return t
	}

	throwFmt("internTok: unknown macro name %q (closed TOK set)", s)

	return tokInvalid
}

func internTokMaybe(s string) TOK {
	return tokByName[s]
}

func internTokSTR(s STR) TOK {
	if int(s) < len(tokBySTR) {
		if t := tokBySTR[s]; t != tokInvalid {
			return t
		}
	}

	throwFmt("internTokSTR: unknown macro name %q (closed TOK set)", s.string())

	return tokInvalid
}

func internTokMaybeSTR(s STR) TOK {
	if int(s) < len(tokBySTR) {
		return tokBySTR[s]
	}

	return tokInvalid
}

func (t TOK) str() STR {
	return tokName[t]
}

func (t TOK) string() string {
	return tokName[t].string()
}

func (t TOK) String() string {
	return t.string()
}
