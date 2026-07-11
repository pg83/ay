package main

import (
	"path"
	"strings"
)

var (
	testNodesKV             = KV{P: pkCP, PC: pcLightBlue}
	testContextRequirements = Requirements{Network: nwRestricted}
	testRunRequirements     = Requirements{CPU: 1, Network: nwRestricted, RAM: 8, HasRAMDisk: true}
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

type TestSuiteInfo struct {
	ProjectPath string
	BinaryPath  string
	CppSources  []string
}

func emitTestRunNodes(ctxEmit *StreamingEmitter, runEmit *StreamingEmitter, p *Platform, info TestSuiteInfo, ldRef NodeRef, resourceGlobals []ResourceDecl) []NodeRef {
	ctxRef := ctxEmit.emit(buildTestCtxNode(ctxEmit.nodeArenas(), p))
	unittest := buildUnittestNode(runEmit.nodeArenas(), p, info, resourceGlobals)

	unittest.DepRefs = []NodeRef{ldRef, ctxRef}

	unittestRef := runEmit.emit(unittest)
	clangFormat := buildClangFormatNode(runEmit.nodeArenas(), p, info)

	clangFormat.DepRefs = []NodeRef{ctxRef}

	clangFormatRef := runEmit.emit(clangFormat)

	return []NodeRef{unittestRef, clangFormatRef}
}

func buildTestCtxNode(na *NodeArenas, p *Platform) *Node {
	return &Node{
		Platform: p,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(internStr(testYMakePython3).any(),
			(source(testAppendFileScriptRel)).any(),
			internStr(testContextPath).any(),
			arg3.any(),
			internStr(`  "build_type": "`+p.BuildType+`",`).any(),
			argFlags.any(),
			argTestsRequestedYes.any(),
			arg4.any(),
			arg5.any()))}),
		Env:          nil,
		Inputs:       na.inputList(na.vfsList(source(testAppendFileScriptRel))),
		KV:           &testNodesKV,
		Outputs:      na.vfsList(bldCommonTestContext),
		Requirements: &testContextRequirements,
		Resources:    usesPython3,
	}
}

func buildUnittestNode(na *NodeArenas, p *Platform, info TestSuiteInfo, resourceGlobals []ResourceDecl) *Node {
	resultsDir := path.Join(info.ProjectPath, "test-results", "unittest")

	cmdArgs := []ANY{
		internStr(testToolHostPath).any(),
		argRunTest.any(),
		argYaStartCommandFile.any(),
		argMeta.any(), (build(path.Join(resultsDir, "meta.json"))).any(),
		argTrace.any(), (build(path.Join(resultsDir, "ytest.report.trace"))).any(),
		argTimeout.any(), arg60.any(),
		argLogPath.any(), (build(path.Join(resultsDir, "run_test.log"))).any(),
		argTestSize.any(), argSmall.any(),
		argTestType.any(), argUnittest.any(),
		argTestCiType.any(), argTest.any(),
		argContextFilename.any(), internStr(testContextPath).any(),
		argSourceRoot.any(), internStr(testSourceRoot).any(),
		argBuildRoot.any(), internStr(testBuildRoot).any(),
		argTestSuiteName.any(), argUnittest.any(),
		argProjectPath.any(), internStr(info.ProjectPath).any(),
		argTestRelatedPath.any(), (source(info.ProjectPath)).any(),
		argTargetPlatformDescriptor.any(), internStr(targetPlatformDescriptor(p)).any(),
		argRemoveTos.any(),
		argGdbPath.any(), internStr(testGDBPath).any(),
		argResultMaxFileSize.any(), arg0.any(),
		argVerifyResults.any(),
		argTestsLimitInChunk.any(), arg100000.any(),
		argOutputStyle.any(), argNinja.any(),
		argPythonBin.any(), internStr(testPythonBin).any(),
		argSupportsTestParameters.any(),
		argSmoothShutdownSignals.any(), argSigusr2.any(),
		argCompressionFilter.any(), argZstd.any(),
		argCompressionLevel.any(), arg1.any(),
	}

	for _, r := range sortedResourceGlobals(resourceGlobals) {
		cmdArgs = append(cmdArgs, argGlobalResource.any(), r.Token.any())
	}

	cmdArgs = append(cmdArgs,
		argRamLimitGb.any(), arg8.any(),
		argTar.any(), (build(path.Join(resultsDir, "testing_out_stuff.tar.zstd"))).any(),
		internStr(testToolHostPath).any(),
		argRunUt.any(),
		argBinary.any(), internStr(info.BinaryPath).any(),
		argTracePath.any(), (build(path.Join(resultsDir, "ytest.report.trace"))).any(),
		argOutputDir.any(), (build(path.Join(resultsDir, "testing_out_stuff"))).any(),
		argModulo.any(), arg1.any(),
		argModuloIndex.any(), arg0.any(),
		argPartitionMode.any(), argSequential.any(),
		argProjectPath.any(), internStr(info.ProjectPath).any(),
		argListTimeout.any(), arg30.any(),
		argVerbose.any(),
		argGdbPath.any(), internStr(testGDBPath).any(),
		argYaEndCommandFile.any(),
	)

	return &Node{
		Platform: p,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Cwd: cwdVFS(testBuildRoot)}),
		Env:    testEnv(p, "unittest"),
		Inputs: na.inputList(na.vfsList(source(info.ProjectPath))),
		KV: &KV{
			P:                pkTS,
			Path:             path.Join(info.ProjectPath, "unittest"),
			PC:               pcYellow,
			RunTestNode:      true,
			ShowOutBool:      true,
			HasSpecialRunner: true,
			DisableCache:     true,
		},
		Outputs:      testOutputs(info.ProjectPath, "unittest"),
		Requirements: &testRunRequirements,
	}
}

