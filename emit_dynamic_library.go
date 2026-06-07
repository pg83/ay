package main

var (
	ldLinkDynLibPath = ldLinkDynLibVFS.String()
)

func emitDynamicLibrary(ctx *genCtx, instance ModuleInstance, d *moduleData) *moduleEmitResult {
	if len(d.moduleStmt.Args) == 0 {
		ThrowFmt("gen: %s DYNAMIC_LIBRARY requires a basename argument", instance.Path)
	}

	if len(d.dynamicLibraryFrom) == 0 {
		ThrowFmt("gen: %s DYNAMIC_LIBRARY requires DYNAMIC_LIBRARY_FROM(...)", instance.Path)
	}

	if d.exportsScript == nil {
		ThrowFmt("gen: %s DYNAMIC_LIBRARY requires EXPORTS_SCRIPT(...)", instance.Path)
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

	seen := make(map[string]struct{}, len(peerPaths)+len(dynLibRPathHelperPeers))
	peerArchiveRefs := make([]NodeRef, 0, len(peerPaths))
	peerArchivePaths := make([]VFS, 0, len(peerPaths))
	pluginRefs := []NodeRef{}
	pluginPaths := []VFS{}
	pluginSeen := map[VFS]struct{}{}
	addInclSeen := map[VFS]struct{}{}
	var cFlagsSeen BitSet
	var cxxFlagsSeen BitSet
	var cOnlyFlagsSeen BitSet
	var rpathFlagsSeen BitSet
	var peerAddInclGlobal []VFS
	var peerCFlagsGlobal []ARG
	var peerCXXFlagsGlobal []ARG
	var peerCOnlyFlagsGlobal []ARG
	var peerRPathFlagsGlobal []ARG
	addEachVFS := func(seenSet map[VFS]struct{}, dst *[]VFS, src []VFS) {
		for _, x := range src {
			if _, dup := seenSet[x]; dup {
				continue
			}

			seenSet[x] = struct{}{}
			*dst = append(*dst, x)
		}
	}

	for _, p := range peerPaths {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		peerInstance := derivePeerInstance(ctx, instance, d, p)
		peerResult := genModule(ctx, peerInstance)

		if peerResult.ARPath != nil {
			peerArchiveRefs = append(peerArchiveRefs, peerResult.ARRef)
			peerArchivePaths = append(peerArchivePaths, *peerResult.ARPath)
		}

		addEachVFS(addInclSeen, &peerAddInclGlobal, peerResult.AddInclGlobal)
		addEachARG(&cFlagsSeen, &peerCFlagsGlobal, peerResult.CFlagsGlobal)
		addEachARG(&cxxFlagsSeen, &peerCXXFlagsGlobal, peerResult.CXXFlagsGlobal)
		addEachARG(&cOnlyFlagsSeen, &peerCOnlyFlagsGlobal, peerResult.COnlyFlagsGlobal)
		addEachARG(&rpathFlagsSeen, &peerRPathFlagsGlobal, peerResult.RPathFlagsGlobal)

		for i, pp := range peerResult.LDPluginPaths {
			if _, dup := pluginSeen[pp]; dup {
				continue
			}

			pluginSeen[pp] = struct{}{}
			pluginRefs = append(pluginRefs, peerResult.LDPluginRefs[i])
			pluginPaths = append(pluginPaths, pp)
		}
	}

	for _, p := range dynLibRPathHelperPeers {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		peerInstance := derivePeerInstance(ctx, instance, d, p)
		peerResult := genModule(ctx, peerInstance)
		addEachARG(&rpathFlagsSeen, &peerRPathFlagsGlobal, peerResult.RPathFlagsGlobal)
	}

	fixElfRef, fixElfPath := ctx.tool("tools/fix_elf")

	outputName := "lib" + d.moduleStmt.Args[0] + ".so"
	outputPath := Build(instance.Path + "/" + outputName).String()
	vcsCPath := Build(instance.Path + "/__vcs_version__.c").String()
	vcsOPath := Build(instance.Path + "/__vcs_version__.c.pic.o").String()

	cmd0 := composeLDCmdVcsInfo(instance.Platform.Tools, vcsCPath)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, vcsCPath, vcsOPath, d.cFlags, nil, d.moduleScopeCFlags, d.flags.NoCompilerWarnings)
	cmd2 := composeDynLibCmd(instance.Platform, instance.Path, outputPath, outputName, vcsOPath, peerArchivePaths, pluginPaths, d.dynamicLibraryFrom, *d.exportsScript, fixElfPath.String())
	cmd3 := composeLDCmdLinkOrCopy(instance.Platform.Tools, instance.Path)
	envVcsOnly := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}
	envFull := ctx.host.ToolEnv()

	inputs := composeDynLibInputs(peerArchivePaths, pluginPaths, fixElfPath, instance.Path, *d.exportsScript, ctx.scripts)

	depRefs := make([]NodeRef, 0, len(peerArchiveRefs)+len(pluginRefs)+1)
	depRefs = append(depRefs, peerArchiveRefs...)
	depRefs = append(depRefs, pluginRefs...)

	if fixElfRef != (NodeRef(0)) {
		depRefs = append(depRefs, fixElfRef)
	}

	n := &Node{
		Cmds: []Cmd{
			{CmdArgs: cmd0, Env: envVcsOnly},
			{CmdArgs: cmd1, Env: envFull},
			{CmdArgs: cmd2, Cwd: "$(B)", Env: envFull},
			{CmdArgs: cmd3, Env: envVcsOnly},
		},
		Env:              envFull,
		Inputs:           inputs,
		Outputs:          []VFS{Build(instance.Path + "/" + outputName)},
		KV:               KV{P: pkLD, PC: pcLightBlue, ShowOut: "yes"},
		Tags:             instance.Platform.Tags, // read-only; Platform.Tags is immutable during emit
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: instance.Path, ModuleLang: "cpp", ModuleTag: "dll", ModuleType: "so"},
		DepRefs:          depRefs,
	}

	if fixElfRef != (NodeRef(0)) {
		n.ForeignDepRefs = []NodeRef{fixElfRef}
	}

	ref := ctx.emit.Emit(bindNodePlatform(withResources(n, resourcePatternClangTool, resourcePatternLLDRoot, resourcePatternYMakePython3), instance.Platform))
	addInclGlobal := dedupVFS(d.addInclGlobal, peerAddInclGlobal)
	cFlagsGlobal := dedupARG(d.cFlagsGlobal, peerCFlagsGlobal)
	cxxFlagsGlobal := dedupARG(d.cxxFlagsGlobal, peerCXXFlagsGlobal)
	cOnlyFlagsGlobal := dedupARG(d.cOnlyFlagsGlobal, peerCOnlyFlagsGlobal)

	return &moduleEmitResult{
		ARPath:                       nil,
		isPROGRAM:                    false,
		LDRef:                        ref,
		LDPath:                       vfsPtr(Build(instance.Path + "/" + outputName)),
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
		InducedDeps:                  append([]string(nil), d.inducedDeps...),
		Peerdirs:                     append([]string(nil), d.peerdirs...),
		ModuleStmtName:               d.moduleStmt.Name,
	}
}

