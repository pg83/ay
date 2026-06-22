package main

import "strings"

// luajit21CwdRel is the cwd the objdump runs in.
const luajit21CwdRel = "contrib/libs/luajit_21"

// emitLJ precompiles a single .lua source to a .raw build output.
func emitLJ(instance ModuleInstance, luaSrc, rawOut, compilerBin VFS, compilerLDRef NodeRef, cwd STR, emit Emitter) NodeRef {
	na := emit.nodeArenas()

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(na.strList(compilerBin.str(), argB2.str(), argDashG.str(), luaSrc.str(), rawOut.str())),
			Cwd:     cwd,
			Env:     env,
		}),
		Env:              env,
		Inputs:           na.inputList(na.vfsList(compilerBin, luaSrc)),
		KV:               KV{P: pkLJ, PC: pcLightCyan},
		Outputs:          na.vfsList(rawOut),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		ForeignDepRefs:   depRefs(compilerLDRef),
	}

	return emit.emit(node)
}

// emitLuaJit21 models LJ_21_ARCHIVE: compile each .lua to a .raw, then wire two
// archive_by_keys outputs — LuaScripts.inc over the raws and LuaSources.inc over
// the sources. Runs before emitArchives so the appended entries are emitted.
func emitLuaJit21(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	if d.lj21 == nil {
		return
	}

	luas := d.lj21.Luas
	compilerLDRef, compilerBin := ctx.tool(argLuajit21Compiler)
	reg := codegenRegForInstance(ctx, instance)
	cwd := source(luajit21CwdRel).str()

	raws := make([]string, len(luas))

	for i, lua := range luas {
		raw := strings.TrimSuffix(lua, ".lua") + ".raw"
		raws[i] = raw

		luaSrc := resolveSourceVFS(ctx, instance, lua, d.srcDirs)
		rawOut := build(instance.Path.rel() + "/" + raw)
		ref := emitLJ(instance, luaSrc, rawOut, compilerBin, compilerLDRef, cwd, ctx.emit)

		// Register the $(S) lua source as a propagated source input so
		// emitArchive folds it into the archive's closure leaves.
		reg.register(&GeneratedFileInfo{
			ProducerKvP:  pkLJ,
			OutputPath:   rawOut,
			ProducerRef:  ref,
			SourceInputs: []VFS{luaSrc},
		})
	}

	d.archives = append(d.archives,
		ArchiveEntry{Name: "LuaScripts.inc", DontCompress: true, Files: raws, Keys: luas, PropagateSourceMembers: true},
		ArchiveEntry{Name: "LuaSources.inc", DontCompress: true, Files: append([]string(nil), luas...), Keys: luas, PropagateSourceMembers: true},
	)
}