func buildClangFormatNode(na *NodeArenas, p *Platform, info TestSuiteInfo) *Node {
	resultsDir := path.Join(info.ProjectPath, "test-results", "clang_format")

	cmdArgs := []ANY{
		internStr(testToolHostPath).any(),
		argRunTest.any(),
		argYaStartCommandFile.any(),
		argMeta.any(), (build(path.Join(resultsDir, "meta.json"))).any(),
		argTrace.any(), (build(path.Join(resultsDir, "ytest.report.trace"))).any(),
		argTimeout.any(), arg60.any(),
		argLogPath.any(), (build(path.Join(resultsDir, "run_test.log"))).any(),
		argTestSize.any(), argSmall.any(),
		argTestType.any(), argClangFormat.any(),
		argTestCiType.any(), argStyle.any(),
		argContextFilename.any(), internStr(testContextPath).any(),
		argSourceRoot.any(), internStr(testSourceRoot).any(),
		argBuildRoot.any(), internStr(testBuildRoot).any(),
		argTestSuiteName.any(), argClangFormat.any(),
		argProjectPath.any(), internStr(info.ProjectPath).any(),
		argTestRelatedPath.any(), (source(testSVNInterfaceRel)).any(),
	}

	for _, src := range info.CppSources {
		cmdArgs = append(cmdArgs, argTestRelatedPath.any(), (source(src)).any())
	}

	cmdArgs = append(cmdArgs,
		argTestRelatedPath.any(), (source(testClangFormatConfigRel)).any(),
		argTestRelatedPath.any(), (source(testClangFormatWrapperRel)).any(),
		argTargetPlatformDescriptor.any(), internStr(targetPlatformDescriptor(p)).any(),
		argRemoveTos.any(),
		argGdbPath.any(), internStr(testGDBPath).any(),
		argResultMaxFileSize.any(), arg0.any(),
		argVerifyResults.any(),
		argTestsLimitInChunk.any(), arg100000.any(),
		argOutputStyle.any(), argNinja.any(),
		argPythonBin.any(), internStr(testPythonBin).any(),
		argSupportsTestParameters.any(),
		argCompressionFilter.any(), argZstd.any(),
		argCompressionLevel.any(), arg1.any(),
		argGlobalResource.any(), internStr(testClangFormatResource).any(),
		argRamLimitGb.any(), arg8.any(),
		argTar.any(), (build(path.Join(resultsDir, "testing_out_stuff.tar.zstd"))).any(),
		internStr(testToolHostPath).any(),
		argRunCustomLint.any(),
		argSourceRoot.any(), internStr(testSourceRoot).any(),
		argBuildRoot.any(), internStr(testBuildRoot).any(),
		argProjectPath.any(), (source(info.ProjectPath)).any(),
		argTracePath.any(), (build(path.Join(resultsDir, "ytest.report.trace"))).any(),
		argOutPath.any(), (build(path.Join(resultsDir, "testing_out_stuff"))).any(),
		argLintName.any(), argClangFormat.any(),
		argWrapperScript.any(), internStr(testClangFormatWrapperRel).any(),
		argDepends.any(), internStr(info.ProjectPath).any(),
		argConfig.any(), (source(testClangFormatConfigRel)).any(),
		argGlobalResource.any(), internStr(testClangFormatResource).any(),
		(source(testSVNInterfaceRel)).any(),
	)

	for _, src := range info.CppSources {
		cmdArgs = append(cmdArgs, (source(src)).any())
	}

	cmdArgs = append(cmdArgs, argYaEndCommandFile.any())

	inputs := []VFS{
		source(testClangFormatConfigRel),
		source(testSVNInterfaceRel),
		source(testClangFormatWrapperRel),
		source(info.ProjectPath),
	}

	for _, src := range info.CppSources {
		inputs = append(inputs, source(src))
	}

	return &Node{
		Platform: p,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Cwd: cwdVFS(testBuildRoot)}),
		Env:    testEnv(p, "clang_format"),
		Inputs: na.inputList(inputs),
		KV: &KV{
			P:                pkTS,
			Path:             path.Join(info.ProjectPath, "clang_format"),
			PC:               pcYellow,
			RunTestNode:      true,
			ShowOutBool:      true,
			HasSpecialRunner: true,
		},
		Outputs:      testOutputs(info.ProjectPath, "clang_format"),
		Requirements: &testRunRequirements,
	}
}

