package main

import "encoding/base64"

var (
	ldVcsInfoPath      = ldVcsInfoVFS.string()
	ldSvnInterfacePath = ldSvnInterfaceVFS.string()
	ldLinkExePath      = ldLinkExeVFS.string()
	ldFsToolsPath      = ldFsToolsVFS.string()
	vcsJSONBase64      = base64.StdEncoding.EncodeToString([]byte(vcsJSONContent))
)

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
	sbomRefs []NodeRef,
	sbomPaths []VFS,
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
	noOptimize bool,
	wantsStrip bool,
	useArcadiaLibm bool,
	wantsSplitDwarf bool,
	programModuleTag STR,
	hasBundles bool,
	tc ModuleToolchain,
	hostP *Platform,
	scripts ScriptDeps,
	emit Emitter,
	vcsRef NodeRef,
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

	binaryDir := instance.Path.rel()

	binPrefix := binaryDir + "/"
	outputVFS := build(binPrefix + binaryName)
	vcsCVFS := build(binPrefix + "__vcs_version__.c")
	vcsOVFS := build(binPrefix + "__vcs_version__.c" + instance.Platform.objectSuffix())

	vcsCPath := vcsCVFS.string()
	vcsOPath := vcsOVFS.string()
	outputPath := outputVFS.string()

	cmd0 := composeLDCmdVcsInfo(tc, vcsCPath)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, tc, vcsCPath, vcsOPath, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags, noCompilerWarnings, noOptimize)
	cmd2 := composeLDCmdLinkExe(instance.Platform, tc, outputPath, vcsOPath, ccPaths, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths, objcopyPaths, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal, exportsScript, wantsStrip, useArcadiaLibm)
	splitDwarfCmds := composeLDSplitDwarfCmds(na, tc, outputPath, wantsSplitDwarf)

	envVcsOnly := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	envFull := hostP.toolEnv()

	sbomEmbed := len(sbomPaths) > 0
	sbomJSON := build(binPrefix + "__sbomdata.json").string()

	cmds := na.cmdList(Cmd{CmdArgs: na.chunkList(cmd0), Env: envVcsOnly}, Cmd{CmdArgs: na.chunkList(cmd1), Env: envFull})

	if sbomEmbed {
		linkSbom := composeLDCmdLinkSbom(tc, ldSbomLang(instance), binaryDir, sbomJSON, sbomPaths)
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(linkSbom), Cwd: strB, Env: envVcsOnly})
	}

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(cmd2), Cwd: strB, Env: envFull})

	emitCopy := instance.Platform.Flags[envOPENSOURCE] == strYes || len(dynamicPaths) > 0

	if emitCopy {
		cmd3 := composeLDCmdLinkOrCopy(tc, binaryDir, dynamicPaths...)
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(cmd3), Env: envVcsOnly})
	}

	for i := range splitDwarfCmds {
		splitDwarfCmds[i].Env = envVcsOnly
	}

	cmds = append(cmds, splitDwarfCmds...)

	if sbomEmbed {
		objcopy := composeLDCmdSbomObjcopy(tc, sbomJSON, outputPath)
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(objcopy), Env: envVcsOnly})
	}

	inputs := composeLDInputs(na, instance.Path.rel(), ccPaths, peerLibPaths, pluginPaths, globalPaths, wholeArchivePaths, dynamicPaths, objcopyPaths, scripts, emitCopy, hasBundles)

	inputTail := make([]VFS, 0, 2)
	inputTail = append(inputTail, ldSvnversionHVFS)

	if exportsScript != nil {
		inputTail = append(inputTail, source(exportsScript.string()))
	}

	inputs = append(inputs, inputTail)

	if len(sbomPaths) > 0 {
		inputs = append(inputs, sbomPaths)
		inputs = append(inputs, []VFS{linkSbomScriptVFS})
	}

	deps := make([]NodeRef, 0, len(ccRefs)+len(pluginRefs)+len(globalRefs)+len(peerLDRefs)+len(dynamicRefs)+len(objcopyRefs))
	deps = append(deps, ccRefs...)
	deps = append(deps, pluginRefs...)
	deps = append(deps, globalRefs...)
	deps = append(deps, peerLDRefs...)
	deps = append(deps, dynamicRefs...)
	deps = append(deps, objcopyRefs...)
	deps = append(deps, sbomRefs...)
	deps = append(deps, vcsRef)
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
		DepRefs:          deps,
		Resources:        instance.Platform.UsesLinkResources,
	}

	if programModuleTag != 0 {
		n.TargetProperties.ModuleTag = programModuleTag
	}

	return emit.emit(n)
}

