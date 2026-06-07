package main

type TOK uint16

const (
	tokInvalid TOK = iota
	tokAllocator
	tokAllocatorImpl
	tokAllPySrcs
	tokArchive
	tokArPlugin
	tokBisonGenC
	tokBisonGenCpp
	tokBuildwithCythonC
	tokBuildwithCythonCpp
	tokBuildOnlyIf
	tokCheckConfigH
	tokCheckDependentDirs
	tokCopy
	tokCopyFile
	tokCopyFileWithContext
	tokCppProtoPlugin
	tokCppProtoPlugin0
	tokCppProtoPlugin2
	tokData
	tokDeclareExternalResource
	tokDefault
	tokDefineVariable
	tokDisable
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
	tokFiles
	tokFlatcFlags
	tokForkSubtests
	tokForkTests
	tokGlobalCflags
	tokGrpc
	tokHeaders
	tokIdeFolder
	tokIncludeTags
	tokInducedDeps
	tokJavaClasspathIgnoreConflictz
	tokJavaSrcs
	tokLdPlugin
	tokLibrary
	tokLicense
	tokLicenseRestriction
	tokLicenseRestrictionExceptions
	tokLicenseTexts
	tokLint
	tokLlvmBc
	tokManualGeneration
	tokMasmflags
	tokMavenGroupId
	tokMessage
	tokNeedCheck
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
	tokStylePython
	tokStyleRuff
	tokSubscriber
	tokSuppressions
	tokTag
	tokTasklet
	tokTaskletsupport
	tokTestSrcs
	tokTimeout
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
	tokYaConfJson
	tokYqlAbiVersion
	tokYqlLastAbiVersion
	tokYqlUdfContrib
	tokYqlUdfYdb
)

