package main

func EmitARNamed(
	instance ModuleInstance,
	archiveBaseName string,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	arPluginPath *VFS,
	tc moduleToolchain,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARNamed: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := Build(instance.Path + "/" + archiveBaseName)

	return emitARNode(instance, archivePath, 0, objRefs, objPaths, peerArchiveRefs, arPluginPath, tc, hostP, emit)
}

func EmitARNamedTagged(
	instance ModuleInstance,
	archiveBaseName string,
	tag STR,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	arPluginPath *VFS,
	tc moduleToolchain,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := Build(instance.Path + "/" + archiveBaseName)

	return emitARNode(instance, archivePath, tag, objRefs, objPaths, peerArchiveRefs, arPluginPath, tc, hostP, emit)
}

func EmitARGlobalNamedTagged(
	instance ModuleInstance,
	archiveBaseName string,
	tag STR,
	objRefs []NodeRef,
	objPaths []VFS,
	tc moduleToolchain,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARGlobalNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := Build(instance.Path + "/" + archiveBaseName)

	return emitARNode(instance, archivePath, tag, objRefs, objPaths, nil, nil, tc, hostP, emit)
}
