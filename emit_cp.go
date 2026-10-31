package main

var cpKV = KV{P: pkCP, PC: pcLightCyan}

func (e *EmitContext) emitCPWithInputs(src VFS, dst VFS, extraInputs []VFS, id NodeRef, tc ModuleToolchain, scripts ScriptDeps) {
	node := composeCPNode(e.instance, src, dst, extraInputs, tc, scripts, e.ctx.na)

	e.emitReservedNode(node, id)
}

func composeCPNode(instance ModuleInstance, src VFS, dst VFS, extraInputs []VFS, tc ModuleToolchain, scripts ScriptDeps, na *NodeArenas) NodeSpec {
	fsTools := copyFsToolsVFS

	cmdArgs := na.anyList(
		tc.Python3,
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

	inputs := na.rootInputList(ownInputs, scripts[fsTools.rel()], na.vfsList(src))

	return NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env: env, Inputs: inputs, KV: &cpKV,
		Outputs:   na.vfsList(dst),
		Resources: usesPython3}, input: InputSpec{text: na.vfsList(append([]VFS{src}, ownInputs...)...)},
	}
}
