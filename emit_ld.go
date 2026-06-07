package main

var (
	ldVcsInfoVFS       = Intern("$(S)/build/scripts/vcs_info.py")
	ldSvnInterfaceVFS  = Intern("$(S)/build/scripts/c_templates/svn_interface.c")
	ldLinkExeVFS       = Intern("$(S)/build/scripts/link_exe.py")
	ldFsToolsVFS       = Intern("$(S)/build/scripts/fs_tools.py")
	ldSvnversionHVFS   = Intern("$(S)/build/scripts/c_templates/svnversion.h")
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
	programModuleTag string,
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
		binaryName = lastPathComponent(instance.Path)
	}

	binaryDir := ldBinaryDir(instance)

	binPrefix := binaryDir + "/"
	outputVFS := Build(binPrefix + binaryName)
	vcsCVFS := Build(binPrefix + "__vcs_version__.c")
	vcsOVFS := Build(binPrefix + "__vcs_version__.c" + instance.Platform.ObjectSuffix())

	vcsCPath := vcsCVFS.String()
	vcsOPath := vcsOVFS.String()
	outputPath := outputVFS.String()

	tools := instance.Platform.Tools
	cmd0 := composeLDCmdVcsInfo(tools, vcsCPath)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, vcsCPath, vcsOPath, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags, noCompilerWarnings)
	cmd2 := composeLDCmdLinkExe(instance.Platform, instance.Path, outputPath, vcsOPath, ccPaths, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths, dynamicPaths, objcopyPaths, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal, exportsScript, wantsStrip)
	cmd3 := composeLDCmdLinkOrCopy(tools, binaryDir, dynamicPaths...)
	splitDwarfCmds := composeLDSplitDwarfCmds(tools, outputPath, wantsSplitDwarf)

	envVcsOnly := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}
	envFull := hostP.ToolEnv()

	cmds := []Cmd{
		{CmdArgs: cmd0, Env: envVcsOnly},
		{CmdArgs: cmd1, Env: envFull},
		{CmdArgs: cmd2, Cwd: "$(B)", Env: envFull},
		{CmdArgs: cmd3, Env: envVcsOnly},
	}

	for i := range splitDwarfCmds {
		splitDwarfCmds[i].Env = envVcsOnly
	}

	cmds = append(cmds, splitDwarfCmds...)

	inputs := composeLDInputs(instance.Path, ccPaths, peerLibPaths, pluginPaths, globalPaths, wholeArchivePaths, dynamicPaths, objcopyPaths, scripts)

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
	outputs := []VFS{outputVFS}

	for _, p := range dynamicPaths {
		outputs = append(outputs, Build(binaryDir+"/"+lastPathComponent(p.Rel())))
	}

	if wantsSplitDwarf {
		outputs = append(outputs, Build(binPrefix+binaryName+".debug"))
	}

	n := &Node{
		Cmds:             cmds,
		Env:              envFull,
		Inputs:           inputs,
		Outputs:          outputs,
		KV:               KV{P: pkLD, PC: pcLightBlue, ShowOut: "yes"},
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		Tags:             instance.Platform.Tags,
		TargetProperties: TargetProperties{ModuleDir: binaryDir, ModuleLang: ldModuleLang(instance), ModuleType: "bin"},
		DepRefs:          depRefs,
	}

	if programModuleTag != "" {
		n.TargetProperties.ModuleTag = programModuleTag
	}

	return emit.Emit(bindNodePlatform(withResources(n, resourcePatternClangTool, resourcePatternLLDRoot, resourcePatternYMakePython3), instance.Platform))
}

func LDOutputPath(instance ModuleInstance, binaryName string) VFS {
	if binaryName == "" {
		binaryName = lastPathComponent(instance.Path)
	}

	return Build(ldBinaryDir(instance) + "/" + binaryName)
}

func ldBinaryDir(instance ModuleInstance) string {
	switch instance.Path {
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

	return instance.Path
}

func ldModuleLang(instance ModuleInstance) string {
	if instance.Language == LangPy {
		return "py3"
	}

	return "cpp"
}

func lastPathComponent(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}

	return p
}

