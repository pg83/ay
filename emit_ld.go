package main

import (
	"encoding/base64"
	"path"
	"slices"
	"strings"
)

var (
	yqlUdfProbeKV      = KV{P: pkUD, PC: pcLightBlue, ShowOut: true}
	ldVcsInfoPath      = ldVcsInfoVFS.string()
	ldSvnInterfacePath = ldSvnInterfaceVFS.string()
	ldLinkExePath      = ldLinkExeVFS.string()
	ldFsToolsPath      = ldFsToolsVFS.string()
	vcsJSONBase64      = base64.StdEncoding.EncodeToString([]byte(vcsJSONContent))
	ldKV               = KV{P: pkLD, PC: pcLightBlue, ShowOut: true}
	ldTestKV           = KV{P: pkLD, PC: pcLightBlue, ShowOut: true, LDTestNode: true}
	ldKV2              = KV{P: pkCP, PC: pcYellow, ShowOut: true}
	vcsRequirements    = Requirements{CPU: 1, Network: nwRestricted, RAM: 16}
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

type DynLibLink struct {
	alias     VFS
	probe     VFS
	probeRef  NodeRef
	soname    string
	fixElf    VFS
	fixElfRef NodeRef
}

func (e *EmitContext) emitLD(
	instance ModuleInstance,
	binaryName string,
	ccRefs []NodeRef,
	ccPaths []VFS,
	peerLDRefs []NodeRef,
	peerLibPaths []VFS,
	peerLinkCmdPaths []VFS,
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
	peerLDValue CompilerFlagValue,
	ownLDValue CompilerFlagValue,
	peerRPathFlagsGlobal []ANY,
	objAddLibsGlobal []ANY,
	linkExcludeLibraries []ANY,
	exportsScript VFS,
	linkerScripts []VFS,
	noExportDynSymbols bool,
	noCompilerWarnings bool,
	noOptimize bool,
	wantsStrip bool,
	useArcadiaLibm bool,
	wantsSplitDwarf bool,
	programModuleTag STR,
	sbomLang STR,
	tc ModuleToolchain,
	hostP *Platform,
	scripts ScriptDeps,
	emit *StreamingEmitter,
	vcsRef NodeRef,
	dyn *DynLibLink,
) NodeRef {
	peerLDFlagsGlobal, ownLDFlags := peerLDValue.Args, ownLDValue.Args
	localLDValue := e.localLinkerFlagValue(peerRPathFlagsGlobal)
	na := emit.nodeArenas()

	for _, input := range ldScriptInputs {
		e.cppStyleSource(input.any())
	}

	globalRefs, globalPaths, objcopyRefs, objcopyPaths = splitGlobalObjects(na, globalRefs, globalPaths, objcopyRefs, objcopyPaths)

	globalInputs := e.moduleGlobals
	pluginPaths := e.globalPlugins()
	peerWholeArchiveCount := len(wholeArchivePaths)
	ownScripts := len(linkerScripts)

	for _, input := range globalInputs {
		switch path.Ext(input.relString()) {
		case ".a":
			wholeArchivePaths = append(wholeArchivePaths, input)
		case ".ld":
			linkerScripts = append(linkerScripts, input)
		case ".supp":
			globalPaths = append(globalPaths, input)
		}
	}

	assert(len(objcopyRefs) == len(objcopyPaths), "emitLD: objcopyRefs/objcopyPaths length mismatch")

	cmdScripts := linkerScripts

	if ownScripts > 0 && len(linkerScripts) > ownScripts {
		cmdScripts = append(slices.Clone(linkerScripts[ownScripts:]), linkerScripts[:ownScripts]...)
	}

	binaryDir := instance.Path.relString()
	binPrefix := binaryDir + "/"
	outputVFS := build(binPrefix, binaryName)
	linkDir := path.Dir(outputVFS.relString())
	vcsCVFS := build(linkDir, "/__vcs_version__.c")
	vcsOVFS := build(linkDir, "/__vcs_version__.c", e.d.unit.objectSuffix(instance.Platform))
	cmd0 := composeLDCmdVcsInfo(na, tc, vcsCVFS)

	var vcsResources []STR

	commandEnv := Environment{s: &e.frame.ifEnv, resourceUses: &vcsResources}
	cmd1 := composeLDCmdVcsCompile(na, instance.Platform, tc, vcsCVFS, vcsOVFS, compilerCommandFlags(moduleCFlags, commandEnv).Args, concat(peerCFlagsGlobal, e.d.cc.OwnCFlagsGlobal), moduleScopeCFlags, compilerCommandFlags(concat(e.d.cc.COnlyFlags, e.d.cc.PeerCOnlyFlagsGlobal, e.d.cc.OwnCOnlyFlagsGlobal), commandEnv).Args, commandEnv, noCompilerWarnings, e.d.flags.NoWShadow, noOptimize)
	cmd2 := composeLDCmdLinkExe(na, instance.Platform, tc, outputVFS, vcsOVFS, ccPaths, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths[peerWholeArchiveCount:], wholeArchiveCmdPaths, objcopyPaths, peerLDFlagsGlobal, ownLDFlags, localLDValue.Args, objAddLibsGlobal, linkExcludeLibraries, exportsScript, cmdScripts, noExportDynSymbols, wantsStrip, useArcadiaLibm, e.d.useDynamicCuda, dyn)
	splitDwarfCmds := composeLDSplitDwarfCmds(na, tc, outputVFS, wantsSplitDwarf)
	envVcsOnly := envVarsVCS
	envFull := hostP.toolEnv()
	sbomEmbed := len(sbomPaths) > 0
	sbomJSON := build(binPrefix, "__sbomdata.json")
	emitCopy := dyn == nil && (instance.Platform.Flags[envOPENSOURCE] == strYes || len(dynamicPaths) > 0)

	var cmdLinkSbom, cmd3, cmdObjcopy []ANY

	if sbomEmbed {
		cmdLinkSbom = composeLDCmdLinkSbom(na, tc, sbomLang, instance.Path.rel(), sbomJSON, sbomPaths)
		cmdObjcopy = composeLDCmdSbomObjcopy(na, tc, sbomJSON, outputVFS)
	}

	if emitCopy {
		cmd3 = composeLDCmdLinkOrCopy(na, tc, binaryDir, dynamicPaths...)
	}

	cmdCap := 5 + len(splitDwarfCmds)

	if dyn != nil && dyn.probe != 0 {
		cmdCap++
	}

	cmds := na.cmds.alloc(cmdCap)[:0]

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(cmd0), Env: envVcsOnly}, Cmd{CmdArgs: na.chunkList(cmd1), Env: envFull, Resources: resourceKeyUnion(vcsResources, peerCompilerResourceKeys(e.d.cc.CFlagValues, len(e.d.cFlagsGlobal) > 0), peerCompilerResourceKeys(e.d.cc.COnlyFlagValues, len(e.d.cOnlyFlagsGlobal) > 0))})

	if sbomEmbed {
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(cmdLinkSbom), Cwd: bldRootDirVFS, Env: envVcsOnly})
	}

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(cmd2), Cwd: bldRootDirVFS, Env: envFull, Resources: resourceKeyUnion(peerLDValue.Resources, ownLDValue.Resources, localLDValue.Resources)})

	if emitCopy {
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(cmd3), Env: envVcsOnly})
	}

	for i := range splitDwarfCmds {
		splitDwarfCmds[i].Env = envVcsOnly
	}

	cmds = append(cmds, splitDwarfCmds...)

	if sbomEmbed {
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(cmdObjcopy), Env: envVcsOnly})
	}

	if dyn != nil && dyn.probe != 0 {
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(na.anyList(dyn.probe.any(), outputVFS.any())), Env: envVcsOnly})
	}

	na.cmds.commit(len(cmds))

	cmds = cmds[:len(cmds):len(cmds)]

	inputTail := na.vfs.alloc(2 + len(linkerScripts) + len(localLDValue.Inputs))[:0]

	inputTail = append(inputTail, ldSvnversionHVFS)

	if exportsScript != 0 {
		inputTail = append(inputTail, exportsScript)
	}

	inputTail = append(inputTail, linkerScripts...)
	inputTail = append(inputTail, localLDValue.Inputs...)

	na.vfs.commit(len(inputTail))

	inputTail = inputTail[:len(inputTail):len(inputTail)]

	extraChunks := [6][]VFS{}
	nExtra := 0

	if len(sbomPaths) > 0 {
		extraChunks[0] = na.vfsList(sbomPaths...)
		extraChunks[1] = na.srcChunk(linkSbomScriptVFS)

		nExtra = 2
	}

	if dyn != nil {
		extraChunks[nExtra] = scripts[ldLinkDynLibVFS.rel()]
		nExtra++

		if dyn.fixElf != 0 {
			extraChunks[nExtra] = na.srcChunk(dyn.fixElf)
			nExtra++
		}

		if dyn.probe != 0 {
			extraChunks[nExtra] = na.srcChunk(dyn.probe)
			nExtra++
		}
	}

	inputs := composeLDInputs(na, ccPaths, peerLibPaths, pluginPaths, globalPaths, wholeArchivePaths, dynamicPaths, objcopyPaths, scripts, emitCopy, na.rootInputList(inputTail, extraChunks[:nExtra]...)...)
	ccBuildRefs := na.objectBuildRefs(ccRefs, ccPaths)
	objcopyBuildRefs := na.objectBuildRefs(objcopyRefs, objcopyPaths)
	deps := na.noderefs.alloc(len(ccBuildRefs) + len(globalRefs) + len(peerLDRefs) + len(dynamicRefs) + len(objcopyBuildRefs) + len(sbomRefs) + 1)[:0]

	deps = append(deps, ccBuildRefs...)
	deps = append(deps, globalRefs...)
	deps = append(deps, peerLDRefs...)
	deps = append(deps, dynamicRefs...)
	deps = append(deps, objcopyBuildRefs...)
	deps = append(deps, sbomRefs...)
	deps = append(deps, vcsRef)
	na.noderefs.commit(len(deps))

	deps = deps[:len(deps):len(deps)]

	outputs := na.vfs.alloc(3 + len(dynamicPaths))[:0]

	outputs = append(outputs, outputVFS)

	if dyn != nil && dyn.alias != 0 {
		outputs = append(outputs, dyn.alias)
	}

	for _, p := range dynamicPaths {
		outputs = append(outputs, build(binaryDir, "/", baseName(p.relString())))
	}

	if wantsSplitDwarf {
		outputs = append(outputs, build(binPrefix, binaryName, ".debug"))
	}

	na.vfs.commit(len(outputs))

	outputs = outputs[:len(outputs):len(outputs)]

	kv := &ldKV

	if e.d.unit.TestLD {
		kv = &ldTestKV
	}

	if dyn != nil && dyn.probe != 0 {
		kv = &yqlUdfProbeKV
	}

	n := NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: cmds,
		Env:  envFull, Inputs: inputs, Outputs: outputs,
		KV:        kv,
		DepRefs:   deps,
		Resources: instance.Platform.UsesLinkResources},
	}

	if dyn != nil {
		foreign := na.noderefs.alloc(2)[:0]

		if dyn.fixElfRef != 0 {
			foreign = append(foreign, dyn.fixElfRef)
		}

		if dyn.probeRef != 0 {
			foreign = append(foreign, dyn.probeRef)
		}

		na.noderefs.commit(len(foreign))
		n.ForeignDepRefs = foreign[:len(foreign):len(foreign)]
	}

	e.appendSdcMeta(&n, binaryName)

	return e.emitModuleNode(n, true)
}

