package main

var tokName = [...]STR{
	tokValidateDataRestart:       internStr("VALIDATE_DATA_RESTART"),
	tokPack:                      internStr("PACK"),
	tokSetCudaNodeRAMRequirement: internStr("SET_CUDA_NODE_RAM_REQUIEREMENT"),
	tokNoCow:                     internStr("NO_COW"),
	tokNoNeedCheck:               internStr("NO_NEED_CHECK"),

	tokPireInline:             internStr("PIRE_INLINE"),
	tokPireInlineCmd:          internStr("PIRE_INLINE_CMD"),
	tokGenerateYtRecord:       internStr("GENERATE_YT_RECORD"),
	tokYtRecordDisablePeerdir: internStr("YT_RECORD_DISABLE_PEERDIR"),
	tokProtoToNamespace:       internStr("PROTO_TO_NAMESPACE"),
	tokPyProtoPlugin:          internStr("PY_PROTO_PLUGIN"),
	tokPyProtoPlugin2:         internStr("PY_PROTO_PLUGIN2"),
	tokYqlPrebuiltTool:        internStr("YQL_PREBUILT_TOOL"),
	tokYqlUdf:                 internStr("YQL_UDF"),
	tokYqlPythonUdf:           internStr("YQL_PYTHON_UDF"),
	tokYqlPython3Udf:          internStr("YQL_PYTHON3_UDF"),
	tokRegisterYqlPythonUdf:   internStr("REGISTER_YQL_PYTHON_UDF"),
	tokUdfNoProbe:             internStr("UDF_NO_PROBE"),

	tokCythonFlags: internStr("CYTHON_FLAGS"),
	tokDllFor:      internStr("DLL_FOR"),

	tokPackageFinal:              internStr("_PACKAGE_FINAL"),
	tokMlaunchLibrary:            internStr("MLAUNCH_LIBRARY"),
	tokMlaunchPipeline:           internStr("MLAUNCH_PIPELINE"),
	tokGolaunchProgram:           internStr("GOLAUNCH_PROGRAM"),
	tokGolaunchSrcs:              internStr("GOLAUNCH_SRCS"),
	tokGolaunchSrcdir:            internStr("GOLAUNCH_SRCDIR"),
	tokRuntimePeerdir:            internStr("RUNTIME_PEERDIR"),
	tokResourcePeerdir:           internStr("RESOURCE_PEERDIR"),
	tokGolaunchFunction:          internStr("GOLAUNCH_FUNCTION"),
	tokGolaunchOverride:          internStr("GOLAUNCH_OVERRIDE"),
	tokNoGolaunchSrcs:            internStr("NO_GOLAUNCH_SRCS"),
	tokGenerateGolaunchCallbacks: internStr("GENERATE_GOLAUNCH_CALLBACKS"),
	tokGolaunchComposite:         internStr("GOLAUNCH_COMPOSITE"),
	tokGolaunchCompositeComps:    internStr("GOLAUNCH_COMPOSITE_COMPONENTS"),
	tokMlaunchComponent:          internStr("MLAUNCH_COMPONENT"),
	tokNodeBuilder:               internStr("NODE_BUILDER"),
	tokSdcDiagsLibrary:           internStr("SDC_DIAGS_LIBRARY"),
	tokSdcDiagsSplitGeneratorV3:  internStr("SDC_DIAGS_SPLIT_GENERATOR_V3"),
	tokSdcDiagsSplitGeneratorV4:  internStr("SDC_DIAGS_SPLIT_GENERATOR_V4"),

	tokRosLibrary:        internStr("ROS_LIBRARY"),
	tokSandboxPy3Task:    internStr("SANDBOX_PY3_TASK"),
	tokSandboxPy23Task:   internStr("SANDBOX_PY23_TASK"),
	tokRosDir:            internStr("ROS_DIR"),
	tokRosPartial:        internStr("ROS_PARTIAL"),
	tokRosSrcs:           internStr("ROS_SRCS"),
	tokRosMsgCmd:         internStr("ROS_MSG_CMD"),
	tokRosZeroCopyMsgCmd: internStr("ROS_ZERO_COPY_MSG_CMD"),
	tokRosSrvCmd:         internStr("ROS_SRV_CMD"),

	tokYtOrmProtoYson: internStr("YT_ORM_PROTO_YSON"),
	tokUsrvStyleJSON:  internStr("USRV_STYLE_JSON_CURRENT_DIRECTORY"),
	tokUsrvStyleYAML:  internStr("USRV_STYLE_YAML_CURRENT_DIRECTORY"),
	tokStyleJSON:      internStr("STYLE_JSON"),
	tokStyleYAML:      internStr("STYLE_YAML"),
	tokStyleYAMLConf:  internStr("STYLE_YAML_WITH_CONFIG"),
	tokGenLibrary:     internStr("GEN_LIBRARY"),
	tokUsrvBuild:      internStr("USRV_BUILD"),
	tokUsrvAllSrcs:    internStr("USRV_ALL_SRCS"),
	tokExectest:       internStr("EXECTEST"),
	tokRun:            internStr("RUN"),
	tokPy3Test:        internStr("PY3TEST"),
	tokPy2Test:        internStr("PY2TEST"),
	tokPy23Test:       internStr("PY23_TEST"),
	tokPytestBin:      internStr("PYTEST_BIN"),

	tokDepends:       internStr("DEPENDS"),
	tokTestCwd:       internStr("TEST_CWD"),
	tokUseRecipe:     internStr("USE_RECIPE"),
	tokExplicitData:  internStr("EXPLICIT_DATA"),
	tokDataFiles:     internStr("DATA_FILES"),
	tokBenchmarkOpts: internStr("BENCHMARK_OPTS"),
	tokFuzzOpts:      internStr("FUZZ_OPTS"),
	tokForkTestFiles: internStr("FORK_TEST_FILES"),

	tokSetListPreSuf:                internStr("SET_LIST_PRE_SUF"),
	tokUnittest:                     internStr("UNITTEST"),
	tokUnittestWithCustomEntryPoint: internStr("UNITTEST_WITH_CUSTOM_ENTRY_POINT"),
	tokYtUnittest:                   internStr("YT_UNITTEST"),
	tokGtest:                        internStr("GTEST"),
	tokGBenchmark:                   internStr("G_BENCHMARK"),
	tokFuzz:                         internStr("FUZZ"),
	tokBoosttest:                    internStr("BOOSTTEST"),
	tokBoosttestWithMain:            internStr("BOOSTTEST_WITH_MAIN"),

	tokDocs:                        internStr("DOCS"),
	tokDocsLibrary:                 internStr("DOCS_LIBRARY"),
	tokDocsDir:                     internStr("DOCS_DIR"),
	tokDocsConfig:                  internStr("DOCS_CONFIG"),
	tokDocsVars:                    internStr("DOCS_VARS"),
	tokDocsIncludeSources:          internStr("DOCS_INCLUDE_SOURCES"),
	tokDocsCopyFiles:               internStr("DOCS_COPY_FILES"),
	tokUsePlantuml:                 internStr("USE_PLANTUML"),
	tokLlvmCompileCxx:              internStr("LLVM_COMPILE_CXX"),
	tokCompileLuaOpenresty:         internStr("COMPILE_LUA_OPENRESTY"),
	tokCompileLua21:                internStr("COMPILE_LUA_21"),
	tokCompileLua:                  internStr("COMPILE_LUA"),
	tokDebVersion:                  internStr("DEB_VERSION"),
	tokTsProtoPackageName:          internStr("TS_PROTO_PACKAGE_NAME"),
	tokStyleCppUserSessions:        internStr("STYLE_CPP_USER_SESSIONS"),
	tokSetCustomClangTidy:          internStr("SET_CUSTOM_CLANG_TIDY"),
	tokYqlStableAbiVersion:         internStr("YQL_STABLE_ABI_VERSION"),
	tokYqlCurrentAbiVersion:        internStr("YQL_CURRENT_ABI_VERSION"),
	tokYqlPrebuiltProgram:          internStr("YQL_PREBUILT_PROGRAM"),
	tokYqlLibrary:                  internStr("YQL_LIBRARY"),
	tokUsrvGenProtoStructs:         internStr("USRV_GEN_PROTO_STRUCTS"),
	tokUsrvGenGrpcServiceV2:        internStr("USRV_GEN_GRPC_SERVICE_V2"),
	tokUsrvGenGrpcClientV2:         internStr("USRV_GEN_GRPC_CLIENT_V2"),
	tokUsrvGenGrpcServiceV2Structs: internStr("USRV_GEN_GRPC_SERVICE_V2_STRUCTS"),
	tokUsrvGenGrpcClientV2Structs:  internStr("USRV_GEN_GRPC_CLIENT_V2_STRUCTS"),

	tokInvalid:                            0,
	tokAddInclSelf:                        internStr("ADDINCLSELF"),
	tokAliceCapability:                    internStr("ALICE_CAPABILITY"),
	tokAliceGenerateProtoIncludes:         internStr("ALICE_GENERATE_PROTO_INCLUDES"),
	tokAliceGenerateFunctionProtoIncludes: internStr("ALICE_GENERATE_FUNCTION_PROTO_INCLUDES"),
	tokAliceTypedCallback:                 internStr("ALICE_TYPED_CALLBACK"),
	tokAllocator:                          internStr("ALLOCATOR"),
	tokAllocatorImpl:                      internStr("ALLOCATOR_IMPL"),
	tokAllPySrcs:                          internStr("ALL_PY_SRCS"),
	tokApphost:                            internStr("APPHOST"),
	tokArchive:                            internStr("ARCHIVE"),
	tokArchiveAsm:                         internStr("ARCHIVE_ASM"),
	tokAsmPreinclude:                      internStr("ASM_PREINCLUDE"),
	tokArPlugin:                           internStr("AR_PLUGIN"),
	tokBaseCodegen:                        internStr("BASE_CODEGEN"),
	tokBisonGenC:                          internStr("BISON_GEN_C"),
	tokBisonGenCpp:                        internStr("BISON_GEN_CPP"),
	tokBpf:                                internStr("BPF"),
	tokBpfStatic:                          internStr("BPF_STATIC"),
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
	tokCppEvProtoPlugin:                   internStr("CPP_EV_PROTO_PLUGIN"),
	tokCppProtoPlugin:                     internStr("CPP_PROTO_PLUGIN"),
	tokCppProtoPlugin0:                    internStr("CPP_PROTO_PLUGIN0"),
	tokCppProtoPlugin2:                    internStr("CPP_PROTO_PLUGIN2"),
	tokGenerateImplib:                     internStr("GENERATE_IMPLIB"),
	tokLinkExcludeLibraries:               internStr("LINK_EXCLUDE_LIBRARIES"),
	tokCudaSrcs:                           internStr("CUDA_SRCS"),
	tokCudaDeviceLinkLibrary:              internStr("CUDA_DEVICE_LINK_LIBRARY"),
	tokNvccDeviceLink:                     internStr("NVCC_DEVICE_LINK"),
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
	tokWithDynamicLibs:                 internStr("WITH_DYNAMIC_LIBS"),
	tokEnable:                          internStr("ENABLE"),
	tokEnv:                             kwENV,
	tokExcludeTags:                     internStr("EXCLUDE_TAGS"),
	tokExportsScript:                   internStr("EXPORTS_SCRIPT"),
	tokExtralibs:                       internStr("EXTRALIBS"),
	tokExtralibsStatic:                 internStr("EXTRALIBS_STATIC"),
	tokFatal:                           internStr("FATAL"),
	tokFbsNamespace:                    internStr("FBS_NAMESPACE"),
	tokFbsLibrary:                      internStr("FBS_LIBRARY"),
	tokFiles:                           kwFILES,
	tokProto2Fbs:                       internStr("PROTO2FBS"),
	tokFlatcFlags:                      internStr("FLATC_FLAGS"),
	tokForkSubtests:                    internStr("FORK_SUBTESTS"),
	tokForkTests:                       internStr("FORK_TESTS"),
	tokFromSandbox:                     internStr("FROM_SANDBOX"),
	tokLargeFiles:                      internStr("LARGE_FILES"),
	tokFromArchive:                     internStr("FROM_ARCHIVE"),
	tokCiGroup:                         internStr("CI_GROUP"),
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
	tokPy2Module:                       internStr("PY2MODULE"),
	tokPy2Program:                      kwPY2_PROGRAM,
	tokPy3Library:                      kwPY3_LIBRARY,
	tokPy3Program:                      kwPY3_PROGRAM,
	tokPy3ProgramBin:                   kwPY3_PROGRAM_BIN,
	tokPython3:                         internStr("PYTHON3"),
	tokPython2Addincl:                  internStr("PYTHON2_ADDINCL"),
	tokPython3Addincl:                  internStr("PYTHON3_ADDINCL"),
	tokPyConstructor:                   internStr("PY_CONSTRUCTOR"),
	tokPyExtraLintFiles:                internStr("PY_EXTRA_LINT_FILES"),
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
	tokTaskletReg:                      internStr("TASKLET_REG"),
	tokTaskletRegExt:                   internStr("TASKLET_REG_EXT"),
	tokTaskletsupport:                  internStr("TASKLETSUPPORT"),
	tokTestSrcs:                        internStr("TEST_SRCS"),
	tokTimeout:                         internStr("TIMEOUT"),
	tokToolchain:                       internStr("TOOLCHAIN"),
	tokDockerImage:                     internStr("DOCKER_IMAGE"),
	tokSdcUnion:                        internStr("SDC_UNION"),
	tokSdcInstall:                      internStr("SDC_INSTALL"),
	tokYaToolsConf:                     internStr("YA_TOOLS_CONF"),
	tokLinkerScript:                    internStr("LINKER_SCRIPT"),
	tokSdcResourceInfo:                 internStr("SDC_RESOURCE_INFO"),
	tokSdcSbResourceInfo:               internStr("SDC_SB_RESOURCE_INFO"),
	tokSdcRosnodeInfo:                  internStr("SDC_ROSNODE_INFO"),
	tokGenPchFile:                      internStr("GEN_PCH_FILE"),
	tokSdcSolibInfo:                    internStr("SDC_SOLIB_INFO"),
	tokUnion:                           kwUNION,
	tokUnittestFor:                     kwUNITTEST_FOR,
	tokUseCommonGoogleApis:             internStr("USE_COMMON_GOOGLE_APIS"),
	tokUseCxx:                          internStr("USE_CXX"),
	tokUseUtil:                         internStr("USE_UTIL"),
	tokUseLightPy2cc:                   internStr("USE_LIGHT_PY2CC"),
	tokUseLlvmBc16:                     internStr("USE_LLVM_BC16"),
	tokUseLlvmBc18:                     internStr("USE_LLVM_BC18"),
	tokUseLlvmBc20:                     internStr("USE_LLVM_BC20"),
	tokSelectClangSAConfig:             internStr("SELECT_CLANG_SA_CONFIG"),
	tokUseSAPlugins:                    internStr("USE_SA_PLUGINS"),
	tokUseNasm:                         internStr("USE_NASM"),
	tokUsePython2:                      internStr("USE_PYTHON2"),
	tokUsePython3:                      internStr("USE_PYTHON3"),
	tokVersion:                         internStr("VERSION"),
	tokWindowsManifest:                 internStr("WINDOWS_MANIFEST"),
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
	tokYqlUdfTest:                      internStr("YQL_UDF_TEST"),
	tokGoLibrary:                       internStr("GO_LIBRARY"),
	tokGoPackageName:                   internStr("GO_PACKAGE_NAME"),
	tokGoGrpcGatewayV2OpenapiSrcs:      internStr("GO_GRPC_GATEWAY_V2_OPENAPI_SRCS"),
	tokGoProgram:                       internStr("GO_PROGRAM"),
	tokGoTestSrcs:                      internStr("GO_TEST_SRCS"),
	tokGoXtestSrcs:                     internStr("GO_XTEST_SRCS"),
	tokGoSkipTests:                     internStr("GO_SKIP_TESTS"),
	tokGoEmbedPattern:                  internStr("GO_EMBED_PATTERN"),
	tokGoEmbedBindir:                   internStr("GO_EMBED_BINDIR"),
	tokGoTestEmbedBindir:               internStr("GO_TEST_EMBED_BINDIR"),
	tokGoXtestEmbedBindir:              internStr("GO_XTEST_EMBED_BINDIR"),
	tokGoEmbedDir:                      internStr("GO_EMBED_DIR"),
	tokGoEmbedTestDir:                  internStr("GO_EMBED_TEST_DIR"),
	tokGoEmbedXtestDir:                 internStr("GO_EMBED_XTEST_DIR"),
	tokCgoSrcs:                         internStr("CGO_SRCS"),
	tokCgoLdflags:                      internStr("CGO_LDFLAGS"),
	tokCgoCflags:                       internStr("CGO_CFLAGS"),

	tokAllResourceFiles:               internStr("ALL_RESOURCE_FILES"),
	tokArchiveByKeys:                  internStr("ARCHIVE_BY_KEYS"),
	tokBisonFlags:                     internStr("BISON_FLAGS"),
	tokBundle:                         internStr("BUNDLE"),
	tokBundleOutput:                   internStr("BUNDLE_OUTPUT"),
	tokCppEnumsSerialization:          internStr("CPP_ENUMS_SERIALIZATION"),
	tokDecimalMd5Lower32Bits:          internStr("DECIMAL_MD5_LOWER_32_BITS"),
	tokDefaultJdkVersion:              internStr("DEFAULT_JDK_VERSION"),
	tokJdkVersion:                     internStr("JDK_VERSION"),
	tokExportYmapsProto:               internStr("EXPORT_YMAPS_PROTO"),
	tokLj21Archive:                    internStr("LJ_21_ARCHIVE"),
	tokProtoDescriptions:              internStr("PROTO_DESCRIPTIONS"),
	tokProtoRegistry:                  internStr("PROTO_REGISTRY"),
	tokSplitCodegen:                   internStr("SPLIT_CODEGEN"),
	tokStructCodegen:                  internStr("STRUCT_CODEGEN"),
	tokStyleDetekt:                    internStr("STYLE_DETEKT"),
	tokYmapsSproto:                    internStr("YMAPS_SPROTO"),
	tokTsPackage:                      internStr("TS_PACKAGE"),
	tokTsLibrary:                      internStr("TS_LIBRARY"),
	tokTsNext:                         internStr("TS_NEXT"),
	tokTsVite:                         internStr("TS_VITE"),
	tokTsTsc:                          internStr("TS_TSC"),
	tokTsTestFor:                      internStr("TS_TEST_FOR"),
	tokTsTestJestFor:                  internStr("TS_TEST_JEST_FOR"),
	tokTsTestVitestFor:                internStr("TS_TEST_VITEST_FOR"),
	tokJavaLibrary:                    internStr("JAVA_LIBRARY"),
	tokJavaLibrarySplit:               internStr("JAVA_LIBRARY_SPLIT"),
	tokJavaLibraryNonSplit:            internStr("JAVA_LIBRARY_NON_SPLIT"),
	tokJavaProgram:                    internStr("JAVA_PROGRAM"),
	tokJavaContrib:                    internStr("JAVA_CONTRIB"),
	tokJavaContribProxy:               internStr("JAVA_CONTRIB_PROXY"),
	tokJavaContribProgram:             internStr("JAVA_CONTRIB_PROGRAM"),
	tokJavaContribAnnotationProcessor: internStr("JAVA_CONTRIB_ANNOTATION_PROCESSOR"),
	tokJavaAnnotationProcessor:        internStr("JAVA_ANNOTATION_PROCESSOR"),
	tokJavaTestLibrary:                internStr("JAVA_TEST_LIBRARY"),
	tokJunit5:                         internStr("JUNIT5"),
	tokJunit6:                         internStr("JUNIT6"),
	tokJtest:                          internStr("JTEST"),
	tokJtestFor:                       internStr("JTEST_FOR"),
	tokExternalJavaLibrary:            internStr("EXTERNAL_JAVA_LIBRARY"),
	tokJarLibrary:                     internStr("JAR_LIBRARY"),
	tokDllJava:                        internStr("DLL_JAVA"),
	tokAddDllsToJar:                   internStr("ADD_DLLS_TO_JAR"),
	tokGoTest:                         internStr("GO_TEST"),
	tokGoTestFor:                      internStr("GO_TEST_FOR"),
	tokGoDll:                          internStr("GO_DLL"),
	tokAllGoSrcs:                      internStr("ALL_GO_SRCS"),
	tokGoMockgenFrom:                  internStr("GO_MOCKGEN_FROM"),
	tokGoMockgenContribFrom:           internStr("GO_MOCKGEN_CONTRIB_FROM"),
	tokGoMockgenTypes:                 internStr("GO_MOCKGEN_TYPES"),
	tokGoMockgenExcludeIfaces:         internStr("GO_MOCKGEN_EXCLUDE_INTERFACES"),
	tokGoMockgenPackage:               internStr("GO_MOCKGEN_PACKAGE"),
	tokGoMockgenReflect:               internStr("GO_MOCKGEN_REFLECT"),
	tokGoMockgenMocks:                 internStr("GO_MOCKGEN_MOCKS"),
	tokGoMockgenSource:                internStr("GO_MOCKGEN_SOURCE"),
	tokGoOapiCodegen:                  internStr("GO_OAPI_CODEGEN"),
	tokGoOapiCodegenV2:                internStr("GO_OAPI_CODEGEN_V2"),
	tokGoOapiCodegenTaxi:              internStr("GO_OAPI_CODEGEN_TAXI"),
	tokGoGrpcGatewayV2Srcs:            internStr("GO_GRPC_GATEWAY_V2_SRCS"),
	tokGoGrpcGatewaySrcs:              internStr("GO_GRPC_GATEWAY_SRCS"),
	tokGoGrpcGatewaySwagger:           internStr("GO_GRPC_GATEWAY_SWAGGER_SRCS"),
	tokProtoAddIncl:                   internStr("PROTO_ADDINCL"),
	tokResolveProto:                   internStr("RESOLVE_PROTO"),
	tokGoTestEmbedPattern:             internStr("GO_TEST_EMBED_PATTERN"),
	tokGoXTestEmbedPattern:            internStr("GO_XTEST_EMBED_PATTERN"),
	tokUseSdcMeta:                     strUSE_SDC_META,
	tokGenerateTopology:               internStr("GENERATE_TOPOLOGY"),
	tokCompileNlg:                     internStr("COMPILE_NLG"),
	tokAllGoTestSrcs:                  internStr("ALL_GO_TEST_SRCS"),
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
	tokPyProtoPlugin
	tokPyProtoPlugin2
	tokPackageFinal
	tokMlaunchLibrary
	tokMlaunchPipeline
	tokGolaunchProgram
	tokGolaunchSrcs
	tokGolaunchSrcdir
	tokRuntimePeerdir
	tokResourcePeerdir
	tokGolaunchFunction
	tokGolaunchOverride
	tokNoGolaunchSrcs
	tokGenerateGolaunchCallbacks
	tokGolaunchComposite
	tokGolaunchCompositeComps
	tokMlaunchComponent
	tokNodeBuilder
	tokSdcDiagsLibrary
	tokSdcDiagsSplitGeneratorV3
	tokSdcDiagsSplitGeneratorV4

	tokRosLibrary
	tokSandboxPy3Task
	tokSandboxPy23Task
	tokRosDir
	tokRosPartial
	tokRosSrcs
	tokRosMsgCmd
	tokRosZeroCopyMsgCmd
	tokRosSrvCmd
	tokAddInclSelf
	tokAliceCapability
	tokAliceGenerateProtoIncludes
	tokAliceGenerateFunctionProtoIncludes
	tokAliceTypedCallback
	tokAllocator
	tokAllocatorImpl
	tokAllPySrcs
	tokApphost
	tokArchive
	tokArchiveAsm
	tokAsmPreinclude
	tokArPlugin
	tokBaseCodegen
	tokBisonGenC
	tokBisonGenCpp
	tokBpf
	tokBpfStatic
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
	tokCppEvProtoPlugin
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
	tokDllFor
	tokDynamicLibrary
	tokDynamicLibraryFrom
	tokWithDynamicLibs
	tokEnable
	tokEnv
	tokExcludeTags
	tokExportsScript
	tokExtralibs
	tokExtralibsStatic
	tokFatal
	tokFbsNamespace
	tokFbsLibrary
	tokFiles
	tokProto2Fbs
	tokFlatcFlags
	tokForkSubtests
	tokForkTests
	tokFromSandbox
	tokLargeFiles
	tokFromArchive
	tokCiGroup
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
	tokPy2Module
	tokPy2Program
	tokPy3Library
	tokPy3Program
	tokPy3ProgramBin
	tokPython3
	tokPython2Addincl
	tokPython3Addincl
	tokPyConstructor
	tokPyExtraLintFiles
	tokPyMain
	tokPyNamespace
	tokProtoToNamespace
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
	tokSetListPreSuf
	tokDepends
	tokTestCwd
	tokUseRecipe
	tokExplicitData
	tokDataFiles
	tokBenchmarkOpts
	tokFuzzOpts
	tokForkTestFiles
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
	tokTaskletReg
	tokTaskletRegExt
	tokTaskletsupport
	tokTestSrcs
	tokTimeout
	tokToolchain
	tokDockerImage
	tokSdcUnion
	tokSdcInstall
	tokYaToolsConf
	tokLinkerScript
	tokSdcResourceInfo
	tokSdcSbResourceInfo
	tokSdcRosnodeInfo
	tokGenPchFile
	tokSdcSolibInfo
	tokUnion
	tokUnittestFor
	tokExectest
	tokRun
	tokPy3Test
	tokPy2Test
	tokPy23Test
	tokPytestBin
	tokUnittest
	tokUnittestWithCustomEntryPoint
	tokYtUnittest
	tokGtest
	tokGBenchmark
	tokFuzz
	tokBoosttest
	tokBoosttestWithMain
	tokUseCommonGoogleApis
	tokUseCxx
	tokUseUtil
	tokUseLightPy2cc
	tokUseLlvmBc16
	tokUseLlvmBc18
	tokUseLlvmBc20
	tokSelectClangSAConfig
	tokUseSAPlugins
	tokUseNasm
	tokUsePython2
	tokUsePython3
	tokVersion
	tokWindowsManifest
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
	tokYqlUdfTest
	tokGoLibrary
	tokGoPackageName
	tokGoGrpcGatewayV2OpenapiSrcs
	tokGoProgram
	tokGoTestSrcs
	tokGoXtestSrcs
	tokGoSkipTests
	tokGoEmbedPattern
	tokGoEmbedBindir
	tokGoTestEmbedBindir
	tokGoXtestEmbedBindir
	tokGoEmbedDir
	tokGoEmbedTestDir
	tokGoEmbedXtestDir
	tokCgoSrcs
	tokCgoLdflags
	tokCgoCflags

	tokAllResourceFiles
	tokArchiveByKeys
	tokBisonFlags
	tokBundle
	tokBundleOutput
	tokCppEnumsSerialization
	tokDecimalMd5Lower32Bits
	tokDefaultJdkVersion
	tokJdkVersion
	tokExportYmapsProto
	tokLj21Archive
	tokProtoDescriptions
	tokProtoRegistry
	tokSplitCodegen
	tokStructCodegen
	tokStyleDetekt
	tokYmapsSproto
	tokGenerateImplib
	tokLinkExcludeLibraries
	tokCudaSrcs
	tokCudaDeviceLinkLibrary
	tokNvccDeviceLink
	tokCudaNvccFlags
	tokSetAppendWithGlobal
	tokDllTool
	tokTsPackage
	tokTsLibrary
	tokTsNext
	tokTsVite
	tokTsTsc
	tokTsTestFor
	tokTsTestJestFor
	tokTsTestVitestFor
	tokDocs
	tokDocsLibrary
	tokDocsDir
	tokDocsConfig
	tokDocsVars
	tokDocsIncludeSources
	tokDocsCopyFiles
	tokUsePlantuml
	tokJavaProgram
	tokJavaContrib
	tokJavaLibrary
	tokJavaLibrarySplit
	tokJavaLibraryNonSplit
	tokJavaContribProxy
	tokJavaContribProgram
	tokJavaContribAnnotationProcessor
	tokJavaAnnotationProcessor
	tokJavaTestLibrary
	tokJunit5
	tokJunit6
	tokJtest
	tokJtestFor
	tokExternalJavaLibrary
	tokJarLibrary
	tokDllJava
	tokAddDllsToJar
	tokGoTest
	tokGoTestFor
	tokGoDll
	tokAllGoSrcs
	tokGoMockgenFrom
	tokYtOrmProtoYson
	tokUsrvGenProtoStructs
	tokUsrvGenGrpcServiceV2
	tokUsrvGenGrpcClientV2
	tokUsrvGenGrpcServiceV2Structs
	tokUsrvGenGrpcClientV2Structs
	tokGoMockgenContribFrom
	tokGoMockgenTypes
	tokGoMockgenExcludeIfaces
	tokGoMockgenPackage
	tokGoMockgenReflect
	tokGoMockgenMocks
	tokGoMockgenSource
	tokGoOapiCodegen
	tokGoOapiCodegenV2
	tokGoOapiCodegenTaxi
	tokGoGrpcGatewayV2Srcs
	tokGoGrpcGatewaySrcs
	tokGoGrpcGatewaySwagger
	tokProtoAddIncl
	tokResolveProto
	tokGoTestEmbedPattern
	tokGoXTestEmbedPattern
	tokUseSdcMeta
	tokGenerateTopology
	tokCompileNlg
	tokAllGoTestSrcs
	tokGenLibrary
	tokUsrvBuild
	tokUsrvAllSrcs
	tokStyleJSON
	tokStyleYAML
	tokStyleYAMLConf
	tokUsrvStyleJSON
	tokUsrvStyleYAML
	tokCompileLua
	tokCompileLua21
	tokCompileLuaOpenresty
	tokLlvmCompileCxx
	tokYqlLibrary
	tokYqlPrebuiltProgram
	tokYqlCurrentAbiVersion
	tokYqlStableAbiVersion
	tokSetCustomClangTidy
	tokStyleCppUserSessions
	tokTsProtoPackageName
	tokDebVersion
	tokCythonFlags
	tokYqlUdf
	tokYqlPythonUdf
	tokYqlPython3Udf
	tokRegisterYqlPythonUdf
	tokUdfNoProbe
	tokYqlPrebuiltTool
	tokGenerateYtRecord
	tokYtRecordDisablePeerdir
	tokPireInline
	tokPireInlineCmd
	tokValidateDataRestart
	tokPack
	tokSetCudaNodeRAMRequirement
	tokNoCow
	tokNoNeedCheck
)

type TOK uint16

func internTokSTR(s STR) TOK {
	return tokBySTR[s]
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
