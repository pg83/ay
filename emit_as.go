package main

import (
	"strings"
)

var asKV = KV{P: pkAS, PC: pcLightGreen}

func emitAS(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit *StreamingEmitter) (NodeRef, VFS) {
	na := emit.nodeArenas()
	outVFS, inVFS := composeASPaths(instance, srcRel, srcVFS, in)
	cmdArgs := composeASCmdArgs(instance, outVFS, inVFS, in)
	env := hostP.toolEnv()

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Cwd: strB,
			Env: env}),
		Env:          env,
		Inputs:       na.inputList(in.IncludeInputs),
		Outputs:      na.vfsList(outVFS),
		KV:           &asKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    instance.Platform.UsesClangOnly,
	}

	if len(in.ExtraDepRefs) > 0 {
		node.DepRefs = in.ExtraDepRefs
	}

	return emit.emit(node), outVFS
}

func composeASPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs) (out, input VFS) {
	if srcVFS.isSource() && srcVFS.rel() != instance.Path.rel()+"/"+srcRel {
		outputRel := composeSrcDirOutputRel(instance.Path.rel(), srcVFS.rel())

		return build(instance.Path.rel(), "/", outputRel, ".o"), srcVFS
	}

	var outRel string

	outName := srcRel + ".o"

	if extIsAsm(srcRel) {
		outName = strings.TrimSuffix(srcRel, ".asm") + ".o"
	}

	if strings.Contains(srcRel, "/") {
		outRel = instance.Path.rel() + "/_/" + outName
	} else {
		outRel = instance.Path.rel() + "/" + outName
	}

	return build(outRel), srcVFS
}

func composeASCmdArgs(instance ModuleInstance, outVFS, inVFS VFS, in ModuleCCInputs) []STR {
	bundle := compileFlagBundleFor(instance.Platform)
	prologueArgs := 2 + len(bundle.ArchArgs) + len(instance.Platform.SysrootArgs)
	warnBundle := pickWarningFlags(in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)
	ownCFlags := composeOwnAndPeerCFlagsAtOwnSlot(in.ModuleCompileEnv, instance.Platform)
	includes := composeASIncludes(in)
	betweenBlocks := len(catboostOpenSourceDefine)

	betweenBlocks += len(in.ModuleScopeCFlags)

	fixed := prologueArgs + len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		len(bundle.CFlags) + len(warnBundle) + len(bundle.Defines) + len(ownCFlags) +
		len(bundle.NoLibcBlock) + betweenBlocks + len(bundle.NoLibcBlock) + len(in.SFlags) + 4

	cmdArgs := make([]STR, 0, fixed+len(includes))

	cmdArgs = append(cmdArgs, in.TC.CC, instance.Platform.TargetArg)
	cmdArgs = appendArgStr(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, instance.Platform.SysrootArgs...)
	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, warnBundle, bundle.Defines, ownCFlags, in.ModuleScopeCFlags, catboostOpenSourceDefineFor(instance.Platform))
	cmdArgs = appendArgStr(cmdArgs, in.SFlags)
	cmdArgs = append(cmdArgs, argDashC.str(), argDashO.str(), (outVFS).str(), (inVFS).str())
	cmdArgs = append(cmdArgs, includes...)

	return cmdArgs
}

func composeASIncludes(in ModuleCCInputs) []STR {
	out := make([]STR, 0, len(ccIncludesPrefix)+len(in.AddIncl)+len(in.PeerAddInclGlobal))

	out = appendArgStr(out, ccIncludesPrefix)
	out = appendAddIncl(out, in.AddIncl, in.InclArgs)
	out = appendAddIncl(out, in.PeerAddInclGlobal, in.InclArgs)

	return out
}

func (e *EmitContext) emitLibraryAsmSource(meta SrcMeta) {
	src := meta.Source
	ctx, instance, d := e.ctx, e.instance, e.d
	srcVFS := src.vfs()
	srcRel := src.string()

	if srcVFS == 0 {
		srcVFS = e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	} else {
		srcRel = strings.TrimPrefix(srcVFS.rel(), instance.Path.rel()+"/")
	}

	in := e.ccInputsFor(srcVFS)
	asIn := in
	scanIn := in

	if len(d.asmAddIncl) > 0 {
		scanIn.AddIncl = dedup(in.AddIncl, d.asmAddIncl)
		scanIn.ScanCfg = newScanContext(ctx.parsers, scanIn.AddIncl, scanIn.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())

		asIn.AddIncl = scanIn.AddIncl
	}

	asIn.IncludeInputs = walkClosure(e.scanner, srcVFS, scanIn.ScanCfg)
	asIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, asIn.IncludeInputs)

	ref, outPath := emitAS(instance, srcRel, srcVFS, asIn, ctx.host, ctx.emit)

	e.collectObj(ref, outPath, meta)
}
