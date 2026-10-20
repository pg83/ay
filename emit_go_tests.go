package main

import (
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
	project := instance.Path.relString()

	var suites []*TestSuite
	var fmtFiles []VFS

	isTestFor := e.d.moduleStmt.Name == tokGoTest && e.d.setVars[strGO_TEST_IMPORT_PATH] != 0

	if e.goRes != nil && !isTestFor {
		fmtFiles = e.goRes.GoFiles
	}

	for _, src := range concat(e.d.goTestSrcs, e.d.goXTestSrcs) {
		if e.ctx.fs.isFile(instance.Path.rel(), src.string()) {
			fmtFiles = append(fmtFiles, source(project, "/", src.string()))
		}
	}

	{
		var dirs []string

		byDir := map[string][]VFS{}
		seen := map[VFS]bool{}

		for _, f := range fmtFiles {
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
			suites = append(suites, goFmtSuite(project, byDir[dir], globals, e.d.test.env))
		}
	}

	if goNeedLint(instance.Path) {
		suites = append(suites, goVetSuite(project, goRef, vetReport, globals, e.d.test.env))
	}

	if e.d.moduleStmt.Name == tokGoTest {
		suites = append(suites, e.goTestRunSuite(goRef, globals))
	}

	return suites
}

func (e *EmitContext) goTestRunSuite(binRef NodeRef, globals []ResourceDecl) *TestSuite {
	instance, d := e.instance, e.d
	project := instance.Path.relString()
	name := programBinaryName(instance, d.moduleStmt)
	binary := build(project, "/", name)
	size := d.test.size

	if size == "" {
		size = "small"
	}

	timeout := d.test.timeout

	if timeout == 0 {
		timeout = testSizeTimeouts[size]
	}

	data := slices.Clone(d.test.data)

	if d.setVars[strADD_SRCDIR_TO_TEST_DATA] != strNo {
		data = append(data, "arcadia/"+project)
	}

	slices.Sort(data)

	var related []VFS

	for _, entry := range slices.Compact(data) {
		if rel, ok := strings.CutPrefix(entry, "arcadia/"); ok {
			related = append(related, source(path.Clean(rel)))
		}
	}

	tags := slices.Clone(d.test.tags)

	slices.Sort(tags)

	return &TestSuite{
		Project:       project,
		Name:          name,
		Type:          "go_test",
		CIType:        "test",
		Size:          size,
		Timeout:       timeout,
		Related:       related,
		Inputs:        []VFS{source(project)},
		Deps:          []NodeRef{binRef},
		Globals:       globals,
		Tags:          slices.Compact(tags),
		Env:           d.test.env,
		RunCwd:        d.test.cwd,
		Canonization:  true,
		TestParams:    true,
		SmoothSignals: []string{"SIGUSR2"},
		RunCmd: func(wd string) []ANY {
			partition := d.test.partition

			if partition == "" {
				partition = "SEQUENTIAL"
			}

			return []ANY{
				internStr(testToolHostPath).any(), strRunGoTest.any(),
				strBinary.any(), binary.any(),
				strTestWorkDir.any(), build(wd).any(),
				strTracefile.any(), build(wd, "/ytest.report.trace").any(),
				strModulo.any(), strOne.any(),
				strModuloIndex.any(), strZero.any(),
				strPartitionMode.any(), internStr(partition).any(),
				strOutputDir.any(), build(wd, "/testing_out_stuff").any(),
				strProjectPath.any(), internStr(project).any(),
				strTimeout.any(), internStr(strconv.Itoa(timeout)).any(),
				strVerbose.any(),
			}
		},
	}
}

func goFmtSuite(project string, files []VFS, globals []ResourceDecl, env []string) *TestSuite {
	gofmt := strGoToolsResourceGlobalBinGofmt.any()

	for _, g := range globals {
		if g.GlobalVar.string() == "GO_TOOLS_RESOURCE_GLOBAL" {
			gofmt = intern(g.Value.string() + "/bin/gofmt").any()
		}
	}

	return &TestSuite{
		Project:    project,
		Name:       "gofmt",
		Type:       "gofmt",
		CIType:     "style",
		Size:       "small",
		Timeout:    testSizeTimeouts["small"],
		Related:    files,
		Inputs:     append([]VFS{source(project)}, files...),
		Globals:    globals,
		Env:        env,
		TestParams: true,
		RunCmd: func(wd string) []ANY {
			out := []ANY{
				internStr(testToolHostPath).any(), strRunGoFmt.any(),
				strGofmt.any(), gofmt,
				argSourceRoot.any(), strS.any(),
				argTracePath.any(), build(wd, "/ytest.report.trace").any(),
				argOutPath.any(), build(wd, "/testing_out_stuff").any(),
			}

			for _, f := range files {
				out = append(out, f.any())
			}

			return out
		},
	}
}

func goVetSuite(project string, goRef NodeRef, vetReport VFS, globals []ResourceDecl, env []string) *TestSuite {
	return &TestSuite{
		Project:    project,
		Name:       "govet",
		Type:       "govet",
		CIType:     "style",
		Size:       "small",
		Timeout:    testSizeTimeouts["small"],
		Inputs:     []VFS{source(project), vetReport},
		Deps:       []NodeRef{goRef},
		Globals:    globals,
		Env:        env,
		TestParams: true,
		RunCmd: func(wd string) []ANY {
			return []ANY{
				internStr(testToolHostPath).any(), strRunCheck.any(),
				argSourceRoot.any(), strS.any(),
				strChecker.any(), strRunGoVet.any(),
				strCheckName.any(), strGovet.any(),
				argTracePath.any(), build(wd, "/ytest.report.trace").any(),
				argOutPath.any(), build(wd, "/testing_out_stuff").any(),
				argLogPath.any(), build(wd, "/testing_out_stuff/run_check.log").any(),
				vetReport.any(),
				strNoSnippetFromStdout.any(),
			}
		},
	}
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
