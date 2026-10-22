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

		if project := result.testProject; project != 0 {
			if _, ok := groups[project]; !ok {
				projects = append(projects, project)
			}

			groups[project] = append(groups[project], result)
		}

		if ctx.peerTests {
			for _, peer := range result.testPeers {
				visit(peer)
			}
		}
	}

	for _, result := range ctx.testRoots {
		visit(result)
	}

	var suites []*TestSuite

	for _, project := range projects {
		results := groups[project]

		for _, result := range results {
			if result.java != nil {
				result.testsEmitted = true

				for _, ref := range ctx.javaEmitTests(result.java) {
					ctx.emit.result(ref)
				}
			}
		}

		for _, suite := range ctx.moduleTestSuites(results...) {
			if !mergeCheckSuite(suites, suite) {
				suites = append(suites, suite)
			}
		}
	}

	for _, suite := range suites {
		for _, ref := range ctx.emitTestSuite(suite) {
			ctx.emit.result(ref)
		}
	}
}
