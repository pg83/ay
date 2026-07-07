package main

import "strings"

var (
	llvmBcKV  = KV{P: pkBC, PC: pcLightGreen}
	llvmBcKV2 = KV{P: pkLD, PC: pcLightRed}
	llvmBcKV3 = KV{P: pkOP, PC: pcYellow}
)

func (e *EmitContext) emitLlvmBcStmt(stmt *LlvmBcStmt) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na

	const (
		clangWrapper = "$(S)/build/scripts/clang_wrapper.py"
		optWrapper   = "$(S)/build/scripts/llvm_opt_wrapper.py"
	)

	python := d.tc.Python3.string()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	reqs := Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)}
	clangRoot := resolveResourceGlobalRef(stmt.ClangBCRoot, e.peers.ResourceGlobals)
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
		inputVFS, producer := e.llvmBcSourceInfo(src)
		in := e.ccInputsFor(inputVFS)
		bcOut := build(e.llvmBcRootRelArcSrc(src), stmt.Suffix, ".bc")
		bcArgs := composeBCCompileCmd(python, clangWrapper, clangxx, instance.Platform, in, inputVFS, bcOut)
		cv := walkClosure(e.scanner, inputVFS, in.ScanCfg)
		deps := resolveCodegenDepRefsInclView(ctx, instance, ctx.na, cv, depRefs(producer)...)
		allInputs := na.inputList(na.vfsList(clangWrapperVFS, cv.self), cv.buckets...)

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

		node := Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkListSTR(bcArgs), Env: env}),
			Env:          env,
			Inputs:       allInputs,
			Outputs:      na.vfsList(bcOut),
			KV:           &llvmBcKV,
			Requirements: reqs,
			DepRefs:      deps,
			Resources:    usesPython3Clang16,
		}

		ref := ctx.emit.emitNode(node)

		bcRefs = append(bcRefs, ref)
		bcPaths = append(bcPaths, bcOut)
	}

	mergedOut := build(instance.Path.relString(), "/", stmt.Name, "_merged", stmt.Suffix, ".bc")
	ldArgs := []STR{internStr(llvmLink)}

	for _, p := range bcPaths {
		ldArgs = append(ldArgs, (p).fullSTR())
	}

	ldArgs = append(ldArgs, argDashO.str(), (mergedOut).fullSTR())

	mergeInputs := na.inputList(bcPaths)

	if linksCopy {
		mergeInputs = append(mergeInputs, ctx.scripts[copyFsToolsVFS])
	}

	ldNode := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkListSTR(ldArgs), Env: env}),
		Env:          env,
		Inputs:       mergeInputs,
		Outputs:      na.vfsList(mergedOut),
		KV:           &llvmBcKV2,
		Requirements: reqs,
		DepRefs:      append([]NodeRef(nil), bcRefs...),
		Resources:    usesPython3Clang16,
	}

	ldRef := ctx.emit.emitNode(ldNode)
	optOutName := stmt.Name + "_optimized" + stmt.Suffix + ".bc"
	optOut := build(instance.Path.relString(), "/", optOutName)
	optArgs := []STR{internStr(python), internStr(optWrapper), internStr(opt), (mergedOut).fullSTR(), argDashO.str(), (optOut).fullSTR()}
	passes := []string{"default<O2>", "globalopt", "globaldce"}

	if len(stmt.Symbols) > 0 {
		passes = append(passes, "internalize")
		optArgs = append(optArgs, internV("-internalize-public-api-list=", strings.Join(stmt.Symbols, "#")))
	}

	optArgs = append(optArgs, internV(`-passes="`, strings.Join(passes, ","), `"`))

	optInputs := make([]VFS, 0, 2+len(bcSourceInputs))

	optInputs = append(optInputs, mergedOut)
	optInputs = append(optInputs, optWrapperVFS)

	optChunks := na.inputList(concat(optInputs, bcSourceInputs))

	optNode := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkListSTR(optArgs), Env: env}),
		Env:          env,
		Inputs:       optChunks,
		Outputs:      na.vfsList(optOut),
		KV:           &llvmBcKV3,
		Requirements: reqs,
		DepRefs:      []NodeRef{ldRef},
		Resources:    usesPython3Clang16,
	}

	opRef := ctx.emit.emitNode(optNode)

	if stmt.GenerateMachineCode {
		return
	}

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:    optOut,
		ProducerRef:   opRef,
		GeneratorRefs: nil,
	})

	e.resources = append(e.resources, ResourceEntry{
		Path:      optOutName,
		Key:       "/llvm_bc/" + stmt.Name,
		EndsBatch: true,
	})
}

func composeBCCompileCmd(python, clangWrapper, clangBC string, platform *Platform, in ModuleCCInputs, inVFS, outVFS VFS) []STR {
	bundle := compileFlagBundleFor(platform)
	warningBundle := pickWarningFlags(in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)
	ownCFlags := composeOwnAndPeerCFlagsAtOwnSlot(in.ModuleCompileEnv, platform)
	ownGlobalBucket := composeOwnAndPeerGlobalBucket(in.ModuleCompileEnv, true)
	ownExtras := in.CXXFlags

	if len(platform.CXXFlags) > 0 {
		ownExtras = concat(ownExtras, platform.CXXFlags)
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
	args = append(args, argWnoUnknownWarningOption.str(), argEmitLlvm.str(), argDashC.str(), (inVFS).fullSTR(), argDashO.str(), (outVFS).fullSTR())

	return args
}

func (e *EmitContext) llvmBcSourceInfo(src string) (inputVFS VFS, producer NodeRef) {
	ctx, instance := e.ctx, e.instance
	reg := e.codegen
	outVFS := copyFileOutputVFS(instance.Path.relString(), src)

	if info := reg.lookup(outVFS); info != nil {
		return outVFS, info.ProducerRef
	}

	if buildVFS := e.generatedModuleSourceVFS(src); buildVFS != nil {
		ref := NodeRef(0)

		if info := reg.lookup(*buildVFS); info != nil {
			ref = info.ProducerRef
		}

		return *buildVFS, ref
	}

	return e.requireProducedInput("LLVM_BC source", src, copyFileInputVFS(ctx.fs, instance.Path, src)), NodeRef(0)
}

func (e *EmitContext) llvmBcRootRelArcSrc(src string) string {
	ctx, instance := e.ctx, e.instance

	if reg := e.codegen; reg.lookup(copyFileOutputVFS(instance.Path.relString(), src)) != nil {
		return src
	}

	if e.generatedModuleSourceVFS(src) != nil {
		return src
	}

	if sourceInputVFS(ctx.fs, instance.Path, src) != nil {
		return instance.Path.relString() + "/" + src
	}

	return src
}
