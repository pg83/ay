package main

import "strings"

var ljKV = KV{P: pkLJ, PC: pcLightCyan}

const luajit21CwdRel = "contrib/libs/luajit_21"

type LuaCompileStmt struct {
	Resolved ResolvedSource
	Source   string
	Runtime  string
	Name     string
}

func (e *EmitContext) emitLuaCompile(stmt LuaCompileStmt) {
	src := stmt.Resolved.withGenerated(e.codegen, stmt.Source, e.d.srcDirs)
	out := copyFileOutputVFS(e.instance.Path.relString(), stmt.Source+".o")
	ref := e.ctx.emit.reserve()
	compilerRef, compiler := e.ctx.tool(internArg(stmt.Runtime + "/compiler"))

	pending := e.pendingEmit(func() {
		e.emitLJReserved(src, out, compiler, compilerRef, source(stmt.Runtime), stmt.Name, ref)
	})

	e.register(GeneratedFileSpec{OutputPath: out, ProducerRef: ref, OnUse: pending})

	if !e.producersOnly() {
		e.codegen.use(out)
		e.collectObj(ref, out, SrcMeta{Global: true, Prio: stmtPrioDefault})
	}
}

func (e *EmitContext) emitLJReserved(luaSrc, rawOut, compilerBin VFS, compilerLDRef NodeRef, cwd VFS, name string, id NodeRef) {
	na := e.ctx.na
	env := envVarsVCS
	args := []ANY{compilerBin.any(), argB2.any(), argDashG.any(), luaSrc.any()}

	if name != "" {
		args = append(args, argN.any(), internStr(name).any())
	}

	args = append(args, rawOut.any())

	node := Node{
		Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(na.anyList(args...)),
			Cwd:     cwd,
			Env:     env,
		}),
		Env:    env,
		Inputs: NodeInputs{files: na.inputList(na.vfsList(compilerBin), na.vfsList(luaSrc)), roots: na.inputList(na.vfsList(luaSrc))},

		KV:             &ljKV,
		Outputs:        na.vfsList(rawOut),
		ForeignDepRefs: na.refList(compilerLDRef),
	}

	e.emitReservedNode(node, id)
}

func (e *EmitContext) emitLuaJit21() {
	ctx, instance, d := e.ctx, e.instance, e.d
	compilerLDRef, compilerBin := ctx.tool(argLuajit21Compiler)
	cwd := source(luajit21CwdRel)

	for _, lua := range d.lj21.Luas {
		luaSrc := resolveSourceVFS(ctx, instance, lua, d.srcDirs)
		rawOut := build(instance.Path.relString(), "/", strings.TrimSuffix(lua, ".lua"), ".raw")
		ref := ctx.emit.reserve()

		pe := func() {
			e.emitLJReserved(luaSrc, rawOut, compilerBin, compilerLDRef, cwd, "", ref)
		}

		pending := e.pendingEmit(pe)

		e.register(GeneratedFileSpec{
			OutputPath:  rawOut,
			ProducerRef: ref,

			OnUse: pending,
		})
	}
}
