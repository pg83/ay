package main

var nvccFlagsHead = []ANY{
	internStr("-Xfatbin=-compress-all").any(),
	internStr("--expt-extended-lambda").any(),
	internStr("--expt-relaxed-constexpr").any(),
	internStr("--allow-unsupported-compiler").any(),
	internStr("--dont-use-profile").any(),
	internStr("--libdevice-directory=$(B)/resources/CUDA/nvvm/libdevice").any(),
	internStr("--keep").any(),
}

var nvccFlagsTail = []ANY{
	internStr("--compiler-bindir=$(B)/resources/CUDA_HOST_TOOLCHAIN/bin/clang").any(),
	internStr("-I$(B)/resources/OS_SDK_ROOT/usr/include/x86_64-linux-gnu").any(),
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
	closure := dedupClosure(na, []VFS{srcCV.self, runtimeCV.self}, srcCV.buckets, runtimeCV.buckets)
	mtimeRef, mtimeVFS := ctx.tool(cudaMtimeArg)
	pidRef, pidVFS := ctx.tool(cudaCustomPidArg)
	cuCxxTail := blocks.cxxTail

	head := []([]ANY){
		na.anyList(wrapccPython3STR.any(), cudaCompileScriptVFS.any(), cudaMtimeFlagStr.any(), mtimeVFS.any(), cudaCustomPidFlagStr.any(), pidVFS.any(), cudaNvccBinStr.any(), cudaNvccStdStr.any()),
		nvccFlagsHead,
		na.anyList(internV("--keep-dir=$(B)/", instance.Path.relString()).any()),
		nvccFlagsTail,
		na.anyChunkAny(in.CudaNvccFlags),
		na.anyList(argDashC.any(), inVFS.any(), argDashO.any(), outVFS.any()),
	}

	total := len(head) + 6
	chunks := na.chunks.alloc(total)
	k := 0

	for _, h := range head {
		chunks[k] = h
		k++
	}

	chunks[k] = blocks.includes
	chunks[k+1] = na.anyList(cudaCflagsStr.any())
	chunks[k+2] = p.CCHead
	chunks[k+3] = blocks.flags
	chunks[k+4] = cuCxxTail
	chunks[k+5] = na.anyList(cudaNvccStdStr.any())
	k += 6
	na.chunks.commit(k)

	cmdArgs := ArgChunks(chunks[:k])
	env := envVarsVCSCuda
	cudaDeps := na.noderefs.alloc(2 + len(in.ExtraDepRefs))

	cudaDeps[0] = mtimeRef
	cudaDeps[1] = pidRef

	cdn := 2 + copy(cudaDeps[2:], in.ExtraDepRefs)

	na.noderefs.commit(cdn)

	cudaDeps = cudaDeps[:cdn:cdn]

	node := Node{
		Platform:     p,
		Cmds:         na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
		Env:          env,
		Inputs:       na.inputList(closure, na.vfsList(cudaCompileScriptVFS, mtimeVFS, pidVFS)),
		Outputs:      na.vfsList(outVFS),
		KV:           &cudaKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    p.CCUsesResources,
		DepRefs:      cudaDeps,
	}

	e.collectObj(ctx.emit.emitNode(node), outVFS, meta)
}
