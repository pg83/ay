package main

import (
	"path"
	"strings"
)

const (
	testToolHostPath          = "$(TEST_TOOL_HOST)/test_tool"
	testContextPath           = "$(B)/common_test.context"
	testBuildRoot             = "$(B)"
	testSourceRoot            = "$(S)"
	testPythonBin             = "$(PYTHON)/python"
	testGDBPath               = "$(GDB)/gdb/bin/gdb"
	testYMakePython3          = "$(YMAKE_PYTHON3)/bin/python3"
	testClangSymbolizerPath   = "$(CLANG)/bin/llvm-symbolizer"
	testClangCCPath           = "$(CLANG)/bin/clang"
	testClangCXXPath          = "$(CLANG)/bin/clang++"
	testClangFormatResource   = "CLANG_FORMAT_RESOURCE_GLOBAL::$(CLANG_FORMAT)"
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

func emitTestRunNodes(ctxEmit Emitter, runEmit Emitter, p *Platform, info testSuiteInfo, ldRef NodeRef, resourceGlobals []resourceDecl) []NodeRef {
	ctxRef := ctxEmit.Emit(buildTestCtxNode(p))

	unittest := buildUnittestNode(p, info, resourceGlobals)
	unittest.DepRefs = []NodeRef{ldRef, ctxRef}
	unittestRef := runEmit.Emit(unittest)

	clangFormat := buildClangFormatNode(p, info)
	clangFormat.DepRefs = []NodeRef{ctxRef}
	clangFormatRef := runEmit.Emit(clangFormat)
	return []NodeRef{unittestRef, clangFormatRef}
}

func buildTestCtxNode(p *Platform) *Node {
	cacheTrue := true

	return &Node{
		Platform: p,
		Cache:    &cacheTrue,
		Cmds: []Cmd{{
			CmdArgs: argChunks{[]STR{
				internStr(testYMakePython3),
				(Source(testAppendFileScriptRel)).str(),
				internStr(testContextPath),
				arg3.str(),
				internStr(`  "build_type": "` + p.BuildType + `",`),
				argFlags.str(),
				argTestsRequestedYes.str(),
				arg4.str(),
				arg5.str(),
			}},
		}},
		Env:              nil,
		Inputs:           inputChunks{{Source(testAppendFileScriptRel)}},
		KV:               KV{P: pkCP, PC: pcLightBlue},
		Outputs:          []VFS{bldCommonTestContext},
		Requirements:     Requirements{Network: nwRestricted},
		TargetProperties: TargetProperties{},
		usesResources:    []string{resourcePatternYMakePython3},
	}
}

func buildUnittestNode(p *Platform, info testSuiteInfo, resourceGlobals []resourceDecl) *Node {
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
	}

	// --global-resource entries are the external-resource globals reachable through
	// the module-under-test's PEERDIR closure (the toolchain RESOURCES_LIBRARYs),
	// sorted by global-var name (upstream collects them in a std::set), replacing
	// the former hardcoded list.
	for _, r := range sortedResourceGlobals(resourceGlobals) {
		cmdArgs = append(cmdArgs, argGlobalResource.str(), r.Token)
	}

	cmdArgs = append(cmdArgs,
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
	)

	return &Node{
		Platform: p,
		Cache:    &cacheFalse,
		Cmds: []Cmd{{
			CmdArgs: argChunks{cmdArgs},
			Cwd:     internStr(testBuildRoot),
		}},
		Env:    testEnv(p, "unittest"),
		Inputs: inputChunks{{Source(info.ProjectPath)}},
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
			Network:    nwRestricted,
			RAM:        8,
			HasRAMDisk: true,
		},
		TargetProperties: TargetProperties{ModuleLang: mlCPP},
	}
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

	return &Node{
		Platform: p,
		Cache:    &cacheTrue,
		Cmds: []Cmd{{
			CmdArgs: argChunks{cmdArgs},
			Cwd:     internStr(testBuildRoot),
		}},
		Env:    testEnv(p, "clang_format"),
		Inputs: inputChunks{inputs},
		KV: KV{
			P:                pkTS,
			Path:             path.Join(info.ProjectPath, "clang_format"),
			PC:               pcYellow,
			RunTestNode:      true,
			ShowOutBool:      true,
			HasSpecialRunner: true,
		},
		Outputs: testOutputs(info.ProjectPath, "clang_format"),
		// The style/lint run node is tagless — carry the platform's TestTags (empty)
		// so it overrides the platform Tags rather than inheriting the sandboxing set.
		Tags: p.TestTags,
		Requirements: Requirements{
			CPU:        1,
			Network:    nwRestricted,
			RAM:        8,
			HasRAMDisk: true,
		},
		TargetProperties: TargetProperties{ModuleLang: mlUnknown},
	}
}

func testEnv(_ *Platform, testName string) EnvVars {
	return EnvVars{
		{Name: envARCADIA_BUILD_ROOT, Value: strB},
		{Name: envARCADIA_ROOT_DISTBUILD, Value: strS},
		{Name: envARCADIA_SOURCE_ROOT, Value: strS},
		{Name: envASAN_OPTIONS, Value: strTestAsanOpt},
		{Name: envASAN_SYMBOLIZER_PATH, Value: strTestClangSymbolizer},
		{Name: envGORACE, Value: strTestGorace},
		{Name: envLSAN_OPTIONS, Value: strTestAsanOpt},
		{Name: envLSAN_SYMBOLIZER_PATH, Value: strTestClangSymbolizer},
		{Name: envMSAN_OPTIONS, Value: strTestMsanOpt},
		{Name: envMSAN_SYMBOLIZER_PATH, Value: strTestClangSymbolizer},
		{Name: envTESTING_SAVE_OUTPUT, Value: strYes},
		{Name: envTEST_NAME, Value: internStr(testName)},
		{Name: envTSAN_SYMBOLIZER_PATH, Value: strTestClangSymbolizer},
		{Name: envUBSAN_OPTIONS, Value: strTestUbsanOpt},
		{Name: envUBSAN_SYMBOLIZER_PATH, Value: strTestClangSymbolizer},
		{Name: envYA_CC, Value: strTestClangCC},
		{Name: envYA_CXX, Value: strTestClangCXX},
		{Name: envYA_MANDATORY_ENV_VARS, Value: strTestMandatoryEnvVars},
		{Name: envYA_NO_RESPAWN, Value: strYes},
		{Name: envYA_PYTHON_BIN, Value: strTestPythonBin},
		{Name: envYA_TC, Value: strNo},
		{Name: envYA_TEST_RUNNER, Value: strOne},
	}
}

func sandboxingNodeTags(p *Platform) []STR {
	if p == nil || p.Flags[envSANDBOXING] != strYes {
		return nil
	}

	return internStrs([]string{
		string(p.Target),
		p.BuildType,
		"FAKEID=sandboxing",
		"SANDBOXING=yes",
	})
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

	srcBase := instance.Path.Rel()

	if d.moduleStmt.Name == tokUnittestFor && len(d.moduleStmt.Args) > 0 {
		srcBase = path.Clean(d.moduleStmt.Args[0])
	}

	cppSources := make([]string, 0, len(d.srcs))

	for _, src := range d.srcs {
		switch strings.ToLower(path.Ext(src)) {
		case ".c", ".cc", ".cpp", ".cxx":
			cppSources = append(cppSources, path.Clean(path.Join(srcBase, src)))
		}
	}

	return &testSuiteInfo{
		ProjectPath: instance.Path.Rel(),
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
