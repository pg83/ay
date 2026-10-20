package main

func (e *EmitContext) genExecTest() *ModuleEmitResult {
	if e.d.test == nil || e.d.test.Commands == "" {
		throwFmt("EXECTEST must have at least one RUN macro")
	}

	globals := e.scriptModuleResources(nil, "build/platform/python/ymake_python3")
	name := fullPathTestName(e.instance.Path.relString())

	if len(e.d.moduleStmt.Args) > 0 {
		name = e.d.moduleStmt.Args[0].string()
	}

	out := build(e.instance.Path.relString(), "/", name, ".pkg.fake")
	na := e.ctx.na

	node := Node{Module: true, Platform: e.instance.Platform, KV: &unionKV, Env: envVarsVCS, Outputs: na.vfsList(out), Resources: usesPython3,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(e.d.tc.Python3.any(), strC2.any(), internStr("open('"+out.string()+"','w').close()").any())), Env: envVarsVCS})}

	ref := e.emitLazyModuleNode(node, nil, out)
	result := e.scriptModuleResult(ref, out, nil, globals)

	if e.ctx.testMode {
		result.testSuiteInfo = buildTestSuiteInfo(e.instance, e.d, out)
	}

	return result
}
