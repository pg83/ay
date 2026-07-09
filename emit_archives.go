package main

import "strings"

var archivesKV = KV{P: pkAR, PC: pcLightRed}

func (e *EmitContext) emitArchiveStmt(a ArchiveEntry) {
	ctx := e.ctx
	toolLDRef, toolBinPath := ctx.tool(argToolsArchiver)

	e.emitArchive(a, toolBinPath, toolLDRef, ctx.emit, e.codegen)
}

func (e *EmitContext) emitArchive(
	a ArchiveEntry,
	toolBinPath VFS,
	toolLDRef NodeRef,
	emit *StreamingEmitter,
	reg *CodegenRegistry,
) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := emit.nodeArenas()
	archiveVFS := build(instance.Path.relString(), "/", a.Name)
	producerRefs := []NodeRef{}
	deduper := dedupers.get()

	defer dedupers.put(deduper)

	pathMark := len(e.prodVFS)

	for _, f := range a.Files {
		isPRProduced := false

		if info := reg.use(copyFileOutputVFS(instance.Path.relString(), f)); info != nil {
			isPRProduced = true

			if deduper.add(info.ProducerRef.strID()) {
				producerRefs = append(producerRefs, info.ProducerRef)
			}
		}

		var absVFS VFS

		if isPRProduced {
			absVFS = copyFileOutputVFS(instance.Path.relString(), f)
		} else {
			absVFS = e.requireProducedInput("ARCHIVE member", f, resolveSourceVFS(ctx, instance, f, d.srcDirs))
		}

		e.prodVFS = append(e.prodVFS, absVFS)
	}

	pathPerFile := e.prodVFSTake(pathMark)
	cmdArgs := na.anys.alloc(8 + len(a.Files))[:0]

	cmdArgs = append(cmdArgs, (toolBinPath).any(), argQ.any(), argX.any())

	if a.DontCompress {
		cmdArgs = append(cmdArgs, argP.any())
	}

	for _, absVFS := range pathPerFile {
		if a.Keys != nil {
			cmdArgs = append(cmdArgs, absVFS.any())
		} else {
			cmdArgs = append(cmdArgs, internV(absVFS.prefix(), absVFS.relString(), ":").any())
		}
	}

	if a.Keys != nil {
		cmdArgs = append(cmdArgs, argDashK.any(), internStr(strings.Join(a.Keys, ":")).any())
	}

	cmdArgs = append(cmdArgs, argDashO.any(), internV(archiveVFS.prefix(), archiveVFS.relString()).any())
	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

	inputs := na.vfs.alloc(len(pathPerFile))[:0]

	deduper.reset()

	for _, p := range pathPerFile {
		if !deduper.add(p.strID()) {
			continue
		}

		inputs = append(inputs, p)
	}

	na.vfs.commit(len(inputs))

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
		KV:           &archivesKV,
		Outputs:      na.vfsList(archiveVFS),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      deps,
		Resources:    instance.Platform.UsesPython3Clang,
	}

	arRef := emit.emitNode(n)

	var leaves []VFS

	for _, p := range pathPerFile {
		if info := reg.use(p); info != nil && len(info.SourceInputs) > 0 {
			leaves = append(leaves, info.SourceInputs...)
		} else if a.PropagateSourceMembers && info == nil {
			leaves = append(leaves, p)
		}
	}

	e.register(GeneratedFileInfo{
		OutputPath:    archiveVFS,
		ProducerRef:   arRef,
		ClosureLeaves: leaves,
	})
}
