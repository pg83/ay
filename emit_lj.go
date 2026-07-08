package main

import "strings"

var ljKV = KV{P: pkLJ, PC: pcLightCyan}

const luajit21CwdRel = "contrib/libs/luajit_21"

func emitLJ(instance ModuleInstance, luaSrc, rawOut, compilerBin VFS, compilerLDRef NodeRef, cwd VFS, emit *StreamingEmitter) NodeRef {
	na := emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}}

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
		ForeignDepRefs: depRefs(compilerLDRef),
	}

	return emit.emitNode(node)
}

func (e *EmitContext) emitLuaJit21() {
	ctx, instance, d := e.ctx, e.instance, e.d

	if d.lj21 == nil {
		return
	}

	compilerLDRef, compilerBin := ctx.tool(argLuajit21Compiler)
	reg := e.codegen
	cwd := source(luajit21CwdRel)

	for _, lua := range d.lj21.Luas {
		luaSrc := resolveSourceVFS(ctx, instance, lua, d.srcDirs)
		rawOut := build(instance.Path.relString(), "/", strings.TrimSuffix(lua, ".lua"), ".raw")
		ref := emitLJ(instance, luaSrc, rawOut, compilerBin, compilerLDRef, cwd, ctx.emit)

		reg.register(&GeneratedFileInfo{
			OutputPath:   rawOut,
			ProducerRef:  ref,
			SourceInputs: []VFS{luaSrc},
		})
	}
}
