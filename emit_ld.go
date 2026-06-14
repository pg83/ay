package main

var (
	ldVcsInfoPath      = ldVcsInfoVFS.string()
	ldSvnInterfacePath = ldSvnInterfaceVFS.string()
	ldLinkExePath      = ldLinkExeVFS.string()
	ldFsToolsPath      = ldFsToolsVFS.string()
)

// ldScriptInputs seeds the link node's $(S) tooling inputs: the wrapper scripts it
// invokes plus the non-script vcs template. Each .py wrapper's import closure
// (thinlto_cache, process_command_files, process_whole_archive_option, …) is added
// from the script table in composeLDInputs — not hand-listed here.
var ldScriptInputs = []VFS{
	ldVcsInfoVFS,
	ldSvnInterfaceVFS,
	ldLinkExeVFS,
	copyFsToolsVFS,
}

func emitLD(
	instance ModuleInstance,
	binaryName string,
	ccRefs []NodeRef,
	ccPaths []VFS,
	peerLDRefs []NodeRef,
	peerLibPaths []VFS,
	peerLinkCmdPaths []VFS,
	pluginRefs []NodeRef,
	pluginPaths []VFS,
	globalRefs []NodeRef,
	globalPaths []VFS,
	wholeArchiveRefs []NodeRef,
	wholeArchivePaths []VFS,
	wholeArchiveCmdPaths []VFS,
	dynamicRefs []NodeRef,
	dynamicPaths []VFS,
	objcopyRefs []NodeRef,
	objcopyPaths []VFS,
	moduleCFlags []ARG,
	peerCFlagsGlobal []ARG,
	moduleScopeCFlags []ARG,
	peerLDFlagsGlobal []ARG,
	ownLDFlags []ARG,
	ownRPathFlags []ARG,
	peerRPathFlagsGlobal []ARG,
	objAddLibsGlobal []ARG,
	exportsScript *STR,
	noCompilerWarnings bool,
	wantsStrip bool,
	wantsSplitDwarf bool,
	programModuleTag STR,
	tc ModuleToolchain,
	hostP *Platform,
	scripts ScriptDeps,
	emit Emitter,
) NodeRef {
	na := emit.nodeArenas()

	if len(ccRefs) != len(ccPaths) {
		throwFmt("EmitLD: ccRefs/ccPaths length mismatch (%d vs %d)", len(ccRefs), len(ccPaths))
	}

	if len(peerLDRefs) != len(peerLibPaths) {
		throwFmt("EmitLD: peerLDRefs/peerLibPaths length mismatch (%d vs %d)", len(peerLDRefs), len(peerLibPaths))
	}

	if len(pluginRefs) != len(pluginPaths) {
		throwFmt("EmitLD: pluginRefs/pluginPaths length mismatch (%d vs %d)", len(pluginRefs), len(pluginPaths))
	}

	if len(globalRefs) != len(globalPaths) {
		throwFmt("EmitLD: globalRefs/globalPaths length mismatch (%d vs %d)", len(globalRefs), len(globalPaths))
	}

	if len(wholeArchiveRefs) != len(wholeArchivePaths) {
		throwFmt("EmitLD: wholeArchiveRefs/wholeArchivePaths length mismatch (%d vs %d)", len(wholeArchiveRefs), len(wholeArchivePaths))
	}

	if len(objcopyRefs) != len(objcopyPaths) {
		throwFmt("EmitLD: objcopyRefs/objcopyPaths length mismatch (%d vs %d)", len(objcopyRefs), len(objcopyPaths))
	}

	if len(dynamicRefs) != len(dynamicPaths) {
		throwFmt("EmitLD: dynamicRefs/dynamicPaths length mismatch (%d vs %d)", len(dynamicRefs), len(dynamicPaths))
	}

	if binaryName == "" {
		binaryName = lastPathComponent(instance.Path.rel())
	}

	binaryDir := instance.Path.rel()

	binPrefix := binaryDir + "/"
	outputVFS := build(binPrefix + binaryName)
	vcsCVFS := build(binPrefix + "__vcs_version__.c")
	vcsOVFS := build(binPrefix + "__vcs_version__.c" + instance.Platform.objectSuffix())

	vcsCPath := vcsCVFS.string()
	vcsOPath := vcsOVFS.string()
	outputPath := outputVFS.string()

	cmd0 := composeLDCmdVcsInfo(tc, vcsCPath)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, tc, vcsCPath, vcsOPath, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags, noCompilerWarnings)
	cmd2 := composeLDCmdLinkExe(instance.Platform, tc, outputPath, vcsOPath, ccPaths, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths, objcopyPaths, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal, exportsScript, wantsStrip)
	cmd3 := composeLDCmdLinkOrCopy(tc, binaryDir, dynamicPaths...)
	splitDwarfCmds := composeLDSplitDwarfCmds(na, tc, outputPath, wantsSplitDwarf)

	envVcsOnly := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	envFull := hostP.toolEnv()

	cmds := na.cmdList(Cmd{CmdArgs: na.chunkList(cmd0), Env: envVcsOnly}, Cmd{CmdArgs: na.chunkList(cmd1), Env: envFull}, Cmd{CmdArgs: na.chunkList(cmd2), Cwd: strB, Env: envFull}, Cmd{CmdArgs: na.chunkList(cmd3), Env: envVcsOnly})

	for i := range splitDwarfCmds {
		splitDwarfCmds[i].Env = envVcsOnly
	}

	cmds = append(cmds, splitDwarfCmds...)

	inputs := composeLDInputs(na, instance.Path.rel(), ccPaths, peerLibPaths, pluginPaths, globalPaths, wholeArchivePaths, dynamicPaths, objcopyPaths, scripts)

	inputTail := make([]VFS, 0, 2)
	inputTail = append(inputTail, ldSvnversionHVFS)

	if exportsScript != nil {
		inputTail = append(inputTail, source(exportsScript.string()))
	}

	inputs = append(inputs, inputTail)

	// Whole-archive is a LINK ATTRIBUTE of a subset of the peer archives (the link
	// command wraps them in --whole-archive), not an independent dependency source:
	// every wholeArchiveRef is already in peerLDRefs. So it is NOT appended here —
	// peerLDRefs already covers it, and appending would duplicate the dep.
	depRefs := make([]NodeRef, 0, len(ccRefs)+len(pluginRefs)+len(globalRefs)+len(peerLDRefs)+len(dynamicRefs)+len(objcopyRefs))
	depRefs = append(depRefs, ccRefs...)
	depRefs = append(depRefs, pluginRefs...)
	depRefs = append(depRefs, globalRefs...)
	depRefs = append(depRefs, peerLDRefs...)
	depRefs = append(depRefs, dynamicRefs...)
	depRefs = append(depRefs, objcopyRefs...)
	depRefs = append(depRefs, emitVCSNode(emit, hostP))
	outputs := []VFS{outputVFS}

	for _, p := range dynamicPaths {
		outputs = append(outputs, build(binaryDir+"/"+lastPathComponent(p.rel())))
	}

	if wantsSplitDwarf {
		outputs = append(outputs, build(binPrefix+binaryName+".debug"))
	}

	n := &Node{
		Platform:         instance.Platform,
		Cmds:             cmds,
		Env:              envFull,
		Inputs:           inputs,
		Outputs:          outputs,
		KV:               KV{P: pkLD, PC: pcLightBlue, ShowOut: true},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: binaryDir, ModuleLang: ldModuleLang(instance), ModuleType: mtBin},
		DepRefs:          depRefs,
		Resources:        instance.Platform.UsesLinkResources,
	}

	if programModuleTag != 0 {
		n.TargetProperties.ModuleTag = programModuleTag
	}

	return emit.emit(n)
}

