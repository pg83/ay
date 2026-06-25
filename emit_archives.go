package main

import "strings"

func emitArchives(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	if len(d.archives) == 0 {
		return
	}

	toolLDRef, toolBinPath := ctx.tool(argToolsArchiver)

	reg := ctx.codegenFor(instance)

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
	emit *StreamingEmitter,
	reg *CodegenRegistry,
) {
	na := emit.nodeArenas()

	archiveVFS := build(instance.Path.rel(), "/", a.Name)
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
			absVFS = build(instance.Path.rel(), "/", f)
		} else {
			absVFS = resolveSourceVFS(ctx, instance, f, d.srcDirs)
		}

		absStr := absVFS.string()

		pathPerFile = append(pathPerFile, absVFS)

		if a.Keys != nil {
			cmdArgs = append(cmdArgs, internStr(absStr))
		} else {
			cmdArgs = append(cmdArgs, internV(absStr, ":"))
		}
	}

	if a.Keys != nil {
		cmdArgs = append(cmdArgs, argDashK.str(), internStr(strings.Join(a.Keys, ":")))
	}

	cmdArgs = append(cmdArgs, argDashO.str(), internStr(archivePath))

	inputs := make([]VFS, 0, len(pathPerFile))
	deduper.reset()

	for _, p := range pathPerFile {
		if !deduper.add(p) {
			continue
		}

		inputs = append(inputs, p)
	}

	deps := concat(producerRefs, depRefs(toolLDRef))

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	n := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:          env,
		Inputs:       na.inputList(inputs, na.srcChunk(toolBinPath)),
		KV:           &archivesKV,
		Outputs:      na.vfsList(archiveVFS),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      deps,
		Resources:    instance.Platform.UsesPython3Clang,
	}

	arRef := emit.emit(n)

	{
		var leaves []VFS

		for _, p := range pathPerFile {
			if info := reg.lookup(p); info != nil && len(info.SourceInputs) > 0 {
				leaves = append(leaves, info.SourceInputs...)
			} else if a.PropagateSourceMembers && info == nil {
				leaves = append(leaves, p)
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

var (
	archivesKV = KV{P: pkAR, PC: pcLightRed}
)
