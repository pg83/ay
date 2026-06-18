package main

// emitFromSandboxes emits the SB (Sandbox fetch) node for every FROM_SANDBOX in
// the module and registers its OUT/OUT_NOAUTO files as build outputs bound to
// that node. A name so declared is a fetched build artifact: a RUN_PROGRAM/macro
// IN that consumes it then resolves (via runProgramInputVFS' codegen-registry
// probe) to the $(B) fetch output and takes the SB node as a dependency, instead
// of resolving to a nonexistent on-disk source path. Must run before the module's
// RUN_PROGRAM/RUN_PYTHON emit so the registry already holds the outputs.
func emitFromSandboxes(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	for _, fs := range d.fromSandboxes {
		emitFromSandbox(ctx, instance, d, fs)
	}
}

func emitFromSandbox(ctx *GenCtx, instance ModuleInstance, d *ModuleData, stmt *FromSandboxStmt) {
	na := ctx.emit.nodeArenas()
	id := stmt.ResourceId.string()

	// Mirrors upstream's FROM_SANDBOX .CMD: $YMAKE_PYTHON3 fetch_from_sandbox.py
	// … --resource-id $Id (--untar-to | --copy-to-dir) $PREFIX [--executable] --
	// $OUT $OUT_NOAUTO, run in the module build dir.
	mode := "--untar-to"
	if stmt.File {
		mode = "--copy-to-dir"
	}

	args := []STR{
		d.tc.Python3,
		buildScriptsFetchFromSandboxPy.str(),
		argYaStartCommandFile.str(),
		internStr("--resource-file"),
		internStr("$(RESOURCE_ROOT)/sbr/" + id + "/resource"),
		internStr("--resource-id"),
		stmt.ResourceId,
		internStr(mode),
		internStr(stmt.Prefix),
	}

	if stmt.Executable {
		args = append(args, internStr("--executable"))
	}

	args = append(args, arg2.str())

	outVFSs := make([]VFS, 0, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles))

	for _, f := range stmt.OUTFiles {
		args = append(args, f)
		outVFSs = append(outVFSs, copyFileOutputVFS(instance.Path.rel(), f.string()))
	}

	for _, f := range stmt.OUTNoAutoFiles {
		args = append(args, f)
		outVFSs = append(outVFSs, copyFileOutputVFS(instance.Path.rel(), f.string()))
	}

	args = append(args, argYaEndCommandFile.str())

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform:         instance.Platform,
		Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(args), Cwd: build(instance.Path.rel()).str(), Env: env}),
		Env:              env,
		Inputs:           na.inputList(ctx.scripts[buildScriptsFetchFromSandboxPy]),
		KV:               KV{P: pkSB, PC: pcYellow, ShowOut: true},
		Outputs:          na.vfsList(outVFSs...),
		Requirements:     Requirements{CPU: float64(1), Network: nwFull, RAM: float64(32)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Resources:        usesPython3,
	}

	ref := ctx.emit.emit(node)

	// Register each fetched output as a codegen producer so a consuming IN resolves
	// to the $(B) path and depends on this SB node. The files are opaque fetched
	// artifacts (no parsed includes); OUTPUT_INCLUDES, when present, ride as the
	// outputs' registered includes for downstream scans.
	parsed := fromSandboxOutputIncludes(stmt)

	for _, out := range outVFSs {
		registerBoundGeneratedParsedOutput(ctx, instance, pkSB, out, parsed, ref, nil)
	}
}

func fromSandboxOutputIncludes(stmt *FromSandboxStmt) []IncludeDirective {
	if len(stmt.OutputIncludes) == 0 {
		return nil
	}

	includes := make([]IncludeDirective, 0, len(stmt.OutputIncludes))

	for _, f := range stmt.OutputIncludes {
		if v := f.vfs(); v != 0 {
			f = internStr(v.rel())
		}

		includes = append(includes, IncludeDirective{kind: includeQuoted, target: f})
	}

	return includes
}
