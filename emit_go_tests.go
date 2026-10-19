package main

import (
	"path"
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

	if e.goRes != nil {
		var dirs []string

		byDir := map[string][]VFS{}
		seen := map[VFS]bool{}

		for _, f := range e.goRes.GoFiles {
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
			suites = append(suites, goFmtSuite(project, byDir[dir], globals))
		}
	}

	if goNeedLint(instance.Path) {
		suites = append(suites, goVetSuite(project, goRef, vetReport, globals))
	}

	return suites
}

func goFmtSuite(project string, files []VFS, globals []ResourceDecl) *TestSuite {
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

func goVetSuite(project string, goRef NodeRef, vetReport VFS, globals []ResourceDecl) *TestSuite {
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
