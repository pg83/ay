package main

var (
	ldVcsInfoPath      = ldVcsInfoVFS.String()
	ldSvnInterfacePath = ldSvnInterfaceVFS.String()
	ldLinkExePath      = ldLinkExeVFS.String()
	ldFsToolsPath      = ldFsToolsVFS.String()
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

func EmitLD(
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
	exportsScript *string,
	noCompilerWarnings bool,
	wantsStrip bool,
	wantsSplitDwarf bool,
	programModuleTag STR,
	tc moduleToolchain,
	hostP *Platform,
	scripts scriptDeps,
	emit Emitter,
) NodeRef {
	if len(ccRefs) != len(ccPaths) {
		ThrowFmt("EmitLD: ccRefs/ccPaths length mismatch (%d vs %d)", len(ccRefs), len(ccPaths))
	}

	if len(peerLDRefs) != len(peerLibPaths) {
		ThrowFmt("EmitLD: peerLDRefs/peerLibPaths length mismatch (%d vs %d)", len(peerLDRefs), len(peerLibPaths))
	}

	if len(pluginRefs) != len(pluginPaths) {
		ThrowFmt("EmitLD: pluginRefs/pluginPaths length mismatch (%d vs %d)", len(pluginRefs), len(pluginPaths))
	}

	if len(globalRefs) != len(globalPaths) {
		ThrowFmt("EmitLD: globalRefs/globalPaths length mismatch (%d vs %d)", len(globalRefs), len(globalPaths))
	}

	if len(wholeArchiveRefs) != len(wholeArchivePaths) {
		ThrowFmt("EmitLD: wholeArchiveRefs/wholeArchivePaths length mismatch (%d vs %d)", len(wholeArchiveRefs), len(wholeArchivePaths))
	}

	if len(objcopyRefs) != len(objcopyPaths) {
		ThrowFmt("EmitLD: objcopyRefs/objcopyPaths length mismatch (%d vs %d)", len(objcopyRefs), len(objcopyPaths))
	}

	if len(dynamicRefs) != len(dynamicPaths) {
		ThrowFmt("EmitLD: dynamicRefs/dynamicPaths length mismatch (%d vs %d)", len(dynamicRefs), len(dynamicPaths))
	}

	if binaryName == "" {
		binaryName = lastPathComponent(instance.Path.Rel())
	}

	binaryDir := ldBinaryDir(instance)

	binPrefix := binaryDir + "/"
	outputVFS := Build(binPrefix + binaryName)
	vcsCVFS := Build(binPrefix + "__vcs_version__.c")
	vcsOVFS := Build(binPrefix + "__vcs_version__.c" + instance.Platform.ObjectSuffix())

	vcsCPath := vcsCVFS.String()
	vcsOPath := vcsOVFS.String()
	outputPath := outputVFS.String()

	cmd0 := composeLDCmdVcsInfo(tc, vcsCPath)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, tc, vcsCPath, vcsOPath, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags, noCompilerWarnings)
	cmd2 := composeLDCmdLinkExe(instance.Platform, tc, instance.Path.Rel(), outputPath, vcsOPath, ccPaths, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths, dynamicPaths, objcopyPaths, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal, exportsScript, wantsStrip)
	cmd3 := composeLDCmdLinkOrCopy(tc, binaryDir, dynamicPaths...)
	splitDwarfCmds := composeLDSplitDwarfCmds(tc, outputPath, wantsSplitDwarf)

	envVcsOnly := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	envFull := hostP.ToolEnv()

	cmds := []Cmd{
		{CmdArgs: cmd0, Env: envVcsOnly},
		{CmdArgs: cmd1, Env: envFull},
		{CmdArgs: cmd2, Cwd: strB, Env: envFull},
		{CmdArgs: cmd3, Env: envVcsOnly},
	}

	for i := range splitDwarfCmds {
		splitDwarfCmds[i].Env = envVcsOnly
	}

	cmds = append(cmds, splitDwarfCmds...)

	inputs := composeLDInputs(instance.Path.Rel(), ccPaths, peerLibPaths, pluginPaths, globalPaths, wholeArchivePaths, dynamicPaths, objcopyPaths, scripts)

	inputs = append(inputs, ldSvnversionHVFS)

	if exportsScript != nil {
		inputs = append(inputs, Source(*exportsScript))
	}

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
		outputs = append(outputs, Build(binaryDir+"/"+lastPathComponent(p.Rel())))
	}

	if wantsSplitDwarf {
		outputs = append(outputs, Build(binPrefix+binaryName+".debug"))
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
		usesResources:    []string{resourcePatternClangTool + instance.Platform.ClangVer, resourcePatternLLDRoot, resourcePatternYMakePython3},
	}

	if programModuleTag != 0 {
		n.TargetProperties.ModuleTag = programModuleTag
	}

	return emit.Emit(n)
}

