package main

var fromArchiveKV = KV{P: pkFA, PC: pcYellow, ShowOut: true}

type FromArchiveEntry struct {
	Source ANY
	ArchiveExtraction
}

func (e *EmitContext) emitFromArchive(stmt FromArchiveEntry) {
	na := e.ctx.na
	src := e.runProgramInputVFS(stmt.Source.string())
	script := buildScriptsFetchFromArchivePy
	inputs := []VFS{script, src, buildScriptsFetchFromPy}

	var refs []NodeRef

	if info := e.codegen.lookup(src); info != nil {
		refs = append(refs, info.ProducerRef)
	}

	args := []ANY{
		e.d.tc.Python3.any(), script.any(), strArchive.any(), src.any(),
		strFileName.any(), internStr(stmt.Source.string() + "-").any(),
		strUntarTo.any(), internStr(stmt.Prefix).any(),
	}

	for _, rename := range stmt.Renames {
		args = append(args, strRename.any(), rename)
	}

	if stmt.Executable {
		args = append(args, strExecutable.any())
	}

	args = append(args, arg2.any())

	var outputs []VFS

	for _, list := range [][]ANY{stmt.OUTFiles, stmt.OUTNoAutoFiles} {
		for _, file := range list {
			args = append(args, file)
			outputs = append(outputs, copyFileOutputVFS(e.instance.Path.relString(), file.string()))
		}
	}

	node := NodeSpec{Node: Node{Platform: e.instance.Platform, KV: &fromArchiveKV, Env: envVarsVCS,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...)), Cwd: e.instance.Path.rel().build(), Env: envVarsVCS}), Inputs: na.rootInputList(na.vfsList(inputs...)), Outputs: na.vfsList(outputs...),
		DepRefs: na.refList(refs...), Resources: usesPython3},
	}

	ref := e.ctx.emit.reserve()

	pending := e.pendingEmit(func() {
		e.codegen.use(src)
		e.emitReservedNode(node, ref)
	})

	parsed := extractionOutputIncludes(na, stmt.OutputIncludes)

	for i, out := range outputs {
		e.register(GeneratedFileSpec{
			OutputPath: out, ProducerRef: ref, OnUse: pending,
			ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: parsed},
			ProducerMainOut: outputs[0],
		})

		if i < len(stmt.OUTFiles) {
			if extIsArchiveMember(out.relString()) && !e.producersOnly() {
				e.collectObj(ref, out, SrcMeta{Prio: stmtPrioDefault})
			} else if isNativeSourceExt(out.relString()) || isCodegenProducingSrc(out.relString()) {
				e.enqueueSrc(SrcMeta{Source: out.any(), Prio: stmtPrioDefault})
			}
		}
	}
}
