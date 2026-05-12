package main

import (
	"sort"
	"strings"
)

// ar.go — emitter for AR (archive) nodes.
//
// Cherry-picked from PR-15's worktree (post-D01-fix version that
// sorts only `inputs`, preserves declaration order in cmd_args).
// PR-23 retrofitted the signature: `EmitAR` / `EmitARGlobal` now
// take a `ModuleInstance` instead of a (platform, moduleDir) pair.
// Output path uses `ArchiveName(instance.Path)` regardless of
// `instance.Flags.PIC` — the reference graph confirms host AR for
// `build/cow/on` still emits to `libbuild-cow-on.a` (NOT
// `libbuild-cow-on.pic.a`); the host/target axis is captured by the
// host AR's `host_platform=true` and `tags=["tool"]`, not by an
// archive-name suffix.

// isBuildRootCodegenProduct reports whether a member-input path is a
// BUILD_ROOT-rooted codegen artifact that must not appear in the AR
// node's `inputs` slot. The reference graph (sg2.json) constrains AR
// `inputs` to .o files for BUILD_ROOT-rooted entries; everything else
// (generated `.cpp`, `.cc`, `.h`, `.pb.{cc,h}`, `_serialized.{cpp,h}`,
// ANTLR lex/parse outputs, etc.) is wired through the constituent
// CC's own `inputs` slot only.
func isBuildRootCodegenProduct(p string) bool {
	if !strings.HasPrefix(p, "$(BUILD_ROOT)/") {
		return false
	}
	// .o is the only BUILD_ROOT extension legitimately carried by an
	// AR aggregator's `inputs` slot (a member's compiled object).
	// Strip optional .pic.o → .o by suffix check: HasSuffix(".o")
	// catches both bare ".cpp.o" and PIC ".cpp.pic.o" plus ".S.o".
	return !strings.HasSuffix(p, ".o")
}

// archiveNameWithPrefix returns the archive base name using the given
// prefix instead of the default "lib". The prefix is used verbatim
// (e.g. "lib", "libpy3", "libpy3c"). Single special case preserved:
// "util" → "libyutil.a" (prefix substitution still applies, so a
// py3 caller would get "libpy3yutil.a" — but "util" is never a
// Python module in practice).
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

