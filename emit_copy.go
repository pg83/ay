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

func copyFileParsedIncludes(na *NodeArenas, scanner *IncludeScanner, fs FS, moduleDir VFS, entry CopyFileEntry) []IncludeDirective {
	var own, compileExtra []IncludeDirective

	hasCtx := false

	if entry.Text {
		srcVFS := copyFileInputVFS(fs, moduleDir, entry.Src)

		own, compileExtra = scanner.parsedIncludes(srcVFS, nil)
	} else if entry.WithContext {
		hasCtx = true
	}

	out := na.dirs.alloc(len(own) + len(compileExtra) + 1 + len(entry.OutputIncludes))[:0]

	out = append(out, own...)
	out = append(out, compileExtra...)

	if hasCtx {
		srcVFS := copyFileInputVFS(fs, moduleDir, entry.Src)

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
	srcVFS         VFS
	dstVFS         VFS
	ref            NodeRef
	producerSource []VFS
}

func (e *EmitContext) emitCopyFileStmt(entry CopyFileEntry) {
	st, info := e.registerCopyFile(entry)
	demanded := e.instance.Demand != demandNone

	if demanded && extIsArchiveMember(entry.Dst) {
		e.collectObj(st.ref, st.dstVFS, SrcMeta{Prio: stmtPrioDefault})
	}

	// pe captures st (CopyEmitState), which registerCopyFile only produces
	// as its return value — register() there happens before pe can exist,
	// and is skipped entirely on the merge-existing-dst path (info == nil).
	pe := func() {
		e.emitCopyFileNodeSnap(entry, st)
	}
	pending := e.ctx.na.pendingEmit(pe)

	if info != nil {
		info.OnUse = pending
	}
}

func (e *EmitContext) registerCopyFile(entry CopyFileEntry) (CopyEmitState, *GeneratedFileInfo) {
	ctx, instance := e.ctx, e.instance
	reg := e.codegen
	srcVFS := copyFileInputVFS(ctx.fs, instance.Path, entry.Src)
	dstVFS := copyFileOutputVFS(instance.Path.relString(), entry.Dst)

	if srcVFS != dstVFS {
		e.requireProducedInput("COPY_FILE src", entry.Src, srcVFS)
	}

	parsed := copyFileParsedIncludes(ctx.na, e.scanner, ctx.fs, instance.Path, entry)
	ref := ctx.emit.reserve()

	var producerSource []VFS

	if srcInfo := reg.use(srcVFS); srcInfo != nil {
		producerSource = srcInfo.SourceInputs
	}

	if existing := reg.lookup(dstVFS); existing != nil {
		existing.ParsedIncludes = ParsedIncludeSet{parsedIncludesLocal: parsed}
	} else {
		info := GeneratedFileInfo{
			OutputPath:     dstVFS,
			SourcePath:     srcVFS,
			ProducerRef:    ref,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
		}

		if srcVFS != dstVFS {
			fsTools := ctx.scripts[copyFsToolsVFS.rel()]
			leaves := ctx.na.vfs.alloc(len(fsTools) + 1)
			ln := copy(leaves, fsTools)

			if entry.Text || entry.Auto {
				leaves[ln] = srcVFS
				ln++
			}

			ctx.na.vfs.commit(ln)
			info.ClosureLeaves = leaves[:ln:ln]

			if srcVFS.isSource() {
				fsToolsDeps := ctx.scripts[copyFsToolsVFS.rel()]
				block := ctx.na.vfs.alloc(1 + len(fsToolsDeps))

				block[0] = srcVFS

				bn := 1 + copy(block[1:], fsToolsDeps)

				ctx.na.vfs.commit(bn)
				info.SourceInputs = block[:bn:bn]
			}
		}

		if len(producerSource) > 0 {
			fsToolsDeps := ctx.scripts[copyFsToolsVFS.rel()]
			merged := ctx.na.vfs.alloc(len(producerSource) + len(fsToolsDeps))
			mn := copy(merged, producerSource)

			mn += copy(merged[mn:], fsToolsDeps)
			ctx.na.vfs.commit(mn)

			info.SourceInputs = merged[:mn:mn]
		}

		return CopyEmitState{srcVFS: srcVFS, dstVFS: dstVFS, ref: ref, producerSource: producerSource}, e.register(info)
	}

	return CopyEmitState{srcVFS: srcVFS, dstVFS: dstVFS, ref: ref, producerSource: producerSource}, nil
}

func (e *EmitContext) emitCopyFileNodeSnap(entry CopyFileEntry, st CopyEmitState) {
	ctx := e.ctx
	var closure []VFS

	if entry.WithContext || len(entry.OutputIncludes) > 0 {
		raw := rewriteClosureCPSource(ctx.na, e.scanner, e.scanner.walkClosure(st.dstVFS, e.d.scanCtx, scanDomainCC))

		raw = filterSourceVFS(ctx.na, raw)

		dedupers.with(func(deduper *DeDuper) {
			block := ctx.na.vfs.alloc(len(raw) + len(st.producerSource))[:0]

			for _, v := range raw {
				if deduper.add(v.strID()) {
					block = append(block, v)
				}
			}

			block = append(block, st.producerSource...)
			ctx.na.vfs.commit(len(block))

			closure = block[:len(block):len(block)]
		})
	} else if len(st.producerSource) > 0 {
		closure = ctx.na.vfsList(st.producerSource...)
	}

	e.emitCPWithDeps(st.srcVFS, st.dstVFS, nil, closure, st.ref, e.d.cc.ModuleTag, e.d.tc, ctx.scripts)
}
