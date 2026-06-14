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
	tc ModuleToolchain,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		throwFmt("EmitARNamed: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := buildJoined(instance.Path.rel(), archiveBaseName)

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
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		throwFmt("EmitARNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := buildJoined(instance.Path.rel(), archiveBaseName)

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
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		throwFmt("EmitARGlobalNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := buildJoined(instance.Path.rel(), archiveBaseName)

	return emitARNode(instance, archivePath, tag, objRefs, objPaths, nil, nil, tc, hostP, emit)
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

// The resource clang toolchain (build/platform/clang's llvm-ar) is always an
// LLVM archiver in gnu format — link_lib.py's archiver type / format arguments.
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

	// objPaths is the caller's member slice — referenced as its own chunk,
	// never copied; only the script/plugin tail is built locally.
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

	depRefs := make([]NodeRef, 0, len(objRefs)+len(peerArchiveRefs))
	depRefs = append(depRefs, objRefs...)
	depRefs = append(depRefs, peerArchiveRefs...)

	n := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Env: cmdEnv}),
		Env:              topEnv,
		Inputs:           na.inputList(objPaths, inputTail),
		KV:               KV{P: pkAR, PC: pcLightRed, ShowOut: true},
		Outputs:          na.vfsList(archivePath),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: targetProperties,
		DepRefs:          depRefs,
		Resources:        instance.Platform.UsesPython3Clang,
	}

	return emit.emit(n)
}
