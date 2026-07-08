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
		e.enqueueSrc(SrcMeta{Source: copyFileOutputVFS(instance.Path.relString(), partRel).any(), Prio: stmtPrioDefault, Generated: true, Bucket: bkSplitCodegen})
	}
}

func (e *EmitContext) emitSplitCodegen(sc *SplitCodegenStmt) (NodeRef, []string) {
	ctx, instance := e.ctx, e.instance
	na := ctx.emit.nodeArenas()
	moduleDir := instance.Path.relString()
	prefix := sc.Prefix.string()
	toolRes := ctx.toolResult(internArg(filepath.Clean(sc.ToolPath.string())))
	toolLDRef := toolRes.LDRef
	toolBin := *toolRes.LDPath
	inputIn := source(moduleDir, "/", prefix, ".in")
	prefixCpp := build(moduleDir, "/", prefix, ".cpp")
	prefixH := build(moduleDir, "/", prefix, ".h")
	partRels := make([]string, 0, sc.OutNum)
	outputs := na.vfs.alloc(sc.OutNum + 2)[:0]

	for i := 0; i < sc.OutNum; i++ {
		partRel := prefix + "." + strconv.Itoa(i) + ".cpp"

		partRels = append(partRels, partRel)
		outputs = append(outputs, build(moduleDir, "/", partRel))
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
	part0 := build(moduleDir, "/", partRels[0])
	part0Inc := IncludeDirective{kind: includeQuoted, target: includeTarget(part0.rel().any())}
	headerParsed := ctx.na.dirs.alloc(len(sc.OutputIncludes))[:0]

	for _, oi := range sc.OutputIncludes {
		headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(oi)})
	}

	ctx.na.dirs.commit(len(headerParsed))

	headerParsed = headerParsed[:len(headerParsed):len(headerParsed)]

	cppParsed := ctx.na.dirList(part0Inc)
	reg := e.codegen

	reg.register(GeneratedFileInfo{
		OutputPath:     prefixH,
		ProducerRef:    scRef,
		GeneratorRefs:  e.ctx.na.refList(toolLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: headerParsed},
		ClosureLeaves:  e.ctx.na.vfsList(part0, inputIn),
	})

	reg.register(GeneratedFileInfo{
		OutputPath:     prefixCpp,
		ProducerRef:    scRef,
		GeneratorRefs:  e.ctx.na.refList(toolLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: cppParsed},
	})

	for i, partRel := range partRels {
		info := GeneratedFileInfo{
			OutputPath:     build(moduleDir, "/", partRel),
			ProducerRef:    scRef,
			GeneratorRefs:  e.ctx.na.refList(toolLDRef),
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: cppParsed},
		}

		if i == 0 {
			info.ClosureLeaves = ctx.na.vfsList(inputIn)
		}

		reg.register(info)
	}

	node := Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(toolBin, inputIn)),
		Outputs:        outputs,
		KV:             &splitCodegenKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: na.refList(toolLDRef),
	}

	ctx.emit.emitReservedNode(node, scRef)

	return scRef, partRels
}
