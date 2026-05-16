package main

import (
	"sort"
	"strings"
)

// ar.go — emitter for AR (archive) nodes.
//
// cmd_args preserves declaration (SRCS) order; inputs sorts the .o set
// alphabetically. Output path uses ArchiveName(instance.Path) regardless of
// PIC: the host/target axis is captured by host_platform + tags=["tool"],
// not by an archive-name suffix (e.g. build/cow/on's host AR still emits
// libbuild-cow-on.a, not libbuild-cow-on.pic.a).

// isBuildRootCodegenProduct reports whether a member-input path is a
// BUILD_ROOT-rooted codegen artifact that must not appear in AR `inputs`.
// Reference constrains BUILD_ROOT entries in AR `inputs` to .o files;
// generated sources/headers are wired through the constituent CC only.
func isBuildRootCodegenProduct(p string) bool {
	if !strings.HasPrefix(p, "$(B)/") {
		return false
	}
	// .o is the only BUILD_ROOT extension carried in AR `inputs`. The
	// HasSuffix(".o") test covers .cpp.o, .pic.o, and .S.o.
	return !strings.HasSuffix(p, ".o")
}

// isBuildRootCodegenProductRel is the VFS-internal form of
// isBuildRootCodegenProduct. The caller has already verified the path is
// BUILD_ROOT-anchored (VFS.IsBuild()); this checks only the suffix rule.
func isBuildRootCodegenProductRel(rel string) bool {
	return !strings.HasSuffix(rel, ".o")
}

// archiveNameWithPrefix returns the archive base name using the given
// prefix (e.g. "lib", "libpy3", "libpy3c"). Single special case: "util" →
// "<prefix>yutil.a"; "util" is never a Python module in practice.
func archiveNameWithPrefix(moduleDir, prefix string) string {
	if moduleDir == "util" {
		// The "y" infix is baked into the util special-case; preserve
		// it relative to whatever prefix the caller supplies.
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

// ArchiveName returns the on-disk archive base name for a module dir.
//
// Rule (from upstream devtools/ymake/module_confs.cpp:48-57,
// SetDefaultRealprjnameImpl(mod, depth=2) as used by ThreeDirNames):
// join the last min(3, depth) path components with "-", prefix "lib",
// suffix ".a". Single special case: "util" → "libyutil.a".
func ArchiveName(moduleDir string) string {
	return archiveNameWithPrefix(moduleDir, "lib")
}

// Py3ArchiveName returns the archive base name for a PY3_LIBRARY
// module (prefix "libpy3"). Used by Python library types whose
// reference graph uses the "libpy3<name>.a" naming convention.
func Py3ArchiveName(moduleDir string) string {
	return archiveNameWithPrefix(moduleDir, "libpy3")
}

// Py3cArchiveName returns the archive base name for a PY23_NATIVE_LIBRARY
// module (prefix "libpy3c"). Used by native Python C-extension library
// types whose reference graph uses the "libpy3c<name>.a" convention.
func Py3cArchiveName(moduleDir string) string {
	return archiveNameWithPrefix(moduleDir, "libpy3c")
}

// globalArchiveName returns the archive base name for a module's
// GLOBAL_SRCS archive. The name follows the same prefix-truncation
// rules as ArchiveName, but the ".a" suffix is replaced with
// ".global.a".
func globalArchiveName(moduleDir string) string {
	base := ArchiveName(moduleDir)

	return base[:len(base)-2] + ".global.a"
}

// globalArchiveNameWithPrefix is like globalArchiveName but uses an
// explicit prefix (e.g. "libpy3") instead of "lib".
func globalArchiveNameWithPrefix(moduleDir, prefix string) string {
	base := archiveNameWithPrefix(moduleDir, prefix)

	return base[:len(base)-2] + ".global.a"
}

func globalArchiveNameWithPrefixOrName(moduleDir, prefix, name string) string {
	base := archiveNameWithPrefixOrName(moduleDir, prefix, name)

	return base[:len(base)-2] + ".global.a"
}

// emitARNode is the shared implementation behind EmitAR and EmitARGlobal.
// tag (when present, e.g. "global") lands in target_properties.
// peerArchiveRefs go into DepRefs only (NOT cmd_args/inputs): ar(1)
// archives .o files; peer archives are link-time inputs for LD.
//
// objPaths is in caller (declaration) order for cmd_args; `inputs` sorts
// the .o set alphabetically, then appends the link script, then the
// memberInputs union (every member CC's source + headers in DFS order,
// deduped against the .o set). Production passes nil for peerArchiveRefs
// (reference graph carries zero AR-on-AR deps); parameter retained for tests.
func emitARNode(
	instance ModuleInstance,
	archivePath VFS,
	tag *string,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	memberInputs []VFS,
	arPluginPath *VFS,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	scriptVFS := Source("build/scripts/link_lib.py")

	// Built as separate literals (not a shared variable) so
	// downstream mutation of one map can't leak into the other.
	cmdEnv := hostP.ToolEnv()

	// cmd_args: fixed prefix, archive output, then .o paths in
	// declaration (caller-supplied) order. When arPluginPath is non-nil
	// the AR_PLUGIN macro fired on this module's ya.make and
	// `--plugin <path>` is injected between the inner `-- … --` separators
	// of _LD_ARCHIVER (upstream ld.conf:366-368 + ld.conf:373).
	arTool, arType, arFormat := instance.Platform.ArchiverArgs()

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		scriptVFS.String(),
		arTool,
		arType,
		arFormat,
		"$(B)",
		"None",
		"--",
	}
	if arPluginPath != nil {
		cmdArgs = append(cmdArgs, "--plugin", arPluginPath.String())
	}
	cmdArgs = append(cmdArgs, "--", archivePath.String())

	for _, p := range objPaths {
		cmdArgs = append(cmdArgs, p.String())
	}

	// inputs: .o paths sorted alphabetically, then the script path, then
	// memberInputs (union of every member CC's source + headers). Sort a
	// copy so objPaths (used in cmd_args) is not mutated.
	sortedObjPaths := append([]VFS(nil), objPaths...)
	sort.Slice(sortedObjPaths, func(i, j int) bool {
		return string(sortedObjPaths[i].Rel) < string(sortedObjPaths[j].Rel)
	})

	inputs := make([]VFS, 0, len(sortedObjPaths)+2+len(memberInputs))
	inputs = append(inputs, sortedObjPaths...)
	inputs = append(inputs, scriptVFS)
	// AR plugin path slots immediately after the script and before the
	// memberInputs union (verified at sg2.json openssl AR inputs[696]).
	if arPluginPath != nil {
		inputs = append(inputs, *arPluginPath)
	}
	// memberInputs may legitimately repeat across members (a header
	// included from two .c files). Dedup against the union including
	// the .o set so an unexpected collision (e.g. a .o path that
	// also somehow appears as a member input) doesn't double up.
	objSet := map[VFS]struct{}{}
	for _, v := range inputs {
		objSet[v] = struct{}{}
	}

	for _, pV := range memberInputs {
		if _, dup := objSet[pV]; dup {
			continue
		}

		// Drop BUILD_ROOT-rooted codegen products: reference constrains
		// AR `inputs` to .o objects under $(B). Non-.o codegen artifacts
		// (*.pb.{cc,h}, *_serialized.{cpp,h}, ANTLR lex/parse outputs, …)
		// are wired through the constituent CC's `inputs` only.
		if pV.IsBuild() && isBuildRootCodegenProductRel(pV.Rel) {
			continue
		}

		objSet[pV] = struct{}{}
		inputs = append(inputs, pV)
	}

	// Built as separate literals (not a shared variable) so
	// downstream mutation of one map can't leak into the other.
	topEnv := hostP.ToolEnv()

	targetProperties := map[string]string{
		"module_dir":  instance.Path,
		"module_lang": "cpp",
		"module_type": "lib",
	}

	if instance.Language == LangPy {
		targetProperties["module_lang"] = "py3"
	}

	if tag != nil {
		targetProperties["module_tag"] = *tag
	}

	// DepRefs: own CC refs first, then peer archive refs. Peer archives
	// are DepRefs only (NOT cmd_args/inputs) — captures the UID dependency
	// without corrupting the ar(1) command.
	depRefs := make([]NodeRef, 0, len(objRefs)+len(peerArchiveRefs))
	depRefs = append(depRefs, objRefs...)
	depRefs = append(depRefs, peerArchiveRefs...)

	// tags come from instance.Platform (["tool"] on host, [] on target);
	// non-nil empty slice keeps JSON `[]`, not `null`.
	tags := instance.Platform.Tags

	n := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     cmdEnv,
			},
		},
		Env:    topEnv,
		Inputs: inputs,
		KV: map[string]string{
			"p":        "AR",
			"pc":       "light-red",
			"show_out": "yes",
		},
		Outputs:      []VFS{archivePath},
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags:             tags,
		TargetProperties: targetProperties,
		DepRefs:          depRefs,
	}

	return emit.Emit(n)
}

