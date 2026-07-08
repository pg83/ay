package main

import "encoding/base64"

var (
	ldVcsInfoPath      = ldVcsInfoVFS.string()
	ldSvnInterfacePath = ldSvnInterfaceVFS.string()
	ldLinkExePath      = ldLinkExeVFS.string()
	ldFsToolsPath      = ldFsToolsVFS.string()
	vcsJSONBase64      = base64.StdEncoding.EncodeToString([]byte(vcsJSONContent))
	ldKV               = KV{P: pkLD, PC: pcLightBlue, ShowOut: true}
	ldKV2              = KV{P: pkCP, PC: pcYellow, ShowOut: true}
)

var ldScriptInputs = []VFS{
	ldVcsInfoVFS,
	ldSvnInterfaceVFS,
	ldLinkExeVFS,
	copyFsToolsVFS,
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
	moduleCFlags []ANY,
	peerCFlagsGlobal []ANY,
	moduleScopeCFlags []ANY,
	peerLDFlagsGlobal []ANY,
	ownLDFlags []ANY,
	ownRPathFlags []ANY,
	peerRPathFlagsGlobal []ANY,
	objAddLibsGlobal []ANY,
	exportsScript *STR,
	noExportDynSymbols bool,
	noCompilerWarnings bool,
	noOptimize bool,
	wantsStrip bool,
	useArcadiaLibm bool,
	wantsSplitDwarf bool,
	programModuleTag STR,
	sbomLang STR,
	hasBundles bool,
	tc ModuleToolchain,
	hostP *Platform,
	scripts ScriptDeps,
	emit *StreamingEmitter,
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

	binaryDir := instance.Path.relString()
	binPrefix := binaryDir + "/"
	outputVFS := build(binPrefix, binaryName)
	vcsCVFS := build(binPrefix, "__vcs_version__.c")
	vcsOVFS := build(binPrefix, "__vcs_version__.c", instance.Platform.objectSuffix())
	cmd0 := composeLDCmdVcsInfo(tc, vcsCVFS)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, tc, vcsCVFS, vcsOVFS, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags, noCompilerWarnings, noOptimize)
	cmd2 := composeLDCmdLinkExe(instance.Platform, tc, outputVFS, vcsOVFS, ccPaths, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths, objcopyPaths, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal, exportsScript, noExportDynSymbols, wantsStrip, useArcadiaLibm)
	splitDwarfCmds := composeLDSplitDwarfCmds(na, tc, outputVFS, wantsSplitDwarf)
	envVcsOnly := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	envFull := hostP.toolEnv()
	sbomEmbed := len(sbomPaths) > 0
	sbomJSON := build(binPrefix, "__sbomdata.json")
	cmds := na.cmdList(Cmd{CmdArgs: na.chunkList(cmd0), Env: envVcsOnly}, Cmd{CmdArgs: na.chunkList(cmd1), Env: envFull})

	if sbomEmbed {
		linkSbom := composeLDCmdLinkSbom(tc, sbomLang, instance.Path.rel(), sbomJSON, sbomPaths)

		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(linkSbom), Cwd: bldRootDirVFS, Env: envVcsOnly})
	}

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(cmd2), Cwd: bldRootDirVFS, Env: envFull})

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
		objcopy := composeLDCmdSbomObjcopy(tc, sbomJSON, outputVFS)

		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(objcopy), Env: envVcsOnly})
	}

	inputs := composeLDInputs(na, instance.Path.relString(), ccPaths, peerLibPaths, pluginPaths, globalPaths, wholeArchivePaths, dynamicPaths, objcopyPaths, scripts, emitCopy, hasBundles)
	inputTail := make([]VFS, 0, 2)

	inputTail = append(inputTail, ldSvnversionHVFS)

	if exportsScript != nil {
		inputTail = append(inputTail, exportsScript.source())
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
		outputs = append(outputs, build(binaryDir, "/", baseName(p.relString())))
	}

	if wantsSplitDwarf {
		outputs = append(outputs, build(binPrefix, binaryName, ".debug"))
	}

	n := Node{
		Platform:     instance.Platform,
		Cmds:         cmds,
		Env:          envFull,
		Inputs:       inputs,
		Outputs:      outputs,
		KV:           &ldKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      deps,
		Resources:    instance.Platform.UsesLinkResources,
	}

	return emit.emitNode(n)
}

