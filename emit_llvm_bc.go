package main

import "strings"

var (
	llvmBcKV  = KV{P: pkBC, PC: pcLightGreen}
	llvmBcKV2 = KV{P: pkLD, PC: pcLightRed}
	llvmBcKV3 = KV{P: pkOP, PC: pcYellow}
)

type LlvmCompileStmt struct {
	Kind      TOK
	Source    string
	Output    string
	ClangRoot string
	Options   []ANY
}

func (e *EmitContext) llvmCompileNode(src string, out VFS, compiler string, cxx bool, opts []ANY) Node {
	na, d := e.ctx.na, e.d
	input := e.llvmBcSourceInfo(src)
	wrapper := buildScriptsClangWrapperPy
	args := composeBCCompileCmd(d.tc.Python3.string(), wrapper.string(), compiler, e.instance.Platform, newModuleCCInputs(&d.cc), input, out, cxx)
	tail := len(args) - 5
	llvmOpts := strings.Fields(d.setVars[strLlvmOpts].string())

	if len(llvmOpts) > 0 {
		args = append(append(append([]ANY(nil), args[:tail]...), internAnys(llvmOpts)...), args[tail:]...)
	}

	args = append(args, opts...)

	closure := e.scanner.walkClosure(input, d.scanCtx, scanDomainCC)
	inputs := na.dedupClosureChunks(closure)

	inputs = na.inputList(na.vfsList(wrapper), inputs...)

	return Node{
		Platform: e.instance.Platform,
		Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(args), Env: envVarsVCS}),
		Env:      envVarsVCS, Inputs: inputs, InputClosures: []Closure{closure}, Outputs: na.vfsList(out),
		KV: &llvmBcKV, Resources: usesPython3Clang16,
	}
}

func (e *EmitContext) llvmClangRoot(root string) string {
	if path, ok := moduleRootedVFS(e.instance.Path.relString(), root); ok {
		return path.string()
	}

	return resolveResourceGlobalRef(root, e.peers.ResourceGlobals)
}

func (e *EmitContext) emitLlvmCompile(stmt LlvmCompileStmt) {
	out := copyFileOutputVFS(e.instance.Path.relString(), stmt.Output)

	var node Node

	if stmt.Kind == tokBpf || stmt.Kind == tokBpfStatic {
		node = e.bpfCompileNode(stmt, out)
	} else {
		compiler := e.llvmClangRoot(stmt.ClangRoot) + "/bin/clang++"

		node = e.llvmCompileNode(stmt.Source, out, compiler, true, stmt.Options)
	}

	ref := e.ctx.emit.reserve()
	pending := e.pendingEmit(func() { e.emitReservedNode(node, ref) })

	for _, output := range node.Outputs {
		e.register(GeneratedFileInfo{OutputPath: output, ProducerRef: ref, ProducerMainOut: out, OnUse: pending})
	}

	if stmt.Kind == tokBpfStatic && !e.producersOnly() {
		object := node.Outputs[1]

		e.codegen.use(object)
		e.collectObj(ref, object, SrcMeta{Source: object.any(), Prio: stmtPrioDefault})
	}
}

