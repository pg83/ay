package main

import "strings"

func emitR5(
	instance ModuleInstance,
	srcRel string,
	ragel5LD NodeRef,
	rlgenCdLD NodeRef,
	ragel5BinPath VFS,
	rlgenCdBinPath VFS,
	emit *StreamingEmitter,
) (NodeRef, VFS, VFS) {
	na := emit.nodeArenas()

	srcVFS := source(instance.Path.rel(), "/", srcRel)
	tmpVFS := build(instance.Path.rel(), "/", srcRel, ".tmp")
	cppVFS := build(instance.Path.rel(), "/", strings.TrimSuffix(srcRel, ".rl"), ".rl5.cpp")

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	cmd0 := Cmd{
		CmdArgs: na.chunkList(na.strList((ragel5BinPath).str(),
			argDashO.str(),
			(tmpVFS).str(),
			(srcVFS).str())),
		Env: env,
	}
	rlgenMode := argT0

	if instance.Platform.RagelOptimized {
		rlgenMode = argG2
	}

	cmd1 := Cmd{
		CmdArgs: na.chunkList(na.strList((rlgenCdBinPath).str(),
			rlgenMode.str(),
			argDashO.str(),
			(cppVFS).str(),
			(tmpVFS).str())),
		Env: env,
	}
	inputs := []VFS{ragel5BinPath, rlgenCdBinPath, srcVFS}

	node := &Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(cmd0, cmd1),
		Env:            env,
		Inputs:         na.inputList(inputs),
		Outputs:        na.vfsList(tmpVFS, cppVFS),
		KV:             &r5KV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(ragel5LD, rlgenCdLD),
	}

	return emit.emit(node), tmpVFS, cppVFS
}

func emitLibraryRagel5Source(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	srcRel := src.string()

	ragel5LDRef, ragel5BinVFS := ctx.tool(argContribToolsRagel5Ragel)
	rlgenCdLDRef, rlgenCdBinVFS := ctx.tool(argContribToolsRagel5RlgenCd)

	r5Ref, r5TmpOut, r5CppOut := emitR5(instance, srcRel, ragel5LDRef, rlgenCdLDRef, ragel5BinVFS, rlgenCdBinVFS, ctx.emit)

	rlSourceVFS := source(instance.Path.rel(), "/", srcRel)
	reg := ctx.codegenFor(instance)
	reg.register(&GeneratedFileInfo{
		ProducerKvP:    pkR5,
		OutputPath:     r5TmpOut,
		ProducerRef:    r5Ref,
		GeneratorRefs:  []NodeRef{ragel5LDRef, rlgenCdLDRef},
		ParsedIncludes: nil,
	})
	r5Parsed := ctx.scannerFor(instance).parsers.sourceParsedBuckets(rlSourceVFS, nil).bucket(parsedIncludesCpp)

	reg.register(&GeneratedFileInfo{
		ProducerKvP:    pkR5,
		OutputPath:     r5CppOut,
		ProducerRef:    r5Ref,
		GeneratorRefs:  []NodeRef{ragel5LDRef, rlgenCdLDRef},
		ParsedIncludes: r5Parsed,
	})

	ccIn := in
	ccClosure := walkClosure(ctx.scannerFor(instance), r5CppOut, in.ScanCfg)
	ccIn.IncludeInputs = append([]VFS{r5TmpOut}, ccClosure...)
	ccIn.PerSourceCFlags = concat(in.PerSourceCFlags, []ARG{argWnoImplicitFallthrough})
	ccIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, ccIn.IncludeInputs, r5Ref)
	ccRef, ccOut, _ := emitCC(instance, r5CppOut.str(), r5CppOut, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ccRef, OutPath: ccOut}
}

var (
	r5KV = KV{P: pkR5, PC: pcYellow}
)