func splitGlobalObjects(na *NodeArenas, globalRefs []NodeRef, globalPaths []VFS, ownRefs []NodeRef, ownPaths []VFS) ([]NodeRef, []VFS, []NodeRef, []VFS) {
	objects := 0

	for _, p := range globalPaths {
		if isGlobalObject(p) {
			objects++
		}
	}

	if objects == 0 {
		return globalRefs, globalPaths, ownRefs, ownPaths
	}

	archiveRefs := na.noderefs.alloc(len(globalRefs) - objects)[:0]

	for i, p := range globalPaths {
		if !isGlobalObject(p) {
			archiveRefs = append(archiveRefs, globalRefs[i])
		}
	}

	na.noderefs.commit(len(archiveRefs))

	archivePaths := na.vfs.alloc(len(globalPaths) - objects)[:0]

	for _, p := range globalPaths {
		if !isGlobalObject(p) {
			archivePaths = append(archivePaths, p)
		}
	}

	na.vfs.commit(len(archivePaths))

	objRefs := na.noderefs.alloc(objects + len(ownRefs))[:0]

	for i, p := range globalPaths {
		if isGlobalObject(p) {
			objRefs = append(objRefs, globalRefs[i])
		}
	}

	objRefs = append(objRefs, ownRefs...)
	na.noderefs.commit(len(objRefs))

	objPaths := na.vfs.alloc(objects + len(ownPaths))[:0]

	for _, p := range globalPaths {
		if isGlobalObject(p) {
			objPaths = append(objPaths, p)
		}
	}

	objPaths = append(objPaths, ownPaths...)
	na.vfs.commit(len(objPaths))

	return archiveRefs[:len(archiveRefs):len(archiveRefs)], archivePaths[:len(archivePaths):len(archivePaths)], objRefs[:len(objRefs):len(objRefs)], objPaths[:len(objPaths):len(objPaths)]
}