func lDOutputPath(instance ModuleInstance, binaryName string) VFS {
	return build(instance.Path.relString(), "/", binaryName)
}

func emitVCSNode(emit *StreamingEmitter, host *Platform) NodeRef {
	na := emit.nodeArenas()
	output := bldVcsJson

	node := Node{
		Platform: host,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(internStr(currentYatoolPath()).any(),
			argFetch.any(),
			strBase64.any(),
			internStr(vcsJSONBase64).any(),
			output.any()))}),
		KV:           &ldKV2,
		Outputs:      na.vfsList(output),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(16)},
	}

	node.PresetUID = resourceFetchUID("base64:vcs.json:"+vcsJSONBase64, output.string())

	return emit.emitNode(node)
}

func composeLDCmdVcsInfo(tc ModuleToolchain, vcsC VFS) []ANY {
	return []ANY{
		tc.Python3.any(),
		(ldVcsInfoVFS).any(),
		argVcsVcsJson.any(),
		vcsC.any(),
		(ldSvnInterfaceVFS).any(),
	}
}

func composeLDCmdVcsCompile(p *Platform, tc ModuleToolchain, vcsC, vcsO VFS, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags []ANY, noCompilerWarnings, noOptimize bool) []ANY {
	return composeLDCmdVcsCompileForced(p, tc, vcsC, vcsO, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags, noCompilerWarnings, noOptimize, false)
}

func composeLDCmdVcsCompileForced(p *Platform, tc ModuleToolchain, vcsC, vcsO VFS, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags []ANY, noCompilerWarnings, noOptimize, forceConsistentDebug bool) []ANY {
	bundle := compileFlagBundleFor(p)

	if noOptimize {
		bundle.CFlags = suppressOptimize(bundle.CFlags)
	}

	cmdArgs := make([]ANY, 0, 94+len(moduleCFlags)+len(peerCFlagsGlobal)+len(moduleScopeCFlags))

	cmdArgs = append(cmdArgs, tc.CC.any(), p.TargetArg.any())
	cmdArgs = appendAnyLists(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, p.SysrootArgs...)

	cmdArgs = append(cmdArgs,
		argDashC.any(),
		argDashO.any(),
		vcsO.any(),
		vcsC.any(),
	)

	cmdArgs = append(cmdArgs, argIS.any())

	if forceConsistentDebug {
		cmdArgs = appendAnyLists(cmdArgs, debugPrefixMapFlags, xclangDebugCompilationDir)
	}

	preNoLibcExtras := concat(p.CFlags, moduleCFlags, peerCFlagsGlobal)

	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, pickWarningFlags(noCompilerWarnings, false), bundle.Defines, preNoLibcExtras, moduleScopeCFlags, catboostOpenSourceDefineFor(p))

	return cmdArgs
}

