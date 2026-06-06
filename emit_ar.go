package main

func EmitARNamed(
	instance ModuleInstance,
	archiveBaseName string,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	arPluginPath *VFS,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARNamed: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := Build(instance.Path + "/" + archiveBaseName)

	return emitARNode(instance, archivePath, nil, objRefs, objPaths, peerArchiveRefs, arPluginPath, hostP, emit)
}

func EmitARNamedTagged(
	instance ModuleInstance,
	archiveBaseName string,
	tag string,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	arPluginPath *VFS,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := Build(instance.Path + "/" + archiveBaseName)

	return emitARNode(instance, archivePath, &tag, objRefs, objPaths, peerArchiveRefs, arPluginPath, hostP, emit)
}

func EmitARGlobalNamedTagged(
	instance ModuleInstance,
	archiveBaseName string,
	tag string,
	objRefs []NodeRef,
	objPaths []VFS,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARGlobalNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := Build(instance.Path + "/" + archiveBaseName)

	return emitARNode(instance, archivePath, &tag, objRefs, objPaths, nil, nil, hostP, emit)
}
