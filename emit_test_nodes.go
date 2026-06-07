package main

import (
	"path"
	"strings"
)

var (
	// Path constants hoisted by `ay refac consts`.
	bldCommonTestContext = Build("common_test.context")
	// Path constants hoisted by `ay refac consts`.
	any0                        = internAny("0")
	any1                        = internAny("1")
	any100000                   = internAny("100000")
	any3                        = internAny("{")
	any30                       = internAny("30")
	any4                        = internAny("  }")
	any5                        = internAny("}")
	any60                       = internAny("60")
	any8                        = internAny("8")
	anyBinary                   = internAny("--binary")
	anyClangFormat              = internAny("clang_format")
	anyCompressionFilter        = internAny("--compression-filter")
	anyCompressionLevel         = internAny("--compression-level")
	anyConfig                   = internAny("--config")
	anyContextFilename          = internAny("--context-filename")
	anyDepends                  = internAny("--depends")
	anyFlags                    = internAny("  \"flags\": {")
	anyGdbPath                  = internAny("--gdb-path")
	anyGlobalResource           = internAny("--global-resource")
	anyLintName                 = internAny("--lint-name")
	anyListTimeout              = internAny("--list-timeout")
	anyLogPath                  = internAny("--log-path")
	anyMeta                     = internAny("--meta")
	anyModulo                   = internAny("--modulo")
	anyModuloIndex              = internAny("--modulo-index")
	anyNinja                    = internAny("ninja")
	anyOutPath                  = internAny("--out-path")
	anyOutputDir                = internAny("--output-dir")
	anyOutputStyle              = internAny("--output-style")
	anyPartitionMode            = internAny("--partition-mode")
	anyProjectPath              = internAny("--project-path")
	anyPythonBin                = internAny("--python-bin")
	anyRamLimitGb               = internAny("--ram-limit-gb")
	anyRemoveTos                = internAny("--remove-tos")
	anyResultMaxFileSize        = internAny("--result-max-file-size")
	anyRunCustomLint            = internAny("run_custom_lint")
	anyRunTest                  = internAny("run_test")
	anyRunUt                    = internAny("run_ut")
	anySequential               = internAny("SEQUENTIAL")
	anySigusr2                  = internAny("SIGUSR2")
	anySmall                    = internAny("small")
	anySmoothShutdownSignals    = internAny("--smooth-shutdown-signals")
	anyStyle                    = internAny("style")
	anySupportsTestParameters   = internAny("--supports-test-parameters")
	anyTar                      = internAny("--tar")
	anyTargetPlatformDescriptor = internAny("--target-platform-descriptor")
	anyTest                     = internAny("test")
	anyTestCiType               = internAny("--test-ci-type")
	anyTestRelatedPath          = internAny("--test-related-path")
	anyTestSize                 = internAny("--test-size")
	anyTestSuiteName            = internAny("--test-suite-name")
	anyTestType                 = internAny("--test-type")
	anyTestsLimitInChunk        = internAny("--tests-limit-in-chunk")
	anyTestsRequestedYes        = internAny("    \"TESTS_REQUESTED\": \"yes\"")
	anyTimeout                  = internAny("--timeout")
	anyTrace                    = internAny("--trace")
	anyTracePath                = internAny("--trace-path")
	anyUnittest                 = internAny("unittest")
	anyVerbose                  = internAny("--verbose")
	anyVerifyResults            = internAny("--verify-results")
	anyWrapperScript            = internAny("--wrapper-script")
	anyZstd                     = internAny("zstd")
)

