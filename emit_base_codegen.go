package main

import (
	"path/filepath"
)

// emitBaseCodegensForAR emits the module's BASE_CODEGEN producers (kv p=BC).
// prefix.cpp is NOT compiled here — the module re-feeds it via
// SRCS(${BINDIR}/prefix.cpp), so registration must run here first.
func emitBaseCodegensForAR(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) {
	for _, bc := range d.baseCodegens {
		emitBaseCodegen(ctx, instance, bc, in)
	}
}

// emitBaseCodegen emits one BC producer node and registers its prefix.cpp and
// prefix.h outputs in the codegen registry against the tool LD generator.
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

	// Reserve the producer ref before registering outputs so a consumer reads
	// a valid ref.
	bcRef := ctx.emit.reserve()

	// prefix.cpp #includes the generated header. prefix.h carries
	// OUTPUT_INCLUDES as parsed includes so consumers inherit them.
	cppParsed := []IncludeDirective{{kind: includeQuoted, target: internStr(prefixH.rel())}}

	var headerParsed []IncludeDirective

	for _, oi := range bc.OutputIncludes {
		headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: oi})
	}

	registerBoundGeneratedParsedOutput(ctx, instance, pkBC, prefixH, headerParsed, bcRef, []NodeRef{toolLDRef})
	registerBoundGeneratedParsedOutput(ctx, instance, pkBC, prefixCpp, cppParsed, bcRef, []NodeRef{toolLDRef})

	// The header carries its sibling .cpp and the .in source as closure leaves,
	// so every CC node that includes prefix.h inherits them.
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
