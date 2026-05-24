package main

// EmitAR archives .o files (objRefs/objPaths) into
// $(B)/<instance.Path>/<ArchiveName(instance.Path)>. objRefs/objPaths must
// be parallel and in SRCS declaration order (cmd_args preserves it; inputs
// sorts internally). The archive bundles only those objects plus its own
// link script — no member source/header closure.
// peerArchiveRefs go into DepRefs only; production passes nil (reference
// has zero AR-on-AR deps), parameter retained for tests.
func EmitAR(
	instance ModuleInstance,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitAR: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := Build(instance.Path + "/" + ArchiveName(instance.Path))

	return emitARNode(instance, archivePath, nil, objRefs, objPaths, peerArchiveRefs, nil, hostP, emit)
}

// EmitARNamed is EmitAR with an explicit archive base name (e.g.
// Py3ArchiveName, Py3cArchiveName) instead of the default ArchiveName.
// archiveBaseName must be just the filename; the function prepends
// "$(B)/<instance.Path>/". arPluginPath is the AR_PLUGIN's $(S)-rooted
// path, or nil when no AR_PLUGIN macro fired.
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

// EmitARNamedTagged is EmitARNamed with an explicit module_tag
// target_property. PY23_LIBRARY's plain `.a` carries `py3` and
// PY23_NATIVE_LIBRARY's `libpy3c*.a` carries `py3_native`; the rest of the
// named archives remain untagged.
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

// EmitARGlobalNamedTagged emits a GLOBAL_SRCS archive with an explicit
// module_tag (e.g. "py3_global", "py3_native_global"). PY23_LIBRARY uses
// "py3_global"; PY23_NATIVE_LIBRARY uses "py3_native_global".
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
