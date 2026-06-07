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
			CmdArgs: []ANY{
				stringAny(testYMakePython3),
				vfsAny(Source(testAppendFileScriptRel)),
				stringAny(testContextPath),
				stringAny("{"),
				stringAny(`  "build_type": "` + p.BuildType + `",`),
				stringAny(`  "flags": {`),
				stringAny(`    "TESTS_REQUESTED": "yes"`),
				stringAny("  }"),
				stringAny("}"),
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
		stringAny("run_test"),
		stringAny("--ya-start-command-file"),
		stringAny("--meta"), vfsAny(Build(path.Join(resultsDir, "meta.json"))),
		stringAny("--trace"), vfsAny(Build(path.Join(resultsDir, "ytest.report.trace"))),
		stringAny("--timeout"), stringAny("60"),
		stringAny("--log-path"), vfsAny(Build(path.Join(resultsDir, "run_test.log"))),
		stringAny("--test-size"), stringAny("small"),
		stringAny("--test-type"), stringAny("unittest"),
		stringAny("--test-ci-type"), stringAny("test"),
		stringAny("--context-filename"), stringAny(testContextPath),
		stringAny("--source-root"), stringAny(testSourceRoot),
		stringAny("--build-root"), stringAny(testBuildRoot),
		stringAny("--test-suite-name"), stringAny("unittest"),
		stringAny("--project-path"), stringAny(info.ProjectPath),
		stringAny("--test-related-path"), vfsAny(Source(info.ProjectPath)),
		stringAny("--target-platform-descriptor"), stringAny(targetPlatformDescriptor(p)),
		stringAny("--remove-tos"),
		stringAny("--gdb-path"), stringAny(testGDBPath),
		stringAny("--result-max-file-size"), stringAny("0"),
		stringAny("--verify-results"),
		stringAny("--tests-limit-in-chunk"), stringAny("100000"),
		stringAny("--output-style"), stringAny("ninja"),
		stringAny("--python-bin"), stringAny(testPythonBin),
		stringAny("--supports-test-parameters"),
		stringAny("--smooth-shutdown-signals"), stringAny("SIGUSR2"),
		stringAny("--compression-filter"), stringAny("zstd"),
		stringAny("--compression-level"), stringAny("1"),
		stringAny("--global-resource"), stringAny(testClang14Resource),
		stringAny("--global-resource"), stringAny(testClang16Resource),
		stringAny("--global-resource"), stringAny(testClang18Resource),
		stringAny("--global-resource"), stringAny(testClang20Resource),
		stringAny("--global-resource"), stringAny(testClangFormatResource),
		stringAny("--global-resource"), stringAny(testClangResource),
		stringAny("--global-resource"), stringAny(testLLDRootResource),
		stringAny("--global-resource"), stringAny(testYMakePython3Resource),
		stringAny("--ram-limit-gb"), stringAny("8"),
		stringAny("--tar"), vfsAny(Build(path.Join(resultsDir, "testing_out_stuff.tar.zstd"))),
		stringAny(testToolHostPath),
		stringAny("run_ut"),
		stringAny("--binary"), stringAny(info.BinaryPath),
		stringAny("--trace-path"), vfsAny(Build(path.Join(resultsDir, "ytest.report.trace"))),
		stringAny("--output-dir"), vfsAny(Build(path.Join(resultsDir, "testing_out_stuff"))),
		stringAny("--modulo"), stringAny("1"),
		stringAny("--modulo-index"), stringAny("0"),
		stringAny("--partition-mode"), stringAny("SEQUENTIAL"),
		stringAny("--project-path"), stringAny(info.ProjectPath),
		stringAny("--list-timeout"), stringAny("30"),
		stringAny("--verbose"),
		stringAny("--gdb-path"), stringAny(testGDBPath),
		stringAny("--ya-end-command-file"),
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
		stringAny("run_test"),
		stringAny("--ya-start-command-file"),
		stringAny("--meta"), vfsAny(Build(path.Join(resultsDir, "meta.json"))),
		stringAny("--trace"), vfsAny(Build(path.Join(resultsDir, "ytest.report.trace"))),
		stringAny("--timeout"), stringAny("60"),
		stringAny("--log-path"), vfsAny(Build(path.Join(resultsDir, "run_test.log"))),
		stringAny("--test-size"), stringAny("small"),
		stringAny("--test-type"), stringAny("clang_format"),
		stringAny("--test-ci-type"), stringAny("style"),
		stringAny("--context-filename"), stringAny(testContextPath),
		stringAny("--source-root"), stringAny(testSourceRoot),
		stringAny("--build-root"), stringAny(testBuildRoot),
		stringAny("--test-suite-name"), stringAny("clang_format"),
		stringAny("--project-path"), stringAny(info.ProjectPath),
		stringAny("--test-related-path"), vfsAny(Source(testSVNInterfaceRel)),
	}

	for _, src := range info.CppSources {
		cmdArgs = append(cmdArgs, stringAny("--test-related-path"), vfsAny(Source(src)))
	}

	cmdArgs = append(cmdArgs,
		stringAny("--test-related-path"), vfsAny(Source(testClangFormatConfigRel)),
		stringAny("--test-related-path"), vfsAny(Source(testClangFormatWrapperRel)),
		stringAny("--target-platform-descriptor"), stringAny(targetPlatformDescriptor(p)),
		stringAny("--remove-tos"),
		stringAny("--gdb-path"), stringAny(testGDBPath),
		stringAny("--result-max-file-size"), stringAny("0"),
		stringAny("--verify-results"),
		stringAny("--tests-limit-in-chunk"), stringAny("100000"),
		stringAny("--output-style"), stringAny("ninja"),
		stringAny("--python-bin"), stringAny(testPythonBin),
		stringAny("--supports-test-parameters"),
		stringAny("--compression-filter"), stringAny("zstd"),
		stringAny("--compression-level"), stringAny("1"),
		stringAny("--global-resource"), stringAny(testClangFormatResource),
		stringAny("--ram-limit-gb"), stringAny("8"),
		stringAny("--tar"), vfsAny(Build(path.Join(resultsDir, "testing_out_stuff.tar.zstd"))),
		stringAny(testToolHostPath),
		stringAny("run_custom_lint"),
		stringAny("--source-root"), stringAny(testSourceRoot),
		stringAny("--build-root"), stringAny(testBuildRoot),
		stringAny("--project-path"), vfsAny(Source(info.ProjectPath)),
		stringAny("--trace-path"), vfsAny(Build(path.Join(resultsDir, "ytest.report.trace"))),
		stringAny("--out-path"), vfsAny(Build(path.Join(resultsDir, "testing_out_stuff"))),
		stringAny("--lint-name"), stringAny("clang_format"),
		stringAny("--wrapper-script"), stringAny(testClangFormatWrapperRel),
		stringAny("--depends"), stringAny(info.ProjectPath),
		stringAny("--config"), vfsAny(Source(testClangFormatConfigRel)),
		stringAny("--global-resource"), stringAny(testClangFormatResource),
		vfsAny(Source(testSVNInterfaceRel)),
	)

	for _, src := range info.CppSources {
		cmdArgs = append(cmdArgs, vfsAny(Source(src)))
	}

	cmdArgs = append(cmdArgs, stringAny("--ya-end-command-file"))

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