func (e *EmitContext) emitLlvmBcStmt(stmt *LlvmBcStmt) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	suffix := stmt.Suffix

	if suffix == "" && d.cc.ObjectSuffixStem != nil {
		suffix = "." + *d.cc.ObjectSuffixStem
	}

	const (
		optWrapper = "$(S)/build/scripts/llvm_opt_wrapper.py"
	)

	python := d.tc.Python3.string()
	env := envVarsVCS
	clangRoot := e.llvmClangRoot(stmt.ClangBCRoot)
	clangxx := clangRoot + "/bin/clang++"
	llvmLink := clangRoot + "/bin/llvm-link"
	opt := clangRoot + "/bin/opt"
	optWrapperVFS := intern(optWrapper)
	pendNodes := make([]Node, 0, len(stmt.Sources)+2)
	pendRefs := make([]NodeRef, 0, len(stmt.Sources)+2)
	bcRefs := make([]NodeRef, 0, len(stmt.Sources))
	bcPaths := make([]VFS, 0, len(stmt.Sources))
	actions := make([]*SourceActionInfo, 0, len(stmt.Sources)+2)
	linksCopy := false

	for _, src := range stmt.Sources {
		bcOut := build(e.llvmBcRootRelArcSrc(src), suffix, ".bc")
		cxx := !strings.HasSuffix(src, ".c")
		compiler := clangxx

		if !cxx {
			compiler = clangRoot + "/bin/clang"
		}

		node := e.llvmCompileNode(src, bcOut, compiler, cxx, nil)
		allInputs := node.Inputs

		for _, ch := range allInputs {
			for _, v := range ch {
				if v == copyFsToolsVFS {
					linksCopy = true
				}
			}
		}

		ref := ctx.emit.reserve()

		pendNodes = append(pendNodes, node)
		pendRefs = append(pendRefs, ref)
		bcRefs = append(bcRefs, ref)
		bcPaths = append(bcPaths, bcOut)
		actions = append(actions, &SourceActionInfo{DeclSeq: stmt.DeclSeq, Inputs: na.vfsList(e.llvmBcSourceInfo(src))})
	}

	mergedOut := build(instance.Path.relString(), "/", stmt.Name, "_merged", suffix, ".bc")
	ldArgs := []ANY{internStr(llvmLink).any()}

	for _, p := range bcPaths {
		ldArgs = append(ldArgs, p.any())
	}

	ldArgs = append(ldArgs, argDashO.any(), mergedOut.any())

	var mergeInputs InputChunks

	if linksCopy {
		mergeInputs = na.inputList(bcPaths, ctx.scripts[copyFsToolsVFS.rel()])
	} else {
		mergeInputs = na.inputList(bcPaths)
	}

	ldNode := Node{
		Platform:  instance.Platform,
		Cmds:      na.cmdList(Cmd{CmdArgs: na.chunkList(ldArgs), Env: env}),
		Env:       env,
		Inputs:    mergeInputs,
		Outputs:   na.vfsList(mergedOut),
		KV:        &llvmBcKV2,
		DepRefs:   append([]NodeRef(nil), bcRefs...),
		Resources: usesPython3Clang16,
	}

	ldRef := ctx.emit.reserve()

	pendNodes = append(pendNodes, ldNode)
	pendRefs = append(pendRefs, ldRef)
	actions = append(actions, &SourceActionInfo{DeclSeq: stmt.DeclSeq, Inputs: bcPaths})

	optOutName := stmt.Name + "_optimized" + suffix + ".bc"
	optOut := build(instance.Path.relString(), "/", optOutName)
	optArgs := []ANY{internStr(python).any(), internStr(optWrapper).any(), internStr(opt).any(), mergedOut.any(), argDashO.any(), optOut.any()}
	passes := []string{"default<O2>", "globalopt", "globaldce"}

	if len(stmt.Symbols) > 0 {
		passes = append(passes, "internalize")
		optArgs = append(optArgs, internV("-internalize-public-api-list=", strings.Join(stmt.Symbols, "#")).any())
	}

	optArgs = append(optArgs, internV(`-passes="`, strings.Join(passes, ","), `"`).any())

	optNode := Node{
		Platform:  instance.Platform,
		Cmds:      na.cmdList(Cmd{CmdArgs: na.chunkList(optArgs), Env: env}),
		Env:       env,
		Inputs:    na.inputList(na.vfsList(mergedOut), na.vfsList(optWrapperVFS)),
		Outputs:   na.vfsList(optOut),
		KV:        &llvmBcKV3,
		DepRefs:   na.refList(ldRef),
		Resources: usesPython3Clang16,
	}

	opRef := ctx.emit.reserve()

	pendNodes = append(pendNodes, optNode)
	pendRefs = append(pendRefs, opRef)
	actions = append(actions, &SourceActionInfo{DeclSeq: stmt.DeclSeq, Inputs: na.vfsList(mergedOut)})

	pe := func() {
		for i := range pendNodes {
			e.emitReservedNode(pendNodes[i], pendRefs[i])
		}
	}

	pending := e.pendingEmit(pe)

	if stmt.GenerateMachineCode {
		return
	}

	for i, node := range pendNodes {
		e.register(GeneratedFileInfo{
			SourceAction: actions[i],
			OutputPath:   node.Outputs[0],
			ProducerRef:  pendRefs[i],
			OnUse:        pending,
		})
	}

	e.resources = append(e.resources, ResourceEntry{
		DeclSeq:   stmt.DeclSeq,
		Path:      optOutName,
		Key:       "/llvm_bc/" + stmt.Name,
		EndsBatch: true,
	})
}

