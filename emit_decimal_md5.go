package main

var decimalMd5KV = KV{P: pkSV, PC: pcYellow, ShowOut: true}

type DecimalMD5Lower32BitsStmt struct {
	File     string
	FuncName string
	Opts     []STR
}

func (e *EmitContext) emitDecimalMD5Stmt(stmt *DecimalMD5Lower32BitsStmt) {
	instance := e.instance

	e.emitDecimalMD5(stmt)

	if !isCCSourceExt(stmt.File) {
		return
	}

	e.enqueueSrc(SrcMeta{Source: copyFileOutputVFS(instance.Path.relString(), stmt.File).any(), Prio: stmtPrioDefault, Generated: true, Bucket: bkDecimalMD5})
}

func (e *EmitContext) emitDecimalMD5(stmt *DecimalMD5Lower32BitsStmt) NodeRef {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.emit.nodeArenas()
	modulePath := instance.Path.relString()
	outVFS := copyFileOutputVFS(modulePath, stmt.File)
	optVFSs := make([]VFS, 0, len(stmt.Opts))

	for _, opt := range stmt.Opts {
		optVFSs = append(optVFSs, e.requireProducedInput("DECIMAL_MD5 input", opt.string(), copyFileInputVFS(ctx.fs, instance.Path, opt.string())))
	}

	cmdArgs := make([]ANY, 0, 7+len(optVFSs))

	cmdArgs = append(cmdArgs,
		d.tc.Python3.any(),
		decimalMD5PyVFS.any(),
		strFixedOutput.any(),
		internV("--func-name=", stmt.FuncName).any(),
		strLowerBits.any(),
		str32.any(),
		internV("--source-root=", strS.string()).any(),
	)

	for _, v := range optVFSs {
		cmdArgs = append(cmdArgs, v.any())
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	svRef := ctx.emit.emitNode(Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env, Stdout: outVFS}),
		Env:          env,
		Inputs:       na.inputList(na.vfsList(optVFSs...), na.vfsList(decimalMD5PyVFS)),
		KV:           &decimalMd5KV,
		Outputs:      na.vfsList(outVFS),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    usesPython3,
	})

	sourceInputs := make([]VFS, 0, len(optVFSs)+1)

	sourceInputs = append(sourceInputs, optVFSs...)
	sourceInputs = append(sourceInputs, decimalMD5PyVFS)

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:    outVFS,
		ProducerRef:   svRef,
		SourceInputs:  sourceInputs,
		ClosureLeaves: sourceInputs,
	})

	return svRef
}
