package main

import (
	"fmt"
	"strings"
)

func (e *EmitContext) genPeerModule(instance ModuleInstance) *ModuleEmitResult {
	result := genModule(e.ctx, instance)

	if e.ctx.peerTests {
		e.frame.testPeers = append(e.frame.testPeers, result)
	}

	return result
}

func (ctx *GenCtx) prepareDiscoveredModuleTests() []*TestSuite {
	seen := map[*ModuleEmitResult]bool{}
	groups := map[VFS][]*ModuleEmitResult{}

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

	for _, result := range ctx.testRoots {
		visit(result)
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

	outputs := ctx.projectModuleIndex(suites)

	for _, suite := range suites {
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
