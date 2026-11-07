package main

import (
	"cmp"
	"os"
	"path"
	"slices"
	"strings"
)

var (
	testNodesKV                = KV{P: pkCP, PC: pcLightBlue}
	strTestToolHostPath        = internStr(testToolHostPath)
	strTestYMakePython3        = internStr(testYMakePython3)
	srcTestAppendFileScriptRel = source(testAppendFileScriptRel)
)

const (
	testToolHostPath          = "$(TEST_TOOL_HOST)/test_tool"
	testBuildRoot             = "$(B)"
	testSourceRoot            = "$(S)"
	testPythonBin             = "$(PYTHON)/python"
	testGDBPath               = "$(GDB)/gdb/bin/gdb"
	testYMakePython3          = "$(YMAKE_PYTHON3)/bin/python3"
	testClangSymbolizerPath   = "$(CLANG)/bin/llvm-symbolizer"
	testClangCCPath           = "$(CLANG)/bin/clang"
	testClangCXXPath          = "$(CLANG)/bin/clang++"
	testClangFormatWrapperRel = "tools/cpp_style_checker/wrapper.py"
	testAppendFileScriptRel   = "build/scripts/append_file.py"
)

func (e *EmitContext) testRunSuites(binary VFS, ref NodeRef, globals []ResourceDecl) []*TestSuite {
	if !e.ctx.testMode {
		return nil
	}

	testType := testSuiteName(e.d.moduleStmt.Name)

	if testType == 0 {
		return nil
	}

	meta := e.d.test
	project := e.instance.Path.relString()
	name := testType
	benchmark := testType == strGBenchmark || testType == strBenchmark

	if benchmark || testType == strBoostTest {
		name = internStr(path.Base(binary.relString()))
	}

	s := &TestSuite{p: e.ctx.target, projectPath: e.instance.Path.rel(), name: name, testType: testType, ciType: strTest, deps: depRefs(ref), globals: globals, smoothShutdown: []string{"SIGUSR2"}, runInputs: []VFS{e.instance.Path.rel().source()}}

	s.applyTestMeta(meta, testType != strBenchmark && e.d.setVars.get(strADD_SRCDIR_TO_TEST_DATA) != strNo)
	s.skipped = !Environment{s: &e.frame.ifEnv}.is(envSKIP_TEST_VALUE, strEmpty)

	if testType == strFuzz {
		for _, file := range meta.fuzzDicts {
			s.relatedPaths = append(s.relatedPaths, source(file).any())
		}

		corpus, ok := os.LookupEnv("YA_TEST_CORPUS_DATA_PATH")

		if !ok {
			corpus = path.Join("fuzzing", project, "corpus.json")
		}

		s.corpus = corpus

		var corpusArg ANY

		if path.IsAbs(corpus) {
			corpusArg = internStr(corpus).any()
		} else {
			corpusArg = source(corpus).any()
		}

		s.relatedPaths = append([]ANY{corpusArg}, s.relatedPaths...)
	}

	python := testType == strPytest || testType == strPy3test || testType == strExectest

	s.canonicalData = python
	s.projectBuildDeps = python
	s.setupPythonpathEnv = python
	s.pythonNoBytecode = python
	s.disableLeaks = python
	s.supportsCanonization = python || benchmark
	s.supportsTestParameters = python || testType == strUnittest || testType == strGtest

	if benchmark || testType == strBoostTest {
		s.split = 1
	}

	if meta.forkFiles {
		s.files = sortedUniqueByText(meta.testFiles)
	}

	opts := e.ctx.tests

	s.runCmd = func(work string, chunk TestChunk) []ANY {
		return testInnerCommand(s, meta, binary, work, chunk, opts)
	}

	return []*TestSuite{s}
}

func (e *EmitContext) importTestSuites(binary VFS, globals []ResourceDecl) []*TestSuite {
	d := e.d

	if !e.ctx.testMode || !isPyProgramType(d.moduleStmt.Name) && !d.pythonSourceProgram || d.noCheckImportsDisabled {
		return nil
	}

	if e.instance.Platform.ifEnv.bool(envCppAnalysisMode) {
		return nil
	}

	s := &TestSuite{p: e.ctx.target, projectPath: e.instance.Path.rel(), name: strImportTest, testType: strImportTest, ciType: strTest, size: strSmall,
		timeout: 60, runInputs: []VFS{e.instance.Path.rel().source()}, checkFiles: []VFS{binary}, globals: globals,
		env: d.test.env, supportsTestParameters: true, smoothShutdown: []string{"SIGUSR2"}, importCheckEnv: true, noRetries: true, projectBuildDeps: true}

	skips := slices.Clone(d.noCheckImports)

	s.runCmd = func(work string, _ TestChunk) []ANY {
		out := work + "/testing_out_stuff"
		checker := "run_pyimports --markup"

		for _, skip := range skips {
			checker += " --skip " + skip.string()
		}

		checker += " --trace-dir=" + out

		args := []ANY{testToolHostPathArg, strRunCheck.any(), strSourceRoot2.any(), testSourceRootArg, strChecker.any(), testArg(checker), strCheckName.any(), strImportTest.any(),
			strTracePath.any(), testArg(work + "/ytest.report.trace"), strOutPath.any(), testArg(out), strLogPath.any(), testArg(out + "/run_check.log")}

		for _, file := range s.checkFiles {
			args = append(args, file.any())
		}

		return append(args, strNoSnippetFromStdout.any())
	}

	return []*TestSuite{s}
}

func (s *TestSuite) applyRequirements(entries []string) {
	s.requirements = testRequirementsOf(entries)

	for _, name := range testSecretRequirements {
		if _, ok := s.requirements[name]; ok {
			s.tags = sortedUnique(append(s.tags, "ya:external"))

			return
		}
	}
}

func (s *TestSuite) applyTestMeta(meta TestMeta, addSource bool) {
	s.size = internStr(cmp.Or(meta.size, "small"))
	s.timeout = meta.timeout

	if s.timeout == 0 {
		s.timeout = testSizeTimeout(s.size)
	}

	s.tags = sortedUnique(meta.tags)
	s.canonizeSubPath = meta.canonizeSubPath
	s.env = meta.env
	s.applyRequirements(meta.requirements)
	s.depends = meta.depends
	s.dockerImages = sortedUnique(meta.dockerImages)
	s.recipes = testRecipes(meta.recipes)
	s.data = normalizedTestData(meta.data)

	if addSource && !meta.explicitData {
		s.data = append(s.data, "arcadia/"+s.projectPath.string())
	}

	slices.Sort(s.data)
	s.data = slices.Compact(s.data)

	var dataPaths []ANY

	for _, entry := range s.data {
		if rel, ok := strings.CutPrefix(entry, "arcadia/"); ok {
			dataPaths = append(dataPaths, source(path.Clean(rel)).any())
		}
	}

	s.relatedPaths = append(s.relatedPaths, sortedUniqueByText(dataPaths)...)

	if meta.cwd != "" {
		s.runCwd = source(meta.cwd).string()
	}

	s.split = meta.splitFactor

	if s.split == 0 && meta.forkMode != "" {
		s.split = 10
	}
}
