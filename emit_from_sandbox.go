package main

import "strings"

var fromSandboxScriptInputs = []VFS{
	buildScriptsFetchFromSandboxPy,
	buildScriptsProcessCommandFilesPy,
	buildScriptsFetchFromPy,
}

var fromSandboxKV = KV{P: pkSB, PC: pcYellow, ShowOut: true}

func emitFromSandboxes(ctx *GenCtx, instance ModuleInstance, d *ModuleData) (memberRefs []NodeRef, memberPaths []VFS) {
	for _, fs := range d.fromSandboxes {
		refs, paths := emitFromSandbox(ctx, instance, d, fs)

		memberRefs = append(memberRefs, refs...)
		memberPaths = append(memberPaths, paths...)
	}

	return memberRefs, memberPaths
}

func fromSandboxAutoLinkMember(name string) bool {
	return strings.HasSuffix(name, ".a") || strings.HasSuffix(name, ".o")
}

func emitFromSandbox(ctx *GenCtx, instance ModuleInstance, d *ModuleData, stmt *FromSandboxStmt) (memberRefs []NodeRef, memberPaths []VFS) {
	na := ctx.emit.nodeArenas()
	id := stmt.ResourceId.string()
	mode := "--untar-to"

	if stmt.File {
		mode = "--copy-to-dir"
	}

	args := []STR{
		d.tc.Python3,
		buildScriptsFetchFromSandboxPy.str(),
		argYaStartCommandFile.str(),
		strResourceFile,
		internV("$(RESOURCE_ROOT)/sbr/", id, "/resource"),
		strResourceId,
		stmt.ResourceId,
		internStr(mode),
		internStr(stmt.Prefix),
	}

	for _, r := range stmt.Renames {
		args = append(args, strRename, r)
	}

	if stmt.Executable {
		args = append(args, strExecutable)
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
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(args), Cwd: build(instance.Path.rel()).str(), Env: env}),
		Env:          env,
		Inputs:       na.inputList(fromSandboxScriptInputs),
		KV:           &fromSandboxKV,
		Outputs:      na.vfsList(outVFSs...),
		Requirements: Requirements{CPU: float64(1), Network: nwFull, RAM: float64(32)},
		Resources:    usesPython3,
	}

	ref := ctx.emit.emit(node)

	for i, f := range stmt.OUTFiles {
		if fromSandboxAutoLinkMember(f.string()) {
			memberRefs = append(memberRefs, ref)
			memberPaths = append(memberPaths, outVFSs[i])
		}
	}

	parsed := fromSandboxOutputIncludes(stmt)

	for _, out := range outVFSs {
		ctx.codegenFor(instance).register(&GeneratedFileInfo{
			ProducerKvP:     pkSB,
			OutputPath:      out,
			ProducerRef:     ref,
			ParsedIncludes:  parsed,
			SourceInputs:    fromSandboxScriptInputs,
			ProducerMainOut: outVFSs[0],
		})
	}

	return memberRefs, memberPaths
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
