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

func (e *EmitContext) emitLibraryCudaSource(meta SrcMeta) {
	src := meta.Source
	ctx, instance, d := e.ctx, e.instance, e.d
	srcRel := src.string()
	na := ctx.emit.nodeArenas()
	p := instance.Platform
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	in := e.ccInputsFor(srcVFS)
	outVFS, inVFS := composeCCPaths(instance, srcRel, srcVFS, in, ".o")
	blocks := in.CCBlocks
	scanner := e.scanner
	srcCV := walkClosure(scanner, srcVFS, in.ScanCfg)
	runtimeCV := walkClosure(scanner, cudaRuntimeIncludeVFS, in.ScanCfg)
	closure := dedupClosure([]VFS{srcCV.self, runtimeCV.self}, srcCV.buckets, runtimeCV.buckets)
	mtimeRef, mtimeVFS := ctx.tool(cudaMtimeArg)
	pidRef, pidVFS := ctx.tool(cudaCustomPidArg)
	cuCxxTail := blocks.cxxTail

	head := []([]STR){
		na.strList(wrapccPython3STR, cudaCompileScriptVFS.fullSTR(), cudaMtimeFlagStr, mtimeVFS.fullSTR(), cudaCustomPidFlagStr, pidVFS.fullSTR(), cudaNvccBinStr, cudaNvccStdStr),
		nvccFlagsHead,
		na.strList(internV("--keep-dir=$(B)/", instance.Path.relString())),
		nvccFlagsTail,
		in.CudaNvccFlags,
		na.strList(argDashC.str(), (inVFS).fullSTR(), argDashO.str(), (outVFS).fullSTR()),
	}

	total := len(head) + 6
	chunks := na.chunks.alloc(total)
	k := 0

	for _, h := range head {
		chunks[k] = na.anyChunk(h)
		k++
	}

	chunks[k] = na.anyChunkAny(blocks.includes)
	chunks[k+1] = na.anyChunk(na.strList(cudaCflagsStr))
	chunks[k+2] = na.anyChunkAny(p.CCHead)
	chunks[k+3] = na.anyChunkAny(blocks.flags)
	chunks[k+4] = na.anyChunkAny(cuCxxTail)
	chunks[k+5] = na.anyChunk(na.strList(cudaNvccStdStr))
	k += 6
	na.chunks.commit(k)

	cmdArgs := ArgChunks(chunks[:k])

	env := EnvVars{
		{Name: envARCADIA_ROOT_DISTBUILD, Value: strS},
		{Name: cudaPathEnv, Value: cudaPathValueStr},
	}

	node := Node{
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

	e.collectObj(ctx.emit.emitNode(node), outVFS, meta)
}
