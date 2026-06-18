package main

// DecimalMD5Lower32BitsStmt is one DECIMAL_MD5_LOWER_32_BITS(File, FUNCNAME="",
// Opts...) declaration (ymake.core.conf:4239). decimal_md5.py hashes the
// resolved Opts and writes File as a build-root source via stdout.
type DecimalMD5Lower32BitsStmt struct {
	File     string
	FuncName string
	Opts     []STR
}

// DecimalMD5Result carries the C/C++ compiles of macro-produced sources so
// gen.go can archive them through the ordinary generated-source path.
type DecimalMD5Result struct {
	CCRefs    []NodeRef
	CCOutputs []VFS
}

// emitDecimalMD5ForAR emits one SV producer per DECIMAL_MD5_LOWER_32_BITS
// declaration and, for C/C++ outputs, the downstream compile of the generated
// source. Returns the compiles for archive wiring (nil when the module declares
// none).
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

// emitDecimalMD5 emits the SV node for one declaration and registers its output
// as a generated source. The resolved Opt inputs and decimal_md5.py are the
// node's inputs and ride to the downstream compile as the output's closure
// leaves (the codegen vehicle emit_pr.go / emit_cf.go use).
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
		internStr("--fixed-output="),
		internStr("--func-name="+stmt.FuncName),
		internStr("--lower-bits"),
		internStr("32"),
		internStr("--source-root=" + strS.string()),
	)

	for _, v := range optVFSs {
		cmdArgs = append(cmdArgs, v.str())
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	svRef := ctx.emit.emit(&Node{
		Platform:         instance.Platform,
		Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env, Stdout: outVFS.str()}),
		Env:              env,
		Inputs:           na.inputList(na.vfsList(optVFSs...), na.vfsList(decimalMD5PyVFS)),
		KV:               KV{P: pkSV, PC: pcYellow, ShowOut: true},
		Outputs:          na.vfsList(outVFS),
		TargetProperties: TargetProperties{ModuleDir: modulePath},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:        usesPython3,
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
