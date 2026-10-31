package main

import (
	"path/filepath"
	"strconv"
)

var splitCodegenKV = KV{P: pkSC, PC: pcYellow}

func (e *EmitContext) emitSplitCodegenStmt(sc *SplitCodegenStmt) {
	instance := e.instance
	_, parts := e.emitSplitCodegen(sc)

	for _, partRel := range parts {
		e.enqueueSrc(SrcMeta{Source: copyFileOutputVFS(instance.Path.relString(), partRel).any(), Prio: stmtPrioDefault, Seq: sc.DeclSeq})
	}
}

func (e *EmitContext) emitSplitCodegen(sc *SplitCodegenStmt) (NodeRef, []string) {
	ctx, instance := e.ctx, e.instance
	na := ctx.emit.nodeArenas()
	moduleDir := instance.Path.relString()
	prefix := sc.Prefix.string()
	toolRes := e.toolResult(internArg(filepath.Clean(sc.ToolPath.string())))
	toolLDRef := toolRes.LDRef
	toolBin := *toolRes.LDPath

	inputIn := e.requireProducedInput("SPLIT_CODEGEN input", prefix+".in",
		resolveSourceOrGeneratedVFS(ctx, instance, prefix+".in", e.d.srcDirs))

	prefixCpp := copyFileOutputVFS(moduleDir, filepath.Base(prefix)+".cpp")
	prefixH := copyFileOutputVFS(moduleDir, filepath.Base(prefix)+".h")
	partRels := make([]string, 0, sc.OutNum)
	outputs := na.vfs.alloc(sc.OutNum + 2)[:0]

	for i := 0; i < sc.OutNum; i++ {
		partRel := prefix + "." + strconv.Itoa(i) + ".cpp"

		partRels = append(partRels, partRel)
		outputs = append(outputs, copyFileOutputVFS(moduleDir, partRel))
	}

	outputs = append(outputs, prefixCpp, prefixH)
	na.vfs.commit(len(outputs))

	outputs = outputs[:len(outputs):len(outputs)]

	cppParts := sc.OutNum - splitCodegenStreamCount
	cmdArgs := na.anys.alloc(6 + len(sc.Opts))[:0]

	cmdArgs = append(cmdArgs,
		toolBin.any(),
		inputIn.any(),
		prefixCpp.any(),
		prefixH.any(),
		strCppParts.any(),
		internStr(strconv.Itoa(cppParts)).any(),
	)

	cmdArgs = append(cmdArgs, sc.Opts...)
	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

	env := envVarsVCS
	scRef := ctx.emit.reserve()
	part0 := outputs[0]
	headerParsed := ctx.na.dirs.alloc(len(sc.OutputIncludes))[:0]

	for _, oi := range sc.OutputIncludes {
		headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(oi)})
	}

	ctx.na.dirs.commit(len(headerParsed))

	headerParsed = headerParsed[:len(headerParsed):len(headerParsed)]

	pe := func() {
		node := NodeSpec{Node: Node{Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
			Env:  env, Inputs: na.inputList(na.vfsList(toolBin), na.vfsList(inputIn)), Outputs: outputs,
			KV:             &splitCodegenKV,
			ForeignDepRefs: na.refList(toolLDRef)}, input: InputSpec{roots: na.inputList(na.vfsList(inputIn))},
		}

		e.emitReservedNode(node, scRef)
	}

	pending := e.pendingEmit(pe)

	e.collectCPPStyleInputs(inputIn)

	for _, out := range outputs {
		info := GeneratedFileSpec{
			OutputPath:      out,
			ProducerRef:     scRef,
			ProducerMainOut: part0,
			GeneratorRefs:   e.ctx.na.refList(toolLDRef),
			OnUse:           pending,
		}

		if out == prefixH {
			info.ParsedIncludes = ParsedIncludeSet{parsedIncludesLocal: headerParsed}
		}

		e.register(info)
	}

	return scRef, partRels
}