func lDOutputPath(instance ModuleInstance, binaryName string) VFS {
	if binaryName == "" {
		binaryName = lastPathComponent(instance.Path.rel())
	}

	return build(instance.Path.rel() + "/" + binaryName)
}

func ldModuleLang(instance ModuleInstance) ModuleLang {
	if instance.Language == LangPy {
		return mlPy3
	}

	return mlCPP
}

func lastPathComponent(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}

	return p
}

// emitVCSNode emits the node that writes the inline vcs.json stub ({}) to
// $(B)/vcs.json — the input the vcs_info step reads to generate __vcs_version__.c.
// It is a plain build command (it writes a file, it does not fetch a resource), so
// it carries no FETCH kind; consumers depend on it like any producer. Its uid is
// content-stable (set in the node), so emitting it from every link node dedups to
// one. `dump normalize` folds $(B)/vcs.json back to the upstream $(VCS)/vcs.json
// reference and strips this producer (upstream mounts vcs.json, has no producer node).
func emitVCSNode(emit Emitter, host *Platform) NodeRef {
	na := emit.nodeArenas()

	output := bldVcsJson
	node := &Node{
		Platform: host,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.strList(internStr(currentYatoolPath()),
			argFetch.str(),
			strBase64,
			strE30,
			output.str()))}),
		KV:               KV{P: pkCP, PC: pcYellow, ShowOut: true},
		Outputs:          na.vfsList(output),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(16)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: "build/scripts"},
	}

	node.UID = resourceFetchUID("base64:vcs.json:e30=", output.string())
	node.SelfUID = node.UID

	return emit.emit(node)
}

