package main

import "strings"

var ssqlsKV = KV{P: pkSS, PC: pcYellow}

func (e *EmitContext) emitSsqlsSource(meta SrcMeta) {
	na := e.ctx.na
	src := e.moduleSourceVFS(meta.Source)

	var parsed ParsedIncludeSet
	var header VFS

	if src.isSource() {
		parsed = e.ctx.parsers.sourceParsedBuckets(src, nil)

		if len(parsed[parsedIncludesCpp]) == 0 {
			throwFmt("SSQLS requires an existing source file: %s", src.string())
		}

		header = parsed[parsedIncludesCpp][0].target.vfs()
	} else {
		header = build(strings.TrimSuffix(src.relString(), ".ssqls"), ".h")
	}

	cpp := build(strings.TrimSuffix(header.relString(), ".h"), ".cpp")
	toolRef, tool := e.ctx.tool(argMetrikaCoreToolsSsqls)
	ref := e.ctx.emit.reserve()

	pending := e.pendingEmit(func() {
		closure := e.scanner.walkClosure(src, e.d.scanCtx, scanDomainCC)

		e.emitReservedNode(Node{Platform: e.instance.Platform, Env: envVarsVCS,
			Cmds:   na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(tool.any(), src.any(), strS3.any(), srcRootDirVFS.any(), strB3.any(), bldRootDirVFS.any()), meta.Compile.CFlags), Env: envVarsVCS}),
			Inputs: NodeInputs{files: na.inputList(na.vfsList(tool, src), closure.bucketList()...), roots: na.inputList(na.vfsList(src), closure.bucketList()...)}, Outputs: na.vfsList(cpp, header),
			ForeignDepRefs: na.refList(toolRef), KV: &ssqlsKV}, ref)
	})

	for _, output := range []VFS{cpp, header} {
		bucket := parsedIncludesHeader

		if output == cpp {
			bucket = parsedIncludesCpp
		}

		e.register(GeneratedFileInfo{OutputPath: output, PrimaryInput: src, ProducerRef: ref, ProducerMainOut: cpp, GeneratorRefs: na.refList(toolRef), ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed[bucket]}, OnUse: pending})
	}

	meta.Source = cpp.any()
	e.enqueueSrc(meta)
}
