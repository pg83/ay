package main

import "strings"

var (
	ldLinkDynLibPath = ldLinkDynLibVFS.string()
	dynamicLibraryKV = KV{P: pkLD, PC: pcLightBlue, ShowOut: true}
)

func dllOutputName(stmt *ModuleStmt) string {
	prefix := "lib"
	name := ""

	if len(stmt.Args) > 0 {
		name = stmt.Args[0].string()
	}

	for i := 1; i+1 < len(stmt.Args); i++ {
		if stmt.Args[i].string() == "PREFIX" {
			prefix = stmt.Args[i+1].string()
		}
	}

	return prefix + name + ".so"
}

func (e *EmitContext) emitDllShared(ccRefs []NodeRef, ccOutputs []VFS, peerArchiveRefs []NodeRef, peerArchivePaths []VFS, peerSbomRefs []NodeRef, peerSbomPaths []VFS) *ModuleEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na

	if d.exportsScript == nil {
		throwFmt("gen: %s DLL requires EXPORTS_SCRIPT(...)", instance.Path.rel())
	}

	fixElfRef, fixElfPath := ctx.tool(argToolsFixElf)

	if !effectiveNoPlatform(d.flags) {
		cowRes := genModule(ctx, e.derivePeerInstance("build/cow/on"))

		if cowRes.ARPath != nil {
			peerArchiveRefs = append([]NodeRef{cowRes.ARRef}, peerArchiveRefs...)
			peerArchivePaths = append([]VFS{*cowRes.ARPath}, peerArchivePaths...)
		}
	}

	outputName := dllOutputName(d.moduleStmt)
	outputPath := build(instance.Path.rel(), "/", outputName).string()
	vcsCPath := build(instance.Path.rel(), "/__vcs_version__.c").string()
	vcsOPath := build(instance.Path.rel(), "/__vcs_version__.c.pic.o").string()
	cmd0 := composeLDCmdVcsInfo(d.tc, vcsCPath)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, d.tc, vcsCPath, vcsOPath, d.cFlags, nil, d.moduleScopeCFlags, d.flags.NoCompilerWarnings, d.noOptimize)
	cmd2 := composeDynLibCmd(instance.Platform, d.tc, instance.Path.rel(), outputPath, outputName, vcsOPath, ccOutputs, peerArchivePaths, nil, nil, d.exportsScript.string(), fixElfPath.string())
	envVcsOnly := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	envFull := ctx.host.toolEnv()
	ldSbomRefs := peerSbomRefs
	ldSbomPaths := peerSbomPaths
	sbomJSON := build(instance.Path.rel(), "/__sbomdata.json").string()
	sbomEmbed := false

	if sbomActive(ctx, instance) && instance.Platform.BuildRelease {
		if r, p := clangToolchainSbomComponent(ctx, instance.Platform); r != nil && !containsVFS(ldSbomPaths, *p) {
			ldSbomRefs = append(ldSbomRefs, *r)
			ldSbomPaths = append(ldSbomPaths, *p)
		}

		if sbomQualifies(d) {
			if or, op := e.emitSbomComponent(strings.TrimSuffix(outputName, ".so")); or != nil {
				ldSbomRefs = append(ldSbomRefs, *or)
				ldSbomPaths = append(ldSbomPaths, *op)
			}
		}

		sbomEmbed = len(ldSbomPaths) > 0
	}

	cmds := make([]Cmd, 0, 5)

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(cmd0), Env: envVcsOnly}, Cmd{CmdArgs: na.chunkList(cmd1), Env: envFull})

	if sbomEmbed {
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(composeLDCmdLinkSbom(d.tc, d.unit.SbomLang, instance.Path.rel(), sbomJSON, ldSbomPaths)), Cwd: strB, Env: envVcsOnly})
	}

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(cmd2), Cwd: strB, Env: envFull})

	if sbomEmbed {
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(composeLDCmdSbomObjcopy(d.tc, sbomJSON, outputPath)), Env: envVcsOnly})
	}

	inputs := InputChunks{peerArchivePaths, na.srcChunk(fixElfPath), ctx.scripts[ldVcsInfoVFS], ctx.scripts[ldLinkDynLibVFS]}

	inputs = append(inputs, []VFS{ldSvnInterfaceVFS, ldSvnversionHVFS, source(instance.Path.rel(), "/", d.exportsScript.string())})
	inputs = append(inputs, ccOutputs)

	if sbomEmbed {
		inputs = append(inputs, ldSbomPaths)
		inputs = append(inputs, []VFS{linkSbomScriptVFS})
	}

	deps := make([]NodeRef, 0, len(peerArchiveRefs)+len(ccRefs)+len(ldSbomRefs)+1)

	deps = append(deps, peerArchiveRefs...)
	deps = append(deps, ccRefs...)
	deps = append(deps, ldSbomRefs...)
	deps = append(deps, ctx.vcsRef)

	n := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(cmds...),
		Env:          envFull,
		Inputs:       inputs,
		Outputs:      na.vfsList(build(instance.Path.rel(), "/", outputName)),
		KV:           &dynamicLibraryKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      deps,
		Resources:    instance.Platform.UsesLinkResources,
	}

	n.ForeignDepRefs = depRefs(fixElfRef)

	ref := ctx.emit.emitNode(n)

	return &ModuleEmitResult{
		LDRef:          ref,
		LDPath:         vfsPtr(build(instance.Path.rel(), "/", outputName)),
		ModuleStmtName: d.moduleStmt.Name,
		InducedDeps:    d.inducedDeps,
	}
}

