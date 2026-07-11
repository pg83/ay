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
		Env:          env,
		Inputs:       inputs,
		KV:           &cpKV,
		Outputs:      na.vfsList(dst),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      na.refList(jvRef),
		Resources:    usesPython3,
	}

	emit.emitReservedNode(node, id)
}

func emitCP(instance ModuleInstance, src VFS, dst VFS, tc ModuleToolchain, scripts ScriptDeps, emit *StreamingEmitter) NodeRef {
	id := emit.reserve()

	emitCPWithDeps(instance, src, dst, nil, nil, id, 0, tc, scripts, emit)

	return id
}

func emitCPWithDeps(instance ModuleInstance, src VFS, dst VFS, depRefs []NodeRef, extraInputs []VFS, id NodeRef, moduleTag STR, tc ModuleToolchain, scripts ScriptDeps, emit *StreamingEmitter) {
	node := composeCPNode(instance, src, dst, depRefs, extraInputs, moduleTag, tc, scripts, emit.nodeArenas())

	emit.emitReservedNode(node, id)
}

func (e *EmitContext) emitCPWithDeps(src VFS, dst VFS, depRefs []NodeRef, extraInputs []VFS, id NodeRef, moduleTag STR, tc ModuleToolchain, scripts ScriptDeps) {
	node := composeCPNode(e.instance, src, dst, depRefs, extraInputs, moduleTag, tc, scripts, e.ctx.na)

	e.emitReservedNode(node, id)
}

func composeCPNode(instance ModuleInstance, src VFS, dst VFS, depRefs []NodeRef, extraInputs []VFS, moduleTag STR, tc ModuleToolchain, scripts ScriptDeps, na *NodeArenas) Node {
	fsTools := copyFsToolsVFS

	cmdArgs := na.anyList(
		tc.Python3.any(),
		fsTools.any(),
		argCopy.any(),
		src.any(),
		dst.any(),
	)

	env := envVarsVCS
	ownInputs := na.vfs.alloc(1 + len(extraInputs))[:0]

	ownInputs = append(ownInputs, src)

	for _, v := range extraInputs {
		if v == src || v == dst {
			continue
		}

		ownInputs = append(ownInputs, v)
	}

	na.vfs.commit(len(ownInputs))

	ownInputs = ownInputs[:len(ownInputs):len(ownInputs)]

	inputs := na.inputList(scripts[fsTools.rel()], ownInputs)

	return Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:          env,
		Inputs:       inputs,
		KV:           &cpKV,
		Outputs:      na.vfsList(dst),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      na.noderefs.list(depRefs...),
		Resources:    usesPython3,
	}
}
