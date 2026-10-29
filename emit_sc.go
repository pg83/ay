package main

var scKV = KV{P: pkSC, PC: pcYellow}

func (e *EmitContext) collectSCInputs(inputs []VFS) {
	for _, input := range inputs {
		if input.isSource() {
			parser, _ := e.ctx.parsers.registry.registeredParserFor(input.relString())

			if _, ok := parser.(ScIncludeDirectiveParser); ok {
				e.scInputs = append(e.scInputs, input)
			}
		}
	}
}

func (e *EmitContext) registerSCInputs() {
	visit := func(input VFS) {
		if !input.isSource() {
			return
		}

		parser, _ := e.ctx.parsers.registry.registeredParserFor(input.relString())

		if _, ok := parser.(ScIncludeDirectiveParser); ok {
			e.scanner.scanClosure(input, e.d.scanCtx, scanDomainSc)
		}
	}

	for _, meta := range e.d.srcs {
		parser, _ := e.ctx.parsers.registry.registeredParserFor(meta.Source.relOrSelf().string())

		if _, ok := parser.(ScIncludeDirectiveParser); ok {
			visit(e.sourceForEmission(meta))
		}
	}

	for _, input := range e.scInputs {
		visit(input)
	}
}

func (e *EmitContext) emitSCReserved(srcVFS, headerVFS, domschemecBinary VFS, domschemecLDRef NodeRef, id NodeRef) {
	na := e.ctx.na
	env := envVarsVCS
	inputs := na.inputList(na.vfsList(domschemecBinary), na.vfsList(srcVFS))

	node := NodeSpec{Node: Node{Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(na.anyList(domschemecBinary.any(), argDashIn.any(), srcVFS.any(), argDashOut.any(), headerVFS.any())),
			Env:     env,
		}),
		Env: env, Inputs: inputs, KV: &scKV,
		Outputs:        na.vfsList(headerVFS),
		ForeignDepRefs: na.refList(domschemecLDRef)}, input: InputSpec{roots: inputs[1:]},
	}

	e.emitReservedNode(node, id)
}

func (e *EmitContext) emitLibrarySCSource(meta SrcMeta) {
	ctx := e.ctx
	domRes := ctx.toolResult(argToolsDomschemec)
	domLDRef, domBinary := domRes.LDRef, *domRes.LDPath
	srcVFS := e.sourceForEmission(meta)
	headerVFS := build(srcVFS.relString(), ".h")
	scRef := ctx.emit.reserve()
	runtimeInclude := ctx.na.dirList(IncludeDirective{kind: includeQuoted, target: includeTarget(domschemeRuntimeVFS.rel().any())})

	pe := func() {
		e.emitSCReserved(srcVFS, headerVFS, domBinary, domLDRef, scRef)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileSpec{
		OutputPath:     headerVFS,
		ProducerRef:    scRef,
		GeneratorRefs:  e.ctx.na.refList(domLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: runtimeInclude},

		OnUse: pending,
	})
}
