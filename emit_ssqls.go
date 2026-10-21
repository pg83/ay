package main

import "strings"

var ssqlsKV = KV{P: pkSS, PC: pcYellow}

func (e *EmitContext) emitSsqlsSource(meta SrcMeta) {
	na := e.ctx.na
	src := e.moduleSourceVFS(meta.Source)
	base := strings.TrimSuffix(e.moduleSourceRel(meta.Source), ".ssqls")
	cpp := build(e.instance.Path.relString(), "/", base, ".cpp")
	header := build(e.instance.Path.relString(), "/", base, ".h")
	toolRef, tool := e.ctx.tool(argMetrikaCoreToolsSsqls)
	ref := e.ctx.emit.reserve()

	pending := e.pendingEmit(func() {
		e.emitReservedNode(Node{Platform: e.instance.Platform, Env: envVarsVCS,
			Cmds:   na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(tool.any(), src.any(), strS3.any(), srcRootDirVFS.any(), strB3.any(), bldRootDirVFS.any()), meta.Compile.CFlags), Env: envVarsVCS}),
			Inputs: na.inputList(na.vfsList(tool, src)), Outputs: na.vfsList(cpp, header),
			ForeignDepRefs: na.refList(toolRef), KV: &ssqlsKV}, ref)
	})

	for _, output := range []VFS{cpp, header} {
		e.register(GeneratedFileInfo{OutputPath: output, PrimaryInput: src, ProducerRef: ref, ProducerMainOut: cpp, GeneratorRefs: na.refList(toolRef), OnUse: pending})
	}

	meta.Source = cpp.any()
	e.enqueueSrc(meta)
}
