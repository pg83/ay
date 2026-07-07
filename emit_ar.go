package main

import (
	"strings"
)

var arKV = KV{P: pkAR, PC: pcLightRed, ShowOut: true}

const (
	arTypeLLVM  = "LLVM_AR"
	arFormatGNU = "gnu"
)

func emitARNamed(
	instance ModuleInstance,
	archiveBaseName string,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	arPluginPath *VFS,
	tc ModuleToolchain,
	hostP *Platform,
	emit *StreamingEmitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		throwFmt("EmitARNamed: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := buildJoined(instance.Path.relString(), archiveBaseName)

	return emitARNode(instance, archivePath, 0, objRefs, objPaths, peerArchiveRefs, arPluginPath, tc, hostP, emit)
}

func emitARNamedTagged(
	instance ModuleInstance,
	archiveBaseName string,
	tag STR,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	arPluginPath *VFS,
	tc ModuleToolchain,
	hostP *Platform,
	emit *StreamingEmitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		throwFmt("EmitARNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := buildJoined(instance.Path.relString(), archiveBaseName)

	return emitARNode(instance, archivePath, tag, objRefs, objPaths, peerArchiveRefs, arPluginPath, tc, hostP, emit)
}

func emitARGlobalNamedTagged(
	instance ModuleInstance,
	archiveBaseName string,
	tag STR,
	objRefs []NodeRef,
	objPaths []VFS,
	tc ModuleToolchain,
	hostP *Platform,
	emit *StreamingEmitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		throwFmt("EmitARGlobalNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := buildJoined(instance.Path.relString(), archiveBaseName)

	return emitARNode(instance, archivePath, tag, objRefs, objPaths, nil, nil, tc, hostP, emit)
}

func archiveNameWithPrefix(moduleDir, prefix string) string {
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

func emitARNode(
	instance ModuleInstance,
	archivePath VFS,
	tag STR,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	arPluginPath *VFS,
	tc ModuleToolchain,
	hostP *Platform,
	emit *StreamingEmitter,
) NodeRef {
	na := emit.nodeArenas()
	cmdEnv := hostP.toolEnv()
	tail := make([]ANY, 0, 4+len(objPaths))

	if arPluginPath != nil {
		tail = append(tail, argPlugin.any(), (*arPluginPath).any())
	}

	tail = append(tail, arg2.any(), (archivePath).any())

	for _, p := range objPaths {
		tail = append(tail, (p).any())
	}

	cmdArgs := na.chunkList(tc.ARCmdHead, tail)
	inputTail := make([]VFS, 0, 2)

	inputTail = append(inputTail, buildScriptsLinkLibPy)

	if arPluginPath != nil {
		inputTail = append(inputTail, *arPluginPath)
	}

	topEnv := hostP.toolEnv()
	deps := concat(objRefs, peerArchiveRefs)

	n := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Env: cmdEnv}),
		Env:          topEnv,
		Inputs:       na.inputList(objPaths, inputTail),
		KV:           &arKV,
		Outputs:      na.vfsList(archivePath),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      deps,
		Resources:    instance.Platform.UsesPython3Clang,
	}

	return emit.emitNode(n)
}
