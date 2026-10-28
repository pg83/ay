package main

func (e *EmitContext) genGeneratedLibrary() *ModuleEmitResult {
	var globals []ResourceDecl
	var peerRefs []NodeRef
	var peerPaths []VFS

	for _, peer := range e.d.peerdirs {
		res := e.genPeerModule(ModuleInstance{Path: source(peer.string()), Kind: KindLib, Language: LangCPP, Platform: e.instance.Platform})

		globals = append(globals, res.ResourceGlobalClosure...)
		peerPaths = append(peerPaths, res.PeerArchiveClosurePaths...)
		peerRefs = append(peerRefs, res.PeerArchiveClosureRefs...)

		if res.ARPath != nil {
			peerPaths = append(peerPaths, *res.ARPath)
			peerRefs = append(peerRefs, res.ARRef)
		}
	}

	dedupers.with(func(dd *DeDuper) {
		n := 0

		for i, output := range peerPaths {
			if dd.addStable(output.strID()) {
				peerPaths[n] = output
				peerRefs[n] = peerRefs[i]
				n++
			}
		}

		peerPaths = peerPaths[:n]
		peerRefs = peerRefs[:n]
	})

	globals = e.scriptModuleResources(globals, "build/platform/python/ymake_python3")
	e.emitDeclaredProducers(nil)

	module := e.instance.Path.relString()
	name := realPrjName(module)

	if len(e.d.moduleStmt.Args) > 0 {
		name = e.d.moduleStmt.Args[0].string()
	}

	out := build(module, "/", name, ".pkg.fake")
	na := e.ctx.na
	inputs := append(dedup(e.outs), buildScriptsTouchPy)

	node := Node{Module: true, Platform: e.instance.Platform, KV: &unionKV, Env: envVarsVCS,
		Inputs: NodeInputs{files: na.rootInputList(na.vfsList(inputs...))}, Outputs: na.vfsList(out), Resources: usesPython3,
		Cmds: na.cmdList(e.pythonScriptCmd("touch.py", out.string()))}

	ref := e.emitLazyModuleNode(node, nil, out)

	e.codegen.mustInfo(out).ModuleType = e.d.moduleStmt.Name

	result := e.scriptModuleResult(ref, out, peerPaths, globals)

	result.TestSuites = e.styleSuites(globals)
	result.PeerArchiveClosureRefs = na.refList(peerRefs...)
	result.PeerArchiveClosurePaths = na.vfsList(peerPaths...)

	return result
}
