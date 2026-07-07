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
		rdRef, rdOut := e.emitArchiveAsmRodata(a.Name+".rodata", rodataRef)

		e.collectObj(rdRef, rdOut, SrcMeta{Prio: stmtPrioDefault, Generated: true, Bucket: bkArchiveAsm})
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
	cmdArgs := make([]ANY, 0, 4+len(a.Files)+2)

	cmdArgs = append(cmdArgs, (toolBinPath).any(), argQ.any())

	if a.DontCompress {
		cmdArgs = append(cmdArgs, argP.any())
	}

	producerRefs := []NodeRef{}

	deduper.reset()

	pathPerFile := make([]VFS, 0, len(a.Files))

	for _, f := range a.Files {
		var memberVFS VFS

		if info := reg.lookup(copyFileOutputVFS(instance.Path.relString(), f)); info != nil {
			memberVFS = copyFileOutputVFS(instance.Path.relString(), f)

			if deduper.add(info.ProducerRef.strID()) {
				producerRefs = append(producerRefs, info.ProducerRef)
			}
		} else {
			memberVFS = e.requireProducedInput("ARCHIVE_ASM member", f, resolveSourceVFS(ctx, instance, f, d.srcDirs))
		}

		pathPerFile = append(pathPerFile, memberVFS)
		cmdArgs = append(cmdArgs, internV(memberVFS.string(), ":").any())
	}

	cmdArgs = append(cmdArgs, argDashO.any(), (rodataVFS).any())

	inputs := make([]VFS, 0, len(pathPerFile))

	deduper.reset()

	for _, p := range pathPerFile {
		if deduper.add(p.strID()) {
			inputs = append(inputs, p)
		}
	}

	deps := concat(producerRefs, depRefs(toolLDRef))
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

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
		if info := reg.lookup(p); info != nil && len(info.SourceInputs) > 0 {
			leaves = dedup(leaves, info.SourceInputs)
		} else if info == nil {
			leaves = dedup(leaves, []VFS{p})
		}
	}

	reg.register(&GeneratedFileInfo{
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
	leaves := walkClosure(e.scanner, rodataPath, d.cc.ScanCfg)
	yasmLDRef, _ := ctx.tool(argContribToolsYasm)
	ref, _, outPath := emitRD(instance, rodataRel, rodataPath, yasmLDRef, leaves, []NodeRef{producerRef}, d.cc.TC, ctx.emit)

	return ref, outPath
}