func lDOutputPath(instance ModuleInstance, binaryName string) VFS {
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

const vcsJSONContent = `{
    "ARCADIA_SOURCE_HG_HASH": "0000000000000000000000000000000000000000",
    "ARCADIA_SOURCE_LAST_AUTHOR": "<UNKNOWN>",
    "ARCADIA_SOURCE_LAST_CHANGE": -1,
    "ARCADIA_SOURCE_PATH": "/",
    "ARCADIA_SOURCE_REVISION": -1,
    "ARCADIA_SOURCE_URL": "",
    "BRANCH": "unknown-vcs-branch",
    "BUILD_DATE": "",
    "BUILD_TIMESTAMP": 0,
    "BUILD_HOST": "localhost",
    "BUILD_USER": "nobody",
    "CUSTOM_VERSION": "",
    "RELEASE_VERSION": "",
    "PROGRAM_VERSION": "Arc info:\n    Branch: unknown-vcs-branch\n    Commit: 0000000000000000000000000000000000000000\n    Author: <UNKNOWN>\n    Summary: No VCS\n\n",
    "SCM_DATA": "Arc info:\n    Branch: unknown-vcs-branch\n    Commit: 0000000000000000000000000000000000000000\n    Author: <UNKNOWN>\n    Summary: No VCS\n",
    "VCS": "arc",
    "ARCADIA_PATCH_NUMBER": 0,
    "ARCADIA_TAG": ""
}`

func emitVCSNode(emit Emitter, host *Platform) NodeRef {
	na := emit.nodeArenas()

	output := bldVcsJson
	node := &Node{
		Platform: host,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.strList(internStr(currentYatoolPath()),
			argFetch.str(),
			strBase64,
			internStr(vcsJSONBase64),
			output.str()))}),
		KV:               KV{P: pkCP, PC: pcYellow, ShowOut: true},
		Outputs:          na.vfsList(output),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(16)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: "build/scripts"},
	}

	node.UID = resourceFetchUID("base64:vcs.json:"+vcsJSONBase64, output.string())
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

func composeLDCmdVcsCompile(p *Platform, tc ModuleToolchain, vcsCPath, vcsOPath string, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags []ARG, noCompilerWarnings, noOptimize bool) []STR {
	bundle := compileFlagBundleFor(p)

	if noOptimize {
		bundle.CFlags = suppressOptimize(bundle.CFlags)
	}

	cmdArgs := make([]STR, 0, 94+len(moduleCFlags)+len(peerCFlagsGlobal)+len(moduleScopeCFlags))
	cmdArgs = append(cmdArgs, tc.CC, p.TargetArg)
	cmdArgs = appendArgStr(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, p.SysrootArgs...)
	cmdArgs = append(cmdArgs,
		argDashC.str(),
		argDashO.str(),
		internStr(vcsOPath),
		internStr(vcsCPath),
	)
	cmdArgs = append(cmdArgs, argIS.str())

	preNoLibcExtras := make([]ARG, 0, len(p.CFlags)+len(moduleCFlags)+len(peerCFlagsGlobal))
	preNoLibcExtras = append(preNoLibcExtras, p.CFlags...)
	preNoLibcExtras = append(preNoLibcExtras, moduleCFlags...)
	preNoLibcExtras = append(preNoLibcExtras, peerCFlagsGlobal...)

	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, pickWarningFlags(noCompilerWarnings, false), bundle.Defines, preNoLibcExtras, moduleScopeCFlags, catboostOpenSourceDefineFor(p))

	return cmdArgs
}

