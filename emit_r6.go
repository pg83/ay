package main

import "strings"

var (
	ragel6ArgOptimized = internArg(ragel6DefaultFlagOptimized)
	ragel6ArgDebug     = internArg(ragel6DefaultFlagDebug)
	ragel6ConstArgs    = []ANY{argL.any(), argIS.any(), argDashO.any()}
	r6KV               = KV{P: pkR6, PC: pcYellow}
)

const (
	ragel6DefaultFlagOptimized = "-CG2"
	ragel6DefaultFlagDebug     = "-CT0"
	ragel6DefaultOutExt        = ".rl6.cpp"
)

func ragel6OutVFS(instance ModuleInstance, srcRel string) VFS {
	dir := instance.Path.relString() + "/"
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

func (e *EmitContext) emitR6(srcRel string, inVFS VFS, ragel6LD NodeRef, ragel6BinaryPath VFS, ragel6Flags []ANY, closure []VFS, id NodeRef) {
	instance := e.instance
	na := e.ctx.na
	outVFS := ragel6OutVFS(instance, srcRel)
	effectiveFlags := ragel6Flags

	if len(effectiveFlags) == 0 {
		if instance.Platform.RagelOptimized {
			effectiveFlags = []ANY{ragel6ArgOptimized.any()}
		} else {
			effectiveFlags = []ANY{ragel6ArgDebug.any()}
		}
	}

	head := na.anys.alloc(1 + len(effectiveFlags))[:0]

	head = append(head, ragel6BinaryPath.any())
	head = appendAnyLists(head, effectiveFlags)
	na.anys.commit(len(head))

	head = head[:len(head):len(head)]

	cmdArgs := na.chunkList(head, ragel6ConstArgs, na.anyList(outVFS.any(), inVFS.any()))
	env := envVarsVCS
	head2 := na.vfsList(ragel6BinaryPath)

	if inVFS.isBuild() {
		head2 = na.vfsList(ragel6BinaryPath, inVFS)
	}

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Env: env}),
		Env:            env,
		Inputs:         na.inputList(head2, na.vfsList(closure...)),
		Outputs:        na.vfsList(outVFS),
		KV:             &r6KV,
		ForeignDepRefs: na.refList(ragel6LD),
	}

	e.emitReservedNode(node, id)
}

func (e *EmitContext) emitLibraryRagel6Source(meta SrcMeta) {
	ctx, instance, d := e.ctx, e.instance, e.d
	src := meta.Source
	srcRel := e.moduleSourceRel(src)

	ragelLDRef, ragelBinaryVFS := ctx.tool(argContribToolsRagel6)
	rl6SourceVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	r6Out := ragel6OutVFS(instance, srcRel)

	r6Parsed := e.scanner.parsedBucketForInput(rl6SourceVFS, parsedIncludesCpp, nil)

	r6Ref := ctx.emit.reserve()

	if isCxxSource(r6Out.relString()) {
		meta.Source = r6Out.any()
		meta.Compile.CFlags = cflagsWnoImplicitFallthrough(e.ctx.na, meta.Compile.CFlags)
		e.enqueueSrc(meta)
	}

	scanner := e.scanner
	scanCtx := d.scanCtx
	ragel6Flags := ctx.na.anyList(d.cc.Ragel6Flags...)

	pe := func() {
		rl6Closure := scanner.walkClosure(r6Out, scanCtx, scanDomainCC).collect(ctx.na, func(v VFS) bool {
			return v.isSource() && !extIsEnumSerialized(v.relString())
		})

		e.emitR6(srcRel, rl6SourceVFS, ragelLDRef, ragelBinaryVFS, ragel6Flags, rl6Closure, r6Ref)
	}
	pending := e.ctx.na.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     r6Out,
		ProducerRef:    r6Ref,
		GeneratorRefs:  e.ctx.na.refList(ragelLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: r6Parsed},
		OnUse:          pending,
	})
}
