package main

// emitResourceFetch emits the FETCH node that downloads one external resource,
// returning the node's NodeRef. Emitted at most once per resource name (deduped
// through ctx.fetchRefs); every consumer that splices $(<Name>) depends on it.
func emitResourceFetch(ctx *GenCtx, decl ResourceDecl) NodeRef {
	na := ctx.na

	if ref, ok := ctx.fetchRefs.get(decl.Name); ok {
		return ref
	}

	output := build("resources/" + decl.Name.string())
	node := &Node{
		Platform: ctx.host,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.strList(internStr(currentYatoolPath()),
			argFetch.str(),
			argB.str(),
			argS.str(),
			decl.URI,
			output.str()))}),
		Inputs:           na.inputList(fetchScriptInputs(ctx.scripts)),
		KV:               KV{P: pkFETCH, PC: pcYellow, ShowOut: true},
		Outputs:          na.vfsList(output),
		Requirements:     Requirements{CPU: float64(1), Network: nwFull, RAM: float64(32)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: "build/resources"},
	}

	// Stable command-independent uid, set before emit so the finalizer keeps it
	// over the binary-path-bearing command hash.
	node.UID = resourceFetchUID(decl.URI.string(), output.string())
	node.SelfUID = node.UID

	ref := ctx.emit.emit(node)
	ctx.fetchRefs.put(decl.Name, ref)

	return ref
}
