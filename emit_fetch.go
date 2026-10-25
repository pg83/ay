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
	args := []ANY{internStr(currentYatoolPath()).any(), argFetch.any()}
	identity := decl.URI.string()

	if decl.Descriptor != 0 {
		args = append(args, strResourceJson.any(), decl.Descriptor.any(), strHostPlatform.any(), internStr(hostPlatformKey(ctx.host)).any(), argB.any(), argS.any(), output.any())
		identity = decl.Descriptor.string() + "\x00" + resourceDescriptorPlatform(hostPlatformKey(ctx.host))
	} else {
		args = append(args, argB.any(), argS.any(), decl.URI.any(), output.any())
	}

	node := Node{
		Platform:     ctx.host,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...))}),
		Inputs:       NodeInputs{files: na.inputList(fetchScriptInputs(na, ctx.scripts))},
		KV:           &fetchKV,
		Outputs:      na.vfsList(output),
		Requirements: &fetchRequirements,
	}

	node.PresetUID = resourceFetchUID(identity, output.string())

	ctx.emit.deferPublication++

	ref := ctx.emit.emitNode(node)

	ctx.emit.deferPublication--

	ctx.fetchRefs.put(decl.FetchKey, ref)

	if _, exists := ctx.fetchRefs.get(decl.Name); !exists {
		ctx.fetchRefs.put(decl.Name, ref)
	}

	return ref
}
