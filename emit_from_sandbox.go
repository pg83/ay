package main

import "slices"

var (
	fromSandboxKV           = KV{P: pkSB, PC: pcYellow, ShowOut: true}
	fromSandboxRequirements = Requirements{CPU: 1, Network: nwFull, RAM: 32}
)

var fromSandboxScriptInputs = []VFS{
	buildScriptsFetchFromSandboxPy,
	buildScriptsProcessCommandFilesPy,
	buildScriptsFetchFromPy,
}

type SandboxRecipe struct {
	Stmt    *FromSandboxStmt
	Outputs []VFS
	Python3 VFS
}

func sameSandboxStmt(a, b *FromSandboxStmt) bool {
	return a.ResourceId == b.ResourceId && a.File == b.File &&
		a.Executable == b.Executable && a.Prefix == b.Prefix &&
		slices.Equal(a.OUTFiles, b.OUTFiles) && slices.Equal(a.OUTNoAutoFiles, b.OUTNoAutoFiles) &&
		slices.Equal(a.OutputIncludes, b.OutputIncludes) && slices.Equal(a.Renames, b.Renames)
}

func (e *EmitContext) existingSandbox(stmt *FromSandboxStmt) *GeneratedFileInfo {
	outputs := stmt.OUTFiles

	if len(outputs) == 0 {
		outputs = stmt.OUTNoAutoFiles
	}

	if len(outputs) == 0 {
		return nil
	}

	out := copyFileOutputVFS(e.instance.Path.relString(), outputs[0].string())
	info := e.codegen.lookup(out)

	if info == nil || info.OwnerModule != e.instance.Path || info.Sandbox == nil || info.Sandbox.Python3 != e.d.tc.Python3 || !sameSandboxStmt(info.Sandbox.Stmt, stmt) {
		return nil
	}

	return info
}

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
	if info := e.existingSandbox(stmt); info != nil {
		for i, out := range info.Sandbox.Outputs {
			e.collectScriptOutput(out)

			if i < len(stmt.OUTFiles) && fromSandboxAutoLinkMember(stmt.OUTFiles[i].string()) {
				memberRefs = append(memberRefs, info.ProducerRef)
				memberPaths = append(memberPaths, out)
			}
		}

		return memberRefs, memberPaths
	}

	ctx := e.ctx
	node, outVFSs := fromSandboxNode(ctx.emit.nodeArenas(), e.instance, e.d.tc.Python3.any(), stmt)
	ref := ctx.emit.reserve()
	sourceAction := &SourceActionInfo{DeclSeq: stmt.DeclSeq}
	pending := e.pendingEmit(func() { e.emitReservedNode(node, ref) })

	for i, f := range stmt.OUTFiles {
		if fromSandboxAutoLinkMember(f.string()) {
			memberRefs = append(memberRefs, ref)
			memberPaths = append(memberPaths, outVFSs[i])
		}
	}

	parsed := extractionOutputIncludes(ctx.na, stmt.OutputIncludes)
	recipe := &SandboxRecipe{Stmt: stmt, Outputs: outVFSs, Python3: e.d.tc.Python3}

	for _, out := range outVFSs {
		e.register(GeneratedFileInfo{
			SourceAction:   sourceAction,
			OutputPath:     out,
			Sandbox:        recipe,
			ProducerRef:    ref,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},

			ProducerMainOut: outVFSs[0],
			OnUse:           pending,
		})
	}

	return memberRefs, memberPaths
}

func fromSandboxNode(na *NodeArenas, instance ModuleInstance, python3 ANY, stmt *FromSandboxStmt) (Node, []VFS) {
	id := stmt.ResourceId.string()
	mode := "--untar-to"

	if stmt.File {
		mode = "--copy-to-dir"
	}

	args := na.anys.alloc(12 + 2*len(stmt.Renames) + len(stmt.OUTFiles) + len(stmt.OUTNoAutoFiles))[:0]

	args = append(args,
		python3,
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

	return Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(args), Cwd: instance.Path.rel().build(), Env: env}),
		Env:          env,
		Inputs:       na.inputList(fromSandboxScriptInputs),
		KV:           &fromSandboxKV,
		Outputs:      na.vfsList(outVFSs...),
		Requirements: &fromSandboxRequirements,
		Resources:    usesPython3,
	}, outVFSs
}

func extractionOutputIncludes(na *NodeArenas, files []ANY) []IncludeDirective {
	if len(files) == 0 {
		return nil
	}

	includes := na.dirs.alloc(len(files))[:0]

	for _, f := range files {
		if v := f.vfs(); v != 0 {
			f = v.rel().any()
		}

		includes = append(includes, IncludeDirective{kind: includeQuoted, target: includeTarget(f)})
	}

	na.dirs.commit(len(includes))

	return includes[:len(includes):len(includes)]
}
