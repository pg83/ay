package main

import (
	"path/filepath"
)

func emitBaseCodegensForAR(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) {
	for _, bc := range d.baseCodegens {
		emitBaseCodegen(ctx, instance, bc, in)
	}
}

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

	bcRef := ctx.emit.reserve()

	cppParsed := []IncludeDirective{{kind: includeQuoted, target: internStr(prefixH.rel())}}

	var headerParsed []IncludeDirective

	for _, oi := range bc.OutputIncludes {
		headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: oi})
	}

	registerBoundGeneratedParsedOutput(ctx, instance, pkBC, prefixH, headerParsed, bcRef, []NodeRef{toolLDRef})
	registerBoundGeneratedParsedOutput(ctx, instance, pkBC, prefixCpp, cppParsed, bcRef, []NodeRef{toolLDRef})

	reg := ctx.codegenFor(instance)
	reg.addClosureLeaf(prefixH, prefixCpp)
	reg.addClosureLeaf(prefixH, inputIn)

	node := &Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(toolBin, inputIn)),
		Outputs:        []VFS{prefixCpp, prefixH},
		KV:             &baseCodegenKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(toolLDRef),
	}

	ctx.emit.emitReserved(node, bcRef)
}

var (
	baseCodegenKV = KV{P: pkBC, PC: pcYellow}
)