const (
	testToolHostPath          = "$(TEST_TOOL_HOST-sbr:12080295773)/test_tool"
	testContextPath           = "$(B)/common_test.context"
	testBuildRoot             = "$(B)"
	testSourceRoot            = "$(S)"
	testPythonBin             = "$(PYTHON)/python"
	testGDBPath               = "$(GDB)/gdb/bin/gdb"
	testYMakePython3          = "$(YMAKE_PYTHON3)/bin/python3"
	testClangSymbolizerPath   = "$(CLANG-1274503668)/bin/llvm-symbolizer"
	testClangCCPath           = "$(CLANG-1274503668)/bin/clang"
	testClangCXXPath          = "$(CLANG-1274503668)/bin/clang++"
	testClang14Resource       = "CLANG14_RESOURCE_GLOBAL::$(CLANG14-1922233694)"
	testClang16Resource       = "CLANG16_RESOURCE_GLOBAL::$(CLANG16-1380963495)"
	testClang18Resource       = "CLANG18_RESOURCE_GLOBAL::$(CLANG18-1866954364)"
	testClang20Resource       = "CLANG20_RESOURCE_GLOBAL::$(CLANG20-178457234)"
	testClangFormatResource   = "CLANG_FORMAT_RESOURCE_GLOBAL::$(CLANG_FORMAT-2463648791)"
	testClangResource         = "CLANG_RESOURCE_GLOBAL::$(CLANG-1380963495)"
	testLLDRootResource       = "LLD_ROOT_RESOURCE_GLOBAL::$(LLD_ROOT-3107549726)"
	testYMakePython3Resource  = "YMAKE_PYTHON3_RESOURCE_GLOBAL::$(YMAKE_PYTHON3-1002064631)"
	testSVNInterfaceRel       = "build/scripts/c_templates/svn_interface.c"
	testClangFormatConfigRel  = "build/config/tests/cpp_style/.clang-format"
	testClangFormatWrapperRel = "tools/cpp_style_checker/wrapper.py"
	testAppendFileScriptRel   = "build/scripts/append_file.py"
	testMandatoryEnvVars      = "ASAN_OPTIONS:ASAN_SYMBOLIZER_PATH:LSAN_OPTIONS:LSAN_SYMBOLIZER_PATH:MSAN_OPTIONS:MSAN_SYMBOLIZER_PATH:TSAN_SYMBOLIZER_PATH:UBSAN_OPTIONS:UBSAN_SYMBOLIZER_PATH:YA_MANDATORY_ENV_VARS"
)

type testSuiteInfo struct {
	ProjectPath string
	BinaryPath  string
	CppSources  []string
}

func emitTestRunNodes(ctxEmit Emitter, runEmit Emitter, p *Platform, info testSuiteInfo, ldRef NodeRef) []NodeRef {
	ctxRef := ctxEmit.Emit(buildTestCtxNode(p))

	unittest := buildUnittestNode(p, info)
	unittest.DepRefs = []NodeRef{ldRef, ctxRef}
	unittestRef := runEmit.Emit(unittest)

	clangFormat := buildClangFormatNode(p, info)
	clangFormat.DepRefs = []NodeRef{ctxRef}
	clangFormatRef := runEmit.Emit(clangFormat)
	return []NodeRef{unittestRef, clangFormatRef}
}

func buildTestCtxNode(p *Platform) *Node {
	cacheTrue := true

	return bindNodePlatform(withResources(&Node{
		Cache: &cacheTrue,
		Cmds: []Cmd{{
			CmdArgs: []ANY{
				internAny(testYMakePython3),
				vfsAny(Source(testAppendFileScriptRel)),
				internAny(testContextPath),
				any3,
				internAny(`  "build_type": "` + p.BuildType + `",`),
				anyFlags,
				anyTestsRequestedYes,
				any4,
				any5,
			},
		}},
		Env:              nil,
		Inputs:           []VFS{Source(testAppendFileScriptRel)},
		KV:               KV{P: pkCP, PC: pcLightBlue},
		Outputs:          []VFS{bldCommonTestContext},
		Platform:         string(p.Target),
		Requirements:     Requirements{Network: "restricted"},
		Tags:             sandboxingNodeTags(p),
		TargetProperties: TargetProperties{},
	}, resourcePatternYMakePython3), p)
}