func isGlobalObject(p VFS) bool {
	return strings.HasSuffix(p.relString(), ".o")
}

func lDOutputPath(instance ModuleInstance, binaryName string) VFS {
	return build(instance.Path.relString(), "/", binaryName)
}

func emitVCSNode(emit *StreamingEmitter, host *Platform) NodeRef {
	na := emit.nodeArenas()
	output := bldVcsJson

	node := NodeSpec{Node: Node{Platform: host,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(internStr(currentYatoolPath()).any(),
			argFetch.any(),
			strBase64.any(),
			internStr(vcsJSONBase64).any(),
			output.any()))}),
		KV:           &ldKV2,
		Outputs:      na.vfsList(output),
		Requirements: &vcsRequirements},
	}

	node.PresetUID = resourceFetchUID("base64:vcs.json:"+vcsJSONBase64, output.string())

	emit.deferPublication++

	defer func() { emit.deferPublication-- }()

	return emit.emitNode(node)
}

func composeLDCmdVcsInfo(na *NodeArenas, tc ModuleToolchain, vcsC VFS) []ANY {
	return na.anyList(
		tc.Python3.any(),
		ldVcsInfoVFS.any(),
		argVcsVcsJson.any(),
		vcsC.any(),
		ldSvnInterfaceVFS.any(),
	)
}

