package main

func emitArchives(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	if len(d.archives) == 0 {
		return
	}

	toolLDRef, toolBinPath := ctx.tool(argToolsArchiver)

	reg := codegenRegForInstance(ctx, instance)

	for _, a := range d.archives {
		emitArchive(instance, a, d, toolBinPath, toolLDRef, ctx.emit, reg)
	}
}

func emitArchive(
	instance ModuleInstance,
	a ArchiveEntry,
	d *ModuleData,
	toolBinPath VFS,
	toolLDRef NodeRef,
	emit Emitter,
	reg *CodegenRegistry,
) {
	na := emit.nodeArenas()

	archiveVFS := build(instance.Path.rel() + "/" + a.Name)
	archivePath := archiveVFS.string()

	cmdArgs := make([]STR, 0, 4+len(a.Files)+2)
	cmdArgs = append(cmdArgs, (toolBinPath).str(), argQ.str(), argX.str())

	if a.DontCompress {
		cmdArgs = append(cmdArgs, argP.str())
	}

	producerRefs := []NodeRef{}
	deduper.reset()
	pathPerFile := make([]VFS, 0, len(a.Files))

	for _, f := range a.Files {
		isPRProduced := false

		if d.prOutputProducer != nil {
			if ref, ok := d.prOutputProducer[internStr(f)]; ok {
				isPRProduced = true

				if deduper.add(VFS(ref)) {
					producerRefs = append(producerRefs, ref)
				}
			}
		}

		rel := instance.Path.rel() + "/" + f
		var absVFS VFS

		if isPRProduced {
			absVFS = build(rel)
		} else {
			absVFS = source(rel)
		}

		absStr := absVFS.string()

		pathPerFile = append(pathPerFile, absVFS)
		cmdArgs = append(cmdArgs, internStr(absStr+":"))
	}

	cmdArgs = append(cmdArgs, argDashO.str(), internStr(archivePath))

	// Archive-node inputs are exactly the files the archiver reads (the archived
	// members) plus the archiver tool. RUN_PROGRAM source INFiles and non-archived
	// sibling PR outputs the command never names — they are build-order concerns
	// carried by producerRefs / toolLDRef DepRefs, not action inputs — so they are
	// not listed here.
	inputs := make([]VFS, 0, len(pathPerFile))
	deduper.reset()

	for _, p := range pathPerFile {
		if !deduper.add(p) {
			continue
		}

		inputs = append(inputs, p)
	}

	depRefs := make([]NodeRef, 0, len(producerRefs)+1)
	depRefs = append(depRefs, producerRefs...)

	if toolLDRef != (NodeRef(0)) {
		depRefs = append(depRefs, toolLDRef)
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	n := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:              env,
		Inputs:           na.inputList(inputs, na.srcChunk(toolBinPath)),
		KV:               KV{P: pkAR, PC: pcLightRed},
		Outputs:          na.vfsList(archiveVFS),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		DepRefs:          depRefs,
		usesResources:    instance.Platform.UsesPython3Clang,
	}

	arRef := emit.emit(n)

	{
		// Propagate each archived member's source inputs (e.g. the .py behind a
		// .pyc compiled by a RUN_PROGRAM) as non-expanded closure leaves of the
		// archive output, so a CC unit that #includes the archived .inc picks them
		// up transitively through the cached window — replacing the former
		// per-CC-source fixup for the runtime_py3 bootstrap.
		var leaves []VFS

		for _, p := range pathPerFile {
			if info := reg.lookup(p); info != nil && len(info.SourceInputs) > 0 {
				leaves = dedupVFS(leaves, info.SourceInputs)
			}
		}

		reg.register(&GeneratedFileInfo{
			ProducerKvP:   pkAR,
			OutputPath:    archiveVFS,
			ProducerRef:   arRef,
			ClosureLeaves: leaves,
		})
	}
}
