package main

import "strings"

var ljKV = KV{P: pkLJ, PC: pcLightCyan}

const luajit21CwdRel = "contrib/libs/luajit_21"

func (e *EmitContext) emitLJReserved(luaSrc, rawOut, compilerBin VFS, compilerLDRef NodeRef, cwd VFS, id NodeRef) {
	na := e.ctx.na
	env := envVarsVCS

	node := Node{
		Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(na.anyList(compilerBin.any(), argB2.any(), argDashG.any(), luaSrc.any(), rawOut.any())),
			Cwd:     cwd,
			Env:     env,
		}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(compilerBin), na.vfsList(luaSrc)),
		KV:             &ljKV,
		Outputs:        na.vfsList(rawOut),
		ForeignDepRefs: na.refList(compilerLDRef),
	}

	e.emitReservedNode(node, id)
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
			e.emitLJReserved(luaSrc, rawOut, compilerBin, compilerLDRef, cwd, ref)
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