func composeLDCmdVcsCompile(na *NodeArenas, p *Platform, tc ModuleToolchain, vcsC, vcsO VFS, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags, cOnlyFlags []ANY, env Environment, noCompilerWarnings, noWShadow, noOptimize bool) []ANY {
	return composeLDCmdVcsCompileForced(na, p, tc, vcsC, vcsO, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags, cOnlyFlags, env, noCompilerWarnings, noWShadow, noOptimize, false)
}

func composeLDCmdVcsCompileForced(na *NodeArenas, p *Platform, tc ModuleToolchain, vcsC, vcsO VFS, moduleCFlags, peerCFlagsGlobal, moduleScopeCFlags, cOnlyFlags []ANY, env Environment, noCompilerWarnings, noWShadow, noOptimize, forceConsistentDebug bool) []ANY {
	bundle := compileFlagBundleFor(p)
	extraCFlags := compilerExtraCFlags(env)

	if noOptimize {
		bundle.CFlags = suppressOptimize(bundle.CFlags)
	}

	bound := len(extraCFlags) + 16 + 2*(len(debugPrefixMapFlags)+len(xclangDebugCompilationDir)) +
		len(bundle.ArchArgs) + len(p.SysrootArgs) + len(bundle.CFlags) +
		len(warningFlags) + 1 + len(bundle.Defines) +
		len(p.CFlags) + len(moduleCFlags) + len(peerCFlagsGlobal) +
		2*len(bundle.NoLibcBlock) + 1 + len(moduleScopeCFlags) + len(cOnlyFlags)

	cmdArgs := na.anys.alloc(bound)[:0]

	cmdArgs = append(cmdArgs, tc.CC.any())
	cmdArgs = appendAnyLists(cmdArgs, p.TargetArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, p.SysrootArgs...)
	cmdArgs = append(cmdArgs, extraCFlags...)

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

	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, pickWarningFlags(noCompilerWarnings, noWShadow), bundle.Defines, preNoLibcExtras, moduleScopeCFlags, catboostOpenSourceDefineFor(p))
	cmdArgs = append(cmdArgs, cOnlyFlags...)
	na.anys.commit(len(cmdArgs))

	return cmdArgs[:len(cmdArgs):len(cmdArgs)]
}

