package main

// tokName maps each TOK to its interned macro-name STR; TOK.String() resolves it
// through the global intern table.
var tokName = [...]STR{
	tokInvalid:                         0,
	tokAllocator:                       internString("ALLOCATOR"),
	tokAllocatorImpl:                   internString("ALLOCATOR_IMPL"),
	tokAllPySrcs:                       internString("ALL_PY_SRCS"),
	tokArchive:                         internString("ARCHIVE"),
	tokArPlugin:                        internString("AR_PLUGIN"),
	tokBisonGenC:                       internString("BISON_GEN_C"),
	tokBisonGenCpp:                     internString("BISON_GEN_CPP"),
	tokBuildwithCythonC:                internString("BUILDWITH_CYTHON_C"),
	tokBuildwithCythonCpp:              internString("BUILDWITH_CYTHON_CPP"),
	tokBuildOnlyIf:                     internString("BUILD_ONLY_IF"),
	tokCheckConfigH:                    internString("CHECK_CONFIG_H"),
	tokCheckDependentDirs:              internString("CHECK_DEPENDENT_DIRS"),
	tokCopy:                            internString("COPY"),
	tokCopyFile:                        internString("COPY_FILE"),
	tokCopyFileWithContext:             internString("COPY_FILE_WITH_CONTEXT"),
	tokCppProtoPlugin:                  internString("CPP_PROTO_PLUGIN"),
	tokCppProtoPlugin0:                 internString("CPP_PROTO_PLUGIN0"),
	tokCppProtoPlugin2:                 internString("CPP_PROTO_PLUGIN2"),
	tokData:                            internString("DATA"),
	tokDeclareExternalResource:         internString("DECLARE_EXTERNAL_RESOURCE"),
	tokDefault:                         internString("DEFAULT"),
	tokDefineVariable:                  internString("DEFINE_VARIABLE"),
	tokDisable:                         internString("DISABLE"),
	tokDll:                             internString("DLL"),
	tokDynamicLibrary:                  internString("DYNAMIC_LIBRARY"),
	tokDynamicLibraryFrom:              internString("DYNAMIC_LIBRARY_FROM"),
	tokEnable:                          internString("ENABLE"),
	tokEnv:                             internString("ENV"),
	tokExcludeTags:                     internString("EXCLUDE_TAGS"),
	tokExportsScript:                   internString("EXPORTS_SCRIPT"),
	tokExtralibs:                       internString("EXTRALIBS"),
	tokExtralibsStatic:                 internString("EXTRALIBS_STATIC"),
	tokFatal:                           internString("FATAL"),
	tokFiles:                           internString("FILES"),
	tokFlatcFlags:                      internString("FLATC_FLAGS"),
	tokForkSubtests:                    internString("FORK_SUBTESTS"),
	tokForkTests:                       internString("FORK_TESTS"),
	tokGlobalCflags:                    internString("GLOBAL_CFLAGS"),
	tokGrpc:                            internString("GRPC"),
	tokHeaders:                         internString("HEADERS"),
	tokIdeFolder:                       internString("IDE_FOLDER"),
	tokIncludeTags:                     internString("INCLUDE_TAGS"),
	tokInducedDeps:                     internString("INDUCED_DEPS"),
	tokJavaClasspathIgnoreConflictz:    internString("JAVA_CLASSPATH_IGNORE_CONFLICTZ"),
	tokJavaSrcs:                        internString("JAVA_SRCS"),
	tokLdPlugin:                        internString("LD_PLUGIN"),
	tokLibrary:                         internString("LIBRARY"),
	tokLicense:                         internString("LICENSE"),
	tokLicenseRestriction:              internString("LICENSE_RESTRICTION"),
	tokLicenseRestrictionExceptions:    internString("LICENSE_RESTRICTION_EXCEPTIONS"),
	tokLicenseTexts:                    internString("LICENSE_TEXTS"),
	tokLint:                            internString("LINT"),
	tokLlvmBc:                          internString("LLVM_BC"),
	tokManualGeneration:                internString("MANUAL_GENERATION"),
	tokMasmflags:                       internString("MASMFLAGS"),
	tokMavenGroupId:                    internString("MAVEN_GROUP_ID"),
	tokMessage:                         internString("MESSAGE"),
	tokNeedCheck:                       internString("NEED_CHECK"),
	tokNoBuildIf:                       internString("NO_BUILD_IF"),
	tokNoCheckImports:                  internString("NO_CHECK_IMPORTS"),
	tokNoClangCoverage:                 internString("NO_CLANG_COVERAGE"),
	tokNoClangMcdcCoverage:             internString("NO_CLANG_MCDC_COVERAGE"),
	tokNoClangTidy:                     internString("NO_CLANG_TIDY"),
	tokNoCompilerWarnings:              internString("NO_COMPILER_WARNINGS"),
	tokNoExportDynamicSymbols:          internString("NO_EXPORT_DYNAMIC_SYMBOLS"),
	tokNoExtendedSourceSearch:          internString("NO_EXTENDED_SOURCE_SEARCH"),
	tokNoImportTracing:                 internString("NO_IMPORT_TRACING"),
	tokNoJoinSrc:                       internString("NO_JOIN_SRC"),
	tokNoLibc:                          internString("NO_LIBC"),
	tokNoLint:                          internString("NO_LINT"),
	tokNoLto:                           internString("NO_LTO"),
	tokNoMypy:                          internString("NO_MYPY"),
	tokNoOptimize:                      internString("NO_OPTIMIZE"),
	tokNoOptimizePyProtos:              internString("NO_OPTIMIZE_PY_PROTOS"),
	tokNoPlatform:                      internString("NO_PLATFORM"),
	tokNoProfileRuntime:                internString("NO_PROFILE_RUNTIME"),
	tokNoPython2:                       internString("NO_PYTHON2"),
	tokNoPythonCoverage:                internString("NO_PYTHON_COVERAGE"),
	tokNoPythonIncludes:                internString("NO_PYTHON_INCLUDES"),
	tokNoRuntime:                       internString("NO_RUNTIME"),
	tokNoSanitize:                      internString("NO_SANITIZE"),
	tokNoSanitizeCoverage:              internString("NO_SANITIZE_COVERAGE"),
	tokNoSplitDwarf:                    internString("NO_SPLIT_DWARF"),
	tokNoUtil:                          internString("NO_UTIL"),
	tokNoWshadow:                       internString("NO_WSHADOW"),
	tokNoYmakePython:                   internString("NO_YMAKE_PYTHON"),
	tokOnlyTags:                        internString("ONLY_TAGS"),
	tokOpensourceExportReplacement:     internString("OPENSOURCE_EXPORT_REPLACEMENT"),
	tokOpensourceExportReplacementByOs: internString("OPENSOURCE_EXPORT_REPLACEMENT_BY_OS"),
	tokOpensourceProject:               internString("OPENSOURCE_PROJECT"),
	tokOptimizePyProtos:                internString("OPTIMIZE_PY_PROTOS"),
	tokOriginalSource:                  internString("ORIGINAL_SOURCE"),
	tokOwner:                           internString("OWNER"),
	tokPackage:                         internString("PACKAGE"),
	tokPrebuiltProgram:                 internString("PREBUILT_PROGRAM"),
	tokPrimaryOutput:                   internString("PRIMARY_OUTPUT"),
	tokProgram:                         internString("PROGRAM"),
	tokProtocFatalWarnings:             internString("PROTOC_FATAL_WARNINGS"),
	tokProtoLibrary:                    internString("PROTO_LIBRARY"),
	tokProtoNamespace:                  internString("PROTO_NAMESPACE"),
	tokProvides:                        internString("PROVIDES"),
	tokPy23Library:                     internString("PY23_LIBRARY"),
	tokPy23NativeLibrary:               internString("PY23_NATIVE_LIBRARY"),
	tokPy2Library:                      internString("PY2_LIBRARY"),
	tokPy2Program:                      internString("PY2_PROGRAM"),
	tokPy3Library:                      internString("PY3_LIBRARY"),
	tokPy3Program:                      internString("PY3_PROGRAM"),
	tokPy3ProgramBin:                   internString("PY3_PROGRAM_BIN"),
	tokPython3:                         internString("PYTHON3"),
	tokPython3Addincl:                  internString("PYTHON3_ADDINCL"),
	tokPyConstructor:                   internString("PY_CONSTRUCTOR"),
	tokPyMain:                          internString("PY_MAIN"),
	tokPyNamespace:                     internString("PY_NAMESPACE"),
	tokPyRegister:                      internString("PY_REGISTER"),
	tokPySrcs:                          internString("PY_SRCS"),
	tokRecurse:                         internString("RECURSE"),
	tokRecurseForTests:                 internString("RECURSE_FOR_TESTS"),
	tokRecurseRootRelative:             internString("RECURSE_ROOT_RELATIVE"),
	tokRequirements:                    internString("REQUIREMENTS"),
	tokResourcesLibrary:                internString("RESOURCES_LIBRARY"),
	tokRestrictPath:                    internString("RESTRICT_PATH"),
	tokSetAppend:                       internString("SET_APPEND"),
	tokSetResourceUriFromJson:          internString("SET_RESOURCE_URI_FROM_JSON"),
	tokSize:                            internString("SIZE"),
	tokSoProgram:                       internString("SO_PROGRAM"),
	tokSplitDwarf:                      internString("SPLIT_DWARF"),
	tokSplitFactor:                     internString("SPLIT_FACTOR"),
	tokSrc:                             internString("SRC"),
	tokSrcCAmx:                         internString("SRC_C_AMX"),
	tokSrcCAvx:                         internString("SRC_C_AVX"),
	tokSrcCAvx2:                        internString("SRC_C_AVX2"),
	tokSrcCAvx512:                      internString("SRC_C_AVX512"),
	tokSrcCNoLto:                       internString("SRC_C_NO_LTO"),
	tokSrcCSse2:                        internString("SRC_C_SSE2"),
	tokSrcCSse3:                        internString("SRC_C_SSE3"),
	tokSrcCSse4:                        internString("SRC_C_SSE4"),
	tokSrcCSse41:                       internString("SRC_C_SSE41"),
	tokSrcCSsse3:                       internString("SRC_C_SSSE3"),
	tokSrcCXop:                         internString("SRC_C_XOP"),
	tokStrip:                           internString("STRIP"),
	tokStylePython:                     internString("STYLE_PYTHON"),
	tokStyleRuff:                       internString("STYLE_RUFF"),
	tokSubscriber:                      internString("SUBSCRIBER"),
	tokSuppressions:                    internString("SUPPRESSIONS"),
	tokTag:                             internString("TAG"),
	tokTasklet:                         internString("TASKLET"),
	tokTaskletsupport:                  internString("TASKLETSUPPORT"),
	tokTestSrcs:                        internString("TEST_SRCS"),
	tokTimeout:                         internString("TIMEOUT"),
	tokUnion:                           internString("UNION"),
	tokUnittestFor:                     internString("UNITTEST_FOR"),
	tokUseCommonGoogleApis:             internString("USE_COMMON_GOOGLE_APIS"),
	tokUseCxx:                          internString("USE_CXX"),
	tokUseLightPy2cc:                   internString("USE_LIGHT_PY2CC"),
	tokUseLlvmBc16:                     internString("USE_LLVM_BC16"),
	tokUseLlvmBc18:                     internString("USE_LLVM_BC18"),
	tokUseLlvmBc20:                     internString("USE_LLVM_BC20"),
	tokUsePython3:                      internString("USE_PYTHON3"),
	tokVersion:                         internString("VERSION"),
	tokWindowsLongPathManifest:         internString("WINDOWS_LONG_PATH_MANIFEST"),
	tokWithoutLicenseTexts:             internString("WITHOUT_LICENSE_TEXTS"),
	tokWithoutVersion:                  internString("WITHOUT_VERSION"),
	tokWithKotlinGrpc:                  internString("WITH_KOTLIN_GRPC"),
	tokYaConfJson:                      internString("YA_CONF_JSON"),
	tokYqlAbiVersion:                   internString("YQL_ABI_VERSION"),
	tokYqlLastAbiVersion:               internString("YQL_LAST_ABI_VERSION"),
	tokYqlUdfContrib:                   internString("YQL_UDF_CONTRIB"),
	tokYqlUdfYdb:                       internString("YQL_UDF_YDB"),
}

var tokByName = func() map[string]TOK {
	m := make(map[string]TOK, len(tokName))

	for t, s := range tokName {
		if s != 0 {
			m[s.String()] = TOK(t)
		}
	}

	return m
}()

// TOK is the numeric identity of a ya.make macro/directive name (the parser's
// macro tokens). The set is closed: every macro name the parser emits as a
// ModuleStmt/UnknownStmt has a TOK, interned at parse via internTok (fail-fast on
// an unknown name). tokName recovers the string via the global intern table.
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

// internTok maps a macro name to its TOK; the set is closed, so an unknown name
// is a parser/corpus gap and fails fast.
func internTok(s string) TOK {
	if t, ok := tokByName[s]; ok {
		return t
	}

	ThrowFmt("internTok: unknown macro name %q (closed TOK set)", s)
	return tokInvalid
}

func (t TOK) String() string {
	return tokName[t].String()
}
