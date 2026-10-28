package main

var asKV = KV{P: pkAS, PC: pcLightGreen}

func (e *EmitContext) emitAS(srcVFS VFS, in ModuleCCInputs, hostP *Platform) (NodeRef, VFS, bool) {
	instance := e.instance
	na := e.ctx.na
	outVFS, inVFS := in.OutputStem, srcVFS

	if in.OutputStem != 0 {
		outVFS = build(in.OutputStem.relString(), ".o")
	} else {
		outVFS, inVFS = composeASPaths(instance, srcVFS)
	}

	if !e.submitOutput(outVFS) {
		return 0, outVFS, false
	}

	blocks := in.CCBlocks

	if blocks.asHead == nil {
		flags := commonCompilerFlagValue(in, instance.Platform)

		blocks.asHead = composeASModuleArgs(na, instance, in, flags.Args)
		blocks.asResources = flags.Resources
	}

	cmdArgs := na.chunkList(blocks.asHead, in.PerSourceCFlags,
		na.anyList(argDashC.any(), argDashO.any(), outVFS.any(), inVFS.any()), blocks.includes)

	env := hostP.toolEnv()

	node := NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: bldRootDirVFS, Resources: blocks.asResources,
			Env: env}),
		Env: env, Inputs: na.rootInputList(na.vfsList(in.IncludeView.self), in.IncludeView.bucketList()...), Outputs: na.vfsList(outVFS),
		KV:        &asKV,
		Resources: instance.Platform.UsesClangOnly},
	}

	return e.emitNode(node), outVFS, true
}

func composeASPaths(instance ModuleInstance, srcVFS VFS) (out, input VFS) {
	rel := composeSrcDirOutputRel(instance.Path.relString(), srcVFS.relString())

	return build(instance.Path.relString(), "/", rel, ".o"), srcVFS
}

func composeASModuleArgs(na *NodeArenas, instance ModuleInstance, in ModuleCCInputs, ownCFlags []ANY) []ANY {
	bundle := compileFlagBundleFor(instance.Platform)
	prologueArgs := 2 + len(bundle.ArchArgs) + len(instance.Platform.SysrootArgs)
	warnBundle := pickWarningFlags(in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)
	betweenBlocks := len(catboostOpenSourceDefine)

	betweenBlocks += len(in.ModuleScopeCFlags)

	fixed := prologueArgs + 2*len(debugPrefixMapFlags) + 2*len(xclangDebugCompilationDir) +
		len(bundle.CFlags) + len(warnBundle) + len(bundle.Defines) + len(ownCFlags) +
		len(bundle.NoLibcBlock) + betweenBlocks + len(bundle.NoLibcBlock) + len(in.SFlags)

	cmdArgs := na.anys.alloc(fixed)[:0]

	cmdArgs = append(cmdArgs, in.TC.CC.any(), instance.Platform.TargetArg.any())
	cmdArgs = appendAnyLists(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, instance.Platform.SysrootArgs...)

	if in.ForceConsistentDebug {
		cmdArgs = appendAnyLists(cmdArgs, debugPrefixMapFlags, xclangDebugCompilationDir)
	}

	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, warnBundle, bundle.Defines, ownCFlags, in.ModuleScopeCFlags, catboostOpenSourceDefineFor(instance.Platform))
	cmdArgs = appendAnyLists(cmdArgs, in.SFlags)
	na.anys.commit(len(cmdArgs))

	return cmdArgs[:len(cmdArgs):len(cmdArgs)]
}

func (e *EmitContext) emitLibraryAsmSource(meta SrcMeta, in ModuleCCInputs) {
	src := meta.Source
	ctx, d := e.ctx, e.d
	srcVFS := src.vfs()

	if srcVFS == 0 {
		srcVFS = e.sourceForEmission(meta)
	}

	asIn := in

	if asIn.FlatOutput && asIn.OutputStem == 0 {
		asIn.OutputStem = copyFileOutputVFS(e.instance.Path.relString(), src.string())
	}

	asIn.IncludeView = e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainCC)

	ref, outPath, accepted := e.emitAS(srcVFS, asIn, ctx.host)

	if accepted {
		e.collectObj(ref, outPath, meta)
	}
}
