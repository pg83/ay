package main

import (
	"path/filepath"
	"strconv"
)

// emitSplitCodegensForAR emits the module's SPLIT_CODEGEN producers (kv p=SC) and
// the CC compiles of their auto-generated numbered .cpp parts. The noauto
// prefix.cpp is NOT compiled here — the module re-feeds it via
// SRCS(${BINDIR}/prefix.cpp), which the regular (global) source path compiles,
// picking up the SC producer dep through the codegen registry. Registration runs
// here (before that source path) so the registry is populated in time.
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
// the producer ref and the relative paths of the auto-compiled numbered .cpp parts.
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

	// OUT_NUM numbered parts (auto-compiled), then prefix.cpp (noauto) and prefix.h.
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
		internStr("--cpp-parts"),
		internStr(strconv.Itoa(cppParts)),
	)
	cmdArgs = append(cmdArgs, sc.Opts...)

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	// Reserve the producer ref before registering outputs: registration records it
	// so a consumer resolving a generated output to a dep edge reads a valid ref.
	scRef := ctx.emit.reserve()

	// Upstream's flat-input model carries the first numbered part (prefix.0.cpp)
	// and the prefix.in source through the generated closure — never the
	// generated header prefix.h on a generated cpp compilation.
	part0 := build(moduleDir + "/" + partRels[0])
	part0Inc := IncludeDirective{kind: includeQuoted, target: internStr(part0.rel())}

	// prefix.h carries only OUTPUT_INCLUDES as real (traversed) includes. The
	// generated-from edges (prefix.0.cpp + prefix.in) ride as NON-EXPANDED
	// closure leaves, not parsed includes: prefix.0.cpp is a generated cpp, so
	// traversing it would pull the codegen tool's cpp INDUCED_DEPS bucket (a cpp
	// output reads both the h+cpp and cpp buckets) into header consumers, which
	// reference lists only on the compiled cpp parts, not on header includers.
	headerParsed := make([]IncludeDirective, 0, len(sc.OutputIncludes))
	for _, oi := range sc.OutputIncludes {
		headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: oi})
	}

	// The generated .cpp parts and the noauto prefix.cpp #include prefix.0.cpp
	// (not prefix.h). They are themselves cpp outputs that already carry the cpp
	// bucket via their own GeneratorRefs, so traversing prefix.0.cpp only re-adds
	// that bucket (deduped) plus the prefix.0.cpp edge and prefix.in source.
	cppParsed := []IncludeDirective{part0Inc}

	registerBoundGeneratedParsedOutput(ctx, instance, pkSC, prefixH, headerParsed, scRef, []NodeRef{toolLDRef})
	registerBoundGeneratedParsedOutput(ctx, instance, pkSC, prefixCpp, cppParsed, scRef, []NodeRef{toolLDRef})

	for _, partRel := range partRels {
		registerBoundGeneratedParsedOutput(ctx, instance, pkSC, build(moduleDir+"/"+partRel), cppParsed, scRef, []NodeRef{toolLDRef})
	}

	// prefix.0.cpp carries prefix.in as a closure leaf (the scanner ignores its
	// self-include); prefix.h carries the prefix.0.cpp edge and prefix.in so a
	// header consumer inherits the generated-from closure without expanding
	// prefix.0.cpp's window. Reference order: prefix.0.cpp, then prefix.in.
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