func composeBCCompileCmd(python, clangWrapper, clangBC string, platform *Platform, in ModuleCCInputs, inVFS, outVFS VFS, cxx bool) []ANY {
	bundle := compileFlagBundleFor(platform)
	warningBundle := pickWarningFlags(in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)
	ownCFlags := composeOwnAndPeerCFlagsAtOwnSlot(in, platform)
	ownGlobalBucket := composeOwnAndPeerGlobalBucket(in.ModuleCompileEnv)

	args := make([]ANY, 0, 200+len(in.AddIncl)+len(in.PeerAddInclGlobal)+
		len(bundle.Defines)+len(ownCFlags)+2*len(bundle.NoLibcBlock)+
		len(in.ModuleScopeCFlags)+len(in.CXXFlags)+len(platform.CXXFlags)+len(ownGlobalBucket)+
		len(bundle.ArchArgs)+len(bundle.CFlags)+len(warningBundle))

	args = append(args, internStr(python).any(), internStr(clangWrapper).any(), argNo.any(), internStr(clangBC).any())
	args = appendAnyLists(args, ccIncludesPrefix)
	args = appendAddIncl(args, in.AddIncl, in.InclArgs)

	peerAddIncl := in.PeerAddInclGlobal

	if len(peerAddIncl) > 0 && peerAddIncl[0] == googleapisCommonProtosAddIncl {
		args = append(args, in.InclArgs.arg(peerAddIncl[0]).any())
		peerAddIncl = peerAddIncl[1:]
	}

	args = appendAddIncl(args, peerAddIncl, in.InclArgs)
	args = appendCompileFlagPipeline(args, bundle, warningBundle, bundle.Defines, ownCFlags, in.ModuleScopeCFlags, catboostOpenSourceDefineFor(platform))

	if cxx {
		ownExtras := compilerCommandFlags(in.CXXFlags, in.CompilerEnv).Args

		if len(platform.CXXFlags) > 0 {
			ownExtras = concat(ownExtras, platform.CXXFlags)
		}

		args = appendCxxStdAndOwn(args, true, in.Flags.NoCompilerWarnings, true, ownExtras)
		args = appendAnyLists(args, ownGlobalBucket, catboostOpenSourceDefineFor(platform), coreCxxFlags(platform), in.CXXFlagsVar)
	}

	args = append(args, platform.TargetArg.any())
	args = appendAnyLists(args, bundle.ArchArgs, platform.SysrootArgs)

	if cxx {
		args = append(args, argWnoUnknownWarningOption.any())
	}

	args = append(args, argEmitLlvm.any(), argDashC.any(), inVFS.any(), argDashO.any(), outVFS.any())

	return args
}

func (e *EmitContext) llvmBcSourceInfo(src string) VFS {
	ctx, instance := e.ctx, e.instance
	reg := e.codegen
	inputVFS := moduleInputCandidateVFS(instance.Path.relString(), src)

	if inputVFS.isSource() || reg.use(inputVFS) != nil {
		return inputVFS
	}

	if buildVFS := e.generatedModuleSourceVFS(src); buildVFS != nil {
		reg.use(*buildVFS)

		return *buildVFS
	}

	return e.requireProducedInput("LLVM_BC source", src, copyFileInputVFS(ctx.fs, instance.Path, src))
}

func (e *EmitContext) llvmBcRootRelArcSrc(src string) string {
	ctx, instance := e.ctx, e.instance

	if vfs, rooted := moduleRootedVFS(instance.Path.relString(), src); rooted {
		return vfs.relString()
	}

	if reg := e.codegen; reg.lookup(moduleInputCandidateVFS(instance.Path.relString(), src)) != nil {
		return src
	}

	if e.generatedModuleSourceVFS(src) != nil {
		return src
	}

	if vfs, ok := sourceInputVFS(ctx.fs, instance.Path, src); ok {
		return vfs.relString()
	}

	return src
}
