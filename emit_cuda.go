package main

import (
	"path/filepath"
	"strconv"
	"strings"
)

var (
	cudaKV        = KV{P: pkCU, PC: pcLightGreen}
	cudaHostFlags = internAnys([]string{"-D__NV_LEGACY_LAUNCH", "-Wno-unused-function", "-Wno-unused-parameter", "-Wno-deprecated-literal-operator"})
)

func (e *EmitContext) cudaEnv() EnvVars {
	c := e.cudaSettings()

	return e.ctx.na.envList(EnvVar{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}, EnvVar{Name: cudaPathEnv, Value: internV(c.Root, "/nvvm/bin:", c.Root, "/bin").any()})
}

func (e *EmitContext) nvccFlags() []ANY {
	return e.cudaSettings().NvccFlags
}

func (e *EmitContext) cudaCompileArgs(in ModuleCCInputs, mtime, pid VFS, flags []ANY, split bool) ArgChunks {
	na := e.ctx.na
	c := e.cudaSettings()
	head := na.anyList(wrapccPython3STR.any(), cudaCompileScriptVFS.any(), cudaMtimeFlagStr.any(), mtime.any(), cudaCustomPidFlagStr.any(), pid.any())

	var wrapper []ANY

	if c.SkipCompressedDebug {
		wrapper = append(wrapper, strYSkipCompressedDebugInfo.any())
	}

	if !split && c.Sanitize {
		wrapper = append(wrapper, strYSanitize.any())
	}

	blocks := compilerCommandBlocks(in.CCBlocks, true)
	nvcc := na.anyList(internV(c.Root, "/bin/nvcc").any(), internV("-std=c++", c.Std).any())

	return na.chunkList(head, na.anyList(wrapper...), nvcc, e.nvccFlags(), flags, blocks.includes,
		na.anyList(cudaCflagsStr.any()), e.instance.Platform.CCHead, blocks.flags[len(in.ClangWarnings):], blocks.cxxTail,
		na.anyList(internV("-std=c++", c.Std).any()), in.PerSourceCFlags)
}

func (e *EmitContext) emitLibraryCudaSource(meta SrcMeta, in ModuleCCInputs) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	srcVFS := e.resolveModuleSourceVFS(meta.Source, d.cc.SrcDirs)
	outVFS, inVFS := composeCCPaths(instance, e.moduleSourceRel(meta.Source), srcVFS, in, instance.Platform.objectSuffix())
	srcCV := e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainCC)
	runtimeCV := e.scanner.walkClosure(cudaRuntimeIncludeVFS, d.scanCtx, scanDomainCC)
	closure := na.dedupClosureChunks(srcCV, runtimeCV)
	mtimeRef, mtime := ctx.tool(cudaMtimeArg)
	pidRef, pid := ctx.tool(cudaCustomPidArg)
	env := e.cudaEnv()
	args := e.cudaCompileArgs(in, mtime, pid, na.anyList(argDashC.any(), inVFS.any(), argDashO.any(), outVFS.any()), false)
	inputs := na.inputList(na.vfsList(cudaCompileScriptVFS), na.vfsList(mtime, pid))

	inputs = appendInputChunks(na, inputs, closure)

	node := Node{Platform: instance.Platform, Cmds: na.cmdList(Cmd{CmdArgs: args, Env: env, Resources: resourceKeyUnion(e.cudaSettings().Resources, compilerCommandBlocks(in.CCBlocks, true).resources)}), Env: env,
		Inputs: inputs, BuildFromInputs: appendInputChunks(na, inputs[:1], inputs[2:]), Outputs: na.vfsList(outVFS), KV: &cudaKV, Resources: instance.Platform.CCUsesResources,
		DepRefs: na.refList(mtimeRef, pidRef)}

	if env := (Environment{s: &e.frame.ifEnv}); env.bool(envCudaNodeRAMRequirement) {
		ram := throw2(strconv.ParseFloat(strings.TrimPrefix(env.string(envCudaNodeRAMRequirement), "ram:"), 64))

		node.Requirements = &Requirements{CPU: 1, Network: nwRestricted, RAM: ram}
		node.KVExts = []KVExt{{Key: "local_ram_limiter", Val: "1"}}
	}

	ref := e.emitNode(node)

	e.collectObj(ref, outVFS, meta)
}