// EmitAR archives .o files (objRefs/objPaths) into
// $(B)/<instance.Path>/<ArchiveName(instance.Path)>. objRefs/objPaths must
// be parallel and in SRCS declaration order (cmd_args preserves it; inputs
// sorts internally). memberInputs is the union of every member CC's
// inputs (primary source + transitive headers, DFS-discovery order).
// peerArchiveRefs go into DepRefs only; production passes nil (reference
// has zero AR-on-AR deps), parameter retained for tests.
func EmitAR(
	instance ModuleInstance,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	memberInputs []VFS,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitAR: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := Build(instance.Path + "/" + ArchiveName(instance.Path))

	return emitARNode(instance, archivePath, nil, objRefs, objPaths, peerArchiveRefs, memberInputs, nil, hostP, emit)
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
	memberInputs []VFS,
	arPluginPath *VFS,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARNamed: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := Build(instance.Path + "/" + archiveBaseName)

	return emitARNode(instance, archivePath, nil, objRefs, objPaths, peerArchiveRefs, memberInputs, arPluginPath, hostP, emit)
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
	memberInputs []VFS,
	arPluginPath *VFS,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := Build(instance.Path + "/" + archiveBaseName)

	return emitARNode(instance, archivePath, &tag, objRefs, objPaths, peerArchiveRefs, memberInputs, arPluginPath, hostP, emit)
}

// EmitARGlobalNamedTagged is EmitARGlobalNamed with an explicit module_tag
// (e.g. "py3_global", "py3_native_global"). "global" stays the default;
// PY23_LIBRARY uses "py3_global"; PY23_NATIVE_LIBRARY uses "py3_native_global".
func EmitARGlobalNamedTagged(
	instance ModuleInstance,
	archiveBaseName string,
	tag string,
	objRefs []NodeRef,
	objPaths []VFS,
	memberInputs []VFS,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARGlobalNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := Build(instance.Path + "/" + archiveBaseName)

	return emitARNode(instance, archivePath, &tag, objRefs, objPaths, nil, memberInputs, nil, hostP, emit)
}