var tokName = [...]string{
	tokInvalid:                         "",
	tokAllocator:                       "ALLOCATOR",
	tokAllocatorImpl:                   "ALLOCATOR_IMPL",
	tokAllPySrcs:                       "ALL_PY_SRCS",
	tokArchive:                         "ARCHIVE",
	tokArPlugin:                        "AR_PLUGIN",
	tokBisonGenC:                       "BISON_GEN_C",
	tokBisonGenCpp:                     "BISON_GEN_CPP",
	tokBuildwithCythonC:                "BUILDWITH_CYTHON_C",
	tokBuildwithCythonCpp:              "BUILDWITH_CYTHON_CPP",
	tokBuildOnlyIf:                     "BUILD_ONLY_IF",
	tokCheckConfigH:                    "CHECK_CONFIG_H",
	tokCheckDependentDirs:              "CHECK_DEPENDENT_DIRS",
	tokCopy:                            "COPY",
	tokCopyFile:                        "COPY_FILE",
	tokCopyFileWithContext:             "COPY_FILE_WITH_CONTEXT",
	tokCppProtoPlugin:                  "CPP_PROTO_PLUGIN",
	tokCppProtoPlugin0:                 "CPP_PROTO_PLUGIN0",
	tokCppProtoPlugin2:                 "CPP_PROTO_PLUGIN2",
	tokData:                            "DATA",
	tokDeclareExternalResource:         "DECLARE_EXTERNAL_RESOURCE",
	tokDefault:                         "DEFAULT",
	tokDefineVariable:                  "DEFINE_VARIABLE",
	tokDisable:                         "DISABLE",
	tokDll:                             "DLL",
	tokDynamicLibrary:                  "DYNAMIC_LIBRARY",
	tokDynamicLibraryFrom:              "DYNAMIC_LIBRARY_FROM",
	tokEnable:                          "ENABLE",
	tokEnv:                             "ENV",
	tokExcludeTags:                     "EXCLUDE_TAGS",
	tokExportsScript:                   "EXPORTS_SCRIPT",
	tokExtralibs:                       "EXTRALIBS",
	tokExtralibsStatic:                 "EXTRALIBS_STATIC",
	tokFatal:                           "FATAL",
	tokFiles:                           "FILES",
	tokFlatcFlags:                      "FLATC_FLAGS",
	tokForkSubtests:                    "FORK_SUBTESTS",
	tokForkTests:                       "FORK_TESTS",
	tokGlobalCflags:                    "GLOBAL_CFLAGS",
	tokGrpc:                            "GRPC",
	tokHeaders:                         "HEADERS",
	tokIdeFolder:                       "IDE_FOLDER",
	tokIncludeTags:                     "INCLUDE_TAGS",
	tokInducedDeps:                     "INDUCED_DEPS",
	tokJavaClasspathIgnoreConflictz:    "JAVA_CLASSPATH_IGNORE_CONFLICTZ",
	tokJavaSrcs:                        "JAVA_SRCS",
	tokLdPlugin:                        "LD_PLUGIN",
	tokLibrary:                         "LIBRARY",
	tokLicense:                         "LICENSE",
	tokLicenseRestriction:              "LICENSE_RESTRICTION",
	tokLicenseRestrictionExceptions:    "LICENSE_RESTRICTION_EXCEPTIONS",
	tokLicenseTexts:                    "LICENSE_TEXTS",
	tokLint:                            "LINT",
	tokLlvmBc:                          "LLVM_BC",
	tokManualGeneration:                "MANUAL_GENERATION",
	tokMasmflags:                       "MASMFLAGS",
	tokMavenGroupId:                    "MAVEN_GROUP_ID",
	tokMessage:                         "MESSAGE",
	tokNeedCheck:                       "NEED_CHECK",
	tokNoBuildIf:                       "NO_BUILD_IF",
	tokNoCheckImports:                  "NO_CHECK_IMPORTS",
	tokNoClangCoverage:                 "NO_CLANG_COVERAGE",
	tokNoClangMcdcCoverage:             "NO_CLANG_MCDC_COVERAGE",
	tokNoClangTidy:                     "NO_CLANG_TIDY",
	tokNoCompilerWarnings:              "NO_COMPILER_WARNINGS",
	tokNoExportDynamicSymbols:          "NO_EXPORT_DYNAMIC_SYMBOLS",
	tokNoExtendedSourceSearch:          "NO_EXTENDED_SOURCE_SEARCH",
	tokNoImportTracing:                 "NO_IMPORT_TRACING",
	tokNoJoinSrc:                       "NO_JOIN_SRC",
	tokNoLibc:                          "NO_LIBC",
	tokNoLint:                          "NO_LINT",
	tokNoLto:                           "NO_LTO",
	tokNoMypy:                          "NO_MYPY",
	tokNoOptimize:                      "NO_OPTIMIZE",
	tokNoOptimizePyProtos:              "NO_OPTIMIZE_PY_PROTOS",
	tokNoPlatform:                      "NO_PLATFORM",
	tokNoProfileRuntime:                "NO_PROFILE_RUNTIME",
	tokNoPython2:                       "NO_PYTHON2",
	tokNoPythonCoverage:                "NO_PYTHON_COVERAGE",
	tokNoPythonIncludes:                "NO_PYTHON_INCLUDES",
	tokNoRuntime:                       "NO_RUNTIME",
	tokNoSanitize:                      "NO_SANITIZE",
	tokNoSanitizeCoverage:              "NO_SANITIZE_COVERAGE",
	tokNoSplitDwarf:                    "NO_SPLIT_DWARF",
	tokNoUtil:                          "NO_UTIL",
	tokNoWshadow:                       "NO_WSHADOW",
	tokNoYmakePython:                   "NO_YMAKE_PYTHON",
	tokOnlyTags:                        "ONLY_TAGS",
	tokOpensourceExportReplacement:     "OPENSOURCE_EXPORT_REPLACEMENT",
	tokOpensourceExportReplacementByOs: "OPENSOURCE_EXPORT_REPLACEMENT_BY_OS",
	tokOpensourceProject:               "OPENSOURCE_PROJECT",
	tokOptimizePyProtos:                "OPTIMIZE_PY_PROTOS",
	tokOriginalSource:                  "ORIGINAL_SOURCE",
	tokOwner:                           "OWNER",
	tokPackage:                         "PACKAGE",
	tokPrebuiltProgram:                 "PREBUILT_PROGRAM",
	tokPrimaryOutput:                   "PRIMARY_OUTPUT",
	tokProgram:                         "PROGRAM",
	tokProtocFatalWarnings:             "PROTOC_FATAL_WARNINGS",
	tokProtoLibrary:                    "PROTO_LIBRARY",
	tokProtoNamespace:                  "PROTO_NAMESPACE",
	tokProvides:                        "PROVIDES",
	tokPy23Library:                     "PY23_LIBRARY",
	tokPy23NativeLibrary:               "PY23_NATIVE_LIBRARY",
	tokPy2Library:                      "PY2_LIBRARY",
	tokPy2Program:                      "PY2_PROGRAM",
	tokPy3Library:                      "PY3_LIBRARY",
	tokPy3Program:                      "PY3_PROGRAM",
	tokPy3ProgramBin:                   "PY3_PROGRAM_BIN",
	tokPython3:                         "PYTHON3",
	tokPython3Addincl:                  "PYTHON3_ADDINCL",
	tokPyConstructor:                   "PY_CONSTRUCTOR",
	tokPyMain:                          "PY_MAIN",
	tokPyNamespace:                     "PY_NAMESPACE",
	tokPyRegister:                      "PY_REGISTER",
	tokPySrcs:                          "PY_SRCS",
	tokRecurse:                         "RECURSE",
	tokRecurseForTests:                 "RECURSE_FOR_TESTS",
	tokRecurseRootRelative:             "RECURSE_ROOT_RELATIVE",
	tokRequirements:                    "REQUIREMENTS",
	tokResourcesLibrary:                "RESOURCES_LIBRARY",
	tokRestrictPath:                    "RESTRICT_PATH",
	tokSetAppend:                       "SET_APPEND",
	tokSetResourceUriFromJson:          "SET_RESOURCE_URI_FROM_JSON",
	tokSize:                            "SIZE",
	tokSoProgram:                       "SO_PROGRAM",
	tokSplitDwarf:                      "SPLIT_DWARF",
	tokSplitFactor:                     "SPLIT_FACTOR",
	tokSrc:                             "SRC",
	tokSrcCAmx:                         "SRC_C_AMX",
	tokSrcCAvx:                         "SRC_C_AVX",
	tokSrcCAvx2:                        "SRC_C_AVX2",
	tokSrcCAvx512:                      "SRC_C_AVX512",
	tokSrcCNoLto:                       "SRC_C_NO_LTO",
	tokSrcCSse2:                        "SRC_C_SSE2",
	tokSrcCSse3:                        "SRC_C_SSE3",
	tokSrcCSse4:                        "SRC_C_SSE4",
	tokSrcCSse41:                       "SRC_C_SSE41",
	tokSrcCSsse3:                       "SRC_C_SSSE3",
	tokSrcCXop:                         "SRC_C_XOP",
	tokStrip:                           "STRIP",
	tokStylePython:                     "STYLE_PYTHON",
	tokStyleRuff:                       "STYLE_RUFF",
	tokSubscriber:                      "SUBSCRIBER",
	tokSuppressions:                    "SUPPRESSIONS",
	tokTag:                             "TAG",
	tokTasklet:                         "TASKLET",
	tokTaskletsupport:                  "TASKLETSUPPORT",
	tokTestSrcs:                        "TEST_SRCS",
	tokTimeout:                         "TIMEOUT",
	tokUnion:                           "UNION",
	tokUnittestFor:                     "UNITTEST_FOR",
	tokUseCommonGoogleApis:             "USE_COMMON_GOOGLE_APIS",
	tokUseCxx:                          "USE_CXX",
	tokUseLightPy2cc:                   "USE_LIGHT_PY2CC",
	tokUseLlvmBc16:                     "USE_LLVM_BC16",
	tokUseLlvmBc18:                     "USE_LLVM_BC18",
	tokUseLlvmBc20:                     "USE_LLVM_BC20",
	tokUsePython3:                      "USE_PYTHON3",
	tokVersion:                         "VERSION",
	tokWindowsLongPathManifest:         "WINDOWS_LONG_PATH_MANIFEST",
	tokWithoutLicenseTexts:             "WITHOUT_LICENSE_TEXTS",
	tokWithoutVersion:                  "WITHOUT_VERSION",
	tokWithKotlinGrpc:                  "WITH_KOTLIN_GRPC",
	tokYaConfJson:                      "YA_CONF_JSON",
	tokYqlAbiVersion:                   "YQL_ABI_VERSION",
	tokYqlLastAbiVersion:               "YQL_LAST_ABI_VERSION",
	tokYqlUdfContrib:                   "YQL_UDF_CONTRIB",
	tokYqlUdfYdb:                       "YQL_UDF_YDB",
}

var tokByName = func() map[string]TOK {
	m := make(map[string]TOK, len(tokName))
	for t, n := range tokName {
		if n != "" {
			m[n] = TOK(t)
		}
	}
	return m
}()

func internTok(s string) TOK {
	if t, ok := tokByName[s]; ok {
		return t
	}
	ThrowFmt("internTok: unknown macro name %q (closed TOK set)", s)
	return tokInvalid
}

func (t TOK) String() string {
	return tokName[t]
}
