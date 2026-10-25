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
	archiveVFS := copyFileOutputVFS(instance.Path.relString(), a.Name)
	producerRefs := []NodeRef{}
	pathMark := len(e.prodVFS)

	for _, f := range a.Files {
		absVFS := moduleInputCandidateVFS(instance.Path.relString(), f)

		if info := reg.use(absVFS); info != nil {
			producerRefs = append(producerRefs, info.ProducerRef)
		} else {
			absVFS = e.requireProducedInput("ARCHIVE member", f, resolveSourceVFS(ctx, instance, f, d.srcDirs))
		}

		e.prodVFS = append(e.prodVFS, absVFS)
	}

	pathPerFile := e.prodVFSTake(pathMark)
	cmdArgs := na.anys.alloc(8 + len(a.Files))[:0]

	cmdArgs = append(cmdArgs, toolBinPath.any(), argQ.any(), argX.any())

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

	var inputs []VFS

	dedupers.with(func(deduper *DeDuper) {
		producerRefs = dedupInPlaceWith(deduper, producerRefs)
		deduper.reset()
		inputs = na.vfs.alloc(len(pathPerFile))[:0]

		for _, p := range pathPerFile {
			if !deduper.add(p.strID()) {
				continue
			}

			inputs = append(inputs, p)
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
		Env:    env,
		Inputs: NodeInputs{files: na.inputList(inputs, na.srcChunk(toolBinPath)), roots: na.inputList(inputs)},

		KV:        &archivesKV,
		Outputs:   na.vfsList(archiveVFS),
		DepRefs:   deps,
		Resources: instance.Platform.UsesPython3Clang,
	}

	arRef := e.emitNode(n)

	e.register(GeneratedFileInfo{
		OutputPath:  archiveVFS,
		ProducerRef: arRef,
	})
}
