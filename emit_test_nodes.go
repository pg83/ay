package main

import (
	"path"
	"strings"
)

var (
	// Path constants hoisted by `ay refac consts`.
	bldCommonTestContext = Build("common_test.context")
	// Path constants hoisted by `ay refac consts`.
	any0                        = stringAny("0")
	any1                        = stringAny("1")
	any100000                   = stringAny("100000")
	any3                        = stringAny("{")
	any30                       = stringAny("30")
	any4                        = stringAny("  }")
	any5                        = stringAny("}")
	any60                       = stringAny("60")
	any8                        = stringAny("8")
	anyBinary                   = stringAny("--binary")
	anyClangFormat              = stringAny("clang_format")
	anyCompressionFilter        = stringAny("--compression-filter")
	anyCompressionLevel         = stringAny("--compression-level")
	anyConfig                   = stringAny("--config")
	anyContextFilename          = stringAny("--context-filename")
	anyDepends                  = stringAny("--depends")
	anyFlags                    = stringAny("  \"flags\": {")
	anyGdbPath                  = stringAny("--gdb-path")
	anyGlobalResource           = stringAny("--global-resource")
	anyLintName                 = stringAny("--lint-name")
	anyListTimeout              = stringAny("--list-timeout")
	anyLogPath                  = stringAny("--log-path")
	anyMeta                     = stringAny("--meta")
	anyModulo                   = stringAny("--modulo")
	anyModuloIndex              = stringAny("--modulo-index")
	anyNinja                    = stringAny("ninja")
	anyOutPath                  = stringAny("--out-path")
	anyOutputDir                = stringAny("--output-dir")
	anyOutputStyle              = stringAny("--output-style")
	anyPartitionMode            = stringAny("--partition-mode")
	anyProjectPath              = stringAny("--project-path")
	anyPythonBin                = stringAny("--python-bin")
	anyRamLimitGb               = stringAny("--ram-limit-gb")
	anyRemoveTos                = stringAny("--remove-tos")
	anyResultMaxFileSize        = stringAny("--result-max-file-size")
	anyRunCustomLint            = stringAny("run_custom_lint")
	anyRunTest                  = stringAny("run_test")
	anyRunUt                    = stringAny("run_ut")
	anySequential               = stringAny("SEQUENTIAL")
	anySigusr2                  = stringAny("SIGUSR2")
	anySmall                    = stringAny("small")
	anySmoothShutdownSignals    = stringAny("--smooth-shutdown-signals")
	anyStyle                    = stringAny("style")
	anySupportsTestParameters   = stringAny("--supports-test-parameters")
	anyTar                      = stringAny("--tar")
	anyTargetPlatformDescriptor = stringAny("--target-platform-descriptor")
	anyTest                     = stringAny("test")
	anyTestCiType               = stringAny("--test-ci-type")
	anyTestRelatedPath          = stringAny("--test-related-path")
	anyTestSize                 = stringAny("--test-size")
	anyTestSuiteName            = stringAny("--test-suite-name")
	anyTestType                 = stringAny("--test-type")
	anyTestsLimitInChunk        = stringAny("--tests-limit-in-chunk")
	anyTestsRequestedYes        = stringAny("    \"TESTS_REQUESTED\": \"yes\"")
	anyTimeout                  = stringAny("--timeout")
	anyTrace                    = stringAny("--trace")
	anyTracePath                = stringAny("--trace-path")
	anyUnittest                 = stringAny("unittest")
	anyVerbose                  = stringAny("--verbose")
	anyVerifyResults            = stringAny("--verify-results")
	anyWrapperScript            = stringAny("--wrapper-script")
	anyZstd                     = stringAny("zstd")
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
				stringAny(testYMakePython3),
				vfsAny(Source(testAppendFileScriptRel)),
				stringAny(testContextPath),
				any3,
				stringAny(`  "build_type": "` + p.BuildType + `",`),
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
		stringAny(testToolHostPath),
		anyRunTest,
		anyYaStartCommandFile,
		anyMeta, vfsAny(Build(path.Join(resultsDir, "meta.json"))),
		anyTrace, vfsAny(Build(path.Join(resultsDir, "ytest.report.trace"))),
		anyTimeout, any60,
		anyLogPath, vfsAny(Build(path.Join(resultsDir, "run_test.log"))),
		anyTestSize, anySmall,
		anyTestType, anyUnittest,
		anyTestCiType, anyTest,
		anyContextFilename, stringAny(testContextPath),
		anySourceRoot, stringAny(testSourceRoot),
		anyBuildRoot, stringAny(testBuildRoot),
		anyTestSuiteName, anyUnittest,
		anyProjectPath, stringAny(info.ProjectPath),
		anyTestRelatedPath, vfsAny(Source(info.ProjectPath)),
		anyTargetPlatformDescriptor, stringAny(targetPlatformDescriptor(p)),
		anyRemoveTos,
		anyGdbPath, stringAny(testGDBPath),
		anyResultMaxFileSize, any0,
		anyVerifyResults,
		anyTestsLimitInChunk, any100000,
		anyOutputStyle, anyNinja,
		anyPythonBin, stringAny(testPythonBin),
		anySupportsTestParameters,
		anySmoothShutdownSignals, anySigusr2,
		anyCompressionFilter, anyZstd,
		anyCompressionLevel, any1,
		anyGlobalResource, stringAny(testClang14Resource),
		anyGlobalResource, stringAny(testClang16Resource),
		anyGlobalResource, stringAny(testClang18Resource),
		anyGlobalResource, stringAny(testClang20Resource),
		anyGlobalResource, stringAny(testClangFormatResource),
		anyGlobalResource, stringAny(testClangResource),
		anyGlobalResource, stringAny(testLLDRootResource),
		anyGlobalResource, stringAny(testYMakePython3Resource),
		anyRamLimitGb, any8,
		anyTar, vfsAny(Build(path.Join(resultsDir, "testing_out_stuff.tar.zstd"))),
		stringAny(testToolHostPath),
		anyRunUt,
		anyBinary, stringAny(info.BinaryPath),
		anyTracePath, vfsAny(Build(path.Join(resultsDir, "ytest.report.trace"))),
		anyOutputDir, vfsAny(Build(path.Join(resultsDir, "testing_out_stuff"))),
		anyModulo, any1,
		anyModuloIndex, any0,
		anyPartitionMode, anySequential,
		anyProjectPath, stringAny(info.ProjectPath),
		anyListTimeout, any30,
		anyVerbose,
		anyGdbPath, stringAny(testGDBPath),
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
		stringAny(testToolHostPath),
		anyRunTest,
		anyYaStartCommandFile,
		anyMeta, vfsAny(Build(path.Join(resultsDir, "meta.json"))),
		anyTrace, vfsAny(Build(path.Join(resultsDir, "ytest.report.trace"))),
		anyTimeout, any60,
		anyLogPath, vfsAny(Build(path.Join(resultsDir, "run_test.log"))),
		anyTestSize, anySmall,
		anyTestType, anyClangFormat,
		anyTestCiType, anyStyle,
		anyContextFilename, stringAny(testContextPath),
		anySourceRoot, stringAny(testSourceRoot),
		anyBuildRoot, stringAny(testBuildRoot),
		anyTestSuiteName, anyClangFormat,
		anyProjectPath, stringAny(info.ProjectPath),
		anyTestRelatedPath, vfsAny(Source(testSVNInterfaceRel)),
	}

	for _, src := range info.CppSources {
		cmdArgs = append(cmdArgs, anyTestRelatedPath, vfsAny(Source(src)))
	}

	cmdArgs = append(cmdArgs,
		anyTestRelatedPath, vfsAny(Source(testClangFormatConfigRel)),
		anyTestRelatedPath, vfsAny(Source(testClangFormatWrapperRel)),
		anyTargetPlatformDescriptor, stringAny(targetPlatformDescriptor(p)),
		anyRemoveTos,
		anyGdbPath, stringAny(testGDBPath),
		anyResultMaxFileSize, any0,
		anyVerifyResults,
		anyTestsLimitInChunk, any100000,
		anyOutputStyle, anyNinja,
		anyPythonBin, stringAny(testPythonBin),
		anySupportsTestParameters,
		anyCompressionFilter, anyZstd,
		anyCompressionLevel, any1,
		anyGlobalResource, stringAny(testClangFormatResource),
		anyRamLimitGb, any8,
		anyTar, vfsAny(Build(path.Join(resultsDir, "testing_out_stuff.tar.zstd"))),
		stringAny(testToolHostPath),
		anyRunCustomLint,
		anySourceRoot, stringAny(testSourceRoot),
		anyBuildRoot, stringAny(testBuildRoot),
		anyProjectPath, vfsAny(Source(info.ProjectPath)),
		anyTracePath, vfsAny(Build(path.Join(resultsDir, "ytest.report.trace"))),
		anyOutPath, vfsAny(Build(path.Join(resultsDir, "testing_out_stuff"))),
		anyLintName, anyClangFormat,
		anyWrapperScript, stringAny(testClangFormatWrapperRel),
		anyDepends, stringAny(info.ProjectPath),
		anyConfig, vfsAny(Source(testClangFormatConfigRel)),
		anyGlobalResource, stringAny(testClangFormatResource),
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
