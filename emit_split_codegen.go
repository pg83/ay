package main

import (
	"path/filepath"
	"strconv"
)

func emitSplitCodegensForAR(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) *RunProgramsForARResult {
	if len(d.splitCodegens) == 0 {
		return nil
	}

	res := &RunProgramsForARResult{}

	for _, sc := range d.splitCodegens {
		scRef, parts := emitSplitCodegen(ctx, instance, sc, in)

		for _, partRel := range parts {
			ccRef, ccOut := emitCodegenDownstreamCC(ctx, instance, partRel, []NodeRef{scRef}, in)

			res.CCRefs = append(res.CCRefs, ccRef)
			res.CCOutputs = append(res.CCOutputs, ccOut)
		}
	}

	return res
}

func emitSplitCodegen(ctx *GenCtx, instance ModuleInstance, sc *SplitCodegenStmt, in ModuleCCInputs) (NodeRef, []string) {
	na := ctx.emit.nodeArenas()
	moduleDir := instance.Path.rel()
	prefix := sc.Prefix.string()
	toolRes := ctx.toolResult(internArg(filepath.Clean(sc.ToolPath.string())))
	toolLDRef := toolRes.LDRef
	toolBin := *toolRes.LDPath
	inputIn := source(moduleDir, "/", prefix, ".in")
	prefixCpp := build(moduleDir, "/", prefix, ".cpp")
	prefixH := build(moduleDir, "/", prefix, ".h")
	partRels := make([]string, 0, sc.OutNum)
	outputs := make([]VFS, 0, sc.OutNum+2)

	for i := 0; i < sc.OutNum; i++ {
		partRel := prefix + "." + strconv.Itoa(i) + ".cpp"

		partRels = append(partRels, partRel)
		outputs = append(outputs, build(moduleDir, "/", partRel))
	}

	outputs = append(outputs, prefixCpp, prefixH)

	cppParts := sc.OutNum - splitCodegenStreamCount
	cmdArgs := make([]STR, 0, 6+len(sc.Opts))

	cmdArgs = append(cmdArgs,
		toolBin.str(),
		inputIn.str(),
		prefixCpp.str(),
		prefixH.str(),
		strCppParts,
		internStr(strconv.Itoa(cppParts)),
	)
	cmdArgs = append(cmdArgs, sc.Opts...)

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	scRef := ctx.emit.reserve()
	part0 := build(moduleDir, "/", partRels[0])
	part0Inc := IncludeDirective{kind: includeQuoted, target: internStr(part0.rel())}
	headerParsed := make([]IncludeDirective, 0, len(sc.OutputIncludes))

	for _, oi := range sc.OutputIncludes {
		headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: oi})
	}

	cppParsed := []IncludeDirective{part0Inc}
	reg := ctx.codegenFor(instance)

	reg.register(&GeneratedFileInfo{
		ProducerKvP:    pkSC,
		OutputPath:     prefixH,
		ProducerRef:    scRef,
		GeneratorRefs:  []NodeRef{toolLDRef},
		ParsedIncludes: headerParsed,
		ClosureLeaves:  []VFS{part0, inputIn},
	})

	reg.register(&GeneratedFileInfo{
		ProducerKvP:    pkSC,
		OutputPath:     prefixCpp,
		ProducerRef:    scRef,
		GeneratorRefs:  []NodeRef{toolLDRef},
		ParsedIncludes: cppParsed,
	})

	for i, partRel := range partRels {
		info := &GeneratedFileInfo{
			ProducerKvP:    pkSC,
			OutputPath:     build(moduleDir, "/", partRel),
			ProducerRef:    scRef,
			GeneratorRefs:  []NodeRef{toolLDRef},
			ParsedIncludes: cppParsed,
		}

		if i == 0 {
			info.ClosureLeaves = []VFS{inputIn}
		}

		reg.register(info)
	}

	node := &Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(toolBin, inputIn)),
		Outputs:        outputs,
		KV:             &splitCodegenKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(toolLDRef),
	}

	ctx.emit.emitReserved(node, scRef)

	return scRef, partRels
}

var (
	splitCodegenKV = KV{P: pkSC, PC: pcYellow}
)
