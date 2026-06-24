package main

import "strings"

func emitLLVMBC(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs, resourceGlobals []ResourceDecl) {
	na := ctx.na

	if len(d.llvmBc) == 0 {
		return
	}

	const (
		clangWrapper = "$(S)/build/scripts/clang_wrapper.py"
		optWrapper   = "$(S)/build/scripts/llvm_opt_wrapper.py"
	)
	python := d.tc.Python3.string()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	reqs := Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)}

	for _, stmt := range d.llvmBc {
		clangRoot := resolveResourceGlobalRef(stmt.ClangBCRoot, resourceGlobals)
		clangxx := clangRoot + "/bin/clang++"
		llvmLink := clangRoot + "/bin/llvm-link"
		opt := clangRoot + "/bin/opt"

		clangWrapperVFS := intern(clangWrapper)
		optWrapperVFS := intern(optWrapper)

		var bcSourceInputs []VFS

		bcRefs := make([]NodeRef, 0, len(stmt.Sources))
		bcPaths := make([]VFS, 0, len(stmt.Sources))

		linksCopy := false

		for _, src := range stmt.Sources {
			inputVFS, producer := llvmBcSourceInfo(ctx, instance, src)
			bcOut := build(llvmBcRootRelArcSrc(ctx, instance, src) + stmt.Suffix + ".bc")
			bcArgs := composeBCCompileCmd(python, clangWrapper, clangxx, instance.Platform, in, inputVFS, bcOut)

			closure := walkClosure(ctx.scannerFor(instance), inputVFS, in.ScanCfg)

			deps := depRefs(producer)

			if extra := resolveCodegenDepRefs(ctx, instance, closure, deps...); len(extra) > 0 {
				deps = append(deps, extra...)
			}

			allInputs := na.inputList(na.vfsList(clangWrapperVFS),
				closure)

			for _, ch := range allInputs {
				for _, v := range ch {
					if v.isSource() {
						bcSourceInputs = append(bcSourceInputs, v)
					}

					if v == copyFsToolsVFS {
						linksCopy = true
					}
				}
			}

			node := &Node{
				Platform:     instance.Platform,
				Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(bcArgs), Env: env}),
				Env:          env,
				Inputs:       allInputs,
				Outputs:      na.vfsList(bcOut),
				KV:           &llvmBcKV,
				Requirements: reqs,
				DepRefs:      deps,
				Resources:    usesPython3Clang16,
			}
			ref := ctx.emit.emit(node)
			bcRefs = append(bcRefs, ref)
			bcPaths = append(bcPaths, bcOut)
		}

		mergedOut := build(instance.Path.rel() + "/" + stmt.Name + "_merged" + stmt.Suffix + ".bc")
		ldArgs := []STR{internStr(llvmLink)}

		for _, p := range bcPaths {
			ldArgs = append(ldArgs, (p).str())
		}

		ldArgs = append(ldArgs, argDashO.str(), (mergedOut).str())
		mergeInputs := na.inputList(bcPaths)

		if linksCopy {
			mergeInputs = append(mergeInputs, ctx.scripts[copyFsToolsVFS])
		}

		ldNode := &Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(ldArgs), Env: env}),
			Env:          env,
			Inputs:       mergeInputs,
			Outputs:      na.vfsList(mergedOut),
			KV:           &llvmBcKV2,
			Requirements: reqs,
			DepRefs:      append([]NodeRef(nil), bcRefs...),
			Resources:    usesPython3Clang16,
		}
		ldRef := ctx.emit.emit(ldNode)

		optOutName := stmt.Name + "_optimized" + stmt.Suffix + ".bc"
		optOut := build(instance.Path.rel() + "/" + optOutName)
		optArgs := []STR{internStr(python), internStr(optWrapper), internStr(opt), (mergedOut).str(), argDashO.str(), (optOut).str()}
		passes := []string{"default<O2>", "globalopt", "globaldce"}

		if len(stmt.Symbols) > 0 {
			passes = append(passes, "internalize")
			optArgs = append(optArgs, internStr("-internalize-public-api-list="+strings.Join(stmt.Symbols, "#")))
		}

		optArgs = append(optArgs, internStr(`-passes="`+strings.Join(passes, ",")+`"`))

		optInputs := make([]VFS, 0, 2+len(bcSourceInputs))
		optInputs = append(optInputs, mergedOut)
		optInputs = append(optInputs, optWrapperVFS)
		optChunks := na.inputList(dedupVFS(optInputs, bcSourceInputs))

		optNode := &Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(optArgs), Env: env}),
			Env:          env,
			Inputs:       optChunks,
			Outputs:      na.vfsList(optOut),
			KV:           &llvmBcKV3,
			Requirements: reqs,
			DepRefs:      []NodeRef{ldRef},
			Resources:    usesPython3Clang16,
		}
		opRef := ctx.emit.emit(optNode)

		if stmt.GenerateMachineCode {
			continue
		}

		ensureResourcePeer(instance.Path.rel(), d)

		registerBoundGeneratedParsedOutput(ctx, instance, pkOP, optOut, nil, opRef, nil)

		d.resources = append(d.resources, ResourceEntry{
			Path:      optOutName,
			Key:       "/llvm_bc/" + stmt.Name,
			EndsBatch: true,
		})
	}
}

