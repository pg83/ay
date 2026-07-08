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

func copyFileParsedIncludes(scanner *IncludeScanner, fs FS, moduleDir VFS, entry CopyFileEntry) []IncludeDirective {
	out := make([]IncludeDirective, 0, len(entry.OutputIncludes)+1)

	if entry.Text {
		srcVFS := copyFileInputVFS(fs, moduleDir, entry.Src)
		own, compileExtra := scanner.parsedIncludes(srcVFS, nil)

		out = append(out, own...)
		out = append(out, compileExtra...)
	} else if entry.WithContext {
		srcVFS := copyFileInputVFS(fs, moduleDir, entry.Src)

		out = append(out, IncludeDirective{kind: includeQuoted, target: includeTarget(srcVFS.rel().any())})
	}

	for _, include := range entry.OutputIncludes {
		out = append(out, IncludeDirective{
			kind:   includeQuoted,
			target: includeTarget(internStr(copyFileIncludeTarget(moduleDir.relString(), include)).any()),
		})
	}

	return out
}

type CopyEmitState struct {
	srcVFS         VFS
	dstVFS         VFS
	ref            NodeRef
	producerSource []VFS
}

func (e *EmitContext) emitCopyFileStmt(entry CopyFileEntry) {
	st := e.registerCopyFile(entry)

	e.deferPass2(func() {
		e.emitCopyFileNode(entry, st)
	})
}

func (e *EmitContext) registerCopyFile(entry CopyFileEntry) CopyEmitState {
	ctx, instance := e.ctx, e.instance
	scanner := e.scanner
	reg := e.codegen
	srcVFS := copyFileInputVFS(ctx.fs, instance.Path, entry.Src)
	dstVFS := copyFileOutputVFS(instance.Path.relString(), entry.Dst)

	if srcVFS != dstVFS {
		e.requireProducedInput("COPY_FILE src", entry.Src, srcVFS)
	}

	parsed := copyFileParsedIncludes(scanner, ctx.fs, instance.Path, entry)
	ref := ctx.emit.reserve()

	var producerSource []VFS

	if srcInfo := reg.lookup(srcVFS); srcInfo != nil {
		producerSource = srcInfo.SourceInputs
	}

	if existing := reg.lookup(dstVFS); existing != nil {
		existing.ParsedIncludes = ParsedIncludeSet{parsedIncludesLocal: parsed}
	} else {
		info := &GeneratedFileInfo{
			OutputPath:     dstVFS,
			SourcePath:     srcVFS,
			IsText:         entry.Text,
			ProducerRef:    ref,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
		}

		if srcVFS != dstVFS {
			info.ClosureLeaves = append(info.ClosureLeaves, ctx.scripts[copyFsToolsVFS.rel()]...)

			if entry.Text || entry.Auto {
				info.ClosureLeaves = append(info.ClosureLeaves, srcVFS)
			}

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

		reg.register(info)
	}

	return CopyEmitState{srcVFS: srcVFS, dstVFS: dstVFS, ref: ref, producerSource: producerSource}
}

func (e *EmitContext) emitCopyFileNode(entry CopyFileEntry, st CopyEmitState) {
	ctx, instance, d := e.ctx, e.instance, e.d
	scanner := e.scanner
	deps := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, []VFS{st.srcVFS})

	var closure []VFS

	if entry.WithContext || len(entry.OutputIncludes) > 0 {
		raw := rewriteClosureCPSource(ctx.na, scanner, walkClosure(e.scanner, st.dstVFS, d.cc.ScanCfg))

		raw = filterSourceVFS(ctx.na, raw)

		block := ctx.na.vfs.alloc(len(raw) + len(st.producerSource))[:0]

		deduper.reset()

		for _, v := range raw {
			if deduper.add(v.strID()) {
				block = append(block, v)
			}
		}

		block = append(block, st.producerSource...)
		ctx.na.vfs.commit(len(block))

		closure = block[:len(block):len(block)]
	} else if len(st.producerSource) > 0 {
		closure = ctx.na.vfsList(st.producerSource...)
	}

	emitCPWithDeps(instance, st.srcVFS, st.dstVFS, deps, closure, st.ref, d.cc.ModuleTag, d.tc, ctx.scripts, ctx.emit)

	if extIsArchiveMember(entry.Dst) {
		e.collectObj(st.ref, st.dstVFS, SrcMeta{Prio: stmtPrioDefault})
	}
}