func composeDynLibCmd(p *Platform, modulePath, outputPath, outputName, vcsOPath string, peerLibPaths, pluginPaths []VFS, wholeArchivePeers []string, exportsScript, fixElfPath string) []ANY {
	cmdArgs := []ANY{
		internAny(p.Tools.Python3),
		internAny(ldLinkDynLibPath),
		anyTarget, internAny(outputPath),
	}

	if len(pluginPaths) > 0 {
		cmdArgs = append(cmdArgs, anyStartPlugins)

		for _, p := range pluginPaths {
			cmdArgs = append(cmdArgs, vfsAny(p))
		}

		cmdArgs = append(cmdArgs, anyEndPlugins)
	}

	for _, peer := range wholeArchivePeers {
		cmdArgs = append(cmdArgs, anyWholeArchivePeers, internAny(peer))
	}

	cmdArgs = append(cmdArgs,
		anySourceRoot, anyS,
		anyBuildRoot, anyB,
		anyArchLinux,
		anyObjcopyExe, internAny(p.Tools.Objcopy),
		anyFixElf, internAny(fixElfPath),
		p.CXXArg,
		anyWlWholeArchive,
		anyYaStartCommandFile,
		anyYaEndCommandFile,
		anyWlNoWholeArchive,
		internAny(vcsOPath),
		argDashO, internAny(outputPath),
		anyShared,
		internAny("-Wl,-soname,"+outputName),
		p.TargetArg,
		argDashBBin,
		anyWlStartGroup,
	)

	for _, p := range peerLibPaths {
		cmdArgs = append(cmdArgs, internAny(p.Rel()))
	}

	cmdArgs = append(cmdArgs, anyWlEndGroup)
	cmdArgs = append(cmdArgs,
		anyRdynamic,
		internAny("-Wl,--version-script=$(S)/"+modulePath+"/"+exportsScript),
		anyWlNoAsNeeded,
	)

	if p.PIC {
		cmdArgs = append(cmdArgs, argAny(argFPIC))
	}

	cmdArgs = append(cmdArgs,
		anyWlGdbIndex,
		anyWlZNotext,
	)

	if p.PIC {
		cmdArgs = append(cmdArgs, argAny(argFPIC))
	}

	cmdArgs = append(cmdArgs,
		anyFuseLdLld,
		internAny("--ld-path="+p.Tools.LLD),
		anyWlNoRosegment,
		anyWlBuildIdSha1,
		anyNostdlib,
		anyLm,
		anyWlGcSections,
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