func composeLDCmdLinkExe(na *NodeArenas, p *Platform, tc ModuleToolchain, output, vcsO VFS, ccPaths []VFS, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths []VFS, objcopyPaths []VFS, peerLDFlagsGlobal, ownLDFlags, localLDFlags, objAddLibsGlobal, linkExcludeLibraries []ANY, exportsScript VFS, linkerScripts []VFS, noExportDynSymbols, wantsStrip, useArcadiaLibm, useDynamicCuda bool, dyn *DynLibLink) []ANY {
	argCap := 2 + 6 + 1 + 2 + 1 + 1 + 3 + 1 + 2 + 2 + 3 + 16 + 1 + 7 + len(ccPaths) + len(peerLinkCmdPaths) + len(globalPaths) + len(objcopyPaths) + len(peerLDFlagsGlobal) + len(ownLDFlags) + len(localLDFlags) + len(objAddLibsGlobal)

	argCap += 2 + len(pluginPaths) + 2*len(wholeArchiveCmdPaths) + 2*len(wholeArchivePaths) + 2*len(linkExcludeLibraries)
	argCap += p.Linker.ArgCount

	cmdArgs := na.anys.alloc(argCap)[:0]

	if dyn != nil {
		cmdArgs = append(cmdArgs, tc.Python3.any(), ldLinkDynLibVFS.any(), argTarget.any(), output.any())
	} else {
		cmdArgs = append(cmdArgs, tc.Python3.any(), ldLinkExeVFS.any())
	}

	cmdArgs = append(cmdArgs, argStartPlugins.any())

	for _, p := range pluginPaths {
		cmdArgs = append(cmdArgs, p.any())
	}

	cmdArgs = append(cmdArgs, argEndPlugins.any())

	if dyn == nil {
		cmdArgs = append(cmdArgs, argClangVer.any(), internStr(p.ClangVer).any())
		cmdArgs = append(cmdArgs, p.Linker.Roots...)
	}

	for _, p := range wholeArchiveCmdPaths {
		cmdArgs = append(cmdArgs, argWholeArchiveLibs.any(), p.rel().any())
	}

	for _, p := range wholeArchivePaths {
		cmdArgs = append(cmdArgs, argWholeArchiveLibs.any(), p.rel().any())
	}

	if dyn != nil {
		cmdArgs = append(cmdArgs, p.Linker.Roots...)
	}

	cmdArgs = append(cmdArgs, argArchLinux.any())

	if dyn != nil && dyn.alias != 0 {
		cmdArgs = append(cmdArgs, argSoname.any(), dyn.alias.any())
	}

	if useDynamicCuda {
		cmdArgs = append(cmdArgs, strDynamicCuda.any())
	} else if dyn == nil || dyn.alias == 0 {
		cmdArgs = append(cmdArgs, argObjcopyExe.any(), tc.Objcopy.any())
	}

	if dyn != nil && dyn.fixElf != 0 {
		cmdArgs = append(cmdArgs, argFixElf.any(), dyn.fixElf.any())
	}

	for _, lib := range linkExcludeLibraries {
		cmdArgs = append(cmdArgs, strExcludeLibs.any(), lib)
	}

	cmdArgs = append(cmdArgs, tc.CXX.any())
	cmdArgs = appendLinkerGlobals(cmdArgs, p.Linker, globalPaths, objcopyPaths)
	cmdArgs = append(cmdArgs, vcsO.any())

	for _, cp := range ccPaths {
		cmdArgs = append(cmdArgs, cp.any())
	}

	cmdArgs = append(cmdArgs, argDashO.any(), output.any())

	if dyn != nil {
		cmdArgs = append(cmdArgs, p.Linker.Shared...)
		cmdArgs = appendLinkerTemplate(cmdArgs, p.Linker.Soname, "$_SONAME", dyn.soname)
	}

	bundle := compileFlagBundleFor(p)

	cmdArgs = appendAnyLists(cmdArgs, p.TargetArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, p.SysrootArgs...)
	cmdArgs = appendAnyLists(cmdArgs, p.Linker.BeforePeers, p.Linker.StartGroup)

	for _, p := range peerLinkCmdPaths {
		cmdArgs = append(cmdArgs, p.rel().any())
	}

	cmdArgs = appendAnyLists(cmdArgs, p.Linker.EndGroup, p.Linker.AfterPeers)
	cmdArgs = append(cmdArgs, composeProgramLinkTrailer(p, peerLDFlagsGlobal, ownLDFlags, localLDFlags, objAddLibsGlobal, exportsScript, linkerScripts, noExportDynSymbols, wantsStrip, useArcadiaLibm)...)
	na.anys.commit(len(cmdArgs))

	return cmdArgs[:len(cmdArgs):len(cmdArgs)]
}

