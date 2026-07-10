package main

var decimalMd5KV = KV{P: pkSV, PC: pcYellow, ShowOut: true}

type DecimalMD5Lower32BitsStmt struct {
	File     string
	FuncName string
	Opts     []ANY
}

func (e *EmitContext) emitDecimalMD5Stmt(stmt *DecimalMD5Lower32BitsStmt) {
	instance := e.instance

	e.emitDecimalMD5(stmt)

	if !isCCSourceExt(stmt.File) {
		return
	}

	e.enqueueSrc(SrcMeta{Source: copyFileOutputVFS(instance.Path.relString(), stmt.File).any(), Prio: stmtPrioDefault, Bucket: bkDecimalMD5})
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

	svRef := ctx.emit.reserve()
	sourceInputs := na.vfs.alloc(len(optVFSs) + 1)
	sn := copy(sourceInputs, optVFSs)

	sourceInputs[sn] = decimalMD5PyVFS
	sn++
	na.vfs.commit(sn)

	sourceInputs = sourceInputs[:sn:sn]

	optArena := na.vfsList(optVFSs...)
	python3 := d.tc.Python3

	pe := func() {
		cmdArgs := na.anys.alloc(7 + len(optArena))[:0]

		cmdArgs = append(cmdArgs,
			python3.any(),
			decimalMD5PyVFS.any(),
			strFixedOutput.any(),
			internV("--func-name=", stmt.FuncName).any(),
			strLowerBits.any(),
			str32.any(),
			internV("--source-root=", strS.string()).any(),
		)

		for _, v := range optArena {
			cmdArgs = append(cmdArgs, v.any())
		}

		na.anys.commit(len(cmdArgs))

		cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

		env := envVarsVCS

		ctx.emit.emitReservedNode(Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env, Stdout: outVFS}),
			Env:          env,
			Inputs:       na.inputList(optArena, na.vfsList(decimalMD5PyVFS)),
			KV:           &decimalMd5KV,
			Outputs:      na.vfsList(outVFS),
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:    usesPython3,
		}, svRef)
	}
	pending := e.ctx.na.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:    outVFS,
		ProducerRef:   svRef,
		SourceInputs:  sourceInputs,
		ClosureLeaves: sourceInputs,
		OnUse:         pending,
	})

	return svRef
}
