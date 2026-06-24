package main

const (
	cudaResourceGlobalRef      = "$CUDA_RESOURCE_GLOBAL"
	cudaHostToolchainGlobalRef = "$CUDA_HOST_TOOLCHAIN_RESOURCE_GLOBAL"
	osSdkRootGlobalRef         = "$OS_SDK_ROOT_RESOURCE_GLOBAL"
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
	outVFS, inVFS := composeCCPaths(instance, srcRel, srcVFS, in, ".cu.o")
	blocks := in.CCBlocks

	mtimeRef, mtimeVFS := ctx.tool(internArg("tools/mtime0"))
	pidRef, pidVFS := ctx.tool(internArg("tools/custom_pid"))

	staticNvcc := append([]string{}, nvccStaticFlags...)
	staticNvcc = append(staticNvcc,
		"--keep-dir=$(B)/"+instance.Path.rel(),
		"--compiler-bindir="+cudaHostToolchainGlobalRef+"/bin/clang",
		"-I"+osSdkRootGlobalRef+"/usr/include/x86_64-linux-gnu",
		"-Wno-deprecated-gpu-targets",
	)

	prefix := na.strList(
		wrapccPython3STR,
		(source("build/scripts/compile_cuda.py")).str(),
		internStr("--mtime"), mtimeVFS.str(),
		internStr("--custom-pid"), pidVFS.str(),
		internStr(cudaResourceGlobalRef+"/bin/nvcc"),
		internStr(nvccStdFlag),
	)

	cmdArgs := ArgChunks(na.chunkList(
		prefix,
		internStrList(staticNvcc),
		internStrList(nvccGencodeFlags),
		na.strList(argDashC.str(), (inVFS).str(), argDashO.str(), (outVFS).str()),
		blocks.includes,
		na.strList(internStr("--cflags")),
		p.CCHead,
		blocks.flags,
		blocks.cxxTail,
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
		Inputs:       na.inputList(in.IncludeInputs, []VFS{source("build/internal/platform/cuda/cuda_runtime_include.h")}),
		Outputs:      na.vfsList(outVFS),
		KV:           KV{P: pkCU, PC: pcLightGreen},
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    p.CCUsesResources,
		DepRefs:      append([]NodeRef{mtimeRef, pidRef}, in.ExtraDepRefs...),
	}

	return &SourceEmit{Ref: ctx.emit.emit(node), OutPath: outVFS}
}
