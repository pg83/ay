package main

// tokName maps each TOK to its interned macro-name STR; TOK.String() resolves it
// through the global intern table.
var tokName = [...]STR{
	tokInvalid:                         0,
	tokAllocator:                       internStr("ALLOCATOR"),
	tokAllocatorImpl:                   internStr("ALLOCATOR_IMPL"),
	tokAllPySrcs:                       internStr("ALL_PY_SRCS"),
	tokArchive:                         internStr("ARCHIVE"),
	tokArPlugin:                        internStr("AR_PLUGIN"),
	tokBisonGenC:                       internStr("BISON_GEN_C"),
	tokBisonGenCpp:                     internStr("BISON_GEN_CPP"),
	tokBuildwithCythonC:                internStr("BUILDWITH_CYTHON_C"),
	tokBuildwithCythonCpp:              internStr("BUILDWITH_CYTHON_CPP"),
	tokBuildOnlyIf:                     internStr("BUILD_ONLY_IF"),
	tokCheckConfigH:                    internStr("CHECK_CONFIG_H"),
	tokCheckDependentDirs:              internStr("CHECK_DEPENDENT_DIRS"),
	tokCopy:                            internStr("COPY"),
	tokCopyFile:                        internStr("COPY_FILE"),
	tokCopyFileWithContext:             internStr("COPY_FILE_WITH_CONTEXT"),
	tokCppProtoPlugin:                  internStr("CPP_PROTO_PLUGIN"),
	tokCppProtoPlugin0:                 internStr("CPP_PROTO_PLUGIN0"),
	tokCppProtoPlugin2:                 internStr("CPP_PROTO_PLUGIN2"),
	tokData:                            internStr("DATA"),
	tokDeclareExternalResource:         internStr("DECLARE_EXTERNAL_RESOURCE"),
	tokDefault:                         internStr("DEFAULT"),
	tokDefineVariable:                  internStr("DEFINE_VARIABLE"),
	tokDisable:                         internStr("DISABLE"),
	tokDll:                             internStr("DLL"),
	tokDynamicLibrary:                  internStr("DYNAMIC_LIBRARY"),
	tokDynamicLibraryFrom:              internStr("DYNAMIC_LIBRARY_FROM"),
	tokEnable:                          internStr("ENABLE"),
	tokEnv:                             internStr("ENV"),
	tokExcludeTags:                     internStr("EXCLUDE_TAGS"),
	tokExportsScript:                   internStr("EXPORTS_SCRIPT"),
	tokExtralibs:                       internStr("EXTRALIBS"),
	tokExtralibsStatic:                 internStr("EXTRALIBS_STATIC"),
	tokFatal:                           internStr("FATAL"),
	tokFiles:                           internStr("FILES"),
	tokFlatcFlags:                      internStr("FLATC_FLAGS"),
	tokForkSubtests:                    internStr("FORK_SUBTESTS"),
	tokForkTests:                       internStr("FORK_TESTS"),
	tokGlobalCflags:                    internStr("GLOBAL_CFLAGS"),
	tokGrpc:                            internStr("GRPC"),
	tokHeaders:                         internStr("HEADERS"),
	tokIdeFolder:                       internStr("IDE_FOLDER"),
	tokIncludeTags:                     internStr("INCLUDE_TAGS"),
	tokInducedDeps:                     internStr("INDUCED_DEPS"),
	tokJavaClasspathIgnoreConflictz:    internStr("JAVA_CLASSPATH_IGNORE_CONFLICTZ"),
	tokJavaSrcs:                        internStr("JAVA_SRCS"),
	tokLdPlugin:                        internStr("LD_PLUGIN"),
	tokLibrary:                         internStr("LIBRARY"),
	tokLicense:                         internStr("LICENSE"),
	tokLicenseRestriction:              internStr("LICENSE_RESTRICTION"),
	tokLicenseRestrictionExceptions:    internStr("LICENSE_RESTRICTION_EXCEPTIONS"),
	tokLicenseTexts:                    internStr("LICENSE_TEXTS"),
	tokLint:                            internStr("LINT"),
	tokLlvmBc:                          internStr("LLVM_BC"),
	tokManualGeneration:                internStr("MANUAL_GENERATION"),
	tokMasmflags:                       internStr("MASMFLAGS"),
	tokMavenGroupId:                    internStr("MAVEN_GROUP_ID"),
	tokMessage:                         internStr("MESSAGE"),
	tokNeedCheck:                       internStr("NEED_CHECK"),
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
	tokOnlyTags:                        internStr("ONLY_TAGS"),
	tokOpensourceExportReplacement:     internStr("OPENSOURCE_EXPORT_REPLACEMENT"),
	tokOpensourceExportReplacementByOs: internStr("OPENSOURCE_EXPORT_REPLACEMENT_BY_OS"),
	tokOpensourceProject:               internStr("OPENSOURCE_PROJECT"),
	tokOptimizePyProtos:                internStr("OPTIMIZE_PY_PROTOS"),
	tokOriginalSource:                  internStr("ORIGINAL_SOURCE"),
	tokOwner:                           internStr("OWNER"),
	tokPackage:                         internStr("PACKAGE"),
	tokPrebuiltProgram:                 internStr("PREBUILT_PROGRAM"),
	tokPrimaryOutput:                   internStr("PRIMARY_OUTPUT"),
	tokProgram:                         internStr("PROGRAM"),
	tokProtocFatalWarnings:             internStr("PROTOC_FATAL_WARNINGS"),
	tokProtoLibrary:                    internStr("PROTO_LIBRARY"),
	tokProtoNamespace:                  internStr("PROTO_NAMESPACE"),
	tokProvides:                        internStr("PROVIDES"),
	tokPy23Library:                     internStr("PY23_LIBRARY"),
	tokPy23NativeLibrary:               internStr("PY23_NATIVE_LIBRARY"),
	tokPy2Library:                      internStr("PY2_LIBRARY"),
	tokPy2Program:                      internStr("PY2_PROGRAM"),
	tokPy3Library:                      internStr("PY3_LIBRARY"),
	tokPy3Program:                      internStr("PY3_PROGRAM"),
	tokPy3ProgramBin:                   internStr("PY3_PROGRAM_BIN"),
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
	tokResourcesLibrary:                internStr("RESOURCES_LIBRARY"),
	tokRestrictPath:                    internStr("RESTRICT_PATH"),
	tokSetAppend:                       internStr("SET_APPEND"),
	tokSetResourceUriFromJson:          internStr("SET_RESOURCE_URI_FROM_JSON"),
	tokSize:                            internStr("SIZE"),
	tokSoProgram:                       internStr("SO_PROGRAM"),
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
	tokStrip:                           internStr("STRIP"),
	tokStylePython:                     internStr("STYLE_PYTHON"),
	tokStyleRuff:                       internStr("STYLE_RUFF"),
	tokSubscriber:                      internStr("SUBSCRIBER"),
	tokSuppressions:                    internStr("SUPPRESSIONS"),
	tokTag:                             internStr("TAG"),
	tokTasklet:                         internStr("TASKLET"),
	tokTaskletsupport:                  internStr("TASKLETSUPPORT"),
	tokTestSrcs:                        internStr("TEST_SRCS"),
	tokTimeout:                         internStr("TIMEOUT"),
	tokUnion:                           internStr("UNION"),
	tokUnittestFor:                     internStr("UNITTEST_FOR"),
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
	tokYaConfJson:                      internStr("YA_CONF_JSON"),
	tokYqlAbiVersion:                   internStr("YQL_ABI_VERSION"),
	tokYqlLastAbiVersion:               internStr("YQL_LAST_ABI_VERSION"),
	tokYqlUdfContrib:                   internStr("YQL_UDF_CONTRIB"),
	tokYqlUdfYdb:                       internStr("YQL_UDF_YDB"),
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
