package main

var (
	ldLinkDynLibPath = ldLinkDynLibVFS.String()
)

func emitDynamicLibrary(ctx *genCtx, instance ModuleInstance, d *moduleData) *moduleEmitResult {
	if len(d.moduleStmt.Args) == 0 {
		ThrowFmt("gen: %s DYNAMIC_LIBRARY requires a basename argument", instance.Path.Rel())
	}

	if len(d.dynamicLibraryFrom) == 0 {
		ThrowFmt("gen: %s DYNAMIC_LIBRARY requires DYNAMIC_LIBRARY_FROM(...)", instance.Path.Rel())
	}

	if d.exportsScript == nil {
		ThrowFmt("gen: %s DYNAMIC_LIBRARY requires EXPORTS_SCRIPT(...)", instance.Path.Rel())
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
		if _, helper := rpathHelperSet[p]; helper {
			continue
		}

		peerPaths = append(peerPaths, p)
	}

	// Resolve every peer through genModule first (memoized; the recursion may
	// re-enter the deduper), then aggregate per output kind below in sequential
	// leaf passes. The peer-list guard stays a local string-keyed map because it
	// must stay live across the genModule calls.
	seen := make(map[string]struct{}, len(peerPaths)+len(dynLibRPathHelperPeers))
	resolved := make([]*moduleEmitResult, 0, len(peerPaths))

	for _, p := range peerPaths {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		peerInstance := derivePeerInstance(ctx, instance, d, p)
		resolved = append(resolved, genModule(ctx, peerInstance))
	}

	// rpath helper peers contribute only RPathFlagsGlobal.
	rpathOnly := make([]*moduleEmitResult, 0, len(dynLibRPathHelperPeers))

	for _, p := range dynLibRPathHelperPeers {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		peerInstance := derivePeerInstance(ctx, instance, d, p)
		rpathOnly = append(rpathOnly, genModule(ctx, peerInstance))
	}

	// Resource globals, deduped by global-var STR cast into the run-wide
	// VFS-keyed deduper (single-namespace leaf pass, as in genModule).
	var resourceGlobals []resourceDecl
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

	outputName := "lib" + d.moduleStmt.Args[0] + ".so"
	outputPath := Build(instance.Path.Rel() + "/" + outputName).String()
	vcsCPath := Build(instance.Path.Rel() + "/__vcs_version__.c").String()
	vcsOPath := Build(instance.Path.Rel() + "/__vcs_version__.c.pic.o").String()

	cmd0 := composeLDCmdVcsInfo(d.tc, vcsCPath)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, d.tc, vcsCPath, vcsOPath, d.cFlags, nil, d.moduleScopeCFlags, d.flags.NoCompilerWarnings)
	cmd2 := composeDynLibCmd(instance.Platform, d.tc, instance.Path.Rel(), outputPath, outputName, vcsOPath, peerArchivePaths, pluginPaths, d.dynamicLibraryFrom, *d.exportsScript, fixElfPath.String())
	cmd3 := composeLDCmdLinkOrCopy(d.tc, instance.Path.Rel())
	envVcsOnly := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	envFull := ctx.host.ToolEnv()

	inputs := composeDynLibInputs(peerArchivePaths, pluginPaths, fixElfPath, instance.Path.Rel(), *d.exportsScript, ctx.scripts)

	depRefs := make([]NodeRef, 0, len(peerArchiveRefs)+len(pluginRefs)+2)
	depRefs = append(depRefs, peerArchiveRefs...)
	depRefs = append(depRefs, pluginRefs...)
	depRefs = append(depRefs, emitVCSNode(ctx.emit, ctx.host))

	if fixElfRef != (NodeRef(0)) {
		depRefs = append(depRefs, fixElfRef)
	}

	n := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{CmdArgs: cmd0, Env: envVcsOnly},
			{CmdArgs: cmd1, Env: envFull},
			{CmdArgs: cmd2, Cwd: strB, Env: envFull},
			{CmdArgs: cmd3, Env: envVcsOnly},
		},
		Env:              envFull,
		Inputs:           inputs,
		Outputs:          []VFS{Build(instance.Path.Rel() + "/" + outputName)},
		KV:               KV{P: pkLD, PC: pcLightBlue, ShowOut: true},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel(), ModuleLang: mlCPP, ModuleTag: tagDll, ModuleType: mtSO},
		DepRefs:          depRefs,
		usesResources:    []string{resourcePatternClangTool + instance.Platform.ClangVer, resourcePatternLLDRoot, resourcePatternYMakePython3},
	}

	if fixElfRef != (NodeRef(0)) {
		n.ForeignDepRefs = []NodeRef{fixElfRef}
	}

	ref := ctx.emit.Emit(n)
	addInclGlobal := dedupVFS(d.addInclGlobal, peerAddInclGlobal)
	cFlagsGlobal := dedupARG(d.cFlagsGlobal, peerCFlagsGlobal)
	cxxFlagsGlobal := dedupARG(d.cxxFlagsGlobal, peerCXXFlagsGlobal)
	cOnlyFlagsGlobal := dedupARG(d.cOnlyFlagsGlobal, peerCOnlyFlagsGlobal)

	return &moduleEmitResult{
		ARPath:                       nil,
		isPROGRAM:                    false,
		LDRef:                        ref,
		LDPath:                       vfsPtr(Build(instance.Path.Rel() + "/" + outputName)),
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
		Peerdirs:                     append([]string(nil), d.peerdirs...),
		ModuleStmtName:               d.moduleStmt.Name,
	}
}

func composeDynLibCmd(p *Platform, tc moduleToolchain, modulePath, outputPath, outputName, vcsOPath string, peerLibPaths, pluginPaths []VFS, wholeArchivePeers []string, exportsScript, fixElfPath string) []STR {
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
		argDashBBin,
		argWlStartGroup.str(),
	)

	for _, p := range peerLibPaths {
		cmdArgs = append(cmdArgs, internStr(p.Rel()))
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
		internStr("--ld-path="+tc.LLD.String()),
		argWlNoRosegment.str(),
		argWlBuildIdSha1.str(),
		argNostdlib.str(),
		argLm.str(),
		argWlGcSections.str(),
	)

	return cmdArgs
}

func composeDynLibInputs(peerLibPaths, pluginPaths []VFS, fixElfPath VFS, modulePath, exportsScript string, scripts scriptDeps) []VFS {
	buildRootBlock := make([]VFS, 0, len(peerLibPaths)+len(pluginPaths)+1)
	buildRootBlock = append(buildRootBlock, peerLibPaths...)
	buildRootBlock = append(buildRootBlock, pluginPaths...)
	buildRootBlock = append(buildRootBlock, fixElfPath)

	inputs := make([]VFS, 0, len(buildRootBlock)+12)
	inputs = append(inputs, buildRootBlock...)

	// The scripts the link command actually runs (vcs stamp, the link_dyn_lib
	// wrapper, the objcopy/strip fs_tools), each expanded to its import closure via
	// the table — link_dyn_lib pulls in link_exe, process_command_files,
	// thinlto_cache, process_whole_archive_option; fs_tools pulls in
	// process_command_files. Dups are dropped in normalization.
	for _, w := range []VFS{ldVcsInfoVFS, ldLinkDynLibVFS, ldFsToolsVFS} {
		inputs = append(inputs, scripts[w]...)
	}

	// Non-script inputs: the vcs C template + header and the module's exports list.
	inputs = append(inputs,
		ldSvnInterfaceVFS,
		ldSvnversionHVFS,
		Source(modulePath+"/"+exportsScript),
	)

	return inputs
}
