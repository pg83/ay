package main

type DecimalMD5Lower32BitsStmt struct {
	File     string
	FuncName string
	Opts     []STR
}

type DecimalMD5Result struct {
	CCRefs    []NodeRef
	CCOutputs []VFS
}

func emitDecimalMD5ForAR(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) *DecimalMD5Result {
	if len(d.decimalMD5) == 0 {
		return nil
	}

	res := &DecimalMD5Result{}

	for _, stmt := range d.decimalMD5 {
		svRef := emitDecimalMD5(ctx, instance, d, stmt)

		if !isCCSourceExt(stmt.File) {
			continue
		}

		ccRef, ccOut := emitCodegenDownstreamCC(ctx, instance, stmt.File, []NodeRef{svRef}, in)
		res.CCRefs = append(res.CCRefs, ccRef)
		res.CCOutputs = append(res.CCOutputs, ccOut)
	}

	return res
}

func emitDecimalMD5(ctx *GenCtx, instance ModuleInstance, d *ModuleData, stmt *DecimalMD5Lower32BitsStmt) NodeRef {
	na := ctx.emit.nodeArenas()
	modulePath := instance.Path.rel()

	outVFS := copyFileOutputVFS(modulePath, stmt.File)

	optVFSs := make([]VFS, 0, len(stmt.Opts))

	for _, opt := range stmt.Opts {
		optVFSs = append(optVFSs, copyFileInputVFS(ctx.fs, modulePath, opt.string()))
	}

	cmdArgs := make([]STR, 0, 7+len(optVFSs))
	cmdArgs = append(cmdArgs,
		d.tc.Python3,
		decimalMD5PyVFS.str(),
		strFixedOutput,
		internStr("--func-name="+stmt.FuncName),
		strLowerBits,
		str32,
		internStr("--source-root="+strS.string()),
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
		KV:           KV{P: pkSV, PC: pcYellow, ShowOut: true},
		Outputs:      na.vfsList(outVFS),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    usesPython3,
	})

	registerBoundGeneratedParsedOutput(ctx, instance, pkSV, outVFS, nil, svRef, nil)

	reg := codegenRegForInstance(ctx, instance)

	sourceInputs := make([]VFS, 0, len(optVFSs)+1)
	sourceInputs = append(sourceInputs, optVFSs...)
	sourceInputs = append(sourceInputs, decimalMD5PyVFS)
	reg.setSourceInputs(outVFS, sourceInputs)

	for _, v := range sourceInputs {
		reg.addClosureLeaf(outVFS, v)
	}

	return svRef
}
