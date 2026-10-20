package main

func copyFileAutoSourceVFS(modulePath string, d *ModuleData, src STR) *VFS {
	if d.copyFileAutoOutputs == nil {
		return nil
	}

	entry, ok := d.copyFileAutoOutputs[src]

	if !ok {
		return nil
	}

	return ptr(copyFileOutputVFS(modulePath, entry.Dst))
}

func copyFileParsedIncludes(na *NodeArenas, scanner *IncludeScanner, srcVFS, moduleDir VFS, entry CopyFileEntry) []IncludeDirective {
	var own, compileExtra []IncludeDirective

	hasCtx := false

	if entry.Text {
		own, compileExtra, _, _ = scanner.parsedIncludes(srcVFS, nil)
	} else if entry.WithContext {
		hasCtx = true
	}

	out := na.dirs.alloc(len(own) + len(compileExtra) + 1 + len(entry.OutputIncludes))[:0]

	out = append(out, own...)
	out = append(out, compileExtra...)

	if hasCtx {
		out = append(out, IncludeDirective{kind: includeQuoted, target: includeTarget(srcVFS.rel().any())})
	}

	for _, include := range entry.OutputIncludes {
		out = append(out, IncludeDirective{
			kind:   includeQuoted,
			target: includeTarget(internStr(copyFileIncludeTarget(moduleDir.relString(), include)).any()),
		})
	}

	na.dirs.commit(len(out))

	return out[:len(out):len(out)]
}

type CopyEmitState struct {
	srcVFS VFS
	dstVFS VFS
	ref    NodeRef
}

func (e *EmitContext) emitCopyFileStmt(entry CopyFileEntry) {
	st, info := e.registerCopyFile(entry)
	demanded := e.instance.Demand != demandNone

	if demanded && extIsArchiveMember(entry.Dst) {
		e.collectObj(st.ref, st.dstVFS, SrcMeta{Prio: stmtPrioDefault})
	}

	pe := func() {
		e.emitCopyFileNodeSnap(entry, st)
	}

	if info != nil {
		info.OnUse = e.pendingEmit(pe)
	}
}

func (e *EmitContext) registerCopyFile(entry CopyFileEntry) (CopyEmitState, *GeneratedFileInfo) {
	ctx, instance := e.ctx, e.instance
	reg := e.codegen
	srcVFS := copyFileInputVFS(ctx.fs, instance.Path, entry.Src)

	if len(e.d.srcDirs) > 0 {
		srcVFS = resolveSourceVFS(ctx, instance, entry.Src, e.d.srcDirs)
	}

	dstVFS := copyFileOutputVFS(instance.Path.relString(), entry.Dst)

	if existing := reg.lookup(dstVFS); existing != nil && existing.SourcePath == srcVFS {
		return CopyEmitState{srcVFS: srcVFS, dstVFS: dstVFS, ref: existing.ProducerRef}, nil
	}

	if srcVFS != dstVFS {
		e.requireProducedInput("COPY_FILE src", entry.Src, srcVFS)
	}

	parsed := copyFileParsedIncludes(ctx.na, e.scanner, srcVFS, instance.Path, entry)
	ref := ctx.emit.reserve()

	if existing := reg.lookup(dstVFS); existing != nil {
		existing.ParsedIncludes = ParsedIncludeSet{parsedIncludesLocal: parsed}
	} else {
		info := GeneratedFileInfo{
			OutputPath:     dstVFS,
			SourcePath:     srcVFS,
			ProducerRef:    ref,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
		}

		return CopyEmitState{srcVFS: srcVFS, dstVFS: dstVFS, ref: ref}, e.register(info)
	}

	return CopyEmitState{srcVFS: srcVFS, dstVFS: dstVFS, ref: ref}, nil
}

func (e *EmitContext) emitCopyFileNodeSnap(entry CopyFileEntry, st CopyEmitState) {
	ctx := e.ctx

	var closure []VFS

	if !entry.Text {
		closure = e.scanner.walkClosure(st.srcVFS, e.d.scanCtx, scanDomainCC).collect(ctx.na, func(VFS) bool { return true })
	}

	e.emitCPWithInputs(st.srcVFS, st.dstVFS, closure, st.ref, e.d.tc, ctx.scripts)
}
