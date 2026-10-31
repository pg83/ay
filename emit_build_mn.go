package main

var mnKV = KV{P: pkMN, PC: pcYellow}

type BuildMnStmt struct {
	Info ANY
	Name string
	Seq  int
}

func (e *EmitContext) emitBuildMnStmt(stmt *BuildMnStmt) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	module := instance.Path.relString()
	archiverRef, archiverBin := ctx.tool(argToolsArchiver)
	infoVFS := resolveSourceVFS(ctx, instance, stmt.Info.string(), d.srcDirs)
	cppVFS := build(module, "/mn.", stmt.Name, ".cpp")
	rodataVFS := build(module, "/MN_External_", stmt.Name, ".rodata")
	env := envVarsVCS
	ref := ctx.emit.reserve()
	mnSSEInclude := IncludeDirective{kind: includeQuoted, target: includeTarget(strKernelMatrixnetMnSseH.any())}
	python3 := d.tc.Python3

	pe := func() {
		node := NodeSpec{Node: Node{Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(
				python3,
				buildMnScriptVFS.any(),
				strBuildmnf.any(),
				strS.any(),
				archiverBin.any(),
				infoVFS.any(),
				internStr(stmt.Name).any(),
				strRankingSuffix.any(),
				cppVFS.any(),
			)), Env: env}),
			Env: env, Inputs: na.inputList(na.vfsList(archiverBin), na.vfsList(buildMnScriptVFS), na.vfsList(infoVFS)), KV: &mnKV,
			Outputs:        na.vfsList(cppVFS, rodataVFS),
			ForeignDepRefs: na.refList(archiverRef),
			Resources:      usesPython3}, input: InputSpec{roots: na.inputList(na.vfsList(buildMnScriptVFS), na.vfsList(infoVFS))},
		}

		e.emitReservedNode(node, ref)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileSpec{
		OutputPath:     cppVFS,
		ProducerRef:    ref,
		GeneratorRefs:  e.ctx.na.refList(archiverRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: e.ctx.na.dirList(mnSSEInclude)},

		OnUse: pending,
	})

	e.register(GeneratedFileSpec{
		OutputPath:     rodataVFS,
		ProducerRef:    ref,
		GeneratorRefs:  e.ctx.na.refList(archiverRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: e.ctx.na.dirList(IncludeDirective{kind: includeQuoted, target: includeTarget(cppVFS.rel().any())})},
		OnUse:          pending,
	})

	e.enqueueSrc(SrcMeta{Source: cppVFS.any(), Prio: stmtPrioDefault, Seq: stmt.Seq})
	e.enqueueSrc(SrcMeta{Source: rodataVFS.any(), Prio: stmtPrioDefault, Seq: stmt.Seq})
}
