package main

const cudaArchitectures129 = "sm_50:sm_52:sm_60:sm_61:sm_70:sm_75:sm_80:sm_86:sm_89:sm_90:sm_90a:sm_100:sm_100a:sm_120:sm_120a:sm_100f:sm_103:sm_103a:sm_103f:sm_120f"

// Static nvcc flag block (cuda.conf Cuda.nvcc_flags). Resource paths are written
// as $(B)/resources/<name>, which the dump normalizer folds to $(<name>).
var nvccFlagsHead = []STR{
	internStr("-Xfatbin=-compress-all"),
	internStr("--expt-extended-lambda"),
	internStr("--expt-relaxed-constexpr"),
	internStr("--allow-unsupported-compiler"),
	internStr("--dont-use-profile"),
	internStr("--libdevice-directory=$(B)/resources/CUDA/nvvm/libdevice"),
	internStr("--keep"),
}

// The resource-derived flags following the per-module --keep-dir.
var nvccFlagsTail = []STR{
	internStr("--compiler-bindir=$(B)/resources/CUDA_HOST_TOOLCHAIN/bin/clang"),
	internStr("-I$(B)/resources/OS_SDK_ROOT/usr/include/x86_64-linux-gnu"),
}

var (
	cudaCompileScriptVFS  = source("build/scripts/compile_cuda.py")
	cudaRuntimeIncludeVFS = source("build/internal/platform/cuda/cuda_runtime_include.h")
	cudaMtimeArg          = internArg("tools/mtime0")
	cudaCustomPidArg      = internArg("tools/custom_pid")
	cudaNvccBinStr        = internStr("$(B)/resources/CUDA/bin/nvcc")
	cudaMtimeFlagStr      = internStr("--mtime")
	cudaCustomPidFlagStr  = internStr("--custom-pid")
	cudaCflagsStr         = internStr("--cflags")
	cudaNvccStdStr        = internStr("-std=c++20")
	cudaPathEnv           = internEnv("PATH")
	cudaPathValueStr      = internStr("$(B)/resources/CUDA/nvvm/bin:$(B)/resources/CUDA/bin")
)

func emitLibraryCudaSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	na := ctx.emit.nodeArenas()
	p := instance.Platform

	srcVFS := resolveModuleSourceVFS(ctx, instance, d, srcRel, in.SrcDirs)
	outVFS, inVFS := composeCCPaths(instance, srcRel, srcVFS, in, ".o")
	blocks := in.CCBlocks

	// nvcc force-includes cuda_runtime_include.h; its transitive closure is part
	// of every .cu compile's inputs (for most sources their own headers already
	// cover it, but a header-light .cu like operators.cu gets it only from here).
	scanner := ctx.scannerFor(instance)
	closure := dedupVFS(walkClosure(scanner, srcVFS, in.ScanCfg), walkClosure(scanner, cudaRuntimeIncludeVFS, in.ScanCfg))

	mtimeRef, mtimeVFS := ctx.tool(cudaMtimeArg)
	pidRef, pidVFS := ctx.tool(cudaCustomPidArg)

	// nvcc's --cflags carries $CXXFLAGS but not the trailing -D__DATE__/__TIME__
	// (builtinMacroDateTime) nor the -fmacro-prefix-map block (macroPrefixMapFlags)
	// that the regular C++ compile appends after CXXFLAGS.
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
		KV:           KV{P: pkCU, PC: pcLightGreen},
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    p.CCUsesResources,
		DepRefs:      append([]NodeRef{mtimeRef, pidRef}, in.ExtraDepRefs...),
	}

	return &SourceEmit{Ref: ctx.emit.emit(node), OutPath: outVFS}
}
