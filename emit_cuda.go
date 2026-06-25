package main

var nvccFlagsHead = []STR{
	internStr("-Xfatbin=-compress-all"),
	internStr("--expt-extended-lambda"),
	internStr("--expt-relaxed-constexpr"),
	internStr("--allow-unsupported-compiler"),
	internStr("--dont-use-profile"),
	internStr("--libdevice-directory=$(B)/resources/CUDA/nvvm/libdevice"),
	internStr("--keep"),
}

var nvccFlagsTail = []STR{
	internStr("--compiler-bindir=$(B)/resources/CUDA_HOST_TOOLCHAIN/bin/clang"),
	internStr("-I$(B)/resources/OS_SDK_ROOT/usr/include/x86_64-linux-gnu"),
}

var cudaKV = KV{P: pkCU, PC: pcLightGreen}

const cudaArchitectures129 = "sm_50:sm_52:sm_60:sm_61:sm_70:sm_75:sm_80:sm_86:sm_89:sm_90:sm_90a:sm_100:sm_100a:sm_120:sm_120a:sm_100f:sm_103:sm_103a:sm_103f:sm_120f"

func emitLibraryCudaSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	srcRel := src.string()

	na := ctx.emit.nodeArenas()
	p := instance.Platform

	srcVFS := resolveModuleSourceVFS(ctx, instance, d, src, in.SrcDirs)
	outVFS, inVFS := composeCCPaths(instance, srcRel, srcVFS, in, ".o")
	blocks := in.CCBlocks

	scanner := ctx.scannerFor(instance)
	closure := dedup(walkClosure(scanner, srcVFS, in.ScanCfg), walkClosure(scanner, cudaRuntimeIncludeVFS, in.ScanCfg))

	mtimeRef, mtimeVFS := ctx.tool(cudaMtimeArg)
	pidRef, pidVFS := ctx.tool(cudaCustomPidArg)

	cuCxxTail := blocks.cxxTail
	trim := len(builtinMacroDateTime) + len(macroPrefixMapFlags)

	if trim <= len(cuCxxTail) {
		cuCxxTail = cuCxxTail[:len(cuCxxTail)-trim]
	}

	cmdArgs := ArgChunks(na.chunkList(
		na.strList(wrapccPython3STR, cudaCompileScriptVFS.str(), cudaMtimeFlagStr, mtimeVFS.str(), cudaCustomPidFlagStr, pidVFS.str(), cudaNvccBinStr, cudaNvccStdStr),
		nvccFlagsHead,
		na.strList(internStr("--keep-dir=$(B)/"+instance.Path.rel())),
		nvccFlagsTail,
		in.CudaNvccFlags,
		na.strList(argDashC.str(), (inVFS).str(), argDashO.str(), (outVFS).str()),
		blocks.includes,
		na.strList(cudaCflagsStr),
		p.CCHead,
		blocks.flags,
		cuCxxTail,
		na.strList(cudaNvccStdStr),
	))

	env := EnvVars{
		{Name: envARCADIA_ROOT_DISTBUILD, Value: strS},
		{Name: cudaPathEnv, Value: cudaPathValueStr},
	}

	node := &Node{
		Platform:     p,
		Cmds:         na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
		Env:          env,
		Inputs:       na.inputList(closure, na.vfsList(cudaCompileScriptVFS, mtimeVFS, pidVFS)),
		Outputs:      na.vfsList(outVFS),
		KV:           &cudaKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    p.CCUsesResources,
		DepRefs:      append([]NodeRef{mtimeRef, pidRef}, in.ExtraDepRefs...),
	}

	return &SourceEmit{Ref: ctx.emit.emit(node), OutPath: outVFS}
}
