package main

import (
	"strings"
)

func emitARNamed(
	instance ModuleInstance,
	archiveBaseName string,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	arPluginPath *VFS,
	extraInputs []VFS,
	tc ModuleToolchain,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		throwFmt("EmitARNamed: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := buildJoined(instance.Path.rel(), archiveBaseName)

	return emitARNode(instance, archivePath, 0, objRefs, objPaths, peerArchiveRefs, arPluginPath, extraInputs, tc, hostP, emit)
}

func emitARNamedTagged(
	instance ModuleInstance,
	archiveBaseName string,
	tag STR,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	arPluginPath *VFS,
	extraInputs []VFS,
	tc ModuleToolchain,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		throwFmt("EmitARNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := buildJoined(instance.Path.rel(), archiveBaseName)

	return emitARNode(instance, archivePath, tag, objRefs, objPaths, peerArchiveRefs, arPluginPath, extraInputs, tc, hostP, emit)
}

func emitARGlobalNamedTagged(
	instance ModuleInstance,
	archiveBaseName string,
	tag STR,
	objRefs []NodeRef,
	objPaths []VFS,
	tc ModuleToolchain,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		throwFmt("EmitARGlobalNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := buildJoined(instance.Path.rel(), archiveBaseName)

	return emitARNode(instance, archivePath, tag, objRefs, objPaths, nil, nil, nil, tc, hostP, emit)
}

func archiveNameWithPrefix(moduleDir, prefix string) string {
	if moduleDir == "util" {
		base := "libyutil.a"

		return prefix + base[len("lib"):]
	}

	parts := strings.Split(moduleDir, "/")

	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}

	return prefix + strings.Join(parts, "-") + ".a"
}

func archiveNameWithPrefixOrName(moduleDir, prefix, name string) string {
	if name != "" {
		return prefix + name + ".a"
	}

	return archiveNameWithPrefix(moduleDir, prefix)
}

func archiveName(moduleDir string) string {
	return archiveNameWithPrefix(moduleDir, "lib")
}

func globalArchiveNameWithPrefixOrName(moduleDir, prefix, name string) string {
	base := archiveNameWithPrefixOrName(moduleDir, prefix, name)

	return base[:len(base)-2] + ".global.a"
}

const (
	arTypeLLVM  = "LLVM_AR"
	arFormatGNU = "gnu"
)

func emitARNode(
	instance ModuleInstance,
	archivePath VFS,
	tag STR,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	arPluginPath *VFS,
	extraInputs []VFS,
	tc ModuleToolchain,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	na := emit.nodeArenas()

	cmdEnv := hostP.toolEnv()

	tail := make([]STR, 0, 4+len(objPaths))

	if arPluginPath != nil {
		tail = append(tail, argPlugin.str(), (*arPluginPath).str())
	}

	tail = append(tail, arg2.str(), (archivePath).str())

	for _, p := range objPaths {
		tail = append(tail, (p).str())
	}

	cmdArgs := na.chunkList(tc.ARCmdHead, tail)

	inputTail := make([]VFS, 0, 2)
	inputTail = append(inputTail, buildScriptsLinkLibPy)

	if arPluginPath != nil {
		inputTail = append(inputTail, *arPluginPath)
	}

	topEnv := hostP.toolEnv()

	targetProperties := TargetProperties{ModuleDir: instance.Path.rel(), ModuleLang: mlCPP, ModuleType: mtLib}

	if instance.Language == LangPy {
		targetProperties.ModuleLang = mlPy3
	}

	if tag != 0 {
		targetProperties.ModuleTag = tag
	}

	deps := make([]NodeRef, 0, len(objRefs)+len(peerArchiveRefs))
	deps = append(deps, objRefs...)
	deps = append(deps, peerArchiveRefs...)

	n := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Env: cmdEnv}),
		Env:              topEnv,
		Inputs:           na.inputList(objPaths, inputTail, extraInputs),
		KV:               KV{P: pkAR, PC: pcLightRed, ShowOut: true},
		Outputs:          na.vfsList(archivePath),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: targetProperties,
		DepRefs:          deps,
		Resources:        instance.Platform.UsesPython3Clang,
	}

	return emit.emit(n)
}