// emitARNode is the shared implementation used by EmitAR and
// EmitARGlobal. archivePath is the full $(BUILD_ROOT)-rooted output
// path; tag is either "" or "global" and, when non-empty, is added
// to target_properties. peerArchiveRefs are added to DepRefs only —
// NOT to cmd_args or inputs — because ar(1) archives .o files; peer
// archives are link-time inputs for LD.
//
// objPaths must be in caller (declaration) order — they are used
// as-is in cmd_args. PR-31 D11 reshapes inputs against sg.json: the
// archive's `inputs` is `.o files (declaration order, deduped) +
// link script + memberInputs (union of every CC member's inputs in
// DFS-discovery order, deduped, dropping any path that already
// appears in the .o set)`. memberInputs are the per-CC source paths
// + IncludeInputs the walker accumulated.
//
// `instance` provides platform + path. host_platform is set when
// instance.Target == PlatformDefaultLinuxX8664 (D41: platform-
// identity dispatch replaces Flags.PIC on the host/target axis) and
// "tool" is appended to tags (consistent with the host CC convention).
//
// PR-30 D05: production caller now passes nil for peerArchiveRefs;
// the reference graph confirms zero AR-on-AR deps. The parameter is
// retained for tests that pin the historical shape.
func emitARNode(
	instance ModuleInstance,
	archivePath string,
	tag string,
	objRefs []NodeRef,
	objPaths []string,
	peerArchiveRefs []NodeRef,
	memberInputs []string,
	arPlugin string,
	emit Emitter,
) NodeRef {
	scriptPath := "$(SOURCE_ROOT)/build/scripts/link_lib.py"

	// PR-M3-final-path-clean: AR_PLUGIN(name) → `<name>.pyplugin` filename
	// resolves to $(SOURCE_ROOT)/<modulePath>/<filename>. Empty when no
	// AR_PLUGIN declared. Mirrors build/conf/linkers/ld.conf:367-368
	// `_LD_ARCHIVER_KV_PLUGIN=--plugin ${input:_AR_PLUGIN}`.
	var arPluginPath string
	if arPlugin != "" {
		arPluginPath = "$(SOURCE_ROOT)/" + instance.Path + "/" + arPlugin
	}

	// Built as separate literals (not a shared variable) so
	// downstream mutation of one map can't leak into the other.
	cmdEnv := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	// Build the cmd_args list: fixed prefix of 9 elements, then the
	// archive output path, then all .o input paths in declaration
	// (caller-supplied) order. The reference graph stores .o files
	// in SRCS declaration order in cmd_args.
	//
	// PR-M3-final-path-clean: when AR_PLUGIN is declared, the
	// `_LD_ARCHIVER_KV_PLUGIN=--plugin <path>` pair is injected between
	// the two `--` separators of link_lib.py's argv (slots 7 and 10 in
	// the reference). Empirical anchor:
	// contrib/libs/openssl/libcontrib-libs-openssl.a cmd_args[7..11] =
	// `--`, `--plugin`, `$(SOURCE_ROOT)/contrib/libs/openssl/ar.pyplugin`,
	// `--`, `<archivePath>`.
	cmdArgs := []string{
		// TODO(portability): python3 path is captured from the
		// reference build; future work must template this from
		// PlatformConfig or detect it from $PATH.
		"/ix/realm/pg/bin/python3",
		scriptPath,
		"ar",
		"GNU_AR",
		"None",
		"$(BUILD_ROOT)",
		"None",
		"--",
	}

	if arPluginPath != "" {
		cmdArgs = append(cmdArgs, "--plugin", arPluginPath)
	}

	cmdArgs = append(cmdArgs, "--", archivePath)

	cmdArgs = append(cmdArgs, objPaths...)

	// inputs: .o paths sorted alphabetically, then the script path,
	// then memberInputs (PR-31 D11; sg.json union of every member
	// CC's source + headers). Sort the .o copy so objPaths used for
	// cmd_args above is not mutated.
	sortedObjPaths := append([]string{}, objPaths...)
	sort.Strings(sortedObjPaths)

	inputs := make([]string, 0, len(sortedObjPaths)+2+len(memberInputs))
	inputs = append(inputs, sortedObjPaths...)
	inputs = append(inputs, scriptPath)
	// PR-M3-final-path-clean: AR_PLUGIN script appears in inputs after
	// link_lib.py and before the memberInputs span. Empirical anchor:
	// libcontrib-libs-openssl.a inputs[695..697] = link_lib.py,
	// ar.pyplugin, crypto/aes/aes_cbc.c.
	if arPluginPath != "" {
		inputs = append(inputs, arPluginPath)
	}
	// memberInputs may legitimately repeat across members (a header
	// included from two .c files). Dedup against the union including
	// the .o set so an unexpected collision (e.g. a .o path that
	// also somehow appears as a member input) doesn't double up.
	objSet := map[string]struct{}{}
	for _, p := range sortedObjPaths {
		objSet[p] = struct{}{}
	}
	objSet[scriptPath] = struct{}{}
	if arPluginPath != "" {
		objSet[arPluginPath] = struct{}{}
	}

	for _, p := range memberInputs {
		if _, dup := objSet[p]; dup {
			continue
		}

		// PR-M3-l2-aggregator: drop BUILD_ROOT-rooted codegen products.
		// Reference graph (sg2.json) constrains AR `inputs` to .o
		// objects under $(BUILD_ROOT) — every non-.o codegen artifact
		// (e.g. `*.ev.pb.{cc,h}`, `*_serialized.{cpp,h}`, `*.pb.h`,
		// ANTLR `*.{h,cpp}` lex/parse outputs) is wired implicitly via
		// the constituent CC's own `inputs` slot and must not leak into
		// the AR aggregator's `inputs` slot. The .o entries already
		// flow via `sortedObjPaths` above; BUILD_ROOT-rooted member
		// inputs at this point are by definition non-.o (the closure
		// walker yielded a generated header / source through a
		// member's #include chain).
		if isBuildRootCodegenProduct(p) {
			continue
		}

		objSet[p] = struct{}{}
		inputs = append(inputs, p)
	}

	// Built as separate literals (not a shared variable) so
	// downstream mutation of one map can't leak into the other.
	topEnv := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	targetProperties := map[string]string{
		"module_dir":  instance.Path,
		"module_lang": "cpp",
		"module_type": "lib",
	}

	if instance.Language == LangPy {
		targetProperties["module_lang"] = "py3"
	}

	if tag != "" {
		targetProperties["module_tag"] = tag
	}

	// DepRefs: own CC refs first, then peer archive refs. Peer
	// archives are DepRefs only (NOT cmd_args/inputs): ar(1)
	// archives .o files; peer archives are link-time inputs for
	// LD, not AR. Adding them to DepRefs correctly captures the
	// UID dependency without corrupting the AR command.
	depRefs := make([]NodeRef, 0, len(objRefs)+len(peerArchiveRefs))
	depRefs = append(depRefs, objRefs...)
	depRefs = append(depRefs, peerArchiveRefs...)

	tags := []string{}
	if targetIsX8664(instance) {
		tags = []string{"tool"}
	}

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
		Outputs:  []string{archivePath},
		Platform: string(instance.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags:             tags,
		TargetProperties: targetProperties,
		DepRefs:          depRefs,
	}

	// D41: dispatch on Target, not Flags.PIC; x86_64 IS the host axis in M2/M3.
	if targetIsX8664(instance) {
		n.HostPlatform = true
	}

	return emit.Emit(n)
}

