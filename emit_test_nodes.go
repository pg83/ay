package main

import (
	"path"
	"strings"
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
			CmdArgs: []string{
				testYMakePython3,
				Source(testAppendFileScriptRel).String(),
				testContextPath,
				"{",
				`  "build_type": "` + p.BuildType + `",`,
				`  "flags": {`,
				`    "TESTS_REQUESTED": "yes"`,
				"  }",
				"}",
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

	cmdArgs := []string{
		testToolHostPath,
		"run_test",
		"--ya-start-command-file",
		"--meta", Build(path.Join(resultsDir, "meta.json")).String(),
		"--trace", Build(path.Join(resultsDir, "ytest.report.trace")).String(),
		"--timeout", "60",
		"--log-path", Build(path.Join(resultsDir, "run_test.log")).String(),
		"--test-size", "small",
		"--test-type", "unittest",
		"--test-ci-type", "test",
		"--context-filename", testContextPath,
		"--source-root", testSourceRoot,
		"--build-root", testBuildRoot,
		"--test-suite-name", "unittest",
		"--project-path", info.ProjectPath,
		"--test-related-path", Source(info.ProjectPath).String(),
		"--target-platform-descriptor", targetPlatformDescriptor(p),
		"--remove-tos",
		"--gdb-path", testGDBPath,
		"--result-max-file-size", "0",
		"--verify-results",
		"--tests-limit-in-chunk", "100000",
		"--output-style", "ninja",
		"--python-bin", testPythonBin,
		"--supports-test-parameters",
		"--smooth-shutdown-signals", "SIGUSR2",
		"--compression-filter", "zstd",
		"--compression-level", "1",
		"--global-resource", testClang14Resource,
		"--global-resource", testClang16Resource,
		"--global-resource", testClang18Resource,
		"--global-resource", testClang20Resource,
		"--global-resource", testClangFormatResource,
		"--global-resource", testClangResource,
		"--global-resource", testLLDRootResource,
		"--global-resource", testYMakePython3Resource,
		"--ram-limit-gb", "8",
		"--tar", Build(path.Join(resultsDir, "testing_out_stuff.tar.zstd")).String(),
		testToolHostPath,
		"run_ut",
		"--binary", info.BinaryPath,
		"--trace-path", Build(path.Join(resultsDir, "ytest.report.trace")).String(),
		"--output-dir", Build(path.Join(resultsDir, "testing_out_stuff")).String(),
		"--modulo", "1",
		"--modulo-index", "0",
		"--partition-mode", "SEQUENTIAL",
		"--project-path", info.ProjectPath,
		"--list-timeout", "30",
		"--verbose",
		"--gdb-path", testGDBPath,
		"--ya-end-command-file",
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

	cmdArgs := []string{
		testToolHostPath,
		"run_test",
		"--ya-start-command-file",
		"--meta", Build(path.Join(resultsDir, "meta.json")).String(),
		"--trace", Build(path.Join(resultsDir, "ytest.report.trace")).String(),
		"--timeout", "60",
		"--log-path", Build(path.Join(resultsDir, "run_test.log")).String(),
		"--test-size", "small",
		"--test-type", "clang_format",
		"--test-ci-type", "style",
		"--context-filename", testContextPath,
		"--source-root", testSourceRoot,
		"--build-root", testBuildRoot,
		"--test-suite-name", "clang_format",
		"--project-path", info.ProjectPath,
		"--test-related-path", Source(testSVNInterfaceRel).String(),
	}

	for _, src := range info.CppSources {
		cmdArgs = append(cmdArgs, "--test-related-path", Source(src).String())
	}

	cmdArgs = append(cmdArgs,
		"--test-related-path", Source(testClangFormatConfigRel).String(),
		"--test-related-path", Source(testClangFormatWrapperRel).String(),
		"--target-platform-descriptor", targetPlatformDescriptor(p),
		"--remove-tos",
		"--gdb-path", testGDBPath,
		"--result-max-file-size", "0",
		"--verify-results",
		"--tests-limit-in-chunk", "100000",
		"--output-style", "ninja",
		"--python-bin", testPythonBin,
		"--supports-test-parameters",
		"--compression-filter", "zstd",
		"--compression-level", "1",
		"--global-resource", testClangFormatResource,
		"--ram-limit-gb", "8",
		"--tar", Build(path.Join(resultsDir, "testing_out_stuff.tar.zstd")).String(),
		testToolHostPath,
		"run_custom_lint",
		"--source-root", testSourceRoot,
		"--build-root", testBuildRoot,
		"--project-path", Source(info.ProjectPath).String(),
		"--trace-path", Build(path.Join(resultsDir, "ytest.report.trace")).String(),
		"--out-path", Build(path.Join(resultsDir, "testing_out_stuff")).String(),
		"--lint-name", "clang_format",
		"--wrapper-script", testClangFormatWrapperRel,
		"--depends", info.ProjectPath,
		"--config", Source(testClangFormatConfigRel).String(),
		"--global-resource", testClangFormatResource,
		Source(testSVNInterfaceRel).String(),
	)

	for _, src := range info.CppSources {
		cmdArgs = append(cmdArgs, Source(src).String())
	}

	cmdArgs = append(cmdArgs, "--ya-end-command-file")

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

// Path constants hoisted by `ay refac consts`.
var (
	bldCommonTestContext = Build("common_test.context")
)
