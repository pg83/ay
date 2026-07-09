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

	cppInfo := e.codegen.register(GeneratedFileInfo{
		OutputPath:     cppVFS,
		ProducerRef:    ref,
		GeneratorRefs:  e.ctx.na.refList(archiverRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: e.ctx.na.dirList(mnSSEInclude)},
		ClosureLeaves:  e.ctx.na.vfsList(infoVFS, buildMnScriptVFS),
	})

	rodataInfo := e.codegen.register(GeneratedFileInfo{
		OutputPath:     rodataVFS,
		ProducerRef:    ref,
		GeneratorRefs:  e.ctx.na.refList(archiverRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: e.ctx.na.dirList(IncludeDirective{kind: includeQuoted, target: includeTarget(cppVFS.rel().any())})},
	})

	python3 := d.tc.Python3

	pe := &PendingEmit{fn: func() {
		node := Node{
			Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(
				python3.any(),
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
			ForeignDepRefs: na.refList(archiverRef),
			Resources:      usesPython3,
		}

		ctx.emit.emitReservedNode(node, ref)
	}}

	cppInfo.pending = pe
	rodataInfo.pending = pe


	e.enqueueSrc(SrcMeta{Source: cppVFS.any(), Prio: stmtPrioDefault, Generated: true, Seq: stmt.Seq})
	e.enqueueSrc(SrcMeta{Source: internV("MN_External_", stmt.Name, ".rodata").any(), Prio: stmtPrioDefault, Generated: true, Seq: stmt.Seq})
}
