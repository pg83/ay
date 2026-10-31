package main

import "strings"

var archivesKV = KV{P: pkAR, PC: pcLightRed}

func (e *EmitContext) emitArchiveStmt(a ArchiveEntry) {
	ctx := e.ctx
	tool := e.foreignToolResult(argToolsArchiver)

	if tool.LDPath == nil {
		throwFmt("gen: tool %s has no linkable output", argToolsArchiver.string())
	}

	e.emitArchive(a, *tool.LDPath, tool.LDRef, ctx.emit, e.codegen)
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
	pathPerFile := na.vfs.alloc(len(a.Files))[:len(a.Files):len(a.Files)]

	na.vfs.commit(len(pathPerFile))

	for i, f := range a.Files {
		absVFS := moduleInputCandidateVFS(instance.Path.relString(), f)

		if info := reg.lookup(absVFS); info != nil {
			producerRefs = append(producerRefs, info.ProducerRef)
		} else {
			absVFS = e.requireProducedInput("ARCHIVE member", f, resolveSourceVFS(ctx, instance, f, d.srcDirs))
		}

		if absVFS.isSource() && !ctx.fs.isFile(srcRootRel, absVFS.relString()) {
			throwFmt("ARCHIVE: cannot find source file: %s", f)
		}

		pathPerFile[i] = absVFS
	}

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
		cmdArgs = append(cmdArgs, argDashK.any())

		if len(a.Keys) > 0 {
			cmdArgs = append(cmdArgs, internStr(strings.Join(a.Keys, ":")).any())
		}
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

	deps := na.noderefs.alloc(len(producerRefs))[:0]

	deps = append(deps, producerRefs...)

	na.noderefs.commit(len(deps))

	deps = deps[:len(deps):len(deps)]

	env := envVarsVCS

	n := NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env: env, Inputs: na.rootInputList(inputs, na.srcChunk(toolBinPath)), KV: &archivesKV,
		Outputs:        na.vfsList(archiveVFS),
		DepRefs:        deps,
		ForeignDepRefs: na.refList(toolLDRef),
		Resources:      instance.Platform.UsesPython3Clang}, input: InputSpec{roots: na.inputList(inputs)},
	}

	arRef := emit.reserve()

	pending := e.pendingEmit(func() {
		e.emitReservedNode(n, arRef)
	})

	e.register(GeneratedFileSpec{
		OutputPath:  archiveVFS,
		ProducerRef: arRef,
		OnUse:       pending,
	})
}