func composeProgramLinkTrailer(p *Platform, peerLDFlagsGlobal, ownLDFlags, localLDFlags, objAddLibsGlobal []ANY, exportsScript VFS, linkerScripts []VFS, noExportDynSymbols, wantsStrip, useArcadiaLibm bool) []ANY {
	var trailer []ANY

	if !noExportDynSymbols {
		exports := ""

		if exportsScript != 0 {
			exports = exportsScript.string()
		}

		trailer = appendLinkerTemplate(trailer, p.Linker.Exports, "${input:EXPORTS_FILE}", exports)
	}

	trailer = appendLinkerScripts(trailer, p.Linker.Script, linkerScripts)
	trailer = append(trailer, localLDFlags...)
	trailer = appendAnyLists(trailer, peerLDFlagsGlobal, ownLDFlags)
	trailer = appendArgGroupStr(trailer, objAddLibsGlobal)
	trailer = appendAnyLists(trailer, p.Linker.Intercept, p.Linker.SystemLibs)

	if !useArcadiaLibm {
		trailer = append(trailer, argDashLm.any())
	}

	if wantsStrip {
		trailer = append(trailer, p.Linker.Strip...)
	}

	trailer = appendAnyLists(trailer, p.Linker.DCE, p.Linker.ICF, p.Linker.NoPie)

	return trailer
}