func composeLDCmdVcsInfo(tc ModuleToolchain, vcsCPath string) []STR {
	return []STR{
		tc.Python3,
		(ldVcsInfoVFS).str(),
		argVcsVcsJson.str(),
		internStr(vcsCPath),
		(ldSvnInterfaceVFS).str(),
	}
}

func composeLDCmdVcsCompile(p *Platform, tc ModuleToolchain, vcsCPath, vcsOPath string, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags []ARG, noCompilerWarnings bool) []STR {
	bundle := compileFlagBundleFor(p)
	cmdArgs := make([]STR, 0, 94+len(moduleCFlags)+len(peerCFlagsGlobal)+len(moduleScopeCFlags))
	cmdArgs = append(cmdArgs, tc.CC, p.TargetArg)
	cmdArgs = appendArgStr(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs,
		argDashBBin,
		argDashC.str(),
		argDashO.str(),
		internStr(vcsOPath),
		internStr(vcsCPath),
	)
	cmdArgs = append(cmdArgs, argIS.str())

	// The __vcs_version__.c compile sits at the LD node's "own slot": its
	// own-CFLAGS bucket starts with platform-level CFlags (sourced from
	// build/internal/ya.conf — -fno-omit-frame-pointer, -Wno-unknown-argument)
	// just like a regular CC compile assembles via composeOwnAndPeerCFlagsAtOwnSlot.
	// Forgetting p.CFlags here drops those two flags from this sub-cmd while
	// the rest of the module's CC compiles keep them, producing the same
	// post-defines tail divergence in every LD VCS sub-cmd.
	preNoLibcExtras := make([]ARG, 0, len(p.CFlags)+len(moduleCFlags)+len(peerCFlagsGlobal))
	preNoLibcExtras = append(preNoLibcExtras, p.CFlags...)
	preNoLibcExtras = append(preNoLibcExtras, moduleCFlags...)
	preNoLibcExtras = append(preNoLibcExtras, peerCFlagsGlobal...)

	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, pickWarningFlags(noCompilerWarnings, false), bundle.Defines, preNoLibcExtras, moduleScopeCFlags, catboostOpenSourceDefineFor(p))

	return cmdArgs
}

