package main

import (
	"path/filepath"
)

// emitBaseCodegensForAR emits the module's BASE_CODEGEN producers (kv p=BC). Each
// producer takes a tool dependency on the codegen tool's LD node; resolving that
// tool runs the tool PROGRAM through genModule, pulling its ordinary PEERDIR
// closure into the target graph (the reachability rule verified by T-21).
//
// Like SPLIT_CODEGEN, the noauto prefix.cpp is NOT compiled here — the module
// re-feeds it via SRCS(${BINDIR}/prefix.cpp), which the regular (global) source
// path compiles, picking up the BC producer dep through the codegen registry.
// Registration runs here (before that source path) so the registry is populated
// in time. BASE_CODEGEN has no auto-compiled outputs, so nothing is returned.
func emitBaseCodegensForAR(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) {
	for _, bc := range d.baseCodegens {
		emitBaseCodegen(ctx, instance, bc, in)
	}
}

// emitBaseCodegen emits one BC producer node and registers its two outputs
// (prefix.cpp, prefix.h) in the codegen registry against the tool LD generator.
func emitBaseCodegen(ctx *GenCtx, instance ModuleInstance, bc *BaseCodegenStmt, in ModuleCCInputs) {
	na := ctx.emit.nodeArenas()
	moduleDir := instance.Path.rel()
	prefix := bc.Prefix.string()

	toolRes := ctx.toolResult(internArg(filepath.Clean(bc.ToolPath.string())))
	toolLDRef := toolRes.LDRef
	toolBin := *toolRes.LDPath

	inputIn := source(moduleDir + "/" + prefix + ".in")
	prefixCpp := build(moduleDir + "/" + prefix + ".cpp")
	prefixH := build(moduleDir + "/" + prefix + ".h")

	cmdArgs := make([]STR, 0, 4+len(bc.Opts))
	cmdArgs = append(cmdArgs,
		toolBin.str(),
		inputIn.str(),
		prefixCpp.str(),
		prefixH.str(),
	)
	cmdArgs = append(cmdArgs, bc.Opts...)

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	// Reserve the producer ref before registering outputs: registration records it
	// so a consumer resolving a generated output to a dep edge reads a valid ref.
	bcRef := ctx.emit.reserve()

	// prefix.cpp #includes the generated header. prefix.h carries OUTPUT_INCLUDES
	// as parsed includes (empty for plain BASE_CODEGEN; the seven
	// STRUCT_CODEGEN_OUTPUT_INCLUDES headers for STRUCT_CODEGEN) so every consumer
	// that includes prefix.h inherits them.
	cppParsed := []IncludeDirective{{kind: includeQuoted, target: internStr(prefixH.rel())}}

	var headerParsed []IncludeDirective

	for _, oi := range bc.OutputIncludes {
		headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: oi})
	}

	registerBoundGeneratedParsedOutput(ctx, instance, pkBC, prefixH, headerParsed, bcRef, []NodeRef{toolLDRef})
	registerBoundGeneratedParsedOutput(ctx, instance, pkBC, prefixCpp, cppParsed, bcRef, []NodeRef{toolLDRef})

	// The generated header carries its generated-from sibling .cpp and the .in
	// source as non-expanded closure leaves, so every CC node that includes
	// prefix.h inherits them (upstream's flat-input model; matches the sg7
	// kernel/factor_slices evidence). Reference order: prefix.cpp, then prefix.in.
	reg := codegenRegForInstance(ctx, instance)
	reg.addClosureLeaf(prefixH, prefixCpp)
	reg.addClosureLeaf(prefixH, inputIn)

	node := &Node{
		Platform:         instance.Platform,
		Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
		Env:              env,
		Inputs:           na.inputList(na.vfsList(toolBin, inputIn)),
		Outputs:          []VFS{prefixCpp, prefixH},
		KV:               KV{P: pkBC, PC: pcYellow},
		TargetProperties: TargetProperties{ModuleDir: moduleDir},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs:   depRefs(toolLDRef),
	}

	ctx.emit.emitReserved(node, bcRef)
}