func composeLDCmdLinkSbom(na *NodeArenas, tc ModuleToolchain, lang, moddir STR, sbomJSON VFS, sbomPaths []VFS) []ANY {
	cmd := na.anys.alloc(10 + len(sbomPaths))[:0]

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

	na.anys.commit(len(cmd))

	return cmd[:len(cmd):len(cmd)]
}

func composeLDCmdSbomObjcopy(na *NodeArenas, tc ModuleToolchain, sbomJSON, target VFS) []ANY {
	return na.anyList(
		tc.Objcopy.any(),
		strAddSection.any(),
		internV(".rosbomdata=", sbomJSON.prefix(), sbomJSON.relString()).any(),
		target.any(),
	)
}

func composeLDCmdLinkOrCopy(na *NodeArenas, tc ModuleToolchain, modulePath string, dynamicPaths ...VFS) []ANY {
	cmd := na.anys.alloc(5 + len(dynamicPaths))[:0]

	cmd = append(cmd,
		tc.Python3.any(),
		ldFsToolsVFS.any(),
		argLinkOrCopyToDir.any(),
		argNoCheck.any(),
	)

	for _, p := range dynamicPaths {
		cmd = append(cmd, p.any())
	}

	cmd = append(cmd, (build(modulePath)).any())
	na.anys.commit(len(cmd))

	return cmd[:len(cmd):len(cmd)]
}

func composeLDSplitDwarfCmds(na *NodeArenas, tc ModuleToolchain, output VFS, enabled bool) []Cmd {
	if !enabled {
		return nil
	}

	debug := internV(output.relString(), ".debug").build()

	return na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(tc.Objcopy.any(), argOnlyKeepDebug.any(), output.any(), debug.any()))}, Cmd{CmdArgs: na.chunkList(na.anyList(tc.Strip.any(), argStripDebug.any(), output.any()))}, Cmd{CmdArgs: na.chunkList(na.anyList(tc.Objcopy.any(), argRemoveSectionGnuDebuglink.any(), argAddGnuDebuglink.any(), debug.any(), output.any()))})
}

func composeLDInputs(na *NodeArenas, ccPaths []VFS, peerLibPaths []VFS, pluginPaths []VFS, globalPaths []VFS, wholeArchivePaths []VFS, dynamicPaths []VFS, objcopyPaths []VFS, scripts ScriptDeps, emitCopy bool, extra ...[]VFS) InputChunks {
	peerLibChunk := na.vfsList(peerLibPaths...)

	var buildRootBlock []VFS

	dedupers.with(func(deduper *DeDuper) {
		for _, p := range peerLibPaths {
			deduper.add(p.strID())
		}

		buildRootBlock = na.vfs.alloc(len(pluginPaths) + len(globalPaths) + len(wholeArchivePaths) + len(dynamicPaths) + len(ccPaths) + len(objcopyPaths))[:0]

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
		na.vfs.commit(len(buildRootBlock))

		buildRootBlock = buildRootBlock[:len(buildRootBlock):len(buildRootBlock)]
	})

	scriptChunks := [8][]VFS{}
	nScripts := 0

	for _, s := range ldScriptInputs {
		if s == copyFsToolsVFS && !emitCopy {
			continue
		}

		if cl := scripts[s.rel()]; cl != nil {
			scriptChunks[nScripts] = cl
		} else {
			scriptChunks[nScripts] = na.srcChunk(s)
		}

		nScripts++
	}

	chunks := na.inputs.alloc(2 + nScripts + len(extra))[:0]

	chunks = append(chunks, peerLibChunk, buildRootBlock)
	chunks = append(chunks, scriptChunks[:nScripts]...)
	chunks = append(chunks, extra...)
	na.inputs.commit(len(chunks))

	return InputChunks(chunks[:len(chunks):len(chunks)])
}
