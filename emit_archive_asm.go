package main

var archiveAsmKV = KV{P: pkAR, PC: pcLightCyan}

func (e *EmitContext) emitArchiveAsmForAR() {
	ctx, _, d := e.ctx, e.instance, e.d

	if len(d.archiveAsm) == 0 {
		return
	}

	toolLDRef, toolBinPath := ctx.tool(argToolsArchiver)
	reg := e.codegen

	for _, a := range d.archiveAsm {
		rodataRef := e.emitArchiveAsmNode(a, toolBinPath, toolLDRef, reg)
		rodataRel := a.Name + ".rodata"
		rdRef, rdOut := e.emitArchiveAsmRodata(rodataRel, rodataRef)

		e.collectObj(rdRef, rdOut, SrcMeta{Source: build(e.instance.Path.relString(), "/", rodataRel).any(), Prio: stmtPrioDefault, Bucket: bkArchiveAsm})
	}
}

func (e *EmitContext) emitArchiveAsmNode(
	a ArchiveAsmEntry,
	toolBinPath VFS,
	toolLDRef NodeRef,
	reg *CodegenRegistry,
) NodeRef {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.emit.nodeArenas()
	rodataVFS := build(instance.Path.relString(), "/", a.Name, ".rodata")
	producerRefs := []NodeRef{}

	pathMark := len(e.prodVFS)

	for _, f := range a.Files {
		var memberVFS VFS

		if info := reg.use(copyFileOutputVFS(instance.Path.relString(), f)); info != nil {
			memberVFS = copyFileOutputVFS(instance.Path.relString(), f)

			producerRefs = append(producerRefs, info.ProducerRef)
		} else {
			memberVFS = e.requireProducedInput("ARCHIVE_ASM member", f, resolveSourceVFS(ctx, instance, f, d.srcDirs))
		}

		e.prodVFS = append(e.prodVFS, memberVFS)
	}

	pathPerFile := e.prodVFSTake(pathMark)
	cmdArgs := na.anys.alloc(5 + len(a.Files))[:0]

	cmdArgs = append(cmdArgs, toolBinPath.any(), argQ.any())

	if a.DontCompress {
		cmdArgs = append(cmdArgs, argP.any())
	}

	for _, memberVFS := range pathPerFile {
		cmdArgs = append(cmdArgs, internV(memberVFS.prefix(), memberVFS.relString(), ":").any())
	}

	cmdArgs = append(cmdArgs, argDashO.any(), rodataVFS.any())
	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

	var inputs []VFS

	dedupers.with(func(deduper *DeDuper) {
		producerRefs = dedupInPlaceWith(deduper, producerRefs)
		deduper.reset()
		inputs = na.vfs.alloc(len(pathPerFile))[:0]

		for _, p := range pathPerFile {
			if deduper.add(p.strID()) {
				inputs = append(inputs, p)
			}
		}

		na.vfs.commit(len(inputs))
	})

	inputs = inputs[:len(inputs):len(inputs)]

	deps := na.noderefs.alloc(len(producerRefs) + 1)[:0]

	deps = append(deps, producerRefs...)

	if toolLDRef != 0 {
		deps = append(deps, toolLDRef)
	}

	na.noderefs.commit(len(deps))

	deps = deps[:len(deps):len(deps)]

	env := envVarsVCS

	n := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:          env,
		Inputs:       na.inputList(inputs, na.srcChunk(toolBinPath)),
		KV:           &archiveAsmKV,
		Outputs:      na.vfsList(rodataVFS),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      deps,
		Resources:    instance.Platform.UsesPython3Clang,
	}

	rodataRef := ctx.emit.emitNode(n)

	var leaves []VFS

	for _, p := range pathPerFile {
		if info := reg.use(p); info != nil && len(info.SourceInputs) > 0 {
			leaves = append(leaves, info.SourceInputs...)
		} else if info == nil {
			leaves = append(leaves, p)
		}
	}

	leaves = dedupInPlace(leaves)

	e.register(GeneratedFileInfo{
		OutputPath:    rodataVFS,
		ProducerRef:   rodataRef,
		ClosureLeaves: leaves,
	})

	return rodataRef
}

func (e *EmitContext) emitArchiveAsmRodata(rodataRel string, producerRef NodeRef) (NodeRef, VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d

	if instance.Platform.ISA != ISAX8664 {
		throwFmt("gen: unsupported .rodata platform %s for ARCHIVE_ASM %q", instance.Platform.ISA, rodataRel)
	}

	rodataPath := build(instance.Path.relString(), "/", rodataRel)
	leaves := e.scanner.walkClosure(rodataPath, d.scanCtx, scanDomainCC)
	yasmLDRef, _ := ctx.tool(argContribToolsYasm)
	ref, _, outPath := emitRD(instance, rodataRel, rodataPath, yasmLDRef, leaves, []NodeRef{producerRef}, d.cc.TC, ctx.emit)

	return ref, outPath
}
