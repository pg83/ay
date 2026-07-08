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
	tail := na.anys.alloc(4 + len(objPaths))[:0]

	if arPluginPath != nil {
		tail = append(tail, argPlugin.any(), (*arPluginPath).any())
	}

	tail = append(tail, arg2.any(), (archivePath).any())

	for _, p := range objPaths {
		tail = append(tail, (p).any())
	}

	na.anys.commit(len(tail))

	cmdArgs := na.chunkList(tc.ARCmdHead, tail[:len(tail):len(tail)])
	inputTail := na.vfs.alloc(2)[:0]

	inputTail = append(inputTail, buildScriptsLinkLibPy)

	if arPluginPath != nil {
		inputTail = append(inputTail, *arPluginPath)
	}

	na.vfs.commit(len(inputTail))

	inputTail = inputTail[:len(inputTail):len(inputTail)]
	objInputs := na.vfsList(objPaths...)
	topEnv := hostP.toolEnv()
	deps := na.noderefs.alloc(len(objRefs) + len(peerArchiveRefs))
	nd := copy(deps, objRefs)
	nd += copy(deps[nd:], peerArchiveRefs)
	na.noderefs.commit(nd)

	deps = deps[:nd:nd]

	n := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Env: cmdEnv}),
		Env:          topEnv,
		Inputs:       na.inputList(objInputs, inputTail),
		KV:           &arKV,
		Outputs:      na.vfsList(archivePath),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      deps,
		Resources:    instance.Platform.UsesPython3Clang,
	}

	return emit.emitNode(n)
}
