package main

import "strings"

var (
	flexOutputInclude = IncludeDirective{kind: includeQuoted, target: internStr("util/system/compiler.h")}
	flexKV            = KV{P: pkLX, PC: pcYellow}
)

const flexDefaultGenExt = ".cpp"

func flexGeneratedVFS(instance ModuleInstance, srcRel string) VFS {
	if strings.Contains(srcRel, "/") {
		return build(instance.Path.rel() + "/_/" + srcRel + flexDefaultGenExt)
	}

	return build(instance.Path.rel() + "/" + srcRel + flexDefaultGenExt)
}

func emitLibraryFlexSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	srcRel := src.string()

	flexRef, flexBin := ctx.tool(argContribToolsFlexOld)

	srcVFS := resolveModuleSourceVFS(ctx, instance, d, src, in.SrcDirs)
	outVFS := flexGeneratedVFS(instance, srcRel)

	parsed := make([]IncludeDirective, 0, 1)
	parsed = append(parsed, flexOutputInclude)
	parsed = append(parsed, ctx.scannerFor(instance).parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesLocal)...)

	lxRef := ctx.emit.reserve()
	ctx.codegenFor(instance).register(&GeneratedFileInfo{
		ProducerKvP:    pkLX,
		OutputPath:     outVFS,
		ProducerRef:    lxRef,
		GeneratorRefs:  []NodeRef{flexRef},
		ParsedIncludes: parsed,
	})

	window := walkClosure(ctx.scannerFor(instance), outVFS, in.ScanCfg)

	lxClosure := keepOnlySourceVFS(window)

	emitFlexLX(instance, flexRef, flexBin, srcVFS, outVFS, lxClosure, lxRef, ctx.emit)

	ccIn := in
	ccIn.IncludeInputs = window
	ccIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, window, lxRef)

	if strings.HasSuffix(srcRel, ".l") {
		ccIn.IncludeInputs = concat(window, []VFS{srcVFS})
		ccIn.PerSourceCFlags = concat(in.PerSourceCFlags, []ARG{argWnoUnusedVariable})
	}

	ccRef, ccOut, _ := emitCC(instance, outVFS.str(), outVFS, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ccRef, OutPath: ccOut}
}

func emitFlexLX(instance ModuleInstance, flexRef NodeRef, flexBin VFS, srcVFS, outVFS VFS, closure []VFS, id NodeRef, emit *StreamingEmitter) {
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
		KV:             &flexKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: []NodeRef{flexRef},
	}, id)
}