func appendInputChunks(na *NodeArenas, a, b InputChunks) InputChunks {
	chunks := na.inputs.alloc(len(a) + len(b))[:len(a)+len(b)]

	copy(chunks, a)
	copy(chunks[len(a):], b)
	na.inputs.commit(len(chunks))

	return InputChunks(chunks[:len(chunks):len(chunks)])
}

func (e *EmitContext) emitCudaSrcs(stmt CudaSrcsStmt) {
	architectures := strings.Split(stmt.Architectures, ":")

	for i, name := range architectures {
		_, arch, ok := strings.Cut(name, "_")

		if !ok || arch == "" {
			throwFmt("CUDA_SRCS: invalid architecture %q", name)
		}

		architectures[i] = arch
	}

	for i, src := range stmt.Sources {
		e.emitSplitCudaSource(src, architectures, stmt.EmbedPTX, stmt.DeclSeq+i*(len(architectures)+3))
	}
}

func (e *EmitContext) emitSplitCudaSource(src ANY, arches []string, embedPTX bool, seq int) {
	na, d := e.ctx.na, e.d
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	base := build(e.instance.Path.relString(), "/", cudaSourceBase(e.moduleSourceRel(src))).string()
	output := func(suffix string) VFS { return intern(base + suffix) }
	stubArch := arches[len(arches)-1]
	archList := stubArch

	if ch := archList[len(archList)-1]; ch < '0' || ch > '9' {
		archList = archList[:len(archList)-1]
	}

	archList += "0"

	mtimeRef, mtime := e.ctx.tool(cudaMtimeArg)
	pidRef, pid := e.ctx.tool(cudaCustomPidArg)
	in := e.ccInputsFor(CompileSpec{})
	env := e.cudaEnv()

	var sourceClosure, compileClosure InputChunks

	getClosures := func() {
		if sourceClosure != nil {
			return
		}

		srcCV := e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainCC)
		runtimeCV := e.scanner.walkClosure(cudaRuntimeIncludeVFS, d.scanCtx, scanDomainCC)

		sourceClosure = na.dedupClosureChunks(srcCV)
		compileClosure = na.dedupClosureChunks(srcCV, runtimeCV)
	}

	var images []VFS
	e.collectCPPStyleInputs(srcVFS, cudaCompileScriptVFS, cudaRuntimeIncludeVFS)

	for _, arch := range arches {
		outs := na.vfsList(output("."+arch+".cubin"), output("."+arch+".ptx"), output("."+arch+".cudafe1.stub.c"), output("."+arch+".module_id"))
		ref := e.ctx.emit.reserve()

		pending := e.pendingEmit(func() {
			getClosures()
			var flags []ANY

			if d.cuda.SplitSanitize {
				flags = append(flags, strYSanitize.any())
			}

			flags = append(flags, strGencode.any(), internV("arch=compute_", arch, ",code=sm_", arch).any(), strCubin.any(), argDashC.any(), srcVFS.any(), argDashO.any(), outs[0].any(), internV("-Xcicc=--orig_src_file_name=", srcVFS.string()).any())

			if d.cuda.Version != "11.4" {
				flags = append(flags, internV("-Xcicc=--orig_src_path_name=", srcVFS.relString()).any())
			}

			args := e.cudaCompileArgs(in, mtime, pid, na.anyList(flags...), true)

			args = na.chunkList(append(args, na.anyList(strUCudaArchList.any(), internV("-D__CUDA_ARCH_LIST__=", archList).any()))...)

			cmds := []Cmd{{CmdArgs: args, Env: env, Resources: resourceKeyUnion(e.cudaSettings().Resources, compilerCommandBlocks(in.CCBlocks, true).resources)}}

			for i, suffix := range []string{".ptx", ".cudafe1.stub.c", ".module_id"} {
				copyArgs := na.anyList(wrapccPython3STR.any(), copyFsToolsVFS.any(), argCopy.any(), internV(filepath.Base(base), suffix).any(), outs[i+1].any())

				cmds = append(cmds, Cmd{CmdArgs: na.chunkList(copyArgs), Cwd: e.instance.Path.rel().build(), Env: envVarsVCS})
			}

			inputs := na.inputList(na.vfsList(cudaCompileScriptVFS), e.ctx.scripts[copyFsToolsVFS.rel()], na.vfsList(mtime, pid))

			inputs = appendInputChunks(na, inputs, compileClosure)

			e.emitReservedNode(Node{Platform: e.instance.Platform, Cmds: na.cmdList(cmds...), Env: env, Inputs: inputs,
				BuildFromInputs: appendInputChunks(na, inputs[:2], inputs[3:]),
				Outputs:         outs, KV: &cudaKV, DepRefs: na.refList(mtimeRef, pidRef), Resources: e.instance.Platform.CCUsesResources}, ref)
		})

		for i, out := range outs {
			var leaves []VFS

			if i > 0 {
				leaves = na.vfsList(outs[0])
			}

			e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: ref, OnUse: pending, ProducerMainOut: outs[0], ClosureLeaves: leaves})
		}

		if embedPTX {
			images = append(images, outs[1])
		}

		images = append(images, outs[0], outs[3])
	}

	fatbin := output(".fatbin.c")
	fatRef := e.ctx.emit.reserve()

	fatPending := e.pendingEmit(func() {
		getClosures()

		script := buildScriptsFatbinaryWrapperPy
		args := []ANY{wrapccPython3STR.any(), script.any(), internV(d.cuda.Root, "/bin/fatbinary").any(), str64.any(), strCompressAll.any(), internV("--ident=", srcVFS.string()).any()}

		for _, image := range images {
			args = append(args, image.any())
		}

		args = append(args, internV("--embedded-fatbin=", fatbin.string()).any())

		inputs := appendInputChunks(na, na.inputList(na.vfsList(images...), na.vfsList(script)), sourceClosure)

		e.emitReservedNode(Node{Platform: e.instance.Platform, Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...)), Env: envVarsVCS, Resources: e.cudaSettings().RootResources}), Env: envVarsVCS,
			Inputs: inputs, Outputs: na.vfsList(fatbin), KV: &cudaKV, Resources: usesPython3}, fatRef)
	})

	e.register(GeneratedFileInfo{OutputPath: fatbin, ProducerRef: fatRef, OnUse: fatPending})

	hostOuts := na.vfsList(output(".ii"), output(".cudafe1.cpp"))
	hostRef := e.ctx.emit.reserve()
	moduleID := output("." + stubArch + ".module_id")
	stub := output("." + stubArch + ".cudafe1.stub.c")

	hostPending := e.pendingEmit(func() {
		getClosures()
		var flags []ANY

		if d.cuda.SplitSanitize {
			flags = append(flags, strYSanitize.any())
		}

		flags = append(flags, strPreprocess.any(), argDashC.any(), srcVFS.any(), argDashO.any(), hostOuts[0].any())

		args := e.cudaCompileArgs(in, mtime, pid, na.anyList(flags...), true)

		args = na.chunkList(append(args, na.anyList(strUCudaArch.any(), strUCudaArchList.any(), internV("-D__CUDA_ARCH_LIST__=", archList).any()))...)

		frontend := internAnys([]string{d.cuda.Root + "/bin/cudafe++", "--c++" + d.cuda.Std, "--clang", "--clang_version=" + e.instance.Platform.ClangVer + "0000", "-w", "--display_error_number",
			"--orig_src_file_name", srcVFS.relString(), "--unicode_source_kind=UTF-8", "--allow_managed", "--extended-lambda", "--relaxed_constexpr", "--m64", "--parse_templates",
			"--gen_c_file_name", hostOuts[1].string(), "--stub_file_name", filepath.Base(stub.relString()), "--module_id_file_name", moduleID.string(), hostOuts[0].string()})

		inputs := na.inputList(na.vfsList(cudaCompileScriptVFS), na.vfsList(mtime, pid), na.vfsList(moduleID), e.codegen.mustInfo(moduleID).ClosureLeaves)

		inputs = appendInputChunks(na, inputs, compileClosure)

		e.emitReservedNode(Node{Platform: e.instance.Platform, Cmds: na.cmdList(Cmd{CmdArgs: args, Env: env, Resources: resourceKeyUnion(e.cudaSettings().Resources, compilerCommandBlocks(in.CCBlocks, true).resources)}, Cmd{CmdArgs: na.chunkList(na.anyList(frontend...)), Env: envVarsVCS}), Env: env,
			BuildFromInputs: appendInputChunks(na, inputs[:1], inputs[2:]),
			Inputs:          inputs, Outputs: hostOuts, KV: &cudaKV, DepRefs: na.refList(mtimeRef, pidRef), Resources: e.instance.Platform.CCUsesResources}, hostRef)
	})

	e.register(GeneratedFileInfo{OutputPath: hostOuts[0], ProducerRef: hostRef, OnUse: hostPending})

	parsed := na.dirList(IncludeDirective{kind: includeQuoted, target: includeTarget(stub.rel().any())}, IncludeDirective{kind: includeQuoted, target: includeTarget(fatbin.rel().any())})

	e.register(GeneratedFileInfo{OutputPath: hostOuts[1], ProducerRef: hostRef, OnUse: hostPending, ProducerMainOut: hostOuts[0], ClosureLeaves: na.vfsList(hostOuts[0]), ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed}})
	e.enqueueSrc(SrcMeta{Source: hostOuts[1].any(), Prio: stmtPrioDefault, Seq: seq + len(arches) + 2, Compile: CompileSpec{CFlags: cudaHostFlags, FlatOutput: true}})
}

