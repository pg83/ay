package main

import "strings"

func emitArchives(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	if len(d.archives) == 0 {
		return
	}

	toolLDRef, toolBinPath := ctx.tool(argToolsArchiver)

	reg := codegenRegForInstance(ctx, instance)

	for _, a := range d.archives {
		emitArchive(ctx, instance, a, d, toolBinPath, toolLDRef, ctx.emit, reg)
	}
}

func emitArchive(
	ctx *GenCtx,
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

		if info := reg.lookup(copyFileOutputVFS(instance.Path.rel(), f)); info != nil {
			isPRProduced = true

			if deduper.add(VFS(info.ProducerRef)) {
				producerRefs = append(producerRefs, info.ProducerRef)
			}
		}

		var absVFS VFS

		if isPRProduced {
			absVFS = build(instance.Path.rel() + "/" + f)
		} else {
			// Resolve via the module's SRCDIR search, not a blind module-dir
			// prefix, so a SRCDIR-backed member reads $(S)/<srcdir>/<file>.
			absVFS = resolveSourceVFS(ctx, instance, f, d.srcDirs)
		}

		absStr := absVFS.string()

		pathPerFile = append(pathPerFile, absVFS)

		// ARCHIVE_BY_KEYS passes keys via `-k`; plain ARCHIVE suffixes each
		// member with an empty-key `:`.
		if a.Keys != nil {
			cmdArgs = append(cmdArgs, internStr(absStr))
		} else {
			cmdArgs = append(cmdArgs, internStr(absStr+":"))
		}
	}

	if a.Keys != nil {
		cmdArgs = append(cmdArgs, argDashK.str(), internStr(strings.Join(a.Keys, ":")))
	}

	cmdArgs = append(cmdArgs, argDashO.str(), internStr(archivePath))

	// Inputs are exactly the archived members plus the archiver tool. Build-order
	// concerns ride producerRefs / toolLDRef DepRefs, not action inputs.
	inputs := make([]VFS, 0, len(pathPerFile))
	deduper.reset()

	for _, p := range pathPerFile {
		if !deduper.add(p) {
			continue
		}

		inputs = append(inputs, p)
	}

	deps := append(append([]NodeRef(nil), producerRefs...), depRefs(toolLDRef)...)

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
		DepRefs:          deps,
		Resources:        instance.Platform.UsesPython3Clang,
	}

	arRef := emit.emit(n)

	{
		// Propagate each member's source inputs as closure leaves of the archive
		// output, so a CC unit that #includes the archived .inc picks them up
		// transitively.
		var leaves []VFS

		for _, p := range pathPerFile {
			if info := reg.lookup(p); info != nil && len(info.SourceInputs) > 0 {
				leaves = dedupVFS(leaves, info.SourceInputs)
			} else if a.PropagateSourceMembers && info == nil {
				// A direct source member: ride the source into the consumer's closure.
				leaves = dedupVFS(leaves, []VFS{p})
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
