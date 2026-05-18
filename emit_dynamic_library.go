package main

import (
	"sort"
)

var (
	ldLinkDynLibVFS                = Source("build/scripts/link_dyn_lib.py")
	ldThinltoCacheVFS              = Source("build/scripts/thinlto_cache.py")
	ldProcessCommandFilesVFS       = Source("build/scripts/process_command_files.py")
	ldProcessWholeArchiveOptionVFS = Source("build/scripts/process_whole_archive_option.py")

	ldLinkDynLibPath                = ldLinkDynLibVFS.String()
	ldThinltoCachePath              = ldThinltoCacheVFS.String()
	ldProcessCommandFilesPath       = ldProcessCommandFilesVFS.String()
	ldProcessWholeArchiveOptionPath = ldProcessWholeArchiveOptionVFS.String()
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

	peerPaths := make([]string, 0, 1+len(d.dynamicLibraryFrom))
	if !effectiveNoPlatform(instance.Flags) {
		peerPaths = append(peerPaths, "build/cow/on")
	}
	peerPaths = append(peerPaths, d.dynamicLibraryFrom...)

	seen := make(map[string]struct{}, len(peerPaths))
	peerArchiveRefs := make([]NodeRef, 0, len(peerPaths))
	peerArchivePaths := make([]VFS, 0, len(peerPaths))
	pluginRefs := []NodeRef{}
	pluginPaths := []VFS{}
	pluginSeen := map[VFS]struct{}{}
	addInclSeen := map[VFS]struct{}{}
	cFlagsSeen := map[string]struct{}{}
	cxxFlagsSeen := map[string]struct{}{}
	cOnlyFlagsSeen := map[string]struct{}{}
	var peerAddInclGlobal []VFS
	var peerCFlagsGlobal []string
	var peerCXXFlagsGlobal []string
	var peerCOnlyFlagsGlobal []string

	addEach := func(seenSet map[string]struct{}, dst *[]string, src []string) {
		for _, x := range src {
			if _, dup := seenSet[x]; dup {
				continue
			}

			seenSet[x] = struct{}{}
			*dst = append(*dst, x)
		}
	}
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
		addEach(cFlagsSeen, &peerCFlagsGlobal, peerResult.CFlagsGlobal)
		addEach(cxxFlagsSeen, &peerCXXFlagsGlobal, peerResult.CXXFlagsGlobal)
		addEach(cOnlyFlagsSeen, &peerCOnlyFlagsGlobal, peerResult.COnlyFlagsGlobal)

		for i, pp := range peerResult.LDPluginPaths {
			if _, dup := pluginSeen[pp]; dup {
				continue
			}
			pluginSeen[pp] = struct{}{}
			pluginRefs = append(pluginRefs, peerResult.LDPluginRefs[i])
			pluginPaths = append(pluginPaths, pp)
		}
	}

	fixElfPath := Build("tools/fix_elf/fix_elf")
	var fixElfRef NodeRef
	fixElfInst := NewToolInstance(ctx.host, "tools/fix_elf")
	if exc := Try(func() {
		res := genModule(ctx, fixElfInst)
		fixElfRef = res.LDRef
		if res.LDPath != nil {
			fixElfPath = *res.LDPath
		}
	}); exc != nil {
		_ = exc
	}

	outputName := "lib" + d.moduleStmt.Args[0] + ".so"
	outputPath := Build(instance.Path + "/" + outputName).String()
	vcsCPath := Build(instance.Path + "/__vcs_version__.c").String()
	vcsOPath := Build(instance.Path + "/__vcs_version__.c.pic.o").String()

	cmd0 := composeLDCmdVcsInfo(instance.Platform.Tools, vcsCPath)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, vcsCPath, vcsOPath, d.cFlags, nil, defaultPeerCFlags(ctx, instance, d), d.flags.NoCompilerWarnings)
	cmd2 := composeDynLibCmd(instance.Platform, instance.Path, outputPath, outputName, vcsOPath, peerArchivePaths, pluginPaths, d.dynamicLibraryFrom, *d.exportsScript, fixElfPath.String())
	cmd3 := composeLDCmdLinkOrCopy(instance.Platform.Tools, instance.Path)

	envVcsOnly := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}
	envFull := ctx.host.ToolEnv()

	inputs := composeDynLibInputs(peerArchivePaths, pluginPaths, fixElfPath, instance.Path, *d.exportsScript)

	depRefs := make([]NodeRef, 0, len(peerArchiveRefs)+len(pluginRefs)+1)
	depRefs = append(depRefs, peerArchiveRefs...)
	depRefs = append(depRefs, pluginRefs...)
	if fixElfRef != (NodeRef{}) {
		depRefs = append(depRefs, fixElfRef)
	}

	n := &Node{
		Cmds: []Cmd{
			{CmdArgs: cmd0, Env: envVcsOnly},
			{CmdArgs: cmd1, Env: envFull},
			{CmdArgs: cmd2, Cwd: "$(B)", Env: envFull},
			{CmdArgs: cmd3, Env: envVcsOnly},
		},
		Env:     envFull,
		Inputs:  inputs,
		Outputs: []VFS{Build(instance.Path + "/" + outputName)},
		KV: map[string]string{
			"p":        "LD",
			"pc":       "light-blue",
			"show_out": "yes",
		},
		Tags:         append([]string(nil), instance.Platform.Tags...),
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{"cpu": float64(1), "network": "restricted", "ram": float64(32)},
		Sandboxing:   true,
		TargetProperties: map[string]string{
			"module_dir":  instance.Path,
			"module_lang": "cpp",
			"module_tag":  "dll",
			"module_type": "so",
		},
		DepRefs: depRefs,
	}
	if fixElfRef != (NodeRef{}) {
		n.ForeignDepRefs = map[string][]NodeRef{"tool": {fixElfRef}}
	}

	ref := ctx.emit.Emit(n)
	addInclGlobal := mergeDedupVFS(d.addInclGlobal, peerAddInclGlobal)
	cFlagsGlobal := mergeDedup(d.cFlagsGlobal, peerCFlagsGlobal)
	cxxFlagsGlobal := mergeDedup(d.cxxFlagsGlobal, peerCXXFlagsGlobal)
	cOnlyFlagsGlobal := mergeDedup(d.cOnlyFlagsGlobal, peerCOnlyFlagsGlobal)

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