func composeLDCmdLinkExe(p *Platform, tc ModuleToolchain, outputPath, vcsOPath string, ccPaths []VFS, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths []VFS, objcopyPaths []VFS, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal []ARG, exportsScript *STR, wantsStrip, useArcadiaLibm bool) []STR {
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
	cmdArgs = append(cmdArgs, p.SysrootArgs...)

	cmdArgs = append(cmdArgs, argWlStartGroup.str())

	for _, p := range peerLinkCmdPaths {
		cmdArgs = append(cmdArgs, internStr(p.rel()))
	}

	cmdArgs = append(cmdArgs, argWlEndGroup.str())

	cmdArgs = append(cmdArgs, composeProgramLinkTrailer(p, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal, exportsScript, wantsStrip, useArcadiaLibm)...)

	return cmdArgs
}

func composeProgramLinkTrailer(p *Platform, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal []ARG, exportsScript *STR, wantsStrip, useArcadiaLibm bool) []STR {
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
	trailer = appendArgStr(trailer, peerLDFlagsGlobal, ownLDFlags)

	trailer = appendArgGroupStr(trailer, objAddLibsGlobal)
	trailer = append(trailer, p.SystemLibs...)

	if !useArcadiaLibm {
		trailer = append(trailer, argDashLm.str())
	}

	if wantsStrip {
		trailer = append(trailer, argWlStripAll.str())
	}

	trailer = append(trailer, argWlGcSections.str())
	trailer = appendInternStrs(trailer, p.linkerSelectionNoPieFlags())

	return trailer
}

func ldSbomLang(instance ModuleInstance) string {
	if ldModuleLang(instance) == mlPy3 {
		return "PY3"
	}

	return "CPP"
}

func composeLDCmdLinkSbom(tc ModuleToolchain, lang, moddir, sbomJSON string, sbomPaths []VFS) []STR {
	cmd := make([]STR, 0, 10+len(sbomPaths))
	cmd = append(cmd,
		tc.Python3,
		linkSbomScriptVFS.str(),
		strLang, internStr(lang),
		strModPath, internStr(moddir),
		strOutput, internStr(sbomJSON),
		strVcsInfo, argVcsVcsJson.str(),
	)

	for _, p := range sbomPaths {
		cmd = append(cmd, p.str())
	}

	return cmd
}

func composeLDCmdSbomObjcopy(tc ModuleToolchain, sbomJSON, targetPath string) []STR {
	return []STR{
		tc.Objcopy,
		strAddSection,
		internStr(".rosbomdata=" + sbomJSON),
		internStr(targetPath),
	}
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

func composeLDInputs(na *NodeArenas, modulePath string, ccPaths []VFS, peerLibPaths []VFS, pluginPaths []VFS, globalPaths []VFS, wholeArchivePaths []VFS, dynamicPaths []VFS, objcopyPaths []VFS, scripts ScriptDeps, emitCopy bool, hasBundles bool) InputChunks {
	chunks := make(InputChunks, 0, 3+len(ldScriptInputs))

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

	for _, s := range ldScriptInputs {
		if s == copyFsToolsVFS && !emitCopy && !hasBundles {
			continue
		}

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

	cpInstance := instance
	cpInstance.Platform = ctx.target

	for _, name := range plugins {
		src := source(instance.Path.rel() + "/" + name.string())
		dst := build(instance.Path.rel() + "/" + name.string() + ".pyplugin")

		ref := emitCP(cpInstance, src, dst, tc, ctx.scripts, ctx.emit)

		res.Refs = append(res.Refs, ref)
		res.Paths = append(res.Paths, dst)
	}

	return res
}
