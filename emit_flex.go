package main

import "strings"

const flexDefaultGenExt = ".cpp"

var flexOutputInclude = IncludeDirective{kind: includeQuoted, target: internStr("util/system/compiler.h")}

func flexGeneratedVFS(instance ModuleInstance, srcRel string) VFS {
	if strings.Contains(srcRel, "/") {
		return build(instance.Path.rel() + "/_/" + srcRel + flexDefaultGenExt)
	}

	return build(instance.Path.rel() + "/" + srcRel + flexDefaultGenExt)
}

func emitLibraryFlexSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	flexRef, flexBin := ctx.tool(argContribToolsFlexOld)

	srcVFS := resolveModuleSourceVFS(ctx, instance, d, srcRel, in.SrcDirs)
	outVFS := flexGeneratedVFS(instance, srcRel)

	parsed := make([]IncludeDirective, 0, 1)
	parsed = append(parsed, flexOutputInclude)
	parsed = append(parsed, ctx.scannerFor(instance).parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesLocal)...)

	lxRef := ctx.emit.reserve()
	registerBoundGeneratedParsedOutput(ctx, instance, pkLX, outVFS, parsed, lxRef, []NodeRef{flexRef})

	window := walkClosure(ctx.scannerFor(instance), outVFS, in.ScanCfg)

	lxClosure := keepOnlySourceVFS(window)

	emitFlexLX(instance, flexRef, flexBin, srcVFS, outVFS, lxClosure, lxRef, ctx.emit)

	ccSrcRel := strings.TrimPrefix(outVFS.rel(), instance.Path.rel()+"/")

	ccIn := in
	ccIn.IncludeInputs = window
	ccIn.ExtraDepRefs = append([]NodeRef{lxRef}, resolveCodegenDepRefs(ctx, instance, window, lxRef)...)

	if strings.HasSuffix(srcRel, ".l") {
		ccIn.IncludeInputs = dedupVFS(window, []VFS{srcVFS})
		ccIn.PerSourceCFlags = append(append([]ARG(nil), in.PerSourceCFlags...), argWnoUnusedVariable)
	}

	ccRef, ccOut, _ := emitCC(instance, ccSrcRel, outVFS, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ccRef, OutPath: ccOut}
}

func emitFlexLX(instance ModuleInstance, flexRef NodeRef, flexBin VFS, srcVFS, outVFS VFS, closure []VFS, id NodeRef, emit Emitter) {
	na := emit.nodeArenas()

	cmdArgs := na.chunkList(na.strList(
		flexBin.str(),
		internStr(argDashO.string()+outVFS.string()),
		srcVFS.str(),
	))

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	emit.emitReserved(&Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(flexBin, srcVFS), closure),
		Outputs:        na.vfsList(outVFS),
		KV:             KV{P: pkLX, PC: pcYellow},
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: []NodeRef{flexRef},
	}, id)
}
