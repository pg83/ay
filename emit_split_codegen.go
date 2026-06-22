package main

import (
	"path/filepath"
	"strconv"
)

// emitSplitCodegensForAR emits the module's SPLIT_CODEGEN producers (kv p=SC) and
// the CC compiles of their numbered .cpp parts. This runs before the regular
// source path so the codegen registry is populated in time.
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

// emitSplitCodegen emits one SC producer node and registers its outputs, returning
// the producer ref and the numbered .cpp part paths.
func emitSplitCodegen(ctx *GenCtx, instance ModuleInstance, sc *SplitCodegenStmt, in ModuleCCInputs) (NodeRef, []string) {
	na := ctx.emit.nodeArenas()
	moduleDir := instance.Path.rel()
	prefix := sc.Prefix.string()

	toolRes := ctx.toolResult(internArg(filepath.Clean(sc.ToolPath.string())))
	toolLDRef := toolRes.LDRef
	toolBin := *toolRes.LDPath

	inputIn := source(moduleDir + "/" + prefix + ".in")
	prefixCpp := build(moduleDir + "/" + prefix + ".cpp")
	prefixH := build(moduleDir + "/" + prefix + ".h")

	// OUT_NUM numbered parts, then prefix.cpp and prefix.h.
	partRels := make([]string, 0, sc.OutNum)
	outputs := make([]VFS, 0, sc.OutNum+2)

	for i := 0; i < sc.OutNum; i++ {
		partRel := prefix + "." + strconv.Itoa(i) + ".cpp"
		partRels = append(partRels, partRel)
		outputs = append(outputs, build(moduleDir+"/"+partRel))
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

	// Reserve the producer ref before registering outputs, so a consumer reads a
	// valid ref.
	scRef := ctx.emit.reserve()

	// prefix.0.cpp and prefix.in ride the generated closure — never prefix.h on a
	// generated cpp compilation.
	part0 := build(moduleDir + "/" + partRels[0])
	part0Inc := IncludeDirective{kind: includeQuoted, target: internStr(part0.rel())}

	// prefix.h traverses only OUTPUT_INCLUDES; the generated-from edges ride as
	// non-expanded closure leaves, so the tool's cpp bucket stays off header
	// consumers and lands only on the compiled cpp parts.
	headerParsed := make([]IncludeDirective, 0, len(sc.OutputIncludes))

	for _, oi := range sc.OutputIncludes {
		headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: oi})
	}

	// The generated .cpp parts and noauto prefix.cpp #include prefix.0.cpp, not
	// prefix.h.
	cppParsed := []IncludeDirective{part0Inc}

	registerBoundGeneratedParsedOutput(ctx, instance, pkSC, prefixH, headerParsed, scRef, []NodeRef{toolLDRef})
	registerBoundGeneratedParsedOutput(ctx, instance, pkSC, prefixCpp, cppParsed, scRef, []NodeRef{toolLDRef})

	for _, partRel := range partRels {
		registerBoundGeneratedParsedOutput(ctx, instance, pkSC, build(moduleDir+"/"+partRel), cppParsed, scRef, []NodeRef{toolLDRef})
	}

	// prefix.0.cpp carries prefix.in; prefix.h carries both so a header consumer
	// inherits the generated-from closure without expanding prefix.0.cpp's window.
	reg := codegenRegForInstance(ctx, instance)
	reg.addClosureLeaf(part0, inputIn)
	reg.addClosureLeaf(prefixH, part0)
	reg.addClosureLeaf(prefixH, inputIn)

	node := &Node{
		Platform:         instance.Platform,
		Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
		Env:              env,
		Inputs:           na.inputList(na.vfsList(toolBin, inputIn)),
		Outputs:          outputs,
		KV:               KV{P: pkSC, PC: pcYellow},
		TargetProperties: TargetProperties{ModuleDir: moduleDir},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs:   depRefs(toolLDRef),
	}

	ctx.emit.emitReserved(node, scRef)

	return scRef, partRels
}