func (e *EmitContext) emitCudaDeviceLink(archiveName string, local CollectedObjs, plugin *VFS) NodeRef {
	na, d := e.ctx.na, e.d
	c := e.cudaSettings()
	archive := build(e.instance.Path.relString(), "/", archiveName)
	devlink := build(e.instance.Path.relString(), "/devlink", e.instance.Platform.objectSuffix())
	node := composeARNode(e.instance, archive, local.refs, local.outs, plugin, d.tc, e.ctx.host, na)
	args := []ANY{internV(c.Root, "/bin/nvcc").any()}

	args = append(args, e.nvccFlags()...)
	args = append(args, argDashO.any(), devlink.any(), strDlink.any())
	var inputs []VFS

	for _, src := range d.nvccDeviceSrcs {
		v := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
		obj, _ := composeCCPaths(e.instance, e.moduleSourceRel(src), v, newModuleCCInputs(&d.cc), e.instance.Platform.objectSuffix())

		args = append(args, obj.any())
		inputs = append(inputs, obj)
	}

	args = append(args, internV("-I", d.cuda.TargetRoot, "/include").any(), strCompilerOptions.any(), internStr(strings.Join(anyStrs(e.instance.Platform.CCHead), " ")).any())

	arCmd := node.Cmds[0]

	arCmd.CmdArgs = na.chunkList(append(arCmd.CmdArgs, na.anyList(devlink.any()))...)

	env := e.cudaEnv()

	node.Cmds = na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...)), Env: env, Resources: resourceKeyUnion(c.Resources, c.TargetResources)}, arCmd)
	node.Env = na.envList(append(append(EnvVars(nil), node.Env...), env[1])...)
	node.Outputs = na.vfsList(archive, devlink)
	node.Inputs = appendInputChunks(na, node.Inputs, na.inputList(na.vfsList(inputs...)))

	return e.emitNode(node)
}