func composeBCCompileCmd(python, clangWrapper, clangBC string, platform *Platform, in ModuleCCInputs, inVFS, outVFS VFS) []STR {
	bundle := compileFlagBundleFor(platform)
	warningBundle := pickWarningFlags(in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)

	ownCFlags := composeOwnAndPeerCFlagsAtOwnSlot(in, platform)
	ownGlobalBucket := composeOwnAndPeerGlobalBucket(in, true)

	ownExtras := in.CXXFlags

	if len(platform.CXXFlags) > 0 {
		ownExtras = append(append([]ARG{}, ownExtras...), platform.CXXFlags...)
	}

	args := make([]STR, 0, 200+len(in.AddIncl)+len(in.PeerAddInclGlobal)+
		len(bundle.Defines)+len(ownCFlags)+2*len(bundle.NoLibcBlock)+
		len(in.ModuleScopeCFlags)+len(ownExtras)+len(ownGlobalBucket)+
		len(bundle.ArchArgs)+len(bundle.CFlags)+len(warningBundle))

	args = append(args, internStr(python), internStr(clangWrapper), argNo.str(), internStr(clangBC))

	args = appendArgStr(args, ccIncludesPrefix)
	args = appendAddIncl(args, in.AddIncl, in.InclArgs)
	peerAddIncl := in.PeerAddInclGlobal

	if len(peerAddIncl) > 0 && peerAddIncl[0] == googleapisCommonProtosAddIncl {
		args = append(args, in.InclArgs.arg(peerAddIncl[0]))
		peerAddIncl = peerAddIncl[1:]
	}

	args = appendAddIncl(args, peerAddIncl, in.InclArgs)

	args = appendCompileFlagPipeline(args, bundle, warningBundle, bundle.Defines, ownCFlags, in.ModuleScopeCFlags, catboostOpenSourceDefineFor(platform))

	args = appendCxxStdAndOwn(args, true, in.Flags.NoCompilerWarnings, true, ownExtras)

	args = appendArgStr(args, ownGlobalBucket, catboostOpenSourceDefineFor(platform), composePostCatboostBucket(ownGlobalBucket))

	args = append(args, platform.TargetArg)
	args = appendArgStr(args, bundle.ArchArgs)
	args = append(args, argDashBBin)

	args = append(args, argWnoUnknownWarningOption.str(), argEmitLlvm.str(), argDashC.str(), (inVFS).str(), argDashO.str(), (outVFS).str())

	return args
}

func llvmBcSourceInfo(ctx *GenCtx, instance ModuleInstance, src string) (inputVFS VFS, producer NodeRef) {
	reg := codegenRegForInstance(ctx, instance)

	outVFS := copyFileOutputVFS(instance.Path.rel(), src)

	if info := reg.lookup(outVFS); info != nil {
		return outVFS, info.ProducerRef
	}

	if buildVFS := generatedModuleSourceVFS(ctx, instance, src); buildVFS != nil {
		ref := NodeRef(0)

		if info := reg.lookup(*buildVFS); info != nil {
			ref = info.ProducerRef
		}

		return *buildVFS, ref
	}

	return copyFileInputVFS(ctx.fs, instance.Path.rel(), src), NodeRef(0)
}

func llvmBcRootRelArcSrc(ctx *GenCtx, instance ModuleInstance, src string) string {
	if reg := codegenRegForInstance(ctx, instance); reg.lookup(copyFileOutputVFS(instance.Path.rel(), src)) != nil {
		return src
	}

	if generatedModuleSourceVFS(ctx, instance, src) != nil {
		return src
	}

	if sourceInputVFS(ctx.fs, instance.Path.rel(), src) != nil {
		return instance.Path.rel() + "/" + src
	}

	return src
}

var (
	llvmBcKV  = KV{P: pkBC, PC: pcLightGreen}
	llvmBcKV2 = KV{P: pkLD, PC: pcLightRed}
	llvmBcKV3 = KV{P: pkOP, PC: pcYellow}
)
