package main

import (
	"cmp"
	"os"
	"path"
	"slices"
	"strings"
)

var testNodesKV = KV{P: pkCP, PC: pcLightBlue}

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

	if testType == "" {
		return nil
	}

	meta := e.d.test
	project := e.instance.Path.relString()
	name := testType
	benchmark := testType == "g_benchmark" || testType == "benchmark"

	if benchmark || testType == "boost_test" {
		name = path.Base(binary.relString())
	}

	s := &TestSuite{p: e.ctx.target, projectPath: project, name: name, testType: testType, ciType: "test", deps: depRefs(ref), globals: globals, smoothShutdown: []string{"SIGUSR2"}, runInputs: []VFS{source(project)}}

	s.applyTestMeta(meta, testType != "benchmark" && e.d.setVars[strADD_SRCDIR_TO_TEST_DATA] != strNo)

	if testType == "fuzz" {
		for _, file := range meta.fuzzDicts {
			s.relatedPaths = append(s.relatedPaths, source(file).string())
		}

		corpus, ok := os.LookupEnv("YA_TEST_CORPUS_DATA_PATH")

		if !ok {
			corpus = path.Join("fuzzing", project, "corpus.json")
		}

		if !path.IsAbs(corpus) {
			corpus = source(corpus).string()
		}

		s.relatedPaths = append([]string{corpus}, s.relatedPaths...)
	}

	python := testType == "pytest" || testType == "py3test" || testType == "exectest"

	s.canonicalData = python
	s.projectBuildDeps = python
	s.setupPythonpathEnv = python
	s.pythonNoBytecode = python
	s.disableLeaks = python
	s.supportsCanonization = python || benchmark
	s.supportsTestParameters = python || testType == "unittest" || testType == "gtest"

	if benchmark || testType == "boost_test" {
		s.split = 1
	}

	if meta.forkFiles {
		s.files = sortedUnique(meta.testFiles)
	}

	opts := e.ctx.tests

	s.runCmd = func(work string, chunk TestChunk) []ANY {
		return testInnerCommand(s, meta, binary.string(), work, chunk, opts)
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

	project := e.instance.Path.relString()

	s := &TestSuite{p: e.ctx.target, projectPath: project, name: "import_test", testType: "import_test", ciType: "test", size: "small",
		timeout: testSizeTimeouts["small"], runInputs: []VFS{source(project)}, checkFiles: []VFS{binary}, globals: globals,
		env: d.test.env, supportsTestParameters: true, smoothShutdown: []string{"SIGUSR2"}, importCheckEnv: true, noRetries: true, projectBuildDeps: true}

	skips := slices.Clone(d.noCheckImports)

	s.runCmd = func(work string, _ TestChunk) []ANY {
		out := work + "/testing_out_stuff"
		checker := "run_pyimports --markup"

		for _, skip := range skips {
			checker += " --skip " + skip.string()
		}

		checker += " --trace-dir=" + out

		args := []string{testToolHostPath, "run_check", "--source-root", testSourceRoot, "--checker", checker, "--check-name", "import_test",
			"--trace-path", work + "/ytest.report.trace", "--out-path", out, "--log-path", out + "/run_check.log"}

		for _, file := range s.checkFiles {
			args = append(args, file.string())
		}

		return testArgs(append(args, "--no-snippet-from-stdout"))
	}

	return []*TestSuite{s}
}

func (s *TestSuite) applyTestMeta(meta TestMeta, addSource bool) {
	s.size = cmp.Or(meta.size, "small")
	s.timeout = meta.timeout

	if s.timeout == 0 {
		s.timeout = testSizeTimeouts[s.size]
	}

	s.tags = sortedUnique(meta.tags)
	s.canonizeSubPath = meta.canonizeSubPath
	s.env = meta.env
	s.requirements = testRequirementsOf(meta.requirements)
	s.depends = meta.depends
	s.dockerImages = sortedUnique(meta.dockerImages)
	s.recipes = testRecipes(meta.recipes)
	s.data = normalizedTestData(meta.data)

	if addSource && !meta.explicitData {
		s.data = append(s.data, "arcadia/"+s.projectPath)
	}

	slices.Sort(s.data)
	s.data = slices.Compact(s.data)

	var dataPaths []string

	for _, entry := range s.data {
		if rel, ok := strings.CutPrefix(entry, "arcadia/"); ok {
			dataPaths = append(dataPaths, source(path.Clean(rel)).string())
		}
	}

	s.relatedPaths = append(s.relatedPaths, sortedUnique(dataPaths)...)

	if meta.cwd != "" {
		s.runCwd = source(meta.cwd).string()
	}

	s.split = meta.splitFactor

	if s.split == 0 && meta.forkMode != "" {
		s.split = 10
	}
}
