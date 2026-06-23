package main

var (
	ldLinkDynLibPath = ldLinkDynLibVFS.string()
)

func emitDynamicLibrary(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *ModuleEmitResult {
	na := ctx.na

	if len(d.moduleStmt.Args) == 0 {
		throwFmt("gen: %s DYNAMIC_LIBRARY requires a basename argument", instance.Path.rel())
	}

	if len(d.dynamicLibraryFrom) == 0 {
		throwFmt("gen: %s DYNAMIC_LIBRARY requires DYNAMIC_LIBRARY_FROM(...)", instance.Path.rel())
	}

	if d.exportsScript == nil {
		throwFmt("gen: %s DYNAMIC_LIBRARY requires EXPORTS_SCRIPT(...)", instance.Path.rel())
	}

	dynLibRPathHelperPeers := []string{"build/platform/local_so"}
	rpathHelperSet := make(map[string]struct{}, len(dynLibRPathHelperPeers))

	for _, p := range dynLibRPathHelperPeers {
		rpathHelperSet[p] = struct{}{}
	}

	peerPaths := make([]string, 0, 1+len(d.dynamicLibraryFrom))

	if !effectiveNoPlatform(d.flags) {
		peerPaths = append(peerPaths, "build/cow/on")
	}

	for _, p := range d.dynamicLibraryFrom {
		if _, helper := rpathHelperSet[p.string()]; helper {
			continue
		}

		peerPaths = append(peerPaths, p.string())
	}

	seen := make(map[string]struct{}, len(peerPaths)+len(dynLibRPathHelperPeers))
	resolved := make([]*ModuleEmitResult, 0, len(peerPaths))

	for _, p := range peerPaths {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		peerInstance := derivePeerInstance(ctx, instance, d, p)
		resolved = append(resolved, genModule(ctx, peerInstance))
	}

	rpathOnly := make([]*ModuleEmitResult, 0, len(dynLibRPathHelperPeers))

	for _, p := range dynLibRPathHelperPeers {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		peerInstance := derivePeerInstance(ctx, instance, d, p)
		rpathOnly = append(rpathOnly, genModule(ctx, peerInstance))
	}

	var resourceGlobals []ResourceDecl
	deduper.reset()

	for _, pr := range resolved {
		for _, decl := range pr.ResourceGlobalClosure {
			if deduper.add(VFS(decl.GlobalVar)) {
				resourceGlobals = append(resourceGlobals, decl)
			}
		}
	}

	peerArchiveRefs := make([]NodeRef, 0, len(resolved))
	peerArchivePaths := make([]VFS, 0, len(resolved))

	for _, pr := range resolved {
		if pr.ARPath != nil {
			peerArchiveRefs = append(peerArchiveRefs, pr.ARRef)
			peerArchivePaths = append(peerArchivePaths, *pr.ARPath)
		}
	}

	var peerAddInclGlobal []VFS
	deduper.reset()

	for _, pr := range resolved {
		for _, p := range pr.AddInclGlobal {
			if deduper.add(p) {
				peerAddInclGlobal = append(peerAddInclGlobal, p)
			}
		}
	}

	var cFlagsSeen BitSet
	var cxxFlagsSeen BitSet
	var cOnlyFlagsSeen BitSet
	var rpathFlagsSeen BitSet
	var peerCFlagsGlobal []ARG
	var peerCXXFlagsGlobal []ARG
	var peerCOnlyFlagsGlobal []ARG
	var peerRPathFlagsGlobal []ARG

	for _, pr := range resolved {
		addEachARG(&cFlagsSeen, &peerCFlagsGlobal, pr.CFlagsGlobal)
		addEachARG(&cxxFlagsSeen, &peerCXXFlagsGlobal, pr.CXXFlagsGlobal)
		addEachARG(&cOnlyFlagsSeen, &peerCOnlyFlagsGlobal, pr.COnlyFlagsGlobal)
		addEachARG(&rpathFlagsSeen, &peerRPathFlagsGlobal, pr.RPathFlagsGlobal)
	}

	for _, pr := range rpathOnly {
		addEachARG(&rpathFlagsSeen, &peerRPathFlagsGlobal, pr.RPathFlagsGlobal)
	}

	pluginRefs := []NodeRef{}
	pluginPaths := []VFS{}
	deduper.reset()

	for _, pr := range resolved {
		for i, pp := range pr.LDPluginPaths {
			if deduper.add(pp) {
				pluginRefs = append(pluginRefs, pr.LDPluginRefs[i])
				pluginPaths = append(pluginPaths, pp)
			}
		}
	}

	d.tc = resolveModuleToolchain(resourceGlobals, instance.Platform.ClangVer)

	fixElfRef, fixElfPath := ctx.tool(argToolsFixElf)

	outputName := "lib" + d.moduleStmt.Args[0].string() + ".so"
	outputPath := build(instance.Path.rel() + "/" + outputName).string()
	vcsCPath := build(instance.Path.rel() + "/__vcs_version__.c").string()
	vcsOPath := build(instance.Path.rel() + "/__vcs_version__.c.pic.o").string()

	cmd0 := composeLDCmdVcsInfo(d.tc, vcsCPath)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, d.tc, vcsCPath, vcsOPath, d.cFlags, nil, d.moduleScopeCFlags, d.flags.NoCompilerWarnings, d.noOptimize)
	cmd2 := composeDynLibCmd(instance.Platform, d.tc, instance.Path.rel(), outputPath, outputName, vcsOPath, peerArchivePaths, pluginPaths, strStrings(d.dynamicLibraryFrom), d.exportsScript.string(), fixElfPath.string())
	cmd3 := composeLDCmdLinkOrCopy(d.tc, instance.Path.rel())
	envVcsOnly := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	envFull := ctx.host.toolEnv()

	inputs := composeDynLibInputs(na, peerArchivePaths, pluginPaths, fixElfPath, instance.Path.rel(), d.exportsScript.string(), ctx.scripts)

	deps := make([]NodeRef, 0, len(peerArchiveRefs)+len(pluginRefs)+1)
	deps = append(deps, peerArchiveRefs...)
	deps = append(deps, pluginRefs...)
	deps = append(deps, ctx.vcsRef)

	n := &Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmd0), Env: envVcsOnly}, Cmd{CmdArgs: na.chunkList(cmd1), Env: envFull}, Cmd{CmdArgs: na.chunkList(cmd2), Cwd: strB, Env: envFull}, Cmd{CmdArgs: na.chunkList(cmd3), Env: envVcsOnly}),
		Env:          envFull,
		Inputs:       inputs,
		Outputs:      na.vfsList(build(instance.Path.rel() + "/" + outputName)),
		KV:           KV{P: pkLD, PC: pcLightBlue, ShowOut: true},
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Sandboxing:   true,
		DepRefs:      deps,
		Resources:    instance.Platform.UsesLinkResources,
	}

	n.ForeignDepRefs = depRefs(fixElfRef)

	ref := ctx.emit.emit(n)
	addInclGlobal := dedupVFS(d.addInclGlobal, peerAddInclGlobal)
	cFlagsGlobal := dedupARG(d.cFlagsGlobal, peerCFlagsGlobal)
	cxxFlagsGlobal := dedupARG(d.cxxFlagsGlobal, peerCXXFlagsGlobal)
	cOnlyFlagsGlobal := dedupARG(d.cOnlyFlagsGlobal, peerCOnlyFlagsGlobal)

	return &ModuleEmitResult{
		ARPath:                       nil,
		isPROGRAM:                    false,
		LDRef:                        ref,
		LDPath:                       vfsPtr(build(instance.Path.rel() + "/" + outputName)),
		AddInclGlobal:                addInclGlobal,
		OwnAddInclGlobal:             cloneVFSs(d.addInclGlobal),
		CFlagsGlobal:                 cFlagsGlobal,
		CXXFlagsGlobal:               cxxFlagsGlobal,
		COnlyFlagsGlobal:             cOnlyFlagsGlobal,
		RPathFlagsGlobal:             dedupARG(peerRPathFlagsGlobal, d.rpathFlagsGlobal),
		PeerArchiveClosureRefs:       nil,
		PeerArchiveClosurePaths:      nil,
		isPyLibrary:                  false,
		PeerGlobalClosureRefs:        nil,
		PeerGlobalClosurePaths:       nil,
		PeerWholeArchiveClosureRefs:  nil,
		PeerWholeArchiveClosurePaths: nil,
		LDPluginRefs:                 pluginRefs,
		LDPluginPaths:                pluginPaths,
		InducedDeps:                  d.inducedDeps,
		Peerdirs:                     d.peerdirs,
		ModuleStmtName:               d.moduleStmt.Name,
	}
}

