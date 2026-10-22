package main

func (e *EmitContext) genPeerModule(instance ModuleInstance) *ModuleEmitResult {
	result := genModule(e.ctx, instance)

	if e.ctx.peerTests {
		e.frame.testPeers = append(e.frame.testPeers, result)
	}

	return result
}

func (ctx *GenCtx) emitDiscoveredModuleTests() {
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
		return
	}

	for _, suite := range suites {
		ctx.prepareTestDependencies(suite)
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
