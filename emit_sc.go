package main

var scKV = KV{P: pkSC, PC: pcYellow}

func (e *EmitContext) collectSCInputs(inputs []VFS) {
	for _, input := range inputs {
		if input.isSource() {
			if _, ok := e.ctx.parsers.registry.registeredParserFor(input.relString()).(ScIncludeDirectiveParser); ok {
				e.scInputs = append(e.scInputs, input)
			}
		}
	}
}

// SourceFile include facts belong to the first module that registers a parsed
// input, even when that input's action is never used. Register after local
// producers so resolving a generated import still uses complete producer facts.
func (e *EmitContext) registerSCInputs() {
	visit := func(input VFS) {
		if !input.isSource() {
			return
		}

		if _, ok := e.ctx.parsers.registry.registeredParserFor(input.relString()).(ScIncludeDirectiveParser); ok {
			e.scanner.scanClosure(input, e.d.scanCtx, scanDomainSc)
		}
	}

	for _, meta := range e.d.srcs {
		if _, ok := e.ctx.parsers.registry.registeredParserFor(meta.Source.relOrSelf().string()).(ScIncludeDirectiveParser); ok {
			visit(e.resolveModuleSourceVFS(meta.Source, e.d.srcDirs))
		}
	}

	for _, input := range e.scInputs {
		visit(input)
	}
}

func (e *EmitContext) emitSCReserved(srcVFS, headerVFS, domschemecBinary VFS, domschemecLDRef NodeRef, id NodeRef) {
	na := e.ctx.na
	env := envVarsVCS
	closure := e.scanner.walkClosure(srcVFS, e.d.scanCtx, scanDomainSc)

	node := Node{
		Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(na.anyList(domschemecBinary.any(), argDashIn.any(), srcVFS.any(), argDashOut.any(), headerVFS.any())),
			Env:     env,
		}),
		Env:            env,
		Inputs:         appendInputChunks(na, na.inputList(na.vfsList(domschemecBinary)), na.dedupClosureChunks(closure)),
		InputClosure:   closure,
		KV:             &scKV,
		Outputs:        na.vfsList(headerVFS),
		ForeignDepRefs: na.refList(domschemecLDRef),
	}

	e.emitReservedNode(node, id)
}

func (e *EmitContext) emitLibrarySCSource(src ANY) {
	ctx, d := e.ctx, e.d
	domRes := ctx.toolResult(argToolsDomschemec)
	domLDRef, domBinary := domRes.LDRef, *domRes.LDPath
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	headerVFS := build(srcVFS.relString(), ".h")
	scRef := ctx.emit.reserve()
	runtimeInclude := ctx.na.dirList(IncludeDirective{kind: includeQuoted, target: includeTarget(domschemeRuntimeVFS.rel().any())})

	pe := func() {
		e.emitSCReserved(srcVFS, headerVFS, domBinary, domLDRef, scRef)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     headerVFS,
		ProducerRef:    scRef,
		GeneratorRefs:  e.ctx.na.refList(domLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: runtimeInclude},

		OnUse: pending,
	})
}
