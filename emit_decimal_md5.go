package main

var decimalMd5KV = KV{P: pkSV, PC: pcYellow, ShowOut: true}

type DecimalMD5Lower32BitsStmt struct {
	File     string
	FuncName string
	Opts     []STR
}

func (e *EmitContext) emitDecimalMD5ForAR() {
	_, instance, d := e.ctx, e.instance, e.d
	if len(d.decimalMD5) == 0 {
		return
	}

	for _, stmt := range d.decimalMD5 {
		e.emitDecimalMD5(stmt)

		if !isCCSourceExt(stmt.File) {
			continue
		}

		e.emitGenerated(copyFileOutputVFS(instance.Path.rel(), stmt.File).str(), SrcMeta{Prio: stmtPrioDefault, Generated: true, Bucket: bkDecimalMD5})
	}
}

func (e *EmitContext) emitDecimalMD5(stmt *DecimalMD5Lower32BitsStmt) NodeRef {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.emit.nodeArenas()
	modulePath := instance.Path.rel()
	outVFS := copyFileOutputVFS(modulePath, stmt.File)
	optVFSs := make([]VFS, 0, len(stmt.Opts))

	for _, opt := range stmt.Opts {
		optVFSs = append(optVFSs, copyFileInputVFS(ctx.fs, instance.Path, opt.string()))
	}

	cmdArgs := make([]STR, 0, 7+len(optVFSs))

	cmdArgs = append(cmdArgs,
		d.tc.Python3,
		decimalMD5PyVFS.str(),
		strFixedOutput,
		internV("--func-name=", stmt.FuncName),
		strLowerBits,
		str32,
		internV("--source-root=", strS.string()),
	)

	for _, v := range optVFSs {
		cmdArgs = append(cmdArgs, v.str())
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	svRef := ctx.emit.emit(&Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env, Stdout: outVFS.str()}),
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