func composeLDCmdLinkExe(p *Platform, tc ModuleToolchain, output, vcsO VFS, ccPaths []VFS, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths []VFS, objcopyPaths []VFS, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal []ANY, exportsScript *STR, noExportDynSymbols, wantsStrip, useArcadiaLibm bool) []ANY {
	argCap := 2 + 6 + 1 + 2 + 1 + 1 + 3 + 1 + 2 + 2 + 3 + 16 + 1 + len(ccPaths) + len(peerLinkCmdPaths) + len(globalPaths) + len(objcopyPaths) + len(peerLDFlagsGlobal) + len(ownLDFlags) + len(ownRPathFlags) + len(peerRPathFlagsGlobal) + len(objAddLibsGlobal)

	argCap += 2 + len(pluginPaths)

	cmdArgs := make([]ANY, 0, argCap)

	cmdArgs = append(cmdArgs,
		tc.Python3.any(),
		(ldLinkExeVFS).any(),
	)

	cmdArgs = append(cmdArgs, argStartPlugins.any())

	for _, p := range pluginPaths {
		cmdArgs = append(cmdArgs, (p).any())
	}

	cmdArgs = append(cmdArgs, argEndPlugins.any())

	cmdArgs = append(cmdArgs,
		argClangVer.any(), internStr(p.ClangVer).any(),
		argSourceRoot.any(), argS.any(),
		argBuildRoot.any(), argB.any(),
	)

	for _, p := range wholeArchiveCmdPaths {
		cmdArgs = append(cmdArgs, argWholeArchiveLibs.any(), p.rel().any())
	}

	for _, p := range wholeArchivePaths {
		cmdArgs = append(cmdArgs, argWholeArchiveLibs.any(), p.rel().any())
	}

	cmdArgs = append(cmdArgs,
		argArchLinux.any(),
		argObjcopyExe.any(), tc.Objcopy.any(),
		tc.CXX.any(),
		argWlWholeArchive.any(),
		argYaStartCommandFile.any(),
	)

	for _, p := range globalPaths {
		cmdArgs = append(cmdArgs, p.rel().any())
	}

	cmdArgs = append(cmdArgs,
		argYaEndCommandFile.any(),
		argWlNoWholeArchive.any(),
	)

	for _, op := range objcopyPaths {
		cmdArgs = append(cmdArgs, op.rel().any())
	}

	cmdArgs = append(cmdArgs, vcsO.any())

	for _, cp := range ccPaths {
		cmdArgs = append(cmdArgs, (cp).any())
	}

	cmdArgs = append(cmdArgs, argDashO.any(), output.any())

	bundle := compileFlagBundleFor(p)

	cmdArgs = append(cmdArgs, p.TargetArg.any())
	cmdArgs = appendAnyLists(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, p.SysrootArgs...)
	cmdArgs = append(cmdArgs, argWlStartGroup.any())

	for _, p := range peerLinkCmdPaths {
		cmdArgs = append(cmdArgs, p.rel().any())
	}

	cmdArgs = append(cmdArgs, argWlEndGroup.any())
	cmdArgs = append(cmdArgs, composeProgramLinkTrailer(p, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal, exportsScript, noExportDynSymbols, wantsStrip, useArcadiaLibm)...)

	return cmdArgs
}

func composeProgramLinkTrailer(p *Platform, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal []ANY, exportsScript *STR, noExportDynSymbols, wantsStrip, useArcadiaLibm bool) []ANY {
	var trailer []ANY

	if !noExportDynSymbols {
		trailer = append(trailer, argRdynamic.any())

		if exportsScript != nil {
			trailer = append(trailer, internV("-Wl,--version-script=$(S)/", exportsScript.string()).any())
		}
	}

	if p != nil && !p.PIC && p.CompressDebugSections {
		trailer = append(trailer, argWlCompressDebugSectionsZstd.any())
	}

	trailer = append(trailer, p.LinkPreludeExtra...)
	trailer = append(trailer, argWlNoAsNeeded.any())
	trailer = appendAnyLists(trailer, ownRPathFlags)

	if p.PIC {
		trailer = append(trailer, (argFPIC).any())
	}

	trailer = appendInternAnys(trailer, p.linkerSelectionGDBIndexFlags())
	trailer = appendAnyLists(trailer, peerRPathFlagsGlobal)

	if p.PIC {
		trailer = append(trailer, (argFPIC).any())
	}

	trailer = appendInternAnys(trailer, p.linkerSelectionTailFlags())
	trailer = appendAnyLists(trailer, peerLDFlagsGlobal, ownLDFlags)
	trailer = appendArgGroupStr(trailer, objAddLibsGlobal)
	trailer = append(trailer, p.SystemLibs...)

	if !useArcadiaLibm {
		trailer = append(trailer, argDashLm.any())
	}

	if wantsStrip {
		trailer = append(trailer, argWlStripAll.any())
	}

	trailer = append(trailer, argWlGcSections.any())
	trailer = appendInternAnys(trailer, p.linkerSelectionNoPieFlags())

	return trailer
}