func composeLDCmdLinkExe(p *Platform, tc ModuleToolchain, outputPath, vcsOPath string, ccPaths []VFS, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths []VFS, objcopyPaths []VFS, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal []ARG, exportsScript *STR, wantsStrip bool) []STR {
	argCap := 2 + 6 + 1 + 2 + 1 + 1 + 3 + 1 + 2 + 2 + 3 + 16 + 1 + len(ccPaths) + len(peerLinkCmdPaths) + len(globalPaths) + len(objcopyPaths) + len(peerLDFlagsGlobal) + len(ownLDFlags) + len(ownRPathFlags) + len(peerRPathFlagsGlobal) + len(objAddLibsGlobal)

	argCap += 2 + len(pluginPaths)

	cmdArgs := make([]STR, 0, argCap)

	cmdArgs = append(cmdArgs,
		tc.Python3,
		(ldLinkExeVFS).str(),
	)

	cmdArgs = append(cmdArgs, argStartPlugins.str())

	for _, p := range pluginPaths {
		cmdArgs = append(cmdArgs, (p).str())
	}

	cmdArgs = append(cmdArgs, argEndPlugins.str())

	cmdArgs = append(cmdArgs,
		argClangVer.str(), internStr(p.ClangVer),
		argSourceRoot.str(), argS.str(),
		argBuildRoot.str(), argB.str(),
	)

	for _, p := range wholeArchiveCmdPaths {
		cmdArgs = append(cmdArgs, argWholeArchiveLibs.str(), internStr(p.rel()))
	}

	for _, p := range wholeArchivePaths {
		cmdArgs = append(cmdArgs, argWholeArchiveLibs.str(), internStr(p.rel()))
	}

	cmdArgs = append(cmdArgs,
		argArchLinux.str(),
		argObjcopyExe.str(), tc.Objcopy,
		tc.CXX,
		argWlWholeArchive.str(),
		argYaStartCommandFile.str(),
	)

	for _, p := range globalPaths {
		cmdArgs = append(cmdArgs, internStr(p.rel()))
	}

	cmdArgs = append(cmdArgs,
		argYaEndCommandFile.str(),
		argWlNoWholeArchive.str(),
	)

	for _, op := range objcopyPaths {
		cmdArgs = append(cmdArgs, internStr(op.rel()))
	}

	cmdArgs = append(cmdArgs, internStr(vcsOPath))

	for _, cp := range ccPaths {
		cmdArgs = append(cmdArgs, (cp).str())
	}

	cmdArgs = append(cmdArgs, argDashO.str(), internStr(outputPath))

	bundle := compileFlagBundleFor(p)
	cmdArgs = append(cmdArgs, p.TargetArg)
	cmdArgs = appendArgStr(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, argDashBBin)

	cmdArgs = append(cmdArgs, argWlStartGroup.str())

	for _, p := range peerLinkCmdPaths {
		cmdArgs = append(cmdArgs, internStr(p.rel()))
	}

	cmdArgs = append(cmdArgs, argWlEndGroup.str())

	cmdArgs = append(cmdArgs, composeProgramLinkTrailer(p, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal, exportsScript, wantsStrip)...)

	return cmdArgs
}

func composeProgramLinkTrailer(p *Platform, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal []ARG, exportsScript *STR, wantsStrip bool) []STR {
	// EXPORTS_SCRIPT appends the version-script flag right after -rdynamic
	// per upstream's EXPORTS_VALUE in build/conf/linkers/ld.conf:138. The macro
	// arg is already a source-root-relative path, not module-relative.
	trailer := []STR{argRdynamic.str()}

	if exportsScript != nil {
		trailer = append(trailer, internStr("-Wl,--version-script=$(S)/"+exportsScript.string()))
	}

	if p != nil && !p.PIC && p.CompressDebugSections {
		trailer = append(trailer, argWlCompressDebugSectionsZstd.str())
	}

	trailer = append(trailer, p.LinkPreludeExtra...)
	trailer = append(trailer, argWlNoAsNeeded.str())
	trailer = appendArgStr(trailer, ownRPathFlags)

	if p.PIC {
		trailer = append(trailer, (argFPIC).str())
	}

	trailer = appendInternStrs(trailer, p.linkerSelectionGDBIndexFlags())
	trailer = appendArgStr(trailer, peerRPathFlagsGlobal)

	if p.PIC {
		trailer = append(trailer, (argFPIC).str())
	}

	trailer = appendInternStrs(trailer, p.linkerSelectionTailFlags())
	trailer = appendArgStr(trailer, peerLDFlagsGlobal, ownLDFlags, objAddLibsGlobal)
	trailer = append(trailer, p.SystemLibs...)

	if wantsStrip {
		trailer = append(trailer, argWlStripAll.str())
	}

	trailer = append(trailer, argWlGcSections.str())
	trailer = appendInternStrs(trailer, p.linkerSelectionNoPieFlags())

	return trailer
}

func composeLDCmdLinkOrCopy(tc ModuleToolchain, modulePath string, dynamicPaths ...VFS) []STR {
	cmd := []STR{
		tc.Python3,
		(ldFsToolsVFS).str(),
		argLinkOrCopyToDir.str(),
		argNoCheck.str(),
	}

	for _, p := range dynamicPaths {
		cmd = append(cmd, (p).str())
	}

	cmd = append(cmd, (build(modulePath)).str())

	return cmd
}

