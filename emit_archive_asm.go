package main

var archiveAsmKV = KV{P: pkAR, PC: pcLightCyan}

func (e *EmitContext) emitArchiveAsmForAR() *RunProgramsForARResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	if len(d.archiveAsm) == 0 {
		return nil
	}

	toolLDRef, toolBinPath := ctx.tool(argToolsArchiver)
	reg := ctx.codegenFor(instance)
	res := &RunProgramsForARResult{}

	for _, a := range d.archiveAsm {
		rodataRef := emitArchiveAsmNode(ctx, instance, a, d, toolBinPath, toolLDRef, reg)
		rdRef, rdOut := e.emitArchiveAsmRodata(a.Name+".rodata", rodataRef)

		res.CCRefs = append(res.CCRefs, rdRef)
		res.CCOutputs = append(res.CCOutputs, rdOut)
	}

	return res
}

func emitArchiveAsmNode(
	ctx *GenCtx,
	instance ModuleInstance,
	a ArchiveAsmEntry,
	d *ModuleData,
	toolBinPath VFS,
	toolLDRef NodeRef,
	reg *CodegenRegistry,
) NodeRef {
	na := ctx.emit.nodeArenas()
	rodataVFS := build(instance.Path.rel(), "/", a.Name, ".rodata")
	cmdArgs := make([]STR, 0, 4+len(a.Files)+2)

	cmdArgs = append(cmdArgs, (toolBinPath).str(), argQ.str())

	if a.DontCompress {
		cmdArgs = append(cmdArgs, argP.str())
	}

	producerRefs := []NodeRef{}

	deduper.reset()

	pathPerFile := make([]VFS, 0, len(a.Files))

	for _, f := range a.Files {
		var memberVFS VFS

		if info := reg.lookup(copyFileOutputVFS(instance.Path.rel(), f)); info != nil {
			memberVFS = copyFileOutputVFS(instance.Path.rel(), f)

			if deduper.add(VFS(info.ProducerRef)) {
				producerRefs = append(producerRefs, info.ProducerRef)
			}
		} else {
			memberVFS = resolveSourceVFS(ctx, instance, f, d.srcDirs)
		}

		pathPerFile = append(pathPerFile, memberVFS)
		cmdArgs = append(cmdArgs, internV(memberVFS.string(), ":"))
	}

	cmdArgs = append(cmdArgs, argDashO.str(), (rodataVFS).str())

	inputs := make([]VFS, 0, len(pathPerFile))

	deduper.reset()

	for _, p := range pathPerFile {
		if deduper.add(p) {
			inputs = append(inputs, p)
		}
	}

	deps := concat(producerRefs, depRefs(toolLDRef))
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	n := &Node{
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

	rodataRef := ctx.emit.emit(n)

	var leaves []VFS

	for _, p := range pathPerFile {
		if info := reg.lookup(p); info != nil && len(info.SourceInputs) > 0 {
			leaves = dedup(leaves, info.SourceInputs)
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

	rodataPath := build(instance.Path.rel(), "/", rodataRel)
	leaves := walkClosureTail(ctx.scannerFor(instance), rodataPath, d.cc.ScanCfg)
	yasmLDRef, _ := ctx.tool(argContribToolsYasm)
	ref, _, outPath := emitRD(instance, rodataRel, rodataPath, yasmLDRef, leaves, []NodeRef{producerRef}, d.cc.TC, ctx.emit)

	return ref, outPath
}