func (e *EmitContext) emitDynamicLibrary() *ModuleEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d
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

		peerInstance := e.derivePeerInstance(p)

		resolved = append(resolved, genModule(ctx, peerInstance))
	}

	rpathOnly := make([]*ModuleEmitResult, 0, len(dynLibRPathHelperPeers))

	for _, p := range dynLibRPathHelperPeers {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}

		peerInstance := e.derivePeerInstance(p)

		rpathOnly = append(rpathOnly, genModule(ctx, peerInstance))
	}

	var resourceGlobals []ResourceDecl
	deduper.reset()

	for _, pr := range resolved {
		for _, decl := range pr.ResourceGlobalClosure {
			if deduper.add(decl.GlobalVar.strID()) {
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
			if deduper.add(p.strID()) {
				peerAddInclGlobal = append(peerAddInclGlobal, p)
			}
		}
	}

	var peerCFlagsGlobal []ARG
	var peerCXXFlagsGlobal []ARG
	var peerCOnlyFlagsGlobal []ARG
	var peerRPathFlagsGlobal []ARG

	deduper.reset()

	for _, pr := range resolved {
		for _, a := range pr.CFlagsGlobal {
			if deduper.add(a.strID()) {
				peerCFlagsGlobal = append(peerCFlagsGlobal, a)
			}
		}
	}

	deduper.reset()

	for _, pr := range resolved {
		for _, a := range pr.CXXFlagsGlobal {
			if deduper.add(a.strID()) {
				peerCXXFlagsGlobal = append(peerCXXFlagsGlobal, a)
			}
		}
	}

	deduper.reset()

	for _, pr := range resolved {
		for _, a := range pr.COnlyFlagsGlobal {
			if deduper.add(a.strID()) {
				peerCOnlyFlagsGlobal = append(peerCOnlyFlagsGlobal, a)
			}
		}
	}

	deduper.reset()

	for _, pr := range resolved {
		for _, a := range pr.RPathFlagsGlobal {
			if deduper.add(a.strID()) {
				peerRPathFlagsGlobal = append(peerRPathFlagsGlobal, a)
			}
		}
	}

	for _, pr := range rpathOnly {
		for _, a := range pr.RPathFlagsGlobal {
			if deduper.add(a.strID()) {
				peerRPathFlagsGlobal = append(peerRPathFlagsGlobal, a)
			}
		}
	}

	pluginRefs := []NodeRef{}
	pluginPaths := []VFS{}

	deduper.reset()

	for _, pr := range resolved {
		for i, pp := range pr.LDPluginPaths {
			if deduper.add(pp.strID()) {
				pluginRefs = append(pluginRefs, pr.LDPluginRefs[i])
				pluginPaths = append(pluginPaths, pp)
			}
		}
	}

	d.tc = resolveModuleToolchain(resourceGlobals, instance.Platform.ClangVer)

	fixElfRef, fixElfPath := ctx.tool(argToolsFixElf)
	outputName := "lib" + d.moduleStmt.Args[0].string() + ".so"
	outputPath := build(instance.Path.rel(), "/", outputName).string()
	vcsCPath := build(instance.Path.rel(), "/__vcs_version__.c").string()
	vcsOPath := build(instance.Path.rel(), "/__vcs_version__.c.pic.o").string()
	cmd0 := composeLDCmdVcsInfo(d.tc, vcsCPath)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, d.tc, vcsCPath, vcsOPath, d.cFlags, nil, d.moduleScopeCFlags, d.flags.NoCompilerWarnings, d.noOptimize)
	cmd2 := composeDynLibCmd(instance.Platform, d.tc, instance.Path.rel(), outputPath, outputName, vcsOPath, nil, peerArchivePaths, pluginPaths, strStrings(d.dynamicLibraryFrom), d.exportsScript.string(), fixElfPath.string())
	cmd3 := composeLDCmdLinkOrCopy(d.tc, instance.Path.rel())
	envVcsOnly := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	envFull := ctx.host.toolEnv()
	inputs := composeDynLibInputs(na, peerArchivePaths, pluginPaths, fixElfPath, instance.Path.rel(), d.exportsScript.string(), ctx.scripts)
	deps := make([]NodeRef, 0, len(peerArchiveRefs)+len(pluginRefs)+1)

	deps = append(deps, peerArchiveRefs...)
	deps = append(deps, pluginRefs...)
	deps = append(deps, ctx.vcsRef)

	n := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmd0), Env: envVcsOnly}, Cmd{CmdArgs: na.chunkList(cmd1), Env: envFull}, Cmd{CmdArgs: na.chunkList(cmd2), Cwd: strB, Env: envFull}, Cmd{CmdArgs: na.chunkList(cmd3), Env: envVcsOnly}),
		Env:          envFull,
		Inputs:       inputs,
		Outputs:      na.vfsList(build(instance.Path.rel(), "/", outputName)),
		KV:           &dynamicLibraryKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      deps,
		Resources:    instance.Platform.UsesLinkResources,
	}

	n.ForeignDepRefs = depRefs(fixElfRef)

	ref := ctx.emit.emitNode(n)
	addInclGlobal := concat(d.addInclGlobal, peerAddInclGlobal)
	cFlagsGlobal := concat(d.cFlagsGlobal, peerCFlagsGlobal)
	cxxFlagsGlobal := concat(d.cxxFlagsGlobal, peerCXXFlagsGlobal)
	cOnlyFlagsGlobal := concat(d.cOnlyFlagsGlobal, peerCOnlyFlagsGlobal)

	return &ModuleEmitResult{
		ARPath:                       nil,
		isPROGRAM:                    false,
		LDRef:                        ref,
		LDPath:                       vfsPtr(build(instance.Path.rel(), "/", outputName)),
		AddInclGlobal:                addInclGlobal,
		OwnAddInclGlobal:             cloneVFSs(d.addInclGlobal),
		CFlagsGlobal:                 cFlagsGlobal,
		CXXFlagsGlobal:               cxxFlagsGlobal,
		COnlyFlagsGlobal:             cOnlyFlagsGlobal,
		RPathFlagsGlobal:             concat(peerRPathFlagsGlobal, d.rpathFlagsGlobal),
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
		ModuleStmtName:               d.moduleStmt.Name,
	}
}