func buildUnittestNode(p *Platform, info testSuiteInfo) *Node {
	cacheFalse := false
	resultsDir := path.Join(info.ProjectPath, "test-results", "unittest")

	cmdArgs := []ANY{
		internAny(testToolHostPath),
		anyRunTest,
		anyYaStartCommandFile,
		anyMeta, vfsAny(Build(path.Join(resultsDir, "meta.json"))),
		anyTrace, vfsAny(Build(path.Join(resultsDir, "ytest.report.trace"))),
		anyTimeout, any60,
		anyLogPath, vfsAny(Build(path.Join(resultsDir, "run_test.log"))),
		anyTestSize, anySmall,
		anyTestType, anyUnittest,
		anyTestCiType, anyTest,
		anyContextFilename, internAny(testContextPath),
		anySourceRoot, internAny(testSourceRoot),
		anyBuildRoot, internAny(testBuildRoot),
		anyTestSuiteName, anyUnittest,
		anyProjectPath, internAny(info.ProjectPath),
		anyTestRelatedPath, vfsAny(Source(info.ProjectPath)),
		anyTargetPlatformDescriptor, internAny(targetPlatformDescriptor(p)),
		anyRemoveTos,
		anyGdbPath, internAny(testGDBPath),
		anyResultMaxFileSize, any0,
		anyVerifyResults,
		anyTestsLimitInChunk, any100000,
		anyOutputStyle, anyNinja,
		anyPythonBin, internAny(testPythonBin),
		anySupportsTestParameters,
		anySmoothShutdownSignals, anySigusr2,
		anyCompressionFilter, anyZstd,
		anyCompressionLevel, any1,
		anyGlobalResource, internAny(testClang14Resource),
		anyGlobalResource, internAny(testClang16Resource),
		anyGlobalResource, internAny(testClang18Resource),
		anyGlobalResource, internAny(testClang20Resource),
		anyGlobalResource, internAny(testClangFormatResource),
		anyGlobalResource, internAny(testClangResource),
		anyGlobalResource, internAny(testLLDRootResource),
		anyGlobalResource, internAny(testYMakePython3Resource),
		anyRamLimitGb, any8,
		anyTar, vfsAny(Build(path.Join(resultsDir, "testing_out_stuff.tar.zstd"))),
		internAny(testToolHostPath),
		anyRunUt,
		anyBinary, internAny(info.BinaryPath),
		anyTracePath, vfsAny(Build(path.Join(resultsDir, "ytest.report.trace"))),
		anyOutputDir, vfsAny(Build(path.Join(resultsDir, "testing_out_stuff"))),
		anyModulo, any1,
		anyModuloIndex, any0,
		anyPartitionMode, anySequential,
		anyProjectPath, internAny(info.ProjectPath),
		anyListTimeout, any30,
		anyVerbose,
		anyGdbPath, internAny(testGDBPath),
		anyYaEndCommandFile,
	}

	return bindNodePlatform(&Node{
		Cache: &cacheFalse,
		Cmds: []Cmd{{
			CmdArgs: cmdArgs,
			Cwd:     testBuildRoot,
		}},
		Env:    testEnv(p, "unittest"),
		Inputs: []VFS{Source(info.ProjectPath)},
		KV: KV{
			P:                pkTS,
			Path:             path.Join(info.ProjectPath, "unittest"),
			PC:               pcYellow,
			RunTestNode:      true,
			ShowOutBool:      true,
			HasSpecialRunner: true,
		},
		Outputs:  testOutputs(info.ProjectPath, "unittest"),
		Platform: string(p.Target),
		Requirements: Requirements{
			CPU:        1,
			Network:    "restricted",
			RAM:        8,
			HasRAMDisk: true,
		},
		Tags:             sandboxingNodeTags(p),
		TargetProperties: TargetProperties{ModuleLang: "cpp"},
	}, p)
}

