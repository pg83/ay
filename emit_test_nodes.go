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
			CmdArgs: []STR{
				internStr(testYMakePython3),
				(Source(testAppendFileScriptRel)).str(),
				internStr(testContextPath),
				arg3.str(),
				internStr(`  "build_type": "` + p.BuildType + `",`),
				argFlags.str(),
				argTestsRequestedYes.str(),
				arg4.str(),
				arg5.str(),
			},
		}},
		Env:              nil,
		Inputs:           []VFS{Source(testAppendFileScriptRel)},
		KV:               KV{P: pkCP, PC: pcLightBlue},
		Outputs:          []VFS{bldCommonTestContext},
		Requirements:     Requirements{Network: "restricted"},
		Tags:             sandboxingNodeTags(p),
		TargetProperties: TargetProperties{},
	}, resourcePatternYMakePython3), p)
}

func buildUnittestNode(p *Platform, info testSuiteInfo) *Node {
	cacheFalse := false
	resultsDir := path.Join(info.ProjectPath, "test-results", "unittest")

	cmdArgs := []STR{
		internStr(testToolHostPath),
		argRunTest.str(),
		argYaStartCommandFile.str(),
		argMeta.str(), (Build(path.Join(resultsDir, "meta.json"))).str(),
		argTrace.str(), (Build(path.Join(resultsDir, "ytest.report.trace"))).str(),
		argTimeout.str(), arg60.str(),
		argLogPath.str(), (Build(path.Join(resultsDir, "run_test.log"))).str(),
		argTestSize.str(), argSmall.str(),
		argTestType.str(), argUnittest.str(),
		argTestCiType.str(), argTest.str(),
		argContextFilename.str(), internStr(testContextPath),
		argSourceRoot.str(), internStr(testSourceRoot),
		argBuildRoot.str(), internStr(testBuildRoot),
		argTestSuiteName.str(), argUnittest.str(),
		argProjectPath.str(), internStr(info.ProjectPath),
		argTestRelatedPath.str(), (Source(info.ProjectPath)).str(),
		argTargetPlatformDescriptor.str(), internStr(targetPlatformDescriptor(p)),
		argRemoveTos.str(),
		argGdbPath.str(), internStr(testGDBPath),
		argResultMaxFileSize.str(), arg0.str(),
		argVerifyResults.str(),
		argTestsLimitInChunk.str(), arg100000.str(),
		argOutputStyle.str(), argNinja.str(),
		argPythonBin.str(), internStr(testPythonBin),
		argSupportsTestParameters.str(),
		argSmoothShutdownSignals.str(), argSigusr2.str(),
		argCompressionFilter.str(), argZstd.str(),
		argCompressionLevel.str(), arg1.str(),
		argGlobalResource.str(), internStr(testClang14Resource),
		argGlobalResource.str(), internStr(testClang16Resource),
		argGlobalResource.str(), internStr(testClang18Resource),
		argGlobalResource.str(), internStr(testClang20Resource),
		argGlobalResource.str(), internStr(testClangFormatResource),
		argGlobalResource.str(), internStr(testClangResource),
		argGlobalResource.str(), internStr(testLLDRootResource),
		argGlobalResource.str(), internStr(testYMakePython3Resource),
		argRamLimitGb.str(), arg8.str(),
		argTar.str(), (Build(path.Join(resultsDir, "testing_out_stuff.tar.zstd"))).str(),
		internStr(testToolHostPath),
		argRunUt.str(),
		argBinary.str(), internStr(info.BinaryPath),
		argTracePath.str(), (Build(path.Join(resultsDir, "ytest.report.trace"))).str(),
		argOutputDir.str(), (Build(path.Join(resultsDir, "testing_out_stuff"))).str(),
		argModulo.str(), arg1.str(),
		argModuloIndex.str(), arg0.str(),
		argPartitionMode.str(), argSequential.str(),
		argProjectPath.str(), internStr(info.ProjectPath),
		argListTimeout.str(), arg30.str(),
		argVerbose.str(),
		argGdbPath.str(), internStr(testGDBPath),
		argYaEndCommandFile.str(),
	}

	return bindNodePlatform(&Node{
		Cache: &cacheFalse,
		Cmds: []Cmd{{
			CmdArgs: cmdArgs,
			Cwd:     internStr(testBuildRoot),
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
		Outputs: testOutputs(info.ProjectPath, "unittest"),
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

	cmdArgs := []STR{
		internStr(testToolHostPath),
		argRunTest.str(),
		argYaStartCommandFile.str(),
		argMeta.str(), (Build(path.Join(resultsDir, "meta.json"))).str(),
		argTrace.str(), (Build(path.Join(resultsDir, "ytest.report.trace"))).str(),
		argTimeout.str(), arg60.str(),
		argLogPath.str(), (Build(path.Join(resultsDir, "run_test.log"))).str(),
		argTestSize.str(), argSmall.str(),
		argTestType.str(), argClangFormat.str(),
		argTestCiType.str(), argStyle.str(),
		argContextFilename.str(), internStr(testContextPath),
		argSourceRoot.str(), internStr(testSourceRoot),
		argBuildRoot.str(), internStr(testBuildRoot),
		argTestSuiteName.str(), argClangFormat.str(),
		argProjectPath.str(), internStr(info.ProjectPath),
		argTestRelatedPath.str(), (Source(testSVNInterfaceRel)).str(),
	}

	for _, src := range info.CppSources {
		cmdArgs = append(cmdArgs, argTestRelatedPath.str(), (Source(src)).str())
	}

	cmdArgs = append(cmdArgs,
		argTestRelatedPath.str(), (Source(testClangFormatConfigRel)).str(),
		argTestRelatedPath.str(), (Source(testClangFormatWrapperRel)).str(),
		argTargetPlatformDescriptor.str(), internStr(targetPlatformDescriptor(p)),
		argRemoveTos.str(),
		argGdbPath.str(), internStr(testGDBPath),
		argResultMaxFileSize.str(), arg0.str(),
		argVerifyResults.str(),
		argTestsLimitInChunk.str(), arg100000.str(),
		argOutputStyle.str(), argNinja.str(),
		argPythonBin.str(), internStr(testPythonBin),
		argSupportsTestParameters.str(),
		argCompressionFilter.str(), argZstd.str(),
		argCompressionLevel.str(), arg1.str(),
		argGlobalResource.str(), internStr(testClangFormatResource),
		argRamLimitGb.str(), arg8.str(),
		argTar.str(), (Build(path.Join(resultsDir, "testing_out_stuff.tar.zstd"))).str(),
		internStr(testToolHostPath),
		argRunCustomLint.str(),
		argSourceRoot.str(), internStr(testSourceRoot),
		argBuildRoot.str(), internStr(testBuildRoot),
		argProjectPath.str(), (Source(info.ProjectPath)).str(),
		argTracePath.str(), (Build(path.Join(resultsDir, "ytest.report.trace"))).str(),
		argOutPath.str(), (Build(path.Join(resultsDir, "testing_out_stuff"))).str(),
		argLintName.str(), argClangFormat.str(),
		argWrapperScript.str(), internStr(testClangFormatWrapperRel),
		argDepends.str(), internStr(info.ProjectPath),
		argConfig.str(), (Source(testClangFormatConfigRel)).str(),
		argGlobalResource.str(), internStr(testClangFormatResource),
		(Source(testSVNInterfaceRel)).str(),
	)

	for _, src := range info.CppSources {
		cmdArgs = append(cmdArgs, (Source(src)).str())
	}

	cmdArgs = append(cmdArgs, argYaEndCommandFile.str())

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
			Cwd:     internStr(testBuildRoot),
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
		Outputs: testOutputs(info.ProjectPath, "clang_format"),
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
