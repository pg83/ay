package main

import (
	"fmt"
	"strings"
)

func (e *EmitContext) genPeerModule(instance ModuleInstance) *ModuleEmitResult {
	result := genModule(e.ctx, instance)

	e.ctx.consumeModuleWarnings(result)

	if e.ctx.peerTests {
		e.frame.testPeers = append(e.frame.testPeers, result)
	}

	return result
}

func (ctx *GenCtx) prepareDiscoveredModuleTests(discovered []DiscoveredTargets) []*TestSuite {
	if len(ctx.testRoots) == 0 {
		return nil
	}

	seen := map[*ModuleEmitResult]bool{}
	groups := map[VFS][]*ModuleEmitResult{}
	live := ctx.emit.reachableNodes(ctx.emit.publicationFrontier())

	var projects []VFS
	var visit func(*ModuleEmitResult)

	visit = func(result *ModuleEmitResult) {
		if seen[result] {
			return
		}

		seen[result] = true

		if ctx.peerTests {
			for _, peer := range result.testPeers {
				visit(peer)
			}
		}

		if project := result.testProject; project != 0 {
			if _, ok := groups[project]; !ok {
				projects = append(projects, project)
			}

			groups[project] = append(groups[project], result)
		}
	}

	visitRoot := func(result *ModuleEmitResult) {
		if result.ModuleStmtName == tokPy2Library && ctx.moduleName(result.testProject.relString()) == tokPy23Library &&
			(result.ARRef == 0 || !live.has(uint32(result.ARRef))) && (result.GlobalRef == nil || !live.has(uint32(*result.GlobalRef))) {
			return
		}

		visit(result)
	}

	if ctx.testMode {
		rootProjects := map[VFS][]*ModuleEmitResult{}

		for _, result := range ctx.testRoots {
			rootProjects[result.testProject] = append(rootProjects[result.testProject], result)
		}

		for _, targets := range discovered {
			for _, dir := range targets.TestOrder {
				project := source(dir)

				for _, result := range rootProjects[project] {
					visitRoot(result)
				}

				delete(rootProjects, project)
			}
		}
	}

	for _, result := range ctx.testRoots {
		visitRoot(result)
	}

	var suites []*TestSuite
	var javaSuites []*TestSuite

	for _, project := range projects {
		results := groups[project]

		for _, result := range results {
			if result.java != nil {
				result.testsEmitted = true
				javaSuites = append(javaSuites, ctx.javaTestSuites(result.java)...)
			}
		}

		for _, suite := range ctx.moduleTestSuites(results...) {
			if !mergeCheckSuite(suites, suite) {
				suites = append(suites, suite)
			}
		}
	}

	suites = append(javaSuites, suites...)

	if len(suites) == 0 {
		return nil
	}

	prepared := suites[:0]

	for _, suite := range suites {
		ctx.prepareTestDependencies(suite)

		if err := ctx.testDataReadError(suite); err != nil {
			ctx.onWarn(Warn{Kind: WarnModuleFailed, Message: fmt.Sprintf("suite configuration failed for %s: %s", suite.projectPath, err)})

			continue
		}

		prepared = append(prepared, suite)
	}

	return prepared
}

func (ctx *GenCtx) emitDiscoveredModuleTests(suites []*TestSuite) {
	if len(suites) == 0 {
		return
	}

	outputs, mainOutputs := ctx.projectModuleIndex(suites)

	for _, suite := range suites {
		if suite.importCheckEnv && ctx.testSuiteSelected(suite) && len(outputs[source(suite.projectPath)]) == 0 {
			ctx.onWarn(Warn{Kind: WarnTestDependency, Message: fmt.Sprintf("cannot resolve project dependency %s for import_test", suite.projectPath)})

			continue
		}

		for _, project := range suite.dependencyProjects {
			suite.deps = append(suite.deps, mainOutputs[project]...)
		}

		if suite.projectBuildDeps {
			suite.deps = append(suite.deps, outputs[source(suite.projectPath)]...)
		}

		for _, ref := range ctx.emitTestSuite(suite) {
			ctx.emit.result(ref)
		}
	}
}

func (ctx *GenCtx) testDataReadError(suite *TestSuite) error {
	if ctx.tests.cacheTests && ctx.testSuiteSelected(suite) {
		for _, entry := range suite.data {
			if rel, ok := strings.CutPrefix(entry, "arcadia/"); ok {
				if err := ctx.fs.testPathReadError(rel); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
