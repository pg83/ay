package main

import "strings"

var rcKV = KV{P: pkRC, PC: pcYellow}

// GENERATE_YT_RECORD (build/conf/project_specific/yt.conf): run the
// record_codegen host tool on a YAML record schema, producing
// <name>.record.cpp (compiled into the module) and <name>.record.h.
func (e *EmitContext) ytRecordInput(stmt *GenerateYTRecordStmt) VFS {
	return source(e.instance.Path.relString(), "/", stmt.Yaml.string())
}

func (e *EmitContext) ytRecordOutputs(stmt *GenerateYTRecordStmt) (VFS, VFS) {
	base := e.instance.Path.relString() + "/" + strings.TrimSuffix(stmt.Yaml.string(), ".yaml")

	return build(base, ".record.cpp"), build(base, ".record.h")
}

func (e *EmitContext) emitYTRecordStmt(stmt *GenerateYTRecordStmt) {
	ctx, instance := e.ctx, e.instance
	na := ctx.emit.nodeArenas()

	yamlInput := e.ytRecordInput(stmt)
	cppOut, hOut := e.ytRecordOutputs(stmt)

	rcRef := ctx.emit.reserve()

	pe := func() {
		toolRes := ctx.toolResult(argToolsYTRecordCodegenBin)
		toolLDRef := toolRes.LDRef
		toolBin := *toolRes.LDPath

		cmdArgs := na.anys.alloc(8)[:0]
		cmdArgs = append(cmdArgs,
			toolBin.any(),
			argInput.any(), yamlInput.any(),
			argOutputRoot.any(), argB.any(),
			argOutputCpp.any(), cppOut.any(),
		)

		for _, oi := range stmt.OutputIncludes {
			cmdArgs = append(cmdArgs, argOutputInclude.any(), oi)
		}

		na.anys.commit(len(cmdArgs))

		cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

		env := envVarsVCS

		node := Node{
			Platform:       instance.Platform,
			Cmds:           na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
			Env:            env,
			Inputs:         na.inputList(na.vfsList(toolBin), na.vfsList(yamlInput)),
			Outputs:        na.vfsList(cppOut, hOut),
			KV:             &rcKV,
			ForeignDepRefs: na.refList(toolLDRef),
		}

		e.emitReservedNode(node, rcRef)
	}

	pending := ctx.na.pendingEmit(pe)

	headerParsed := []IncludeDirective{
		{kind: includeQuoted, target: includeTarget(internStr("yt/yt/client/table_client/record_codegen_h.h").any())},
		{kind: includeQuoted, target: includeTarget(internStr("yt/yt/client/table_client/row_base.h").any())},
		{kind: includeQuoted, target: includeTarget(internStr("library/cpp/yt/memory/leaky_singleton.h").any())},
	}

	for _, oi := range stmt.OutputIncludes {
		headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(oi)})
	}

	cppParsed := []IncludeDirective{
		{kind: includeQuoted, target: includeTarget(hOut.rel().any())},
		{kind: includeQuoted, target: includeTarget(internStr("yt/yt/client/table_client/record_codegen_cpp.h").any())},
	}

	e.register(GeneratedFileInfo{
		OutputPath:     hOut,
		ProducerRef:    rcRef,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: headerParsed},
		ClosureLeaves:  ctx.na.vfsList(cppOut, yamlInput),
		OnUse:          pending,
	})

	e.register(GeneratedFileInfo{
		OutputPath:     cppOut,
		ProducerRef:    rcRef,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: cppParsed},
		ClosureLeaves:  ctx.na.vfsList(yamlInput),
		OnUse:          pending,
	})

	e.enqueueSrc(SrcMeta{Source: cppOut.any(), Prio: stmtPrioDefault, Seq: stmt.DeclSeq})
}