func composeLDSplitDwarfCmds(na *NodeArenas, tc ModuleToolchain, outputPath string, enabled bool) []Cmd {
	if !enabled {
		return nil
	}

	debugPath := outputPath + ".debug"

	return na.cmdList(Cmd{CmdArgs: na.chunkList(na.strList(tc.Objcopy, argOnlyKeepDebug.str(), internStr(outputPath), internStr(debugPath)))}, Cmd{CmdArgs: na.chunkList(na.strList(tc.Strip, argStripDebug.str(), internStr(outputPath)))}, Cmd{CmdArgs: na.chunkList(na.strList(tc.Objcopy, argRemoveSectionGnuDebuglink.str(), argAddGnuDebuglink.str(), internStr(debugPath), internStr(outputPath)))})
}

func composeLDInputs(na *NodeArenas, modulePath string, ccPaths []VFS, peerLibPaths []VFS, pluginPaths []VFS, globalPaths []VFS, wholeArchivePaths []VFS, dynamicPaths []VFS, objcopyPaths []VFS, scripts ScriptDeps) InputChunks {
	chunks := make(InputChunks, 0, 3+len(ldScriptInputs))

	// peerLibPaths is the caller's member slice, dup-free by construction (gen's
	// peerArchive collection pass adds via deduper) — referenced as its own
	// chunk, never copied; it seeds the dedup set the remaining $(B) categories
	// are filtered through, so the flat order stays byte-identical.
	deduper.reset()

	for _, p := range peerLibPaths {
		if !deduper.add(p) {
			throwFmt("composeLDInputs: %s: duplicate peer lib path %s", modulePath, p.rel())
		}
	}

	chunks = append(chunks, peerLibPaths)

	buildRootBlock := make([]VFS, 0, len(pluginPaths)+len(globalPaths)+len(wholeArchivePaths)+len(dynamicPaths)+len(ccPaths)+len(objcopyPaths))
	appendBuildRoot := func(paths []VFS) {
		for _, p := range paths {
			if !deduper.add(p) {
				continue
			}

			buildRootBlock = append(buildRootBlock, p)
		}
	}

	appendBuildRoot(pluginPaths)
	appendBuildRoot(globalPaths)
	appendBuildRoot(wholeArchivePaths)
	appendBuildRoot(dynamicPaths)
	appendBuildRoot(ccPaths)

	appendBuildRoot(objcopyPaths)

	chunks = append(chunks, buildRootBlock)

	// ldScriptInputs seeds the link's $(S) tooling; expand each wrapper to its
	// import closure via the table (e.g. link_exe -> process_command_files,
	// thinlto_cache, process_whole_archive_option) — a shared table slice,
	// referenced as its own chunk. Non-script entries (svn_interface.c) are not
	// in the table and pass through. Dups (link_exe and fs_tools both import
	// process_command_files) are dropped in normalization.
	for _, s := range ldScriptInputs {
		if cl := scripts[s]; cl != nil {
			chunks = append(chunks, cl)
		} else {
			chunks = append(chunks, na.srcChunk(s))
		}
	}

	return chunks
}

type LdPluginsResult struct {
	Refs  []NodeRef
	Paths []VFS
}

func emitOwnLDPlugins(ctx *GenCtx, instance ModuleInstance, plugins []STR, tc ModuleToolchain) *LdPluginsResult {
	if len(plugins) == 0 {
		return nil
	}

	res := &LdPluginsResult{
		Refs:  make([]NodeRef, 0, len(plugins)),
		Paths: make([]VFS, 0, len(plugins)),
	}

	for _, name := range plugins {
		src := source(instance.Path.rel() + "/" + name.string())
		dst := build(instance.Path.rel() + "/" + name.string() + ".pyplugin")

		ref, ok := ctx.ldPluginCPCache[dst]

		if !ok {
			ref = emitCP(instance, src, dst, tc, ctx.scripts, ctx.emit)
			ctx.ldPluginCPCache[dst] = ref
		}

		res.Refs = append(res.Refs, ref)
		res.Paths = append(res.Paths, dst)
	}

	return res
}
