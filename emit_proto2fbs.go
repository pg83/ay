package main

import "path"

var proto2FbsKV = KV{P: pkFBS, PC: pcYellow}

type Proto2FbsStmt struct {
	Source ANY
	Seq    int
}

func (e *EmitContext) emitProto2Fbs(stmt Proto2FbsStmt, output VFS) {
	ctx, d := e.ctx, e.d
	na := ctx.na
	input := e.runProgramInputVFS(stmt.Source.string())
	toolRef, tool := ctx.tool(argContribLibsFlatbuffersFlatc)
	bindir := e.instance.Path.rel().build()
	temporary := build(e.instance.Path.relString(), "/", path.Base(output.relString()))
	fsTools := copyFsToolsVFS
	ref := ctx.emit.reserve()

	node := NodeSpec{Node: Node{Platform: e.instance.Platform, Env: envVarsVCS, KV: &proto2FbsKV,
		Cmds: na.cmdList(
			Cmd{CmdArgs: na.chunkList(na.anyList(tool.any(), argI.any(), str4.any(), argI.any(), srcRootDirVFS.any(), strProto.any(), input.any())), Cwd: bindir.cwd(), Env: envVarsVCS},
			Cmd{CmdArgs: na.chunkList(na.anyList(d.tc.Python3.any(), fsTools.any(), argRename.any(), temporary.any(), output.any())), Env: envVarsVCS},
		), Inputs: na.rootInputList(na.vfsList(tool, input), ctx.scripts[fsTools.rel()]), Outputs: na.vfsList(output), ForeignDepRefs: na.refList(toolRef), Resources: usesPython3},
	}

	node.input.roots = na.inputList(node.Inputs[0][1:], node.Inputs[1:]...)

	var parsed ParsedIncludeSet

	if input.isBuild() {
		parsed = e.codegen.buildParsedFor(input)
	} else {
		parsed = ctx.parsers.sourceParsedBuckets(input, nil)
	}

	pending := e.pendingEmit(func() {
		e.codegen.use(input)
		e.emitReservedNode(node, ref)
	})

	e.register(GeneratedFileSpec{
		OutputPath: output, SourcePath: input, PrimaryInput: input, ProducerRef: ref,
		GeneratorRefs: na.refList(toolRef), OnUse: pending,
		ParsedIncludes: ParsedIncludeSet{
			parsedIncludesHeader: parsed[parsedIncludesHeader],
			parsedIncludesCpp:    parsed[parsedIncludesCpp],
		},
	})

	e.enqueueSrc(SrcMeta{Source: output.any(), Prio: stmtPrioDefault, Seq: stmt.Seq})
}
