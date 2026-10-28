package main

import "path"

var gettextKV = KV{P: pkPO, PC: pcYellow}

func (e *EmitContext) gettextOutput(input VFS) VFS {
	if e.d.gettextKeepPath {
		return build(relStem(input.relString()), ".mo")
	}

	return build(e.instance.Path.relString(), "/", relStem(path.Base(input.relString())), ".mo")
}

func (e *EmitContext) emitGettextSource(input, output VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	ref := ctx.emit.reserve()
	python := wrapccPython3STR

	pending := e.pendingEmit(func() {
		e.emitReservedNode(Node{
			Platform:  instance.Platform,
			Cmds:      na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(python.any(), gettextScript.any(), strO.any(), output.any(), input.any())), Env: envVarsVCS}),
			Env:       envVarsVCS,
			Inputs:    NodeInputs{files: na.rootInputList(na.vfsList(gettextScript, input))},
			Outputs:   na.vfsList(output),
			KV:        &gettextKV,
			Resources: usesPython3,
		}, ref)
	})

	e.register(GeneratedFileSpec{OutputPath: output, ProducerRef: ref, OnUse: pending})
}
