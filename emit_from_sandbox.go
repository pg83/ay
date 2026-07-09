package main

var fromSandboxScriptInputs = []VFS{
	buildScriptsFetchFromSandboxPy,
	buildScriptsProcessCommandFilesPy,
	buildScriptsFetchFromPy,
}

var fromSandboxKV = KV{P: pkSB, PC: pcYellow, ShowOut: true}

func (e *EmitContext) emitFromSandboxes() (memberRefs []NodeRef, memberPaths []VFS) {
	_, _, d := e.ctx, e.instance, e.d

	for _, fs := range d.fromSandboxes {
		refs, paths := e.emitFromSandbox(fs)

		memberRefs = append(memberRefs, refs...)
		memberPaths = append(memberPaths, paths...)
	}

	return memberRefs, memberPaths
}

func fromSandboxAutoLinkMember(name string) bool {
	return extIsArchiveMember(name)
}

func (e *EmitContext) emitFromSandbox(stmt *FromSandboxStmt) (memberRefs []NodeRef, memberPaths []VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.emit.nodeArenas()
	id := stmt.ResourceId.string()
	mode := "--untar-to"

	if stmt.File {
		mode = "--copy-to-dir"
	}

	args := na.anys.alloc(12 + 2*len(stmt.Renames) + len(stmt.OUTFiles) + len(stmt.OUTNoAutoFiles))[:0]

	args = append(args,
		d.tc.Python3.any(),
		buildScriptsFetchFromSandboxPy.any(),
		argYaStartCommandFile.any(),
		strResourceFile.any(),
		internV("$(RESOURCE_ROOT)/sbr/", id, "/resource").any(),
		strResourceId.any(),
		stmt.ResourceId,
		internStr(mode).any(),
		internStr(stmt.Prefix).any(),
	)

	for _, r := range stmt.Renames {
		args = append(args, strRename.any(), r)
	}

	if stmt.Executable {
		args = append(args, strExecutable.any())
	}

	args = append(args, arg2.any())

	outVFSs := make([]VFS, 0, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles))

	for _, f := range stmt.OUTFiles {
		args = append(args, f)
		outVFSs = append(outVFSs, copyFileOutputVFS(instance.Path.relString(), f.string()))
	}

	for _, f := range stmt.OUTNoAutoFiles {
		args = append(args, f)
		outVFSs = append(outVFSs, copyFileOutputVFS(instance.Path.relString(), f.string()))
	}

	args = append(args, argYaEndCommandFile.any())
	na.anys.commit(len(args))

	args = args[:len(args):len(args)]

	env := envVarsVCS

	ref := ctx.emit.reserve()

	pe := &PendingEmit{fn: func() {
		node := Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(args), Cwd: instance.Path.rel().build(), Env: env}),
			Env:          env,
			Inputs:       na.inputList(fromSandboxScriptInputs),
			KV:           &fromSandboxKV,
			Outputs:      na.vfsList(outVFSs...),
			Requirements: Requirements{CPU: float64(1), Network: nwFull, RAM: float64(32)},
			Resources:    usesPython3,
		}

		ctx.emit.emitReservedNode(node, ref)
	}}

	e.noteOwn(pe)

	for i, f := range stmt.OUTFiles {
		if fromSandboxAutoLinkMember(f.string()) {
			memberRefs = append(memberRefs, ref)
			memberPaths = append(memberPaths, outVFSs[i])
		}
	}

	parsed := fromSandboxOutputIncludes(ctx.na, stmt)

	for _, out := range outVFSs {
		info := e.codegen.register(GeneratedFileInfo{
			OutputPath:      out,
			ProducerRef:     ref,
			ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: parsed},
			SourceInputs:    fromSandboxScriptInputs,
			ProducerMainOut: outVFSs[0],
		})

		info.pending = pe
	}

	return memberRefs, memberPaths
}

func fromSandboxOutputIncludes(na *NodeArenas, stmt *FromSandboxStmt) []IncludeDirective {
	if len(stmt.OutputIncludes) == 0 {
		return nil
	}

	includes := na.dirs.alloc(len(stmt.OutputIncludes))[:0]

	for _, f := range stmt.OutputIncludes {
		if v := f.vfs(); v != 0 {
			f = v.rel().any()
		}

		includes = append(includes, IncludeDirective{kind: includeQuoted, target: includeTarget(f)})
	}

	na.dirs.commit(len(includes))

	return includes[:len(includes):len(includes)]
}
