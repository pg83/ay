package main

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
		Inputs:       na.inputList(fetchScriptInputs(ctx.scripts)),
		KV:           KV{P: pkFETCH, PC: pcYellow, ShowOut: true},
		Outputs:      na.vfsList(output),
		Requirements: Requirements{CPU: float64(1), Network: nwFull, RAM: float64(32)},
		Sandboxing:   true,
	}

	node.UID = resourceFetchUID(decl.URI.string(), output.string())
	node.SelfUID = node.UID

	ref := ctx.emit.emit(node)
	ctx.fetchRefs.put(decl.Name, ref)

	return ref
}
