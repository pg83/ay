package main

var (
	fetchKV           = KV{P: pkFETCH, PC: pcYellow, ShowOut: true}
	fetchRequirements = Requirements{CPU: 1, Network: nwFull, RAM: 32}
)

func emitResourceFetch(ctx *GenCtx, decl ResourceDecl) NodeRef {
	na := ctx.na

	if ref, ok := ctx.fetchRefs.get(decl.FetchKey); ok {
		return ref
	}

	output := build("resources/", decl.Name.string())

	node := Node{
		Platform: ctx.host,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(internStr(currentYatoolPath()).any(),
			argFetch.any(),
			argB.any(),
			argS.any(),
			decl.URI.any(),
			output.any()))}),
		Inputs:       na.inputList(fetchScriptInputs(na, ctx.scripts)),
		KV:           &fetchKV,
		Outputs:      na.vfsList(output),
		Requirements: &fetchRequirements,
	}

	node.PresetUID = resourceFetchUID(decl.URI.string(), output.string())

	ctx.emit.deferPublication++

	ref := ctx.emit.emitNode(node)

	ctx.emit.deferPublication--

	ctx.fetchRefs.put(decl.FetchKey, ref)

	if _, exists := ctx.fetchRefs.get(decl.Name); !exists {
		ctx.fetchRefs.put(decl.Name, ref)
	}

	return ref
}
