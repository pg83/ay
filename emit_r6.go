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

func emitR6(instance ModuleInstance, srcRel string, inVFS VFS, ragel6LD NodeRef, ragel6BinaryPath VFS, ragel6Flags []ARG, closure []VFS, producerRefs []NodeRef, id NodeRef, emit *StreamingEmitter) {
	na := emit.nodeArenas()
	outVFS := ragel6OutVFS(instance, srcRel)
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
	head2 := na.vfsList(ragel6BinaryPath)

	if inVFS.isBuild() {
		head2 = na.vfsList(ragel6BinaryPath, inVFS)
	}

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Env: env}),
		Env:            env,
		Inputs:         na.inputList(head2, closure),
		Outputs:        na.vfsList(outVFS),
		KV:             &r6KV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:        producerRefs,
		ForeignDepRefs: []NodeRef{ragel6LD},
	}

	emit.emitReservedNode(node, id)
}

func (e *EmitContext) emitLibraryRagel6Source(src STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcRel := src.string()
	ragelLDRef, ragelBinaryVFS := ctx.tool(argContribToolsRagel6)
	rl6SourceVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	r6Out := ragel6OutVFS(instance, srcRel)

	var r6Parsed []IncludeDirective

	if rl6SourceVFS.isBuild() {
		if info := e.codegen.lookup(rl6SourceVFS); info != nil {
			r6Parsed = info.ParsedIncludes.bucket(parsedIncludesLocal)
		}
	} else {
		r6Parsed = e.scanner.parsers.sourceParsedBuckets(rl6SourceVFS, nil).bucket(parsedIncludesCpp)
	}

	r6Ref := ctx.emit.reserve()

	var psc []ARG

	if p := d.perSrcCFlagsFor(src); p != nil {
		psc = *p
	}

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     r6Out,
		ProducerRef:    r6Ref,
		GeneratorRefs:  []NodeRef{ragelLDRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: r6Parsed},
		Compile: &CompileSpec{
			FlatOutput: d.flatSrc(src),
			CFlags:     concat(psc, []ARG{argWnoImplicitFallthrough}),
		},
	})

	if isCxxSource(r6Out.rel()) {
		meta := d.srcMetaOf(src)

		meta.Generated = true
		meta.Source = r6Out.str()
		e.enqueueSrc(meta)
	}

	e.deferPass2(func() {
		rl6Closure := walkClosure(e.scanner, r6Out, d.cc.ScanCfg).collect(func(v VFS) bool {
			return v.isSource() && !extIsEnumSerialized(v.rel())
		})

		var producerRefs []NodeRef

		if rl6SourceVFS.isBuild() {
			producerRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, []VFS{rl6SourceVFS})
		}

		emitR6(instance, srcRel, rl6SourceVFS, ragelLDRef, ragelBinaryVFS, d.cc.Ragel6Flags, rl6Closure, producerRefs, r6Ref, ctx.emit)
	})
}
