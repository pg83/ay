package main

import (
	"encoding/base64"
	"path"
	"slices"
	"strconv"
	"strings"
)

var (
	testNodesKV         = KV{P: pkCP, PC: pcLightBlue}
	testRunRequirements = Requirements{CPU: 1, Network: nwRestricted, RAM: 8, HasRAMDisk: true}
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
	Type         string
	SplitIndex   int
	SplitFile    string
	Name         string
	Size         string
	Timeout      int
	Cwd          string
	Data         []string
	Depends      []string
	Recipes      []string
	Tags         []string
	Env          []string
	BinaryArgs   []string
	ForkMode     string
	Partition    string
	SplitFactor  int
	ForkFiles    bool
	ExplicitData bool
	Requirements Requirements

	ProjectPath string
	BinaryPath  string
	CppSources  []string
}

func (ctx *GenCtx) emitTestRunNodes(info TestSuiteInfo, ldRef NodeRef, resourceGlobals []ResourceDecl) []NodeRef {
	emit, p := ctx.emit, ctx.target
	deps := []NodeRef{ldRef}

	for _, dep := range info.Depends {
		res := genModule(ctx, ModuleInstance{Path: source(dep), Kind: KindBin, Platform: p, Demand: demandLinked})

		if res.ARRef != 0 {
			deps = append(deps, res.ARRef)
		}
	}

	ctxRef := ctx.testContext()
	unittest := buildUnittestNode(emit.nodeArenas(), p, info, resourceGlobals)

	unittest.DepRefs = append(deps, ctxRef)

	unittestRef := emit.emit(unittest)
	clangFormat := buildClangFormatNode(emit.nodeArenas(), p, info)

	clangFormat.DepRefs = []NodeRef{ctxRef}

	clangFormatRef := emit.emit(clangFormat)

	return []NodeRef{unittestRef, clangFormatRef}
}

func buildUnittestNode(na *NodeArenas, p *Platform, info TestSuiteInfo, resourceGlobals []ResourceDecl) *Node {
	resultsDir := path.Join(info.ProjectPath, "test-results", info.Name)

	cmdArgs := []ANY{
		internStr(testToolHostPath).any(),
		argRunTest.any(),
		argYaStartCommandFile.any(),
		argMeta.any(), (build(path.Join(resultsDir, "meta.json"))).any(),
		argTrace.any(), (build(path.Join(resultsDir, "ytest.report.trace"))).any(),
		argTimeout.any(), internStr(strconv.Itoa(info.Timeout)).any(),
		argLogPath.any(), (build(path.Join(resultsDir, "run_test.log"))).any(),
		argTestSize.any(), internStr(info.Size).any(),
		argTestType.any(), internStr(info.Type).any(),
		argTestCiType.any(), argTest.any(),
		argContextFilename.any(), internStr(testContextPath).any(),
		argSourceRoot.any(), internStr(testSourceRoot).any(),
		argBuildRoot.any(), internStr(testBuildRoot).any(),
		argTestSuiteName.any(), internStr(info.Name).any(),
		argProjectPath.any(), internStr(info.ProjectPath).any(),
	}

	inputs := make([]VFS, 0, len(info.Data))

	for _, data := range info.Data {
		if strings.HasPrefix(data, "arcadia/") {
			v := source(strings.TrimPrefix(data, "arcadia/"))

			inputs = append(inputs, v)
			cmdArgs = append(cmdArgs, argTestRelatedPath.any(), v.any())
		}
	}

	cmdArgs = append(cmdArgs,

		argTargetPlatformDescriptor.any(), internStr(targetPlatformDescriptor(p)).any(),
		argRemoveTos.any(),
		argGdbPath.any(), internStr(testGDBPath).any(),
		argResultMaxFileSize.any(), arg0.any(),
		argVerifyResults.any(),
		argTestsLimitInChunk.any(), arg100000.any(),
		argOutputStyle.any(), argNinja.any(),
		argPythonBin.any(), internStr(testPythonBin).any(),
	)

	if info.Cwd != "" {
		cmdArgs = append(cmdArgs, strTestRunCwd.any(), source(info.Cwd).any())
	}

	for _, tag := range info.Tags {
		cmdArgs = append(cmdArgs, strTag.any(), internStr(tag).any())
	}

	for _, env := range info.Env {
		cmdArgs = append(cmdArgs, strEnv.any(), internStr(env).any())
	}

	if info.Type == "unittest" || info.Type == "gtest" {
		cmdArgs = append(cmdArgs, argSupportsTestParameters.any())
	}

	if info.Type == "g_benchmark" {
		cmdArgs = append(cmdArgs, strSupportsCanonization.any())
	}

	cmdArgs = append(cmdArgs, argSmoothShutdownSignals.any(), argSigusr2.any(), argCompressionFilter.any(), argZstd.any(), argCompressionLevel.any(), arg1.any())

	for _, r := range sortedResourceGlobals(resourceGlobals) {
		cmdArgs = append(cmdArgs, argGlobalResource.any(), r.Token.any())
	}

	if len(info.Recipes) > 0 {
		recipes := strings.Join(info.Recipes, " \n") + " \n"

		cmdArgs = append(cmdArgs, strRecipes.any(), internStr(base64.StdEncoding.EncodeToString([]byte(recipes))).any())
	}

	cmdArgs = append(cmdArgs, argRamLimitGb.any(), internStr(strconv.FormatFloat(info.Requirements.RAM, 'f', -1, 64)).any(), argTar.any(), build(path.Join(resultsDir, "testing_out_stuff.tar.zstd")).any())
	cmdArgs = append(cmdArgs, testInnerCommand(info, resultsDir)...)
	cmdArgs = append(cmdArgs, argYaEndCommandFile.any())

	return &Node{
		Platform: p,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Cwd: cwdVFS(testBuildRoot)}),
		Env:    testEnv(p, info.Name),
		Inputs: na.inputList(inputs),
		KV: &KV{
			P:                pkTS,
			Path:             path.Join(info.ProjectPath, info.Name),
			PC:               pcYellow,
			RunTestNode:      true,
			ShowOutBool:      true,
			HasSpecialRunner: true,
			DisableCache:     true,
		},
		Outputs:      testOutputs(info.ProjectPath, info.Name),
		Requirements: &info.Requirements,
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

	info := TestSuiteInfo{Size: "small", Requirements: testRunRequirements}

	if d.test != nil {
		info = *d.test
	}

	info.Type = testSuiteName(d.moduleStmt.Name)
	info.Name = info.Type

	if info.Type == "g_benchmark" {
		info.Name = path.Base(ldPath.relString())
	}

	info.ProjectPath = instance.Path.relString()
	info.BinaryPath = ldPath.string()
	info.CppSources = cppSources

	if info.Timeout == 0 {
		switch info.Size {
		case "medium":
			info.Timeout = 600
		case "large":
			info.Timeout = 3600
		default:
			info.Timeout = 60
		}
	}

	info.Data = slices.Clone(info.Data)

	if !info.ExplicitData {
		info.Data = append(info.Data, "arcadia/"+info.ProjectPath)
	}

	slices.Sort(info.Data)
	info.Data = slices.Compact(info.Data)

	return &info
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
