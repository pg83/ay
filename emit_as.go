package main

import (
	"strings"
)

var asKV = KV{P: pkAS, PC: pcLightGreen}

func emitAS(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit *StreamingEmitter) (NodeRef, VFS) {
	na := emit.nodeArenas()
	outVFS, inVFS := composeASPaths(instance, srcRel, srcVFS, in)
	cmdArgs := composeASCmdArgs(na, instance, outVFS, inVFS, in)
	env := hostP.toolEnv()

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Cwd: bldRootDirVFS,
			Env: env}),
		Env:          env,
		Inputs:       na.inputList(na.vfsList(in.IncludeView.self), in.IncludeView.buckets...),
		Outputs:      na.vfsList(outVFS),
		KV:           &asKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    instance.Platform.UsesClangOnly,
	}

	if len(in.ExtraDepRefs) > 0 {
		node.DepRefs = in.ExtraDepRefs
	}

	return emit.emitNode(node), outVFS
}

func composeASPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs) (out, input VFS) {
	if srcVFS.isSource() && srcVFS.relString() != instance.Path.relString()+"/"+srcRel {
		outputRel := composeSrcDirOutputRel(instance.Path.relString(), srcVFS.relString())

		return build(instance.Path.relString(), "/", outputRel, ".o"), srcVFS
	}

	var outRel string

	outName := srcRel + ".o"

	if extIsAsm(srcRel) {
		outName = strings.TrimSuffix(srcRel, ".asm") + ".o"
	}

	if strings.Contains(srcRel, "/") {
		outRel = instance.Path.relString() + "/_/" + outName
	} else {
		outRel = instance.Path.relString() + "/" + outName
	}

	return build(outRel), srcVFS
}

func composeASCmdArgs(na *NodeArenas, instance ModuleInstance, outVFS, inVFS VFS, in ModuleCCInputs) []ANY {
	bundle := compileFlagBundleFor(instance.Platform)
	prologueArgs := 2 + len(bundle.ArchArgs) + len(instance.Platform.SysrootArgs)
	warnBundle := pickWarningFlags(in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)
	ownCFlags := composeOwnAndPeerCFlagsAtOwnSlot(in.ModuleCompileEnv, instance.Platform)
	includes := composeASIncludes(in)
	betweenBlocks := len(catboostOpenSourceDefine)

	betweenBlocks += len(in.ModuleScopeCFlags)

	fixed := prologueArgs + 2*len(debugPrefixMapFlags) + 2*len(xclangDebugCompilationDir) +
		len(bundle.CFlags) + len(warnBundle) + len(bundle.Defines) + len(ownCFlags) +
		len(bundle.NoLibcBlock) + betweenBlocks + len(bundle.NoLibcBlock) + len(in.SFlags) + len(in.PerSourceCFlags) + 4

	cmdArgs := na.anys.alloc(fixed + len(includes))[:0]

	cmdArgs = append(cmdArgs, in.TC.CC.any(), instance.Platform.TargetArg.any())
	cmdArgs = appendAnyLists(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, instance.Platform.SysrootArgs...)

	if in.ForceConsistentDebug {
		cmdArgs = appendAnyLists(cmdArgs, debugPrefixMapFlags, xclangDebugCompilationDir)
	}

	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, warnBundle, bundle.Defines, ownCFlags, in.ModuleScopeCFlags, catboostOpenSourceDefineFor(instance.Platform))
	cmdArgs = appendAnyLists(cmdArgs, in.SFlags)
	cmdArgs = appendAnyLists(cmdArgs, in.PerSourceCFlags)
	cmdArgs = append(cmdArgs, argDashC.any(), argDashO.any(), outVFS.any(), inVFS.any())
	cmdArgs = append(cmdArgs, includes...)
	na.anys.commit(len(cmdArgs))

	return cmdArgs[:len(cmdArgs):len(cmdArgs)]
}

func composeASIncludes(in ModuleCCInputs) []ANY {
	out := make([]ANY, 0, len(ccIncludesPrefix)+len(in.AddIncl)+len(in.PeerAddInclGlobal))

	out = appendAnyLists(out, ccIncludesPrefix)
	out = appendAddIncl(out, in.AddIncl, in.InclArgs)
	out = appendAddIncl(out, in.PeerAddInclGlobal, in.InclArgs)

	return out
}

func (e *EmitContext) emitLibraryAsmSource(meta SrcMeta, in ModuleCCInputs) {
	src := meta.Source
	ctx, instance, d := e.ctx, e.instance, e.d
	srcVFS := src.vfs()
	srcRel := src.string()

	if srcVFS == 0 {
		srcVFS = e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	} else {
		srcRel = trimModulePrefix(srcVFS.relString(), instance.Path.relString())
	}

	asIn := in
	scanIn := in

	if len(d.asmAddIncl) > 0 {
		scanIn.AddIncl = dedup(in.AddIncl, d.asmAddIncl)
		asIn.AddIncl = scanIn.AddIncl
	}

	asIn.IncludeView = e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainAsm)
	asIn.ExtraDepRefs = resolveCodegenDepRefsInclView(ctx, instance, ctx.na, asIn.IncludeView)

	ref, outPath := emitAS(instance, srcRel, srcVFS, asIn, ctx.host, ctx.emit)

	e.collectObj(ref, outPath, meta)
}
