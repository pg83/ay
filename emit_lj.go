package main

import "strings"

var ljKV = KV{P: pkLJ, PC: pcLightCyan}

const luajit21CwdRel = "contrib/libs/luajit_21"

func emitLJReserved(instance ModuleInstance, luaSrc, rawOut, compilerBin VFS, compilerLDRef NodeRef, cwd VFS, id NodeRef, emit *StreamingEmitter) {
	na := emit.nodeArenas()
	env := envVarsVCS

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(na.anyList(compilerBin.any(), argB2.any(), argDashG.any(), luaSrc.any(), rawOut.any())),
			Cwd:     cwd,
			Env:     env,
		}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(compilerBin, luaSrc)),
		KV:             &ljKV,
		Outputs:        na.vfsList(rawOut),
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: na.refList(compilerLDRef),
	}

	emit.emitReservedNode(node, id)
}

func (e *EmitContext) emitLuaJit21() {
	ctx, instance, d := e.ctx, e.instance, e.d

	if d.lj21 == nil {
		return
	}

	compilerLDRef, compilerBin := ctx.tool(argLuajit21Compiler)
	cwd := source(luajit21CwdRel)

	for _, lua := range d.lj21.Luas {
		luaSrc := resolveSourceVFS(ctx, instance, lua, d.srcDirs)
		rawOut := build(instance.Path.relString(), "/", strings.TrimSuffix(lua, ".lua"), ".raw")
		ref := ctx.emit.reserve()

		pe := func() {
			emitLJReserved(instance, luaSrc, rawOut, compilerBin, compilerLDRef, cwd, ref, ctx.emit)
		}
		pending := e.ctx.na.pendingEmit(pe)

		e.register(GeneratedFileInfo{
			OutputPath:   rawOut,
			ProducerRef:  ref,
			SourceInputs: ctx.na.vfsList(luaSrc),
			OnUse:        pending,
		})
	}
}
