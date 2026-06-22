package main

// emitArchiveAsmForAR models ARCHIVE_ASM(NAME <n> [DONTCOMPRESS] Files...).
// Each entry emits an AR `<NAME>.rodata` resource, re-fed as a generated source
// that the yasm pipeline compiles to a non-global compile result. Members' $(S)
// source leaves ride into the .rodata's closure window so the downstream RD
// compile carries them as inputs.
func emitArchiveAsmForAR(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) *RunProgramsForARResult {
	if len(d.archiveAsm) == 0 {
		return nil
	}

	toolLDRef, toolBinPath := ctx.tool(argToolsArchiver)
	reg := codegenRegForInstance(ctx, instance)
	res := &RunProgramsForARResult{}

	for _, a := range d.archiveAsm {
		rodataRef := emitArchiveAsmNode(ctx, instance, a, d, toolBinPath, toolLDRef, reg)

		rdRef, rdOut := emitArchiveAsmRodata(ctx, instance, a.Name+".rodata", rodataRef, in)
		res.CCRefs = append(res.CCRefs, rdRef)
		res.CCOutputs = append(res.CCOutputs, rdOut)
	}

	return res
}

// emitArchiveAsmNode emits the archiver `<NAME>.rodata` resource node.
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

	rodataVFS := build(instance.Path.rel() + "/" + a.Name + ".rodata")

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
		// Each member carries the empty-key `:` suffix, like ARCHIVE.
		cmdArgs = append(cmdArgs, internStr(memberVFS.string()+":"))
	}

	cmdArgs = append(cmdArgs, argDashO.str(), (rodataVFS).str())

	inputs := make([]VFS, 0, len(pathPerFile))
	deduper.reset()

	for _, p := range pathPerFile {
		if deduper.add(p) {
			inputs = append(inputs, p)
		}
	}

	deps := append(append([]NodeRef(nil), producerRefs...), depRefs(toolLDRef)...)
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	n := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:              env,
		Inputs:           na.inputList(inputs, na.srcChunk(toolBinPath)),
		KV:               KV{P: pkAR, PC: pcLightCyan},
		Outputs:          na.vfsList(rodataVFS),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		DepRefs:          deps,
		Resources:        instance.Platform.UsesPython3Clang,
	}

	rodataRef := ctx.emit.emit(n)

	// Propagate each member's $(S) source leaves as the .rodata's closure
	// leaves so the downstream RD compile picks them up.
	var leaves []VFS

	for _, p := range pathPerFile {
		if info := reg.lookup(p); info != nil && len(info.SourceInputs) > 0 {
			leaves = dedupVFS(leaves, info.SourceInputs)
		}
	}

	reg.register(&GeneratedFileInfo{
		ProducerKvP:   pkAR,
		OutputPath:    rodataVFS,
		ProducerRef:   rodataRef,
		ClosureLeaves: leaves,
	})

	return rodataRef
}

// emitArchiveAsmRodata compiles a generated `<NAME>.rodata` through the yasm
// pipeline, threading the producing AR node as the build dependency.
func emitArchiveAsmRodata(ctx *GenCtx, instance ModuleInstance, rodataRel string, producerRef NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	if instance.Platform.ISA != ISAX8664 {
		throwFmt("gen: unsupported .rodata platform %s for ARCHIVE_ASM %q", instance.Platform.ISA, rodataRel)
	}

	rodataPath := build(instance.Path.rel() + "/" + rodataRel)
	leaves := walkClosureTail(ctx.scannerFor(instance), rodataPath, in.ScanCfg)

	yasmLDRef, _ := ctx.tool(argContribToolsYasm)
	ref, _, outPath := emitRD(instance, rodataRel, rodataPath, yasmLDRef, leaves, []NodeRef{producerRef}, in.TC, ctx.emit)

	return ref, outPath
}