func composeDynLibCmd(p *Platform, modulePath, outputPath, outputName, vcsOPath string, peerLibPaths, pluginPaths []VFS, wholeArchivePeers []string, exportsScript, fixElfPath string) []string {
	cmdArgs := []string{
		p.Tools.Python3,
		ldLinkDynLibPath,
		"--target", outputPath,
	}
	if len(pluginPaths) > 0 {
		cmdArgs = append(cmdArgs, "--start-plugins")
		for _, p := range pluginPaths {
			cmdArgs = append(cmdArgs, p.String())
		}
		cmdArgs = append(cmdArgs, "--end-plugins")
	}
	for _, p := range wholeArchivePeers {
		cmdArgs = append(cmdArgs, "--whole-archive-peers", p)
	}
	cmdArgs = append(cmdArgs,
		"--source-root", "$(S)",
		"--build-root", "$(B)",
		"--arch=LINUX",
		"--objcopy-exe", p.Tools.Objcopy,
		"--fix-elf", fixElfPath,
		p.Tools.CXX,
		"-Wl,--whole-archive",
		"--ya-start-command-file",
		"--ya-end-command-file",
		"-Wl,--no-whole-archive",
		vcsOPath,
		"-o", outputPath,
		"-shared",
		"-Wl,-soname,"+outputName,
		"--target="+p.Triple,
		"-B"+binPath,
		"-Wl,--start-group",
	)
	for _, p := range peerLibPaths {
		cmdArgs = append(cmdArgs, p.Rel)
	}
	cmdArgs = append(cmdArgs, "-Wl,--end-group")
	cmdArgs = append(cmdArgs,
		"-rdynamic",
		"-Wl,--version-script=$(S)/"+modulePath+"/"+exportsScript,
		"-Wl,--no-as-needed",
		"-fPIC",
		"-Wl,--gdb-index",
		"-Wl,-z,notext",
		"-fPIC",
		"-fuse-ld=lld",
		"--ld-path=$(LLD_ROOT)/bin/ld.lld",
		"-Wl,--no-rosegment",
		"-Wl,--build-id=sha1",
		"-nostdlib",
		"-lm",
		"-Wl,--gc-sections",
	)

	return cmdArgs
}

func composeDynLibInputs(peerLibPaths, pluginPaths []VFS, fixElfPath VFS, modulePath, exportsScript string) []VFS {
	buildRootBlock := make([]VFS, 0, len(peerLibPaths)+len(pluginPaths)+1)
	buildRootBlock = append(buildRootBlock, peerLibPaths...)
	buildRootBlock = append(buildRootBlock, pluginPaths...)
	buildRootBlock = append(buildRootBlock, fixElfPath)
	sort.Slice(buildRootBlock, func(i, j int) bool { return buildRootBlock[i].String() < buildRootBlock[j].String() })

	inputs := make([]VFS, 0, len(buildRootBlock)+6)
	inputs = append(inputs, buildRootBlock...)
	inputs = append(inputs,
		ldLinkDynLibVFS,
		ldThinltoCacheVFS,
		ldProcessCommandFilesVFS,
		ldProcessWholeArchiveOptionVFS,
		Source(modulePath+"/"+exportsScript),
	)

	return inputs
}