func composeLDCmdVcsInfo(tools Toolchain, vcsCPath string) []ANY {
	return []ANY{
		stringAny(tools.Python3),
		vfsAny(ldVcsInfoVFS),
		stringAny("$(VCS)/vcs.json"),
		stringAny(vcsCPath),
		vfsAny(ldSvnInterfaceVFS),
	}
}

func composeLDCmdVcsCompile(p *Platform, vcsCPath, vcsOPath string, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags []ARG, noCompilerWarnings bool) []ANY {
	bundle := compileFlagBundleFor(p)
	cmdArgs := make([]ANY, 0, 94+len(moduleCFlags)+len(peerCFlagsGlobal)+len(moduleScopeCFlags))
	cmdArgs = append(cmdArgs, p.CCArg, p.TargetArg)
	cmdArgs = appendArgAny(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs,
		argDashBBin,
		argDashC,
		argDashO,
		stringAny(vcsOPath),
		stringAny(vcsCPath),
	)
	cmdArgs = append(cmdArgs, stringAny("-I$(S)"))

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

func composeLDCmdLinkExe(p *Platform, modulePath, outputPath, vcsOPath string, ccPaths []VFS, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths, dynamicPaths []VFS, objcopyPaths []VFS, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal []ARG, exportsScript *string, wantsStrip bool) []ANY {
	argCap := 2 + 6 + 1 + 2 + 1 + 1 + 3 + 1 + 2 + 2 + 3 + 16 + 1 + len(ccPaths) + len(peerLinkCmdPaths) + len(globalPaths) + len(objcopyPaths) + len(peerLDFlagsGlobal) + len(ownLDFlags) + len(ownRPathFlags) + len(peerRPathFlagsGlobal) + len(objAddLibsGlobal)

	argCap += 2 + len(pluginPaths)

	cmdArgs := make([]ANY, 0, argCap)

	cmdArgs = append(cmdArgs,
		stringAny(p.Tools.Python3),
		vfsAny(ldLinkExeVFS),
	)

	cmdArgs = append(cmdArgs, stringAny("--start-plugins"))

	for _, p := range pluginPaths {
		cmdArgs = append(cmdArgs, vfsAny(p))
	}

	cmdArgs = append(cmdArgs, stringAny("--end-plugins"))

	cmdArgs = append(cmdArgs,
		stringAny("--clang-ver"), stringAny(p.ClangVer),
		stringAny("--source-root"), stringAny("$(S)"),
		stringAny("--build-root"), stringAny("$(B)"),
	)

	for _, p := range wholeArchiveCmdPaths {
		cmdArgs = append(cmdArgs, stringAny("--whole-archive-libs"), stringAny(p.Rel()))
	}

	for _, p := range wholeArchivePaths {
		cmdArgs = append(cmdArgs, stringAny("--whole-archive-libs"), stringAny(p.Rel()))
	}

	cmdArgs = append(cmdArgs,
		stringAny("--arch=LINUX"),
		stringAny("--objcopy-exe"), stringAny(p.Tools.Objcopy),
		stringAny(p.Tools.CXX),
		stringAny("-Wl,--whole-archive"),
		stringAny("--ya-start-command-file"),
	)

	for _, p := range globalPaths {
		cmdArgs = append(cmdArgs, stringAny(p.Rel()))
	}

	cmdArgs = append(cmdArgs,
		stringAny("--ya-end-command-file"),
		stringAny("-Wl,--no-whole-archive"),
	)

	for _, op := range objcopyPaths {
		cmdArgs = append(cmdArgs, stringAny(op.Rel()))
	}

	cmdArgs = append(cmdArgs, stringAny(vcsOPath))

	for _, cp := range ccPaths {
		cmdArgs = append(cmdArgs, vfsAny(cp))
	}

	cmdArgs = append(cmdArgs, argDashO, stringAny(outputPath))

	bundle := compileFlagBundleFor(p)
	cmdArgs = append(cmdArgs, p.TargetArg)
	cmdArgs = appendArgAny(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, argDashBBin)

	cmdArgs = append(cmdArgs, stringAny("-Wl,--start-group"))

	for _, p := range peerLinkCmdPaths {
		cmdArgs = append(cmdArgs, stringAny(p.Rel()))
	}

	cmdArgs = append(cmdArgs, stringAny("-Wl,--end-group"))

	cmdArgs = append(cmdArgs, composeProgramLinkTrailer(p, modulePath, dynamicPaths, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal, exportsScript, wantsStrip)...)

	return cmdArgs
}

func composeProgramLinkTrailer(p *Platform, modulePath string, dynamicPaths []VFS, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal []ARG, exportsScript *string, wantsStrip bool) []ANY {
	// EXPORTS_SCRIPT appends the version-script flag right after -rdynamic
	// per upstream's EXPORTS_VALUE in build/conf/linkers/ld.conf:138. The macro
	// arg is already a source-root-relative path, not module-relative.
	_ = modulePath
	_ = dynamicPaths

	trailer := []ANY{stringAny("-rdynamic")}

	if exportsScript != nil {
		trailer = append(trailer, stringAny("-Wl,--version-script=$(S)/"+*exportsScript))
	}

	if p != nil && !p.PIC && p.Flags[envSANDBOXING] == strYes {
		trailer = append(trailer, stringAny("-Wl,--compress-debug-sections=zstd"))
	}

	trailer = appendStringAny(trailer, p.LinkPreludeExtra)
	trailer = append(trailer, stringAny("-Wl,--no-as-needed"))
	trailer = appendArgAny(trailer, ownRPathFlags)

	if p.PIC {
		trailer = append(trailer, argAny(argFPIC))
	}

	trailer = appendStringAny(trailer, p.LinkerSelectionGDBIndexFlags())
	trailer = appendArgAny(trailer, peerRPathFlagsGlobal)

	if p.PIC {
		trailer = append(trailer, argAny(argFPIC))
	}

	trailer = appendStringAny(trailer, p.LinkerSelectionTailFlags())
	trailer = appendArgAny(trailer, peerLDFlagsGlobal, ownLDFlags, objAddLibsGlobal)
	trailer = appendStringAny(trailer, p.SystemLibs)

	if wantsStrip {
		trailer = append(trailer, stringAny("-Wl,--strip-all"))
	}

	trailer = append(trailer, stringAny("-Wl,--gc-sections"))
	trailer = appendStringAny(trailer, p.LinkerSelectionNoPieFlags())

	return trailer
}

func composeLDCmdLinkOrCopy(tools Toolchain, modulePath string, dynamicPaths ...VFS) []ANY {
	cmd := []ANY{
		stringAny(tools.Python3),
		vfsAny(ldFsToolsVFS),
		stringAny("link_or_copy_to_dir"),
		stringAny("--no-check"),
	}

	for _, p := range dynamicPaths {
		cmd = append(cmd, vfsAny(p))
	}

	cmd = append(cmd, vfsAny(Build(modulePath)))

	return cmd
}

func composeLDSplitDwarfCmds(tools Toolchain, outputPath string, enabled bool) []Cmd {
	if !enabled {
		return nil
	}

	debugPath := outputPath + ".debug"

	return []Cmd{
		{CmdArgs: []ANY{stringAny(tools.Objcopy), stringAny("--only-keep-debug"), stringAny(outputPath), stringAny(debugPath)}},
		{CmdArgs: []ANY{stringAny(tools.Strip), stringAny("--strip-debug"), stringAny(outputPath)}},
		{CmdArgs: []ANY{stringAny(tools.Objcopy), stringAny("--remove-section=.gnu_debuglink"), stringAny("--add-gnu-debuglink"), stringAny(debugPath), stringAny(outputPath)}},
	}
}

func composeLDInputs(modulePath string, ccPaths []VFS, peerLibPaths []VFS, pluginPaths []VFS, globalPaths []VFS, wholeArchivePaths []VFS, dynamicPaths []VFS, objcopyPaths []VFS, scripts scriptDeps) []VFS {
	buildRootBlock := make([]VFS, 0, len(peerLibPaths)+len(pluginPaths)+len(globalPaths)+len(wholeArchivePaths)+len(dynamicPaths)+len(ccPaths)+len(objcopyPaths))
	buildRootSeen := make(map[VFS]struct{}, cap(buildRootBlock))
	appendBuildRoot := func(paths []VFS) {
		for _, p := range paths {
			if _, dup := buildRootSeen[p]; dup {
				continue
			}

			buildRootSeen[p] = struct{}{}
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
