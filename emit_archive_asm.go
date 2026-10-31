package main

var archiveAsmKV = KV{P: pkAR, PC: pcLightCyan}

func (e *EmitContext) emitArchiveAsmForAR(a ArchiveAsmEntry) {
	d := e.d
	toolLDRef, toolBinPath := e.tool(argToolsArchiver)
	reg := e.codegen
	rodataRel := a.Name + d.cc.objSufDot() + ".rodata"
	rodataPath := build(e.instance.Path.relString(), "/", rodataRel)

	e.emitArchiveAsmNode(a, rodataPath, toolBinPath, toolLDRef, reg)
	e.enqueueSrc(SrcMeta{Source: rodataPath.any(), Prio: stmtPrioDefault, Seq: a.DeclSeq})
}

func (e *EmitContext) emitArchiveAsmNode(
	a ArchiveAsmEntry,
	rodataVFS VFS,
	toolBinPath VFS,
	toolLDRef NodeRef,
	reg *CodegenRegistry,
) NodeRef {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.emit.nodeArenas()

	if existing := reg.lookup(rodataVFS); existing != nil && existing.OwnerModule == instance.Path && existing.OwnerSerial != e.frame.serial {
		return existing.ProducerRef
	}

	producerRefs := []NodeRef{}
	pathMark := len(e.prodVFS)

	for _, f := range a.Files {
		memberVFS := moduleInputCandidateVFS(instance.Path.relString(), f)

		if info := reg.use(memberVFS); info != nil {
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

	n := NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env: env, Inputs: na.rootInputList(inputs, na.srcChunk(toolBinPath)), KV: &archiveAsmKV,
		Outputs:   na.vfsList(rodataVFS),
		DepRefs:   deps,
		Resources: instance.Platform.UsesPython3Clang}, input: InputSpec{roots: na.inputList(inputs)},
	}

	rodataRef := e.emitNode(n)

	e.register(GeneratedFileSpec{
		OutputPath:  rodataVFS,
		ProducerRef: rodataRef,
	})

	return rodataRef
}
