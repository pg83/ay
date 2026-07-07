package main

type BuildMnStmt struct {
	Info STR
	Name string
	Seq  int
}

var mnKV = KV{P: pkMN, PC: pcYellow}

func (e *EmitContext) emitBuildMnStmt(stmt *BuildMnStmt) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	module := instance.Path.relString()
	archiverRef, archiverBin := ctx.tool(argToolsArchiver)
	infoVFS := resolveSourceVFS(ctx, instance, stmt.Info.string(), d.srcDirs)
	cppVFS := build(module, "/mn.", stmt.Name, ".cpp")
	rodataVFS := build(module, "/MN_External_", stmt.Name, ".rodata")
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(
			d.tc.Python3.any(),
			buildMnScriptVFS.any(),
			strBuildmnf.any(),
			strS.any(),
			archiverBin.any(),
			infoVFS.any(),
			internStr(stmt.Name).any(),
			strRankingSuffix.any(),
			cppVFS.any(),
		)), Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(archiverBin, buildMnScriptVFS, infoVFS)),
		KV:             &mnKV,
		Outputs:        na.vfsList(cppVFS, rodataVFS),
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(archiverRef),
		Resources:      usesPython3,
	}

	ref := ctx.emit.emitNode(node)
	mnSSEInclude := IncludeDirective{kind: includeQuoted, target: strKernelMatrixnetMnSseH}

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     cppVFS,
		ProducerRef:    ref,
		GeneratorRefs:  []NodeRef{archiverRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: []IncludeDirective{mnSSEInclude}},
		ClosureLeaves:  []VFS{infoVFS, buildMnScriptVFS},
	})
	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     rodataVFS,
		ProducerRef:    ref,
		GeneratorRefs:  []NodeRef{archiverRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: []IncludeDirective{{kind: includeQuoted, target: internStr(cppVFS.relString())}}},
	})

	e.enqueueSrc(SrcMeta{Source: cppVFS.fullSTR(), Prio: stmtPrioDefault, Generated: true, Seq: stmt.Seq})
	e.enqueueSrc(SrcMeta{Source: internV("MN_External_", stmt.Name, ".rodata"), Prio: stmtPrioDefault, Generated: true, Seq: stmt.Seq})
}
