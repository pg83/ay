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

// ArchiveName returns the on-disk archive base name for a module dir.
//
// Convention (verified against 38 unique AR outputs in g.json):
//
//   - "util" → "libyutil.a" (special — `y` prefix)
//   - "contrib/libs/X" (exactly depth 3) → "libcontrib-libs-X.a" (keep `contrib`)
//   - "contrib/libs/X/Y/..." (depth ≥ 4) → "liblibs-X-Y-....a" (drop `contrib`)
//   - "library/cpp/X" (exactly depth 3) → "liblibrary-cpp-X.a" (keep `library`)
//   - "library/cpp/X/Y/..." (depth ≥ 4) → "libcpp-X-Y-....a" (drop `library`)
//   - all other paths → "lib<dashed>.a" (dash-join all parts)
//
// Cross-reference: PR-09's naive "lib<dashed>.a" formula was correct
// for build/cow/on (depth 3, not contrib/libs or library/cpp); this
// function generalises.
func ArchiveName(moduleDir string) string {
	if moduleDir == "util" {
		return "libyutil.a"
	}

	parts := strings.Split(moduleDir, "/")

	// contrib/libs/X/Y/... (depth >= 4): drop "contrib", keep from "libs".
	if len(parts) >= 4 && parts[0] == "contrib" && parts[1] == "libs" {
		return "lib" + strings.Join(append([]string{"libs"}, parts[2:]...), "-") + ".a"
	}

	// library/cpp/X/Y/... (depth >= 4): drop "library", keep from "cpp".
	if len(parts) >= 4 && parts[0] == "library" && parts[1] == "cpp" {
		return "lib" + strings.Join(append([]string{"cpp"}, parts[2:]...), "-") + ".a"
	}

	// All other paths (depth <= 3 contrib/libs, depth <= 3 library/cpp,
	// or any non-matching prefix): standard dash-joined formula.
	return "lib" + strings.ReplaceAll(moduleDir, "/", "-") + ".a"
}

// globalArchiveName returns the archive base name for a module's
// GLOBAL_SRCS archive. The name follows the same prefix-truncation
// rules as ArchiveName, but the ".a" suffix is replaced with
// ".global.a".
func globalArchiveName(moduleDir string) string {
	base := ArchiveName(moduleDir)

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
// as-is in cmd_args. inputs uses a separately sorted copy so that
// node UIDs are stable and byte-exact with the reference graph.
//
// `instance` provides platform + path + Flags.PIC. host_platform is
// set when Flags.PIC=true and "tool" is appended to tags
// (consistent with the host CC convention).
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
	emit Emitter,
) NodeRef {
	scriptPath := "$(SOURCE_ROOT)/build/scripts/link_lib.py"

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
		"--",
		archivePath,
	}

	cmdArgs = append(cmdArgs, objPaths...)

	// inputs: .o paths sorted alphabetically, then the script path
	// at the end. The reference graph always has inputs .o section
	// sorted regardless of the declaration order used in cmd_args.
	// Sort a local copy so objPaths (used for cmd_args above) is
	// not mutated.
	sortedObjPaths := append([]string{}, objPaths...)
	sort.Strings(sortedObjPaths)

	inputs := make([]string, 0, len(sortedObjPaths)+1)
	inputs = append(inputs, sortedObjPaths...)
	inputs = append(inputs, scriptPath)

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
	if instance.Flags.PIC {
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

	if instance.Flags.PIC {
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
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitAR: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := "$(BUILD_ROOT)/" + instance.Path + "/" + ArchiveName(instance.Path)

	return emitARNode(instance, archivePath, "", objRefs, objPaths, peerArchiveRefs, emit)
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
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitARGlobal: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := "$(BUILD_ROOT)/" + instance.Path + "/" + globalArchiveName(instance.Path)

	return emitARNode(instance, archivePath, "global", objRefs, objPaths, nil, emit)
}
