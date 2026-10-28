package main

import "strings"

var (
	ragel6ConstArgs          = []ANY{argL.any(), argIS.any(), argDashO.any()}
	ragel6BuildwithConstArgs = ragel6ConstArgs[1:]
	r6KV                     = KV{P: pkR6, PC: pcYellow}
)

const ragel6DefaultOutExt = ".rl6.cpp"

type Ragel6BuildStmt struct {
	Resolved ResolvedSource
	Src      string
	Options  []ANY
	DeclSeq  int
}

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

func (e *EmitContext) emitR6(srcRel string, inVFS VFS, ragel6LD NodeRef, ragel6BinaryPath VFS, ragel6Flags, options, constArgs []ANY, closure []VFS, view Closure, id NodeRef) {
	instance := e.instance
	na := e.ctx.na
	outVFS := ragel6OutVFS(instance, srcRel)
	effectiveFlags := ragel6Flags

	if len(effectiveFlags) == 0 {
		effectiveFlags = instance.Platform.Ragel6Flags
	}

	head := na.anys.alloc(1 + len(effectiveFlags) + len(options))[:0]

	head = append(head, ragel6BinaryPath.any())
	head = appendAnyLists(head, effectiveFlags, options)
	na.anys.commit(len(head))

	head = head[:len(head):len(head)]

	cmdArgs := na.chunkList(head, constArgs, na.anyList(outVFS.any(), inVFS.any()))
	env := envVarsVCS
	head2 := na.vfsList(ragel6BinaryPath)

	if inVFS.isBuild() {
		head2 = na.vfsList(ragel6BinaryPath, inVFS)
	}

	node := NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Env: env}),
		Env: env, Inputs: na.rootInputList(na.vfsList(closure...), head2), Outputs: na.vfsList(outVFS),
		KV:             &r6KV,
		ForeignDepRefs: na.refList(ragel6LD)}, input: InputSpec{roots: na.inputList(head2[1:], na.vfsList(closure...))},
	}

	e.emitReservedNode(node, id)
}

func (e *EmitContext) emitLibraryRagel6Source(meta SrcMeta) {
	e.emitRagel6Source(meta, nil, ragel6ConstArgs)
}

func (e *EmitContext) emitRagel6Build(stmt Ragel6BuildStmt) {
	e.emitRagel6Source(SrcMeta{Source: internStr(stmt.Src).any(), Resolved: stmt.Resolved, Prio: stmtPrioDefault, Seq: stmt.DeclSeq}, stmt.Options, ragel6BuildwithConstArgs)
}

func (e *EmitContext) emitRagel6Source(meta SrcMeta, options, constArgs []ANY) {
	ctx, instance, d := e.ctx, e.instance, e.d
	src := meta.Source
	srcRel := e.moduleSourceRel(src)
	ragelLDRef, ragelBinaryVFS := ctx.tool(argContribToolsRagel6)
	rl6SourceVFS := e.sourceForEmission(meta)
	r6Out := ragel6OutVFS(instance, srcRel)
	r6Parsed := e.scanner.parsedBucketForInput(rl6SourceVFS, parsedIncludesCpp, nil)

	if len(r6Parsed) > 0 && r6Parsed[0].target == includeTarget(rl6SourceVFS.rel().any()) {
		r6Parsed = r6Parsed[1:]
	}

	r6Ref := ctx.emit.reserve()

	if isCxxSource(r6Out.relString()) {
		meta.Source = r6Out.any()
		e.enqueueSrc(meta)
	}

	scanner := e.scanner
	scanCtx := d.scanCtx
	ragel6Flags := ctx.na.anyList(d.cc.Ragel6Flags...)

	pe := func() {
		view := scanner.walkClosure(rl6SourceVFS, scanCtx, scanDomainCC)
		rl6Closure := view.collect(ctx.na, func(VFS) bool { return true })

		e.emitR6(srcRel, rl6SourceVFS, ragelLDRef, ragelBinaryVFS, ragel6Flags, options, constArgs, rl6Closure, view, r6Ref)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileSpec{
		OutputPath:     r6Out,
		ProducerRef:    r6Ref,
		GeneratorRefs:  e.ctx.na.refList(ragelLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: r6Parsed},
		OnUse:          pending,
	})
}