func LDOutputPath(instance ModuleInstance, binaryName string) VFS {
	if binaryName == "" {
		binaryName = lastPathComponent(instance.Path.Rel())
	}

	return Build(ldBinaryDir(instance) + "/" + binaryName)
}

func ldBinaryDir(instance ModuleInstance) string {
	switch instance.Path.Rel() {
	case "contrib/tools/ragel6/bin":
		return "contrib/tools/ragel6"
	case "tools/py3cc/bin":
		return "tools/py3cc"
	case "tools/event2cpp/bin":
		return "tools/event2cpp"
	case "tools/rescompiler/bin":
		return "tools/rescompiler"
	case "tools/rescompressor/bin":
		return "tools/rescompressor"
	case "tools/py3cc/slow/bin":
		return "tools/py3cc/slow"
	}

	return instance.Path.Rel()
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
	output := bldVcsJson
	node := &Node{
		Platform: host,
		Cmds: []Cmd{{CmdArgs: []STR{
			internStr(currentYatoolPath()),
			argFetch.str(),
			strBase64,
			strE30,
			output.str(),
		}}},
		KV:               KV{P: pkCP, PC: pcYellow, ShowOut: true},
		Outputs:          []VFS{output},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(16)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: "build/scripts"},
	}

	node.UID = resourceFetchUID("base64:vcs.json:e30=", output.String())
	node.SelfUID = node.UID

	return emit.Emit(node)
}

func composeLDCmdVcsInfo(tc moduleToolchain, vcsCPath string) []STR {
	return []STR{
		tc.Python3,
		(ldVcsInfoVFS).str(),
		argVcsVcsJson.str(),
		internStr(vcsCPath),
		(ldSvnInterfaceVFS).str(),
	}
}

func composeLDCmdVcsCompile(p *Platform, tc moduleToolchain, vcsCPath, vcsOPath string, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags []ARG, noCompilerWarnings bool) []STR {
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

	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, pickWarningFlags(noCompilerWarnings, false), bundle.Defines, preNoLibcExtras, moduleScopeCFlags)

	return cmdArgs
}

func composeLDCmdLinkExe(p *Platform, tc moduleToolchain, modulePath, outputPath, vcsOPath string, ccPaths []VFS, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths, dynamicPaths []VFS, objcopyPaths []VFS, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal []ARG, exportsScript *string, wantsStrip bool) []STR {
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
		cmdArgs = append(cmdArgs, argWholeArchiveLibs.str(), internStr(p.Rel()))
	}

	for _, p := range wholeArchivePaths {
		cmdArgs = append(cmdArgs, argWholeArchiveLibs.str(), internStr(p.Rel()))
	}

	cmdArgs = append(cmdArgs,
		argArchLinux.str(),
		argObjcopyExe.str(), tc.Objcopy,
		tc.CXX,
		argWlWholeArchive.str(),
		argYaStartCommandFile.str(),
	)

	for _, p := range globalPaths {
		cmdArgs = append(cmdArgs, internStr(p.Rel()))
	}

	cmdArgs = append(cmdArgs,
		argYaEndCommandFile.str(),
		argWlNoWholeArchive.str(),
	)

	for _, op := range objcopyPaths {
		cmdArgs = append(cmdArgs, internStr(op.Rel()))
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
		cmdArgs = append(cmdArgs, internStr(p.Rel()))
	}

	cmdArgs = append(cmdArgs, argWlEndGroup.str())

	cmdArgs = append(cmdArgs, composeProgramLinkTrailer(p, modulePath, dynamicPaths, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal, exportsScript, wantsStrip)...)

	return cmdArgs
}

