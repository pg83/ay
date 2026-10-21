package main

import (
	"cmp"
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
	testClangFormatResource   = "CLANG_FORMAT_RESOURCE_GLOBAL::$(CLANG_FORMAT)"
	testSVNInterfaceRel       = "build/scripts/c_templates/svn_interface.c"
	testClangFormatConfigRel  = "build/config/tests/cpp_style/.clang-format"
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

	if testType == "g_benchmark" || testType == "boost_test" {
		name = path.Base(binary.relString())
	}

	s := &TestSuite{p: e.ctx.target, projectPath: project, name: name, testType: testType, ciType: "test", deps: depRefs(ref), globals: globals, smoothShutdown: []string{"SIGUSR2"}}

	s.applyTestMeta(meta, e.d.setVars[strADD_SRCDIR_TO_TEST_DATA] != strNo)

	python := testType == "pytest" || testType == "py3test" || testType == "exectest"

	s.setupPythonpathEnv = python
	s.pythonNoBytecode = python
	s.disableLeaks = testType == "pytest" || testType == "py3test"
	s.supportsCanonization = python || testType == "g_benchmark"
	s.supportsTestParameters = python || testType == "unittest" || testType == "gtest"

	if testType == "g_benchmark" || testType == "boost_test" {
		s.split = 1
	}

	if meta.forkFiles {
		s.files = sortedUnique(meta.testFiles)
	}

	opts := e.ctx.tests

	s.runCmd = func(work string, chunk TestChunk) []ANY {
		return testInnerCommand(s, meta, binary.string(), work, chunk, opts)
	}

	suites := []*TestSuite{s}

	if !python {
		srcBase := project

		if e.d.moduleStmt.Name == tokUnittestFor && len(e.d.moduleStmt.Args) > 0 {
			srcBase = path.Clean(e.d.moduleStmt.Args[0].string())
		}

		files := []string{testSVNInterfaceRel}

		for _, src := range e.d.srcs {
			if src.Global || src.Compile.Variant != 0 {
				continue
			}

			switch strings.ToLower(path.Ext(src.Source.string())) {
			case ".c", ".cc", ".cpp", ".cxx":
				files = append(files, path.Clean(path.Join(srcBase, src.Source.string())))
			}
		}

		suites = append(suites, e.styleSuite(StyleCheck{Name: "clang_format", Config: testClangFormatConfigRel, Wrapper: testClangFormatWrapperRel, Files: files}, []ResourceDecl{{Token: internStr(testClangFormatResource)}}))
	}

	return suites
}

func (s *TestSuite) applyTestMeta(meta TestMeta, addSource bool) {
	s.size = cmp.Or(meta.size, "small")
	s.timeout = meta.timeout

	if s.timeout == 0 {
		s.timeout = testSizeTimeouts[s.size]
	}

	s.tags = sortedUnique(meta.tags)
	s.env = meta.env
	s.requirements = testRequirementsOf(meta.requirements)
	s.depends = meta.depends
	s.dockerImages = sortedUnique(meta.dockerImages)
	s.recipes = testRecipes(meta.recipes)
	s.data = slices.Clone(meta.data)

	if addSource && !meta.explicitData {
		s.data = append(s.data, "arcadia/"+s.projectPath)
	}

	slices.Sort(s.data)
	s.data = slices.Compact(s.data)

	for _, entry := range s.data {
		if rel, ok := strings.CutPrefix(entry, "arcadia/"); ok {
			s.relatedPaths = append(s.relatedPaths, source(path.Clean(rel)).string())
		}
	}

	if meta.cwd != "" {
		s.runCwd = source(meta.cwd).string()
	}

	s.split = meta.splitFactor

	if s.split == 0 && meta.forkMode != "" {
		s.split = 10
	}
}
