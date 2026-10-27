package main

import (
	"fmt"
	"strings"
)

func (e *EmitContext) genPeerModule(instance ModuleInstance) *ModuleEmitResult {
	result := genModule(e.ctx, instance)

	e.recordPeerModule(result)

	return result
}

func (e *EmitContext) recordPeerModule(result *ModuleEmitResult) {
	e.ctx.consumeModuleWarnings(result)

	if e.ctx.peerTests {
		e.frame.testPeers = append(e.frame.testPeers, result)
	}
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
		javaStart := len(javaSuites)

		for _, result := range groups[project] {
			var own []*TestSuite

			if result.java != nil {
				result.testsEmitted = true

				for _, suite := range ctx.javaTestSuites(result.java) {
					if !mergeCheckSuite(javaSuites[javaStart:], suite) {
						javaSuites = append(javaSuites, suite)
					}
				}

				own = ctx.javaCPPStyleSuites(result.java)
			} else {
				own = ctx.moduleTestSuites(result)
			}

			for _, suite := range own {
				if !mergeCheckSuite(suites, suite) {
					suites = append(suites, suite)
				}
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

	suites = ctx.resolvedTestSuites(suites)

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

func (ctx *GenCtx) resolvedTestSuites(suites []*TestSuite) []*TestSuite {
	var live *BitSet

	resolved := suites[:0]

	for _, suite := range suites {
		if len(suite.unpublishedDeps) == 0 || !ctx.testSuiteSelected(suite) {
			resolved = append(resolved, suite)

			continue
		}

		if live == nil {
			reachable := ctx.emit.reachableNodes(ctx.emit.publicationFrontier())

			live = &reachable
		}

		missing := false

		for _, dependency := range suite.unpublishedDeps {
			output := testDependencyOutput(dependency.result)
			info, _ := ctx.scannerFor(dependency.instance).codegen.byRel.get(output.rel())

			if !live.has(uint32(info.ProducerRef)) {
				missing = true

				ctx.onWarn(Warn{Kind: WarnTestDependency, Message: fmt.Sprintf("Cannot resolve dependency '%s' for test '%s (%s)'", output.relString(), suite.projectPath, suite.name)})
			}
		}

		if missing {
			ctx.onWarn(Warn{Kind: WarnTestDependency, Message: fmt.Sprintf("Skipping test '%s (%s)' due to unresolved dependencies", suite.projectPath, suite.name)})

			continue
		}

		resolved = append(resolved, suite)
	}

	return resolved
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