func composeLDCmdLinkSbom(tc ModuleToolchain, lang, moddir STR, sbomJSON VFS, sbomPaths []VFS) []ANY {
	cmd := make([]ANY, 0, 10+len(sbomPaths))

	cmd = append(cmd,
		tc.Python3.any(),
		linkSbomScriptVFS.any(),
		strLang.any(), lang.any(),
		strModPath.any(), moddir.any(),
		strOutput.any(), sbomJSON.any(),
		strVcsInfo.any(), argVcsVcsJson.any(),
	)

	for _, p := range sbomPaths {
		cmd = append(cmd, p.any())
	}

	return cmd
}

func composeLDCmdSbomObjcopy(tc ModuleToolchain, sbomJSON, target VFS) []ANY {
	return []ANY{
		tc.Objcopy.any(),
		strAddSection.any(),
		internV(".rosbomdata=", sbomJSON.prefix(), sbomJSON.relString()).any(),
		target.any(),
	}
}

func composeLDCmdLinkOrCopy(tc ModuleToolchain, modulePath string, dynamicPaths ...VFS) []ANY {
	cmd := []ANY{
		tc.Python3.any(),
		(ldFsToolsVFS).any(),
		argLinkOrCopyToDir.any(),
		argNoCheck.any(),
	}

	for _, p := range dynamicPaths {
		cmd = append(cmd, (p).any())
	}

	cmd = append(cmd, (build(modulePath)).any())

	return cmd
}

func composeLDSplitDwarfCmds(na *NodeArenas, tc ModuleToolchain, output VFS, enabled bool) []Cmd {
	if !enabled {
		return nil
	}

	debug := internV(output.relString(), ".debug").build()

	return na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(tc.Objcopy.any(), argOnlyKeepDebug.any(), output.any(), debug.any()))}, Cmd{CmdArgs: na.chunkList(na.anyList(tc.Strip.any(), argStripDebug.any(), output.any()))}, Cmd{CmdArgs: na.chunkList(na.anyList(tc.Objcopy.any(), argRemoveSectionGnuDebuglink.any(), argAddGnuDebuglink.any(), debug.any(), output.any()))})
}

func composeLDInputs(na *NodeArenas, modulePath string, ccPaths []VFS, peerLibPaths []VFS, pluginPaths []VFS, globalPaths []VFS, wholeArchivePaths []VFS, dynamicPaths []VFS, objcopyPaths []VFS, scripts ScriptDeps, emitCopy bool, hasBundles bool) InputChunks {
	chunks := make(InputChunks, 0, 3+len(ldScriptInputs))

	deduper.reset()

	for _, p := range peerLibPaths {
		if !deduper.add(p.strID()) {
			throwFmt("composeLDInputs: %s: duplicate peer lib path %s", modulePath, p.relString())
		}
	}

	chunks = append(chunks, peerLibPaths)

	buildRootBlock := make([]VFS, 0, len(pluginPaths)+len(globalPaths)+len(wholeArchivePaths)+len(dynamicPaths)+len(ccPaths)+len(objcopyPaths))

	appendBuildRoot := func(paths []VFS) {
		for _, p := range paths {
			if !deduper.add(p.strID()) {
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

		if cl := scripts[s.rel()]; cl != nil {
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
		src := source(instance.Path.relString(), "/", name.string())
		dst := build(instance.Path.relString(), "/", name.string(), ".pyplugin")
		ref := emitCP(cpInstance, src, dst, tc, ctx.scripts, ctx.emit)

		res.Refs = append(res.Refs, ref)
		res.Paths = append(res.Paths, dst)
	}

	return res
}