func composeDynLibCmd(p *Platform, tc ModuleToolchain, modulePath, outputPath, outputName, vcsOPath string, peerLibPaths, pluginPaths []VFS, wholeArchivePeers []string, exportsScript, fixElfPath string) []STR {
	cmdArgs := []STR{
		tc.Python3,
		internStr(ldLinkDynLibPath),
		argTarget.str(), internStr(outputPath),
	}

	if len(pluginPaths) > 0 {
		cmdArgs = append(cmdArgs, argStartPlugins.str())

		for _, p := range pluginPaths {
			cmdArgs = append(cmdArgs, (p).str())
		}

		cmdArgs = append(cmdArgs, argEndPlugins.str())
	}

	for _, peer := range wholeArchivePeers {
		cmdArgs = append(cmdArgs, argWholeArchivePeers.str(), internStr(peer))
	}

	cmdArgs = append(cmdArgs,
		argSourceRoot.str(), argS.str(),
		argBuildRoot.str(), argB.str(),
		argArchLinux.str(),
		argObjcopyExe.str(), tc.Objcopy,
		argFixElf.str(), internStr(fixElfPath),
		tc.CXX,
		argWlWholeArchive.str(),
		argYaStartCommandFile.str(),
		argYaEndCommandFile.str(),
		argWlNoWholeArchive.str(),
		internStr(vcsOPath),
		argDashO.str(), internStr(outputPath),
		argShared.str(),
		internStr("-Wl,-soname,"+outputName),
		p.TargetArg,
	)
	cmdArgs = append(cmdArgs, p.SysrootArgs...)
	cmdArgs = append(cmdArgs, argWlStartGroup.str())

	for _, p := range peerLibPaths {
		cmdArgs = append(cmdArgs, internStr(p.rel()))
	}

	cmdArgs = append(cmdArgs, argWlEndGroup.str())
	cmdArgs = append(cmdArgs,
		argRdynamic.str(),
		internStr("-Wl,--version-script=$(S)/"+modulePath+"/"+exportsScript),
		argWlNoAsNeeded.str(),
	)

	if p.PIC {
		cmdArgs = append(cmdArgs, (argFPIC).str())
	}

	cmdArgs = append(cmdArgs,
		argWlGdbIndex.str(),
		argWlZNotext.str(),
	)

	if p.PIC {
		cmdArgs = append(cmdArgs, (argFPIC).str())
	}

	cmdArgs = append(cmdArgs,
		argFuseLdLld.str(),
		internStr("--ld-path="+tc.LLD.string()),
		argWlNoRosegment.str(),
		argWlBuildIdSha1.str(),
		argNostdlib.str(),
		argLm.str(),
		argWlGcSections.str(),
	)

	return cmdArgs
}

func composeDynLibInputs(na *NodeArenas, peerLibPaths, pluginPaths []VFS, fixElfPath VFS, modulePath, exportsScript string, scripts ScriptDeps) InputChunks {
	chunks := make(InputChunks, 0, 7)

	chunks = append(chunks, peerLibPaths, pluginPaths, na.srcChunk(fixElfPath))

	for _, w := range []VFS{ldVcsInfoVFS, ldLinkDynLibVFS, ldFsToolsVFS} {
		chunks = append(chunks, scripts[w])
	}

	chunks = append(chunks, []VFS{
		ldSvnInterfaceVFS,
		ldSvnversionHVFS,
		source(modulePath + "/" + exportsScript),
	})

	return chunks
}