func buildClangFormatNode(p *Platform, info testSuiteInfo) *Node {
	cacheTrue := true
	resultsDir := path.Join(info.ProjectPath, "test-results", "clang_format")

	cmdArgs := []ANY{
		internAny(testToolHostPath),
		anyRunTest,
		anyYaStartCommandFile,
		anyMeta, vfsAny(Build(path.Join(resultsDir, "meta.json"))),
		anyTrace, vfsAny(Build(path.Join(resultsDir, "ytest.report.trace"))),
		anyTimeout, any60,
		anyLogPath, vfsAny(Build(path.Join(resultsDir, "run_test.log"))),
		anyTestSize, anySmall,
		anyTestType, anyClangFormat,
		anyTestCiType, anyStyle,
		anyContextFilename, internAny(testContextPath),
		anySourceRoot, internAny(testSourceRoot),
		anyBuildRoot, internAny(testBuildRoot),
		anyTestSuiteName, anyClangFormat,
		anyProjectPath, internAny(info.ProjectPath),
		anyTestRelatedPath, vfsAny(Source(testSVNInterfaceRel)),
	}

	for _, src := range info.CppSources {
		cmdArgs = append(cmdArgs, anyTestRelatedPath, vfsAny(Source(src)))
	}

	cmdArgs = append(cmdArgs,
		anyTestRelatedPath, vfsAny(Source(testClangFormatConfigRel)),
		anyTestRelatedPath, vfsAny(Source(testClangFormatWrapperRel)),
		anyTargetPlatformDescriptor, internAny(targetPlatformDescriptor(p)),
		anyRemoveTos,
		anyGdbPath, internAny(testGDBPath),
		anyResultMaxFileSize, any0,
		anyVerifyResults,
		anyTestsLimitInChunk, any100000,
		anyOutputStyle, anyNinja,
		anyPythonBin, internAny(testPythonBin),
		anySupportsTestParameters,
		anyCompressionFilter, anyZstd,
		anyCompressionLevel, any1,
		anyGlobalResource, internAny(testClangFormatResource),
		anyRamLimitGb, any8,
		anyTar, vfsAny(Build(path.Join(resultsDir, "testing_out_stuff.tar.zstd"))),
		internAny(testToolHostPath),
		anyRunCustomLint,
		anySourceRoot, internAny(testSourceRoot),
		anyBuildRoot, internAny(testBuildRoot),
		anyProjectPath, vfsAny(Source(info.ProjectPath)),
		anyTracePath, vfsAny(Build(path.Join(resultsDir, "ytest.report.trace"))),
		anyOutPath, vfsAny(Build(path.Join(resultsDir, "testing_out_stuff"))),
		anyLintName, anyClangFormat,
		anyWrapperScript, internAny(testClangFormatWrapperRel),
		anyDepends, internAny(info.ProjectPath),
		anyConfig, vfsAny(Source(testClangFormatConfigRel)),
		anyGlobalResource, internAny(testClangFormatResource),
		vfsAny(Source(testSVNInterfaceRel)),
	)

	for _, src := range info.CppSources {
		cmdArgs = append(cmdArgs, vfsAny(Source(src)))
	}

	cmdArgs = append(cmdArgs, anyYaEndCommandFile)

	inputs := []VFS{
		Source(testClangFormatConfigRel),
		Source(testSVNInterfaceRel),
		Source(testClangFormatWrapperRel),
		Source(info.ProjectPath),
	}

	for _, src := range info.CppSources {
		inputs = append(inputs, Source(src))
	}

	return bindNodePlatform(&Node{
		Cache: &cacheTrue,
		Cmds: []Cmd{{
			CmdArgs: cmdArgs,
			Cwd:     testBuildRoot,
		}},
		Env:    testEnv(p, "clang_format"),
		Inputs: inputs,
		KV: KV{
			P:                pkTS,
			Path:             path.Join(info.ProjectPath, "clang_format"),
			PC:               pcYellow,
			RunTestNode:      true,
			ShowOutBool:      true,
			HasSpecialRunner: true,
		},
		Outputs:  testOutputs(info.ProjectPath, "clang_format"),
		Platform: string(p.Target),
		Requirements: Requirements{
			CPU:        1,
			Network:    "restricted",
			RAM:        8,
			HasRAMDisk: true,
		},
		Tags:             nil,
		TargetProperties: TargetProperties{ModuleLang: "unknown"},
	}, p)
}

