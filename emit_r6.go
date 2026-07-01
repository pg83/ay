package main

import "strings"

var (
	ragel6ArgOptimized = internArg(ragel6DefaultFlagOptimized)
	ragel6ArgDebug     = internArg(ragel6DefaultFlagDebug)
	ragel6ConstArgs    = []STR{argL.str(), argIS.str(), argDashO.str()}
	r6KV               = KV{P: pkR6, PC: pcYellow}
)

const (
	ragel6DefaultFlagOptimized = "-CG2"
	ragel6DefaultFlagDebug     = "-CT0"
	ragel6DefaultOutExt        = ".rl6.cpp"
)

func ragel6OutVFS(instance ModuleInstance, srcRel string) VFS {
	dir := instance.Path.rel() + "/"
	base := srcRel

	if i := strings.LastIndexByte(srcRel, '/'); i >= 0 {
		dir = dir + "_/" + srcRel[:i+1]
		base = srcRel[i+1:]
	}

	return build(dir, ragel6OutName(base))
}

func ragel6OutName(base string) string {
	stem := base

	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		stem = base[:i]
	}

	if strings.ContainsRune(stem, '.') {
		return stem
	}

	return stem + ragel6DefaultOutExt
}

func emitR6(instance ModuleInstance, srcRel string, ragel6LD NodeRef, ragel6BinaryPath VFS, ragel6Flags []ARG, closure []VFS, id NodeRef, emit *StreamingEmitter) {
	na := emit.nodeArenas()
	outVFS := ragel6OutVFS(instance, srcRel)
	inVFS := source(instance.Path.rel(), "/", srcRel)
	effectiveFlags := ragel6Flags

	if len(effectiveFlags) == 0 {
		if instance.Platform.RagelOptimized {
			effectiveFlags = []ARG{ragel6ArgOptimized}
		} else {
			effectiveFlags = []ARG{ragel6ArgDebug}
		}
	}

	head := make([]STR, 0, 1+len(effectiveFlags))

	head = append(head, (ragel6BinaryPath).str())
	head = appendArgStr(head, effectiveFlags)

	cmdArgs := na.chunkList(head, ragel6ConstArgs, na.strList((outVFS).str(), (inVFS).str()))
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(ragel6BinaryPath), closure),
		Outputs:        na.vfsList(outVFS),
		KV:             &r6KV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: []NodeRef{ragel6LD},
	}

	emit.emitReserved(node, id)
}

func (e *EmitContext) emitLibraryRagel6Source(src STR) *SourceEmit {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcRel := src.string()
	ragelLDRef, ragelBinaryVFS := ctx.tool(argContribToolsRagel6)
	rl6SourceVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	r6Out := ragel6OutVFS(instance, srcRel)
	r6Parsed := ctx.scannerFor(instance).parsers.sourceParsedBuckets(rl6SourceVFS, nil).bucket(parsedIncludesCpp)
	r6Ref := ctx.emit.reserve()

	var psc []ARG
	if p := d.perSrcCFlagsFor(src); p != nil {
		psc = *p
	}

	ctx.codegenFor(instance).register(&GeneratedFileInfo{
		OutputPath:     r6Out,
		ProducerRef:    r6Ref,
		GeneratorRefs:  []NodeRef{ragelLDRef},
		ParsedIncludes: r6Parsed,
		Compile: &CompileSpec{
			FlatOutput: d.flatSrc(src),
			CFlags:     concat(psc, []ARG{argWnoImplicitFallthrough}),
		},
	})

	window := walkClosure(ctx.scannerFor(instance), r6Out, d.cc.ScanCfg)
	rl6Closure := keepOnlySourceVFS(filterEnSerializedSiblings(window))

	emitR6(instance, srcRel, ragelLDRef, ragelBinaryVFS, d.cc.Ragel6Flags, rl6Closure, r6Ref, ctx.emit)

	if !isCxxSource(r6Out.rel()) {
		return nil
	}

	return e.emitOneSource(r6Out.str())
}
