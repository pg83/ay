package main

const (
	cudaArchitectures129       = "sm_50:sm_52:sm_60:sm_61:sm_70:sm_75:sm_80:sm_86:sm_89:sm_90:sm_90a:sm_100:sm_100a:sm_120:sm_120a:sm_100f:sm_103:sm_103a:sm_103f:sm_120f"
	cudaResourceGlobalRef      = "$(B)/resources/CUDA"
	cudaHostToolchainGlobalRef = "$(B)/resources/CUDA_HOST_TOOLCHAIN"
	osSdkRootGlobalRef         = "$(B)/resources/OS_SDK_ROOT"
	nvccStdFlag                = "-std=c++20"
)

var nvccStaticFlags = []string{
	"-Xfatbin=-compress-all",
	"--expt-extended-lambda",
	"--expt-relaxed-constexpr",
	"--allow-unsupported-compiler",
	"--dont-use-profile",
	"--libdevice-directory=" + cudaResourceGlobalRef + "/nvvm/libdevice",
	"--keep",
}

var nvccGencodeFlags = []string{
	"-gencode", "arch=compute_50,code=compute_50",
	"-gencode", "arch=compute_52,code=sm_52",
	"-gencode", "arch=compute_60,code=compute_60",
	"-gencode", "arch=compute_61,code=compute_61",
	"-gencode", "arch=compute_61,code=sm_61",
	"-gencode", "arch=compute_70,code=sm_70",
	"-gencode", "arch=compute_70,code=compute_70",
	"-gencode=arch=compute_80,code=sm_80",
	"-gencode=arch=compute_86,code=sm_86",
	"-gencode=arch=compute_89,code=sm_89",
	"-gencode=arch=compute_90,code=sm_90",
}

var cudaRuntimeIncludeVFS = source("build/internal/platform/cuda/cuda_runtime_include.h")

func internStrList(ss []string) []STR {
	out := make([]STR, len(ss))

	for i, s := range ss {
		out[i] = internStr(s)
	}

	return out
}

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

	mtimeRef, mtimeVFS := ctx.tool(internArg("tools/mtime0"))
	pidRef, pidVFS := ctx.tool(internArg("tools/custom_pid"))

	staticNvcc := append([]string{}, nvccStaticFlags...)
	staticNvcc = append(staticNvcc,
		"--keep-dir=$(B)/"+instance.Path.rel(),
		"--compiler-bindir="+cudaHostToolchainGlobalRef+"/bin/clang",
		"-I"+osSdkRootGlobalRef+"/usr/include/x86_64-linux-gnu",
	)

	prefix := na.strList(
		wrapccPython3STR,
		(source("build/scripts/compile_cuda.py")).str(),
		internStr("--mtime"), mtimeVFS.str(),
		internStr("--custom-pid"), pidVFS.str(),
		internStr(cudaResourceGlobalRef+"/bin/nvcc"),
		internStr(nvccStdFlag),
	)

	// nvcc's --cflags carries $CXXFLAGS but not the trailing -D__DATE__/__TIME__
	// (builtinMacroDateTime) nor the -fmacro-prefix-map block (macroPrefixMapFlags)
	// that the regular C++ compile appends after CXXFLAGS.
	cuCxxTail := blocks.cxxTail
	trim := len(builtinMacroDateTime) + len(macroPrefixMapFlags)

	if trim <= len(cuCxxTail) {
		cuCxxTail = cuCxxTail[:len(cuCxxTail)-trim]
	}

	cmdArgs := ArgChunks(na.chunkList(
		prefix,
		internStrList(staticNvcc),
		in.CudaNvccFlags,
		na.strList(argDashC.str(), (inVFS).str(), argDashO.str(), (outVFS).str()),
		blocks.includes,
		na.strList(internStr("--cflags")),
		p.CCHead,
		blocks.flags,
		cuCxxTail,
		na.strList(internStr(nvccStdFlag)),
	))

	env := EnvVars{
		{Name: envARCADIA_ROOT_DISTBUILD, Value: strS},
		{Name: internEnv("PATH"), Value: internStr(cudaResourceGlobalRef + "/nvvm/bin:" + cudaResourceGlobalRef + "/bin")},
	}

	node := &Node{
		Platform:     p,
		Cmds:         na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
		Env:          env,
		Inputs:       na.inputList(closure, []VFS{source("build/scripts/compile_cuda.py"), mtimeVFS, pidVFS}),
		Outputs:      na.vfsList(outVFS),
		KV:           KV{P: pkCU, PC: pcLightGreen},
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    p.CCUsesResources,
		DepRefs:      append([]NodeRef{mtimeRef, pidRef}, in.ExtraDepRefs...),
	}

	return &SourceEmit{Ref: ctx.emit.emit(node), OutPath: outVFS}
}