// EmitAR emits an AR node that archives the .o files (passed via
// objRefs/objPaths) into
// $(BUILD_ROOT)/<instance.Path>/<ArchiveName(instance.Path)> for the
// given module context.
//
// objRefs and objPaths must have the same length; they carry only
// the module's own .o files. Callers pass paths in declaration
// (SRCS) order — cmd_args preserves that order. inputs sorts them
// alphabetically internally.
//
// memberInputs is the union of every member CC's inputs (primary
// source + transitive headers; PR-31 D11). The walker accumulates
// this in DFS-discovery order across the SRCS list; the emitter
// folds it into the AR node's `inputs` per the sg.json shape.
//
// peerArchiveRefs are the NodeRefs for peer-module archives (from
// PEERDIR). They are wired as DepRefs so the AR node's UID accounts
// for them, but they are NOT added to cmd_args or inputs — ar(1)
// archives .o files; peer archives are link-time inputs for LD.
//
// Returns the NodeRef for the emitted AR node.
//
// PR-30 D05: production caller now passes nil for peerArchiveRefs;
// the reference graph confirms zero AR-on-AR deps. The parameter is
// retained for tests that pin the historical shape.
func EmitAR(
	instance ModuleInstance,
	objRefs []NodeRef,
	objPaths []string,
	peerArchiveRefs []NodeRef,
	memberInputs []string,
	emit Emitter,
) NodeRef {
	return EmitARWithPlugin(instance, objRefs, objPaths, peerArchiveRefs, memberInputs, "", emit)
}

// EmitARWithPlugin is EmitAR plus an `arPlugin` filename (e.g.
// `ar.pyplugin` from `AR_PLUGIN(ar)`). Empty `arPlugin` is equivalent
// to EmitAR. The plugin path resolves to
// `$(SOURCE_ROOT)/<instance.Path>/<arPlugin>` and is woven into
// cmd_args (between the two `--` separators) and inputs (after the
// link_lib.py script entry).
func EmitARWithPlugin(
	instance ModuleInstance,
	objRefs []NodeRef,
	objPaths []string,
	peerArchiveRefs []NodeRef,
	memberInputs []string,
	arPlugin string,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitAR: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := "$(BUILD_ROOT)/" + instance.Path + "/" + ArchiveName(instance.Path)

	return emitARNode(instance, archivePath, "", objRefs, objPaths, peerArchiveRefs, memberInputs, arPlugin, emit)
}

// EmitARGlobal emits a second AR node for a module's GLOBAL_SRCS,
// producing a .global.a archive with module_tag="global" in
// target_properties.
//
// Global archives do not carry peer-archive DepRefs (GLOBAL_SRCS
// are propagated differently from PEERDIR linkage).
//
// objRefs and objPaths carry only the GLOBAL_SRCS .o files in
// declaration order. cmd_args preserves that order; inputs sorts
// them alphabetically.
//
// Returns the NodeRef for the emitted global AR node.
func EmitARGlobal(
	instance ModuleInstance,
	objRefs []NodeRef,
	objPaths []string,
	memberInputs []string,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARGlobal: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := "$(BUILD_ROOT)/" + instance.Path + "/" + globalArchiveName(instance.Path)

	return emitARNode(instance, archivePath, "global", objRefs, objPaths, nil, memberInputs, "", emit)
}

