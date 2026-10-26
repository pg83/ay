package main

import (
	"cmp"
	"path"
	"slices"
	"strconv"
	"strings"
)

func goNeedLint(p VFS) bool {
	rel := p.relString()

	return p.isSource() && !strings.HasPrefix(rel, "vendor/") && !strings.HasPrefix(rel, "contrib/")
}

func (e *EmitContext) goTestSuites(goRef NodeRef, vetReport VFS, globals []ResourceDecl) []*TestSuite {
	if !e.ctx.testMode {
		return nil
	}

	instance := e.instance
	suites := e.goFmtSuites(globals)

	if goNeedLint(instance.Path) {
		suites = append(suites, e.goVetSuite(vetReport, globals))
	}

	if e.d.moduleStmt.Name == tokGoTest {
		suites = append(suites, e.goTestRunSuite(goRef, globals))
	}

	return suites
}

func (e *EmitContext) goFmtSuites(globals []ResourceDecl) []*TestSuite {
	var suites []*TestSuite
	var fmtFiles []ANY

	isTestFor := e.d.moduleStmt.Name == tokGoTest && e.d.setVars[strGO_TEST_IMPORT_PATH] != 0

	if !isTestFor {
		for _, meta := range e.d.srcs {
			if !meta.Global && meta.Compile.Variant == 0 && strings.HasSuffix(meta.Source.string(), ".go") {
				fmtFiles = append(fmtFiles, meta.Source)
			}
		}
	}

	for _, src := range concat(e.d.goFmtSrcs, e.d.goTestSrcs, e.d.goXTestSrcs) {
		fmtFiles = append(fmtFiles, src)
	}

	{
		var dirs []string

		byDir := map[string][]VFS{}
		seen := map[VFS]bool{}

		for _, src := range fmtFiles {
			f := e.moduleSourceVFS(src)
			_, rooted := moduleRootedVFS(e.instance.Path.relString(), src.string())

			if !rooted && f.isSource() && !e.ctx.fs.isFile(srcRootRel, f.relString()) {
				continue
			}

			if !goNeedLint(f) || seen[f] {
				continue
			}

			seen[f] = true

			dir := path.Dir(f.relString())

			if _, ok := byDir[dir]; !ok {
				dirs = append(dirs, dir)
			}

			byDir[dir] = append(byDir[dir], f)
		}

		for _, dir := range dirs {
			suites = append(suites, e.goFmtSuite(byDir[dir], globals))
		}
	}

	return suites
}

func (e *EmitContext) goSuiteBase(name, testType, ciType string, globals []ResourceDecl) *TestSuite {
	project := e.instance.Path.relString()

	return &TestSuite{
		p:                      e.ctx.target,
		projectPath:            project,
		name:                   name,
		testType:               testType,
		ciType:                 ciType,
		size:                   "small",
		timeout:                testSizeTimeouts["small"],
		runInputs:              []VFS{source(project)},
		globals:                globals,
		env:                    e.d.test.env,
		supportsTestParameters: true,
	}
}

func (e *EmitContext) goTestRunSuite(binRef NodeRef, globals []ResourceDecl) *TestSuite {
	instance, d := e.instance, e.d
	project := instance.Path.relString()
	name := programBinaryName(instance, d.moduleStmt)
	binary := build(project, "/", name)
	s := e.goSuiteBase(name, "go_test", "test", globals)

	s.applyTestMeta(d.test, d.setVars[strADD_SRCDIR_TO_TEST_DATA] != strNo)
	s.skipped = Environment{s: &e.frame.ifEnv}.string(envSKIP_TEST_VALUE) != ""
	s.deps = []NodeRef{binRef}
	s.supportsCanonization = true
	s.canonicalData = true
	s.smoothShutdown = []string{"SIGUSR2"}

	partition := cmp.Or(d.test.partition, "SEQUENTIAL")

	s.runCmd = func(work string, chunk TestChunk) []ANY {
		return []ANY{
			internStr(testToolHostPath).any(), strRunGoTest.any(),
			strBinary.any(), binary.any(),
			strTestWorkDir.any(), intern(work).any(),
			strTracefile.any(), intern(work + "/ytest.report.trace").any(),
			strModulo.any(), internStr(strconv.Itoa(chunk.split)).any(),
			strModuloIndex.any(), internStr(strconv.Itoa(chunk.index)).any(),
			strPartitionMode.any(), internStr(partition).any(),
			strOutputDir.any(), intern(work + "/testing_out_stuff").any(),
			strProjectPath.any(), internStr(project).any(),
			strTimeout.any(), internStr(strconv.Itoa(s.timeout)).any(),
			strVerbose.any(),
		}
	}

	return s
}

func (e *EmitContext) goFmtSuite(files []VFS, globals []ResourceDecl) *TestSuite {
	slices.SortFunc(files, func(a, b VFS) int { return strings.Compare(a.string(), b.string()) })

	s := e.goSuiteBase("gofmt", "gofmt", "style", globals)

	s.projectPath = path.Dir(files[0].relString())
	s.runInputs = []VFS{source(s.projectPath)}
	s.checkFiles = files

	gofmt := strGoToolsResourceGlobalBinGofmt.any()

	for _, g := range globals {
		if g.GlobalVar.string() == "GO_TOOLS_RESOURCE_GLOBAL" {
			gofmt = intern(g.Value.string() + "/bin/gofmt").any()
		}
	}

	for _, f := range files {
		s.relatedPaths = append(s.relatedPaths, f.string())
	}

	s.runCmd = func(work string, _ TestChunk) []ANY {
		out := []ANY{
			internStr(testToolHostPath).any(), strRunGoFmt.any(),
			strGofmt.any(), gofmt,
			argSourceRoot.any(), strS.any(),
			argTracePath.any(), intern(work + "/ytest.report.trace").any(),
			argOutPath.any(), intern(work + "/testing_out_stuff").any(),
		}

		for _, f := range s.checkFiles {
			out = append(out, f.any())
		}

		return out
	}

	return s
}

func (e *EmitContext) goVetSuite(vetReport VFS, globals []ResourceDecl) *TestSuite {
	s := e.goSuiteBase("govet", "govet", "style", globals)

	s.runInputs = append(s.runInputs, vetReport)
	s.projectBuildDeps = true
	s.runCmd = func(work string, _ TestChunk) []ANY {
		return []ANY{
			internStr(testToolHostPath).any(), strRunCheck.any(),
			argSourceRoot.any(), strS.any(),
			strChecker.any(), strRunGoVet.any(),
			strCheckName.any(), strGovet.any(),
			argTracePath.any(), intern(work + "/ytest.report.trace").any(),
			argOutPath.any(), intern(work + "/testing_out_stuff").any(),
			argLogPath.any(), intern(work + "/testing_out_stuff/run_check.log").any(),
			vetReport.any(),
			strNoSnippetFromStdout.any(),
		}
	}

	return s
}

func goDirSources(fs FS, dir string) []string {
	return goDirFiles(fs, dir, false)
}

func goDirFiles(fs FS, dir string, tests bool) []string {
	view := fs.listdir(dirKey(dir))
	names := make([]string, 0, len(view.names))

	for _, packed := range view.names {
		if packed&1 != 0 {
			continue
		}

		name := STR(packed >> 1).string()

		if strings.HasSuffix(name, ".go") && strings.HasSuffix(name, "_test.go") == tests {
			names = append(names, name)
		}
	}

	slices.Sort(names)

	return names
}
