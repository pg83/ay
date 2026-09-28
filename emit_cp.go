package main

var cpKV = KV{P: pkCP, PC: pcLightCyan}

func emitJVCPG4(
	instance ModuleInstance,
	src VFS,
	dst VFS,
	jvRef NodeRef,
	jvPrimary VFS,
	jvInputs []VFS,
	closure []VFS,
	id NodeRef,
	tc ModuleToolchain,
	scripts ScriptDeps,
	emit *StreamingEmitter,
) {
	na := emit.nodeArenas()
	fsTools := copyFsToolsVFS

	cmdArgs := na.anyList(
		tc.Python3.any(),
		fsTools.any(),
		argCopy.any(),
		src.any(),
		dst.any(),
	)

	env := envVarsVCS
	head := na.vfs.alloc(2)[:0]

	head = append(head, jvPrimary)

	if src != jvPrimary {
		head = append(head, src)
	}

	na.vfs.commit(len(head))

	head = head[:len(head):len(head)]

	inputs := na.inputList(head, scripts[fsTools.rel()], na.vfsList(jvInputs...), closure)

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:       env,
		Inputs:    inputs,
		KV:        &cpKV,
		Outputs:   na.vfsList(dst),
		DepRefs:   na.refList(jvRef),
		Resources: usesPython3,
	}

	emit.emitReservedNode(node, id)
}

func emitCP(instance ModuleInstance, src VFS, dst VFS, tc ModuleToolchain, scripts ScriptDeps, emit *StreamingEmitter) NodeRef {
	id := emit.reserve()

	emit.emitReservedNode(composeCPNode(instance, src, dst, nil, tc, scripts, emit.nodeArenas()), id)

	return id
}

func (e *EmitContext) emitCPWithInputs(src VFS, dst VFS, extraInputs []VFS, id NodeRef, tc ModuleToolchain, scripts ScriptDeps) {
	node := composeCPNode(e.instance, src, dst, extraInputs, tc, scripts, e.ctx.na)

	e.emitReservedNode(node, id)
}

func composeCPNode(instance ModuleInstance, src VFS, dst VFS, extraInputs []VFS, tc ModuleToolchain, scripts ScriptDeps, na *NodeArenas) Node {
	fsTools := copyFsToolsVFS

	cmdArgs := na.anyList(
		tc.Python3.any(),
		fsTools.any(),
		argCopy.any(),
		src.any(),
		dst.any(),
	)

	env := envVarsVCS
	ownInputs := na.vfs.alloc(len(extraInputs))[:0]

	for _, v := range extraInputs {
		if v == src || v == dst {
			continue
		}

		ownInputs = append(ownInputs, v)
	}

	na.vfs.commit(len(ownInputs))

	ownInputs = ownInputs[:len(ownInputs):len(ownInputs)]

	inputs := na.inputList(scripts[fsTools.rel()], na.vfsList(src), ownInputs)

	return Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:       env,
		Inputs:    inputs,
		KV:        &cpKV,
		Outputs:   na.vfsList(dst),
		Resources: usesPython3,
	}
}
