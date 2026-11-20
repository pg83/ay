package main

func copyFileParsedIncludes(na *NodeArenas, srcVFS, moduleDir VFS, entry CopyFileEntry) []IncludeDirective {
	out := na.dirs.alloc(1 + len(entry.OutputIncludes))[:0]

	if entry.WithContext {
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
	if !entry.WithContext && e.skipDirectoryInput("COPY_FILE", "$L/TEXT/", []VFS{entry.Resolved.withGeneratedDirs(e.codegen, entry.Src, e.d.srcDirs)}) {
		return
	}

	if !e.submitOutput(copyFileOutputVFS(e.instance.Path.relString(), entry.Dst)) {
		return
	}

	st, info := e.registerCopyFile(entry)
	demanded := !e.producersOnly()

	if demanded && extIsArchiveMember(entry.Dst) {
		e.collectObj(st.ref, st.dstVFS, SrcMeta{Prio: stmtPrioDefault})
	}

	if entry.Auto && isSourceEligibleForCopyAuto(st.dstVFS.relString()) {
		e.enqueueSrc(SrcMeta{Source: st.dstVFS.any(), Prio: stmtPrioDefault, Auto: true})
	}

	pe := func() {
		e.emitCopyFileNodeSnap(entry, st)
	}

	if info != nil {
		info.rareFields().OnUse = e.pendingEmit(pe)
	}
}

func (e *EmitContext) registerCopyFile(entry CopyFileEntry) (CopyEmitState, *GeneratedFileInfo) {
	ctx, instance := e.ctx, e.instance
	reg := e.codegen
	resolved := entry.Resolved
	srcVFS := resolved.withGeneratedDirs(reg, entry.Src, e.d.srcDirs)

	if _, rooted := moduleRootedVFS(instance.Path.relString(), entry.Src); !rooted && srcVFS.isSource() {
		candidate := srcVFS.rel().build()

		if reg.lookup(candidate) == nil {
			candidate = buildClean(entry.Src)
		}

		if reg.lookup(candidate) != nil && !resolved.Existing {
			srcVFS = candidate
		}
	}

	dstVFS := copyFileOutputVFS(instance.Path.relString(), entry.Dst)

	if existing := reg.lookup(dstVFS); existing != nil && existing.SourcePath == srcVFS {
		e.collectScriptOutput(dstVFS)

		return CopyEmitState{srcVFS: srcVFS, dstVFS: dstVFS, ref: existing.ProducerRef}, nil
	}

	if srcVFS != dstVFS {
		e.requireProducedInput("COPY_FILE src", entry.Src, srcVFS)
	}

	parsed := copyFileParsedIncludes(ctx.na, srcVFS, instance.Path, entry)
	ref := ctx.emit.reserve()

	if existing := reg.lookup(dstVFS); existing != nil {
		existing.attributes.parsedIncludes.put(existing.id, ParsedIncludeSet{parsedIncludesLocal: parsed})
	} else {
		e.collectCPPStyleInputs(srcVFS)

		info := GeneratedFileSpec{
			OutputPath:     dstVFS,
			SourcePath:     srcVFS,
			ProducerRef:    ref,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
		}

		if entry.WithContext {
			info.IncludedInputs = &IncludedInputPlan{Roots: ctx.na.vfsList(srcVFS)}
		}

		if srcIsTemplate(srcVFS) {
			if srcVFS.isSource() && !entry.WithContext {
				reg.forwardTextTemplates(dstVFS, ctx.na.vfsList(srcVFS))
			} else {
				info.ClosureLeaves = ctx.na.vfsList(srcVFS)
			}
		}

		return CopyEmitState{srcVFS: srcVFS, dstVFS: dstVFS, ref: ref}, e.register(info)
	}

	return CopyEmitState{srcVFS: srcVFS, dstVFS: dstVFS, ref: ref}, nil
}

func (e *EmitContext) emitCopyFileNodeSnap(entry CopyFileEntry, st CopyEmitState) {
	ctx := e.ctx

	var closure []VFS

	if entry.WithContext {
		closure = e.scanner.walkClosureWithDefaultParser(st.srcVFS, e.d.scanCtx, scanDomainCC, nil).collect(ctx.na, func(VFS) bool { return true })
	}

	closure = append(closure, e.copyLoopSources(st.dstVFS)...)

	node := composeCPNode(e.instance, st.srcVFS, st.dstVFS, closure, e.d.tc, ctx.scripts, ctx.na)

	if entry.WithContext {
		node.input.text = node.input.text[1:]
	}

	e.emitReservedNode(node, st.ref)
}

func (e *EmitContext) copyLoopSources(dst VFS) []VFS {
	e.inputClosure(dst)

	return e.codegen.mustInfo(dst).inputLoopSources()
}

func (e *EmitContext) skipDirectoryInput(macro, link string, inputs []VFS) bool {
	for _, v := range inputs {
		if !v.isSource() || e.ctx.fs.isFileVFSAny(v) {
			continue
		}

		if present, isDir := e.ctx.fs.existsID(srcRootRel, v.rel()); present && isDir {
			dir := e.instance.Path.relString()
			where := makeFileWhere(dir)

			e.ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: where, Error: "$S/" + dir + ": skip processing macro " + macro + " due to unallowed input."})
			e.ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: where, Error: "input dependency in " + macro + " " + link + ymakeVFS(v) + " points to a directory and cannot be processed. Please provide a file path instead."})

			return true
		}
	}

	return false
}