func composeDynLibCmd(p *Platform, tc ModuleToolchain, modulePath, outputPath, outputName, vcsOPath string, ownObjects, peerLibPaths, pluginPaths []VFS, wholeArchivePeers []string, exportsScript, fixElfPath string) []STR {
	cmdArgs := []STR{
		tc.Python3,
		internStr(ldLinkDynLibPath),
		argTarget.str(), internStr(outputPath),
	}

	cmdArgs = append(cmdArgs, argStartPlugins.str())

	for _, p := range pluginPaths {
		cmdArgs = append(cmdArgs, (p).str())
	}

	cmdArgs = append(cmdArgs, argEndPlugins.str())

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
	)

	for _, o := range ownObjects {
		cmdArgs = append(cmdArgs, (o).str())
	}

	cmdArgs = append(cmdArgs,
		argDashO.str(), internStr(outputPath),
		argShared.str(),
		internV("-Wl,-soname,", outputName),
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
		internV("-Wl,--version-script=$(S)/", modulePath, "/", exportsScript),
	)

	cmdArgs = append(cmdArgs, p.LinkPreludeExtra...)
	cmdArgs = append(cmdArgs, argWlNoAsNeeded.str())

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
		internV("--ld-path=", tc.LLD.string()),
		argWlNoRosegment.str(),
		argWlBuildIdSha1.str(),
	)

	cmdArgs = append(cmdArgs, p.SystemLibs...)
	cmdArgs = append(cmdArgs, argLm.str(), argWlGcSections.str())

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
		source(modulePath, "/", exportsScript),
	})

	return chunks
}