func testEnv(_ *Platform, testName string) EnvVars {
	return EnvVars{{Name: "ARCADIA_BUILD_ROOT", Value: testBuildRoot}, {Name: "ARCADIA_ROOT_DISTBUILD", Value: testSourceRoot}, {Name: "ARCADIA_SOURCE_ROOT", Value: testSourceRoot}, {Name: "ASAN_OPTIONS", Value: "exitcode=100"}, {Name: "ASAN_SYMBOLIZER_PATH", Value: testClangSymbolizerPath}, {Name: "GORACE", Value: "halt_on_error=1"}, {Name: "LSAN_OPTIONS", Value: "exitcode=100"}, {Name: "LSAN_SYMBOLIZER_PATH", Value: testClangSymbolizerPath}, {Name: "MSAN_OPTIONS", Value: "exitcode=100:report_umrs=1"}, {Name: "MSAN_SYMBOLIZER_PATH", Value: testClangSymbolizerPath}, {Name: "TESTING_SAVE_OUTPUT", Value: "yes"}, {Name: "TEST_NAME", Value: testName}, {Name: "TSAN_SYMBOLIZER_PATH", Value: testClangSymbolizerPath}, {Name: "UBSAN_OPTIONS", Value: "exitcode=100:print_stacktrace=1,halt_on_error=1"}, {Name: "UBSAN_SYMBOLIZER_PATH", Value: testClangSymbolizerPath}, {Name: "YA_CC", Value: testClangCCPath}, {Name: "YA_CXX", Value: testClangCXXPath}, {Name: "YA_MANDATORY_ENV_VARS", Value: testMandatoryEnvVars}, {Name: "YA_NO_RESPAWN", Value: "yes"}, {Name: "YA_PYTHON_BIN", Value: testPythonBin}, {Name: "YA_TC", Value: "no"}, {Name: "YA_TEST_RUNNER", Value: "1"}}
}

func sandboxingNodeTags(p *Platform) []string {
	if p == nil || p.Flags[envSANDBOXING] != strYes {
		return nil
	}

	return []string{
		string(p.Target),
		p.BuildType,
		"FAKEID=sandboxing",
		"SANDBOXING=yes",
	}
}

func targetPlatformDescriptor(p *Platform) string {
	parts := []string{string(p.Target), p.BuildType}

	if p != nil && p.Flags[envSANDBOXING] == strYes {
		parts = append(parts, "FAKEID=sandboxing", "SANDBOXING=yes")
	}

	return strings.Join(parts, "-")
}

func buildTestSuiteInfo(instance ModuleInstance, d *moduleData, ldPath VFS) *testSuiteInfo {
	if d == nil || d.moduleStmt == nil {
		return nil
	}

	srcBase := instance.Path

	if d.moduleStmt.Name == tokUnittestFor && len(d.moduleStmt.Args) > 0 {
		srcBase = path.Clean(d.moduleStmt.Args[0])
	} else if d.srcDir != nil {
		srcBase = path.Clean(*d.srcDir)
	}

	cppSources := make([]string, 0, len(d.srcs))

	for _, src := range d.srcs {
		switch strings.ToLower(path.Ext(src)) {
		case ".c", ".cc", ".cpp", ".cxx":
			cppSources = append(cppSources, path.Clean(path.Join(srcBase, src)))
		}
	}

	return &testSuiteInfo{
		ProjectPath: instance.Path,
		BinaryPath:  ldPath.String(),
		CppSources:  cppSources,
	}
}

func testOutputs(projectPath, suite string) []VFS {
	resultsDir := path.Join(projectPath, "test-results", suite)

	return []VFS{
		Build(path.Join(resultsDir, "meta.json")),
		Build(path.Join(resultsDir, "ytest.report.trace")),
		Build(path.Join(resultsDir, "run_test.log")),
		Build(path.Join(resultsDir, "testing_out_stuff.tar.zstd")),
	}
}