func composeProgramLinkTrailer(p *Platform, modulePath string, dynamicPaths []VFS, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal []ARG, exportsScript *string, wantsStrip bool) []STR {
	// EXPORTS_SCRIPT appends the version-script flag right after -rdynamic
	// per upstream's EXPORTS_VALUE in build/conf/linkers/ld.conf:138. The macro
	// arg is already a source-root-relative path, not module-relative.
	_ = modulePath
	_ = dynamicPaths

	trailer := []STR{argRdynamic.str()}

	if exportsScript != nil {
		trailer = append(trailer, internStr("-Wl,--version-script=$(S)/"+*exportsScript))
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

	trailer = appendInternStrs(trailer, p.LinkerSelectionGDBIndexFlags())
	trailer = appendArgStr(trailer, peerRPathFlagsGlobal)

	if p.PIC {
		trailer = append(trailer, (argFPIC).str())
	}

	trailer = appendInternStrs(trailer, p.LinkerSelectionTailFlags())
	trailer = appendArgStr(trailer, peerLDFlagsGlobal, ownLDFlags, objAddLibsGlobal)
	trailer = append(trailer, p.SystemLibs...)

	if wantsStrip {
		trailer = append(trailer, argWlStripAll.str())
	}

	trailer = append(trailer, argWlGcSections.str())
	trailer = appendInternStrs(trailer, p.LinkerSelectionNoPieFlags())

	return trailer
}

func composeLDCmdLinkOrCopy(tc moduleToolchain, modulePath string, dynamicPaths ...VFS) []STR {
	cmd := []STR{
		tc.Python3,
		(ldFsToolsVFS).str(),
		argLinkOrCopyToDir.str(),
		argNoCheck.str(),
	}

	for _, p := range dynamicPaths {
		cmd = append(cmd, (p).str())
	}

	cmd = append(cmd, (Build(modulePath)).str())

	return cmd
}

func composeLDSplitDwarfCmds(tc moduleToolchain, outputPath string, enabled bool) []Cmd {
	if !enabled {
		return nil
	}

	debugPath := outputPath + ".debug"

	return []Cmd{
		{CmdArgs: []STR{tc.Objcopy, argOnlyKeepDebug.str(), internStr(outputPath), internStr(debugPath)}},
		{CmdArgs: []STR{tc.Strip, argStripDebug.str(), internStr(outputPath)}},
		{CmdArgs: []STR{tc.Objcopy, argRemoveSectionGnuDebuglink.str(), argAddGnuDebuglink.str(), internStr(debugPath), internStr(outputPath)}},
	}
}

func composeLDInputs(modulePath string, ccPaths []VFS, peerLibPaths []VFS, pluginPaths []VFS, globalPaths []VFS, wholeArchivePaths []VFS, dynamicPaths []VFS, objcopyPaths []VFS, scripts scriptDeps) []VFS {
	buildRootBlock := make([]VFS, 0, len(peerLibPaths)+len(pluginPaths)+len(globalPaths)+len(wholeArchivePaths)+len(dynamicPaths)+len(ccPaths)+len(objcopyPaths))
	deduper.reset()
	appendBuildRoot := func(paths []VFS) {
		for _, p := range paths {
			if !deduper.add(p) {
				continue
			}

			buildRootBlock = append(buildRootBlock, p)
		}
	}

	appendBuildRoot(peerLibPaths)
	appendBuildRoot(pluginPaths)
	appendBuildRoot(globalPaths)
	appendBuildRoot(wholeArchivePaths)
	appendBuildRoot(dynamicPaths)
	appendBuildRoot(ccPaths)

	appendBuildRoot(objcopyPaths)

	out := make([]VFS, 0, len(buildRootBlock)+len(ldScriptInputs)+4)
	out = append(out, buildRootBlock...)

	// ldScriptInputs seeds the link's $(S) tooling; expand each wrapper to its
	// import closure via the table (e.g. link_exe -> process_command_files,
	// thinlto_cache, process_whole_archive_option). Non-script entries
	// (svn_interface.c) are not in the table and pass through. Dups (link_exe and
	// fs_tools both import process_command_files) are dropped in normalization.
	for _, s := range ldScriptInputs {
		if cl := scripts[s]; cl != nil {
			out = append(out, cl...)
		} else {
			out = append(out, s)
		}
	}

	_ = modulePath

	return out
}