func testEnv(_ *Platform, testName string) EnvVars {
	return EnvVars{
		{Name: envARCADIA_BUILD_ROOT, Value: strB.any()},
		{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()},
		{Name: envARCADIA_SOURCE_ROOT, Value: strS.any()},
		{Name: envASAN_OPTIONS, Value: strTestAsanOpt.any()},
		{Name: envASAN_SYMBOLIZER_PATH, Value: strTestClangSymbolizer.any()},
		{Name: envGORACE, Value: strTestGorace.any()},
		{Name: envLSAN_OPTIONS, Value: strTestAsanOpt.any()},
		{Name: envLSAN_SYMBOLIZER_PATH, Value: strTestClangSymbolizer.any()},
		{Name: envMSAN_OPTIONS, Value: strTestMsanOpt.any()},
		{Name: envMSAN_SYMBOLIZER_PATH, Value: strTestClangSymbolizer.any()},
		{Name: envTESTING_SAVE_OUTPUT, Value: strYes.any()},
		{Name: envTEST_NAME, Value: internStr(testName).any()},
		{Name: envTSAN_SYMBOLIZER_PATH, Value: strTestClangSymbolizer.any()},
		{Name: envUBSAN_OPTIONS, Value: strTestUbsanOpt.any()},
		{Name: envUBSAN_SYMBOLIZER_PATH, Value: strTestClangSymbolizer.any()},
		{Name: envYA_CC, Value: strTestClangCC.any()},
		{Name: envYA_CXX, Value: strTestClangCXX.any()},
		{Name: envYA_MANDATORY_ENV_VARS, Value: strTestMandatoryEnvVars.any()},
		{Name: envYA_NO_RESPAWN, Value: strYes.any()},
		{Name: envYA_PYTHON_BIN, Value: strTestPythonBin.any()},
		{Name: envYA_TC, Value: strNo.any()},
		{Name: envYA_TEST_RUNNER, Value: strOne.any()},
	}
}

func targetPlatformDescriptor(p *Platform) string {
	parts := []string{string(p.Target), p.BuildType}

	if p != nil && p.Flags[envSANDBOXING] == strYes {
		parts = append(parts, "FAKEID=sandboxing", "SANDBOXING=yes")
	}

	return strings.Join(parts, "-")
}

func buildTestSuiteInfo(instance ModuleInstance, d *ModuleData, ldPath VFS) *TestSuiteInfo {
	if d == nil || d.moduleStmt == nil {
		return nil
	}

	srcBase := instance.Path.relString()

	if d.moduleStmt.Name == tokUnittestFor && len(d.moduleStmt.Args) > 0 {
		srcBase = path.Clean(d.moduleStmt.Args[0].string())
	}

	cppSources := make([]string, 0, len(d.srcs))

	for _, meta := range d.srcs {
		if meta.Global || meta.Compile.Variant != 0 {
			continue
		}

		src := meta.Source

		switch strings.ToLower(path.Ext(src.string())) {
		case ".c", ".cc", ".cpp", ".cxx":
			cppSources = append(cppSources, path.Clean(path.Join(srcBase, src.string())))
		}
	}

	return &TestSuiteInfo{
		ProjectPath: instance.Path.relString(),
		BinaryPath:  ldPath.string(),
		CppSources:  cppSources,
	}
}

func testOutputs(projectPath, suite string) []VFS {
	resultsDir := path.Join(projectPath, "test-results", suite)

	return []VFS{
		build(path.Join(resultsDir, "meta.json")),
		build(path.Join(resultsDir, "ytest.report.trace")),
		build(path.Join(resultsDir, "run_test.log")),
		build(path.Join(resultsDir, "testing_out_stuff.tar.zstd")),
	}
}