// EmitARNamed emits an AR node using an explicitly supplied archive
// base name (e.g. Py3ArchiveName or Py3cArchiveName) instead of the
// default ArchiveName. Used by Python library module types that require
// the "libpy3…" naming convention.
//
// archiveBaseName must be just the filename (e.g. "libpy3foo.a"), NOT a
// full path — the function prepends "$(BUILD_ROOT)/<instance.Path>/".
func EmitARNamed(
	instance ModuleInstance,
	archiveBaseName string,
	objRefs []NodeRef,
	objPaths []string,
	peerArchiveRefs []NodeRef,
	memberInputs []string,
	emit Emitter,
) NodeRef {
	return EmitARNamedWithPlugin(instance, archiveBaseName, objRefs, objPaths, peerArchiveRefs, memberInputs, "", emit)
}

// EmitARNamedWithPlugin is EmitARNamed plus an `arPlugin` filename
// (e.g. `ar.pyplugin` from `AR_PLUGIN(ar)`). Empty `arPlugin` is
// equivalent to EmitARNamed.
func EmitARNamedWithPlugin(
	instance ModuleInstance,
	archiveBaseName string,
	objRefs []NodeRef,
	objPaths []string,
	peerArchiveRefs []NodeRef,
	memberInputs []string,
	arPlugin string,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARNamed: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := "$(BUILD_ROOT)/" + instance.Path + "/" + archiveBaseName

	return emitARNode(instance, archivePath, "", objRefs, objPaths, peerArchiveRefs, memberInputs, arPlugin, emit)
}

// EmitARNamedTagged is like EmitARNamed but stamps an explicit
// `module_tag=<tag>` onto target_properties. PY23_LIBRARY's plain `.a`
// carries `py3` and PY23_NATIVE_LIBRARY's plain `libpy3c*.a` carries
// `py3_native` per the REF graph; the rest of the named archives stay
// untagged (regular `.a` archives have no module_tag in REF).
func EmitARNamedTagged(
	instance ModuleInstance,
	archiveBaseName string,
	tag string,
	objRefs []NodeRef,
	objPaths []string,
	peerArchiveRefs []NodeRef,
	memberInputs []string,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := "$(BUILD_ROOT)/" + instance.Path + "/" + archiveBaseName

	return emitARNode(instance, archivePath, tag, objRefs, objPaths, peerArchiveRefs, memberInputs, "", emit)
}

// EmitARNamedTaggedWithPlugin extends EmitARNamedTagged with an
// AR_PLUGIN basename. Same shape; passes the plugin through.
func EmitARNamedTaggedWithPlugin(
	instance ModuleInstance,
	archiveBaseName string,
	tag string,
	objRefs []NodeRef,
	objPaths []string,
	peerArchiveRefs []NodeRef,
	memberInputs []string,
	arPlugin string,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARNamedTaggedWithPlugin: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := "$(BUILD_ROOT)/" + instance.Path + "/" + archiveBaseName

	return emitARNode(instance, archivePath, tag, objRefs, objPaths, peerArchiveRefs, memberInputs, arPlugin, emit)
}

// EmitARGlobalNamedTagged is like EmitARGlobalNamed but uses an
// explicit module_tag (e.g. "py3_global", "py3_native_global"). The
// canonical "global" tag remains the default; callers needing the
// alternate shapes (PY23_LIBRARY → "py3_global"; PY23_NATIVE_LIBRARY
// → "py3_native_global") supply the tag explicitly.
func EmitARGlobalNamedTagged(
	instance ModuleInstance,
	archiveBaseName string,
	tag string,
	objRefs []NodeRef,
	objPaths []string,
	memberInputs []string,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARGlobalNamedTagged: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := "$(BUILD_ROOT)/" + instance.Path + "/" + archiveBaseName

	return emitARNode(instance, archivePath, tag, objRefs, objPaths, nil, memberInputs, "", emit)
}

// EmitARGlobalNamed is like EmitARGlobal but uses an explicitly
// supplied archive base name. Used by Python library module types.
func EmitARGlobalNamed(
	instance ModuleInstance,
	archiveBaseName string,
	objRefs []NodeRef,
	objPaths []string,
	memberInputs []string,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARGlobalNamed: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := "$(BUILD_ROOT)/" + instance.Path + "/" + archiveBaseName

	return emitARNode(instance, archivePath, "global", objRefs, objPaths, nil, memberInputs, "", emit)
}
