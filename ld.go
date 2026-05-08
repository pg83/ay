package main

// ld.go — emitter for LD (link executable) nodes.
//
// Per D22, an LD node has ONE Node with FOUR Cmds:
//
//   cmd[0]: vcs_info.py — generates `__vcs_version__.c` and a
//           companion .h into the module's BUILD_ROOT directory from
//           the upstream VCS state. 5 args.
//   cmd[1]: clang compile of `__vcs_version__.c` →
//           `__vcs_version__.c.o`. 94 args. Same toolchain shape as a
//           CC node but with a single `-I$(SOURCE_ROOT)` (no full
//           ccIncludes set) and `-D_musl_=1` / `-D_musl_` sentinels
//           wrapping the two `noLibcUndebugBlock` copies instead of
//           CC's bare `noLibcUndebugBlock × 2`.
//   cmd[2]: link_exe.py — the actual link invocation. 73 args. Carries
//           a `cwd: $(BUILD_ROOT)` because the emitted command-file
//           paths are BUILD_ROOT-relative and link_exe.py resolves them
//           by chdiring there before invoking the linker.
//   cmd[3]: fs_tools.py link_or_copy_to_dir — copies (or hardlinks) the
//           freshly-linked binary into its containing directory's
//           output slot so downstream tools see a stable path. 5 args.
//
// The cmd_args composition is hand-translated from the reference graph
// node `tools/archiver/archiver` (LD, default-linux-aarch64,
// 4 cmds, 35 deps). The `TestEmitLD_ToolsArchiver_ByteExact` test
// pins each of the 4 cmd_args slices entry-by-entry; if a flag bundle
// drifts the test fails with the offending index.
//
// Inputs: PR-35b closes PR-31-D09 — the BUILD_ROOT-rooted block
// (peer .a archives + pyplugin + .global.a + own .cpp.o files) is
// emitted as one alphabetically-sorted set, then the 7-script bundle
// in REGISTRATION ORDER (NOT alphabetical: vcs_info.py,
// svn_interface.c, link_exe.py, thinlto_cache.py,
// process_command_files.py, process_whole_archive_option.py,
// fs_tools.py), then the union of every member CC's inputs (source +
// transitive headers) in DFS-discovery order. Verified against the
// reference `tools/archiver/archiver` LD node's `inputs` array
// (1052 entries).
//
// Per D33 the rule takes a `ModuleInstance`. PR-24 supports only
// PROGRAM modules built with `Flags.PIC=false` (target build); host
// PROGRAM modules are not exercised in M2 (the host axis only matters
// for building the tools the target build invokes — and tools never
// peer back into a PROGRAM target). Reviewers that hit a host LD case
// should land it as a follow-up PR rather than retrofit it here.

import (
	"sort"
)

// EmitLD emits the 4-cmd LD node for a PROGRAM module per D22.
//
// Inputs the caller must provide:
//
//   - `instance`: the PROGRAM module's ModuleInstance. `instance.Path`
//     names the module's directory. For target builds
//     (`Flags.PIC=false`) the binary is emitted to
//     `$(BUILD_ROOT)/<path>/<binaryName>`; PR-24 does not handle host
//     LD specially.
//   - `binaryName`: the linker output's basename. Per PR-28-D01, this
//     comes from the parsed `PROGRAM(name)` macro's argument
//     (`ModuleStmt.Args[0]`); when empty the helper falls back to
//     `lastPathComponent(instance.Path)`. For most PROGRAMs the macro
//     argument matches the directory's trailing component (e.g.
//     `tools/archiver` declares `PROGRAM(archiver)`); the divergent case
//     is `contrib/tools/ragel6/bin/ya.make` which declares
//     `PROGRAM(ragel6)` — the binary is `bin/ragel6` not `bin/bin`.
//   - `ccRefs` / `ccPaths`: the module's own .cpp.o files (typically
//     just `main.cpp.o`), one entry per source. Order matters for
//     cmd[2] argv composition: the entries are emitted between the
//     whole-archive block and the `-o` flag in the order supplied.
//   - `peerLDRefs` / `peerLibPaths`: peer LIBRARY archive paths in
//     PEERDIR walk order (R14 — non-alphabetical). Each `peerLibPath`
//     is BUILD_ROOT-relative (e.g. "build/cow/on/libbuild-cow-on.a"),
//     NOT prefixed with `$(BUILD_ROOT)/` — link_exe.py interprets the
//     argv strings relative to its `cwd`. The `peerLDRefs` are wired
//     as DepRefs so the Merkle hash captures the link-time inputs.
//   - `pluginRefs` / `pluginPaths`: plugin script paths for the
//     `--start-plugins ... --end-plugins` block (e.g. the musl
//     pyplugin). `pluginPaths` are full `$(BUILD_ROOT)/...` paths
//     because they appear verbatim in cmd[2] and in `inputs`. Pass nil
//     when the module has no plugins.
//   - `globalRefs` / `globalPaths`: peer `.global.a` archives that
//     wrap into the `-Wl,--whole-archive ... -Wl,--no-whole-archive`
//     block. `globalPaths` are BUILD_ROOT-relative (same convention
//     as peerLibPaths). Pass nil when none.
//
// Returns the LD NodeRef. The output path is
// `$(BUILD_ROOT)/<instance.Path>/<binaryName>`; the caller can
// re-derive it via `LDOutputPath(instance, binaryName)` if needed.
func EmitLD(
	instance ModuleInstance,
	binaryName string,
	ccRefs []NodeRef,
	ccPaths []string,
	peerLDRefs []NodeRef,
	peerLibPaths []string,
	pluginRefs []NodeRef,
	pluginPaths []string,
	globalRefs []NodeRef,
	globalPaths []string,
	memberInputs []string,
	muslOn bool,
	emit Emitter,
) NodeRef {
	if len(ccRefs) != len(ccPaths) {
		ThrowFmt("EmitLD: ccRefs/ccPaths length mismatch (%d vs %d)", len(ccRefs), len(ccPaths))
	}

	if len(peerLDRefs) != len(peerLibPaths) {
		ThrowFmt("EmitLD: peerLDRefs/peerLibPaths length mismatch (%d vs %d)", len(peerLDRefs), len(peerLibPaths))
	}

	if len(pluginRefs) != len(pluginPaths) {
		ThrowFmt("EmitLD: pluginRefs/pluginPaths length mismatch (%d vs %d)", len(pluginRefs), len(pluginPaths))
	}

	if len(globalRefs) != len(globalPaths) {
		ThrowFmt("EmitLD: globalRefs/globalPaths length mismatch (%d vs %d)", len(globalRefs), len(globalPaths))
	}

	// PR-25 lifts PR-24's host-PIC guard so the cross-platform
	// recursion mechanism (D31) can build host PROGRAM modules
	// (ragel6/yasm tools). The cmd_args composition still uses
	// the target-flavoured bundle — PR-26's flag-bundle work will
	// compose a host-flavoured LD bundle when a host PROGRAM
	// turns out to need different toolchain invocation. For the
	// PR-25 acceptance tests (synthetic host ragel6 PROGRAM) the
	// target-shape LD is structurally sufficient; byte-exact host
	// LD pinning is PR-26+ scope.
	//
	// PR-28-D01: the binary name comes from PROGRAM(name)'s parsed
	// argument. When the caller did not supply it, fall back to the
	// last path component for backwards compatibility with synthetic
	// tests that construct ModuleInstance directly without parsing a
	// ya.make.
	if binaryName == "" {
		binaryName = lastPathComponent(instance.Path)
	}

	outputPath := "$(BUILD_ROOT)/" + instance.Path + "/" + binaryName
	vcsCPath := "$(BUILD_ROOT)/" + instance.Path + "/__vcs_version__.c"
	vcsOPath := "$(BUILD_ROOT)/" + instance.Path + "/__vcs_version__.c.o"

	cmd0 := composeLDCmdVcsInfo(vcsCPath)
	cmd1 := composeLDCmdVcsCompile(vcsCPath, vcsOPath, muslOn)
	cmd2 := composeLDCmdLinkExe(outputPath, vcsOPath, ccPaths, peerLibPaths, pluginPaths, globalPaths)
	cmd3 := composeLDCmdLinkOrCopy(instance.Path)

	// vcs_info.py and fs_tools.py only carry ARCADIA_ROOT_DISTBUILD;
	// the clang compile and link_exe.py invocations both carry the
	// full target-CC env (matches the reference cmd-level env on
	// each cmd).
	envVcsOnly := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}
	envFull := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	cmds := []Cmd{
		{CmdArgs: cmd0, Env: envVcsOnly},
		{CmdArgs: cmd1, Env: envFull},
		{CmdArgs: cmd2, Cwd: "$(BUILD_ROOT)", Env: envFull},
		{CmdArgs: cmd3, Env: envVcsOnly},
	}

	inputs := composeLDInputs(instance.Path, ccPaths, peerLibPaths, pluginPaths, globalPaths)

	// PR-31 D11 + PR-35b: append the per-CC member inputs (source +
	// headers) after the script bundle, deduplicated against the
	// existing set. Matches the sg.json LD shape: BUILD_ROOT block
	// (peers + plugins + globals + own .o, alphabetically sorted by
	// composeLDInputs) + 7 scripts + UNION-of-CC-inputs.
	inputSet := map[string]struct{}{}
	for _, p := range inputs {
		inputSet[p] = struct{}{}
	}

	for _, p := range memberInputs {
		if _, dup := inputSet[p]; dup {
			continue
		}

		inputSet[p] = struct{}{}
		inputs = append(inputs, p)
	}

	// PR-35v: svnversion.h is a c_template consumed by vcs_info.py
	// when generating __vcs_version__.c. ymake registers it as a
	// static input on every PROGRAM LD node, appended after the
	// member-CC input union (verified at index 1051 of the reference
	// tools/archiver LD node with 1052 total inputs, and at the last
	// position of contrib/tools/yasm's LD node with 263 inputs).
	// The CC include scanner does not see this file (it is not
	// #included by any user source), so it must be injected here.
	// Dedup guard is present for safety — in practice the CC closure
	// never contains this path.
	if _, dup := inputSet[ldSvnversionHInput]; !dup {
		inputs = append(inputs, ldSvnversionHInput)
	}

	// DepRefs capture every node whose UID flows into the LD's
	// content hash: own .cpp.o files, plugin inputs, global
	// archives, and peer LIBRARY archives.
	depRefs := make([]NodeRef, 0, len(ccRefs)+len(pluginRefs)+len(globalRefs)+len(peerLDRefs))
	depRefs = append(depRefs, ccRefs...)
	depRefs = append(depRefs, pluginRefs...)
	depRefs = append(depRefs, globalRefs...)
	depRefs = append(depRefs, peerLDRefs...)

	n := &Node{
		Cmds:    cmds,
		Env:     envFull,
		Inputs:  inputs,
		Outputs: []string{outputPath},
		KV: map[string]string{
			"p":        "LD",
			"pc":       "light-blue",
			"show_out": "yes",
		},
		Platform: string(instance.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags: []string{},
		TargetProperties: map[string]string{
			"module_dir":  instance.Path,
			"module_lang": "cpp",
			"module_type": "bin",
		},
		DepRefs: depRefs,
	}

	// PR-25: host PROGRAM modules (cross-platform recursion D31)
	// must carry `host_platform=true` and `tags=["tool"]` to match
	// the convention CC/AR use for host nodes. PR-24's
	// target-only LD never tripped this branch; PR-26 verifies the
	// full host LD bundle byte-exact.
	if instance.Flags.PIC {
		n.HostPlatform = true
		n.Tags = []string{"tool"}
	}

	return emit.Emit(n)
}

// LDOutputPath returns the binary output path for a PROGRAM
// `instance`. Exposed so callers (gen.go) can stash the path in
// `moduleEmitResult` without re-deriving the binary-name rule.
//
// PR-28-D01: `binaryName` parameter mirrors EmitLD's contract — comes
// from PROGRAM(name)'s parsed argument; when empty falls back to the
// last path component.
func LDOutputPath(instance ModuleInstance, binaryName string) string {
	if binaryName == "" {
		binaryName = lastPathComponent(instance.Path)
	}

	return "$(BUILD_ROOT)/" + instance.Path + "/" + binaryName
}

// lastPathComponent returns the trailing path segment of `p`. Empty
// input returns "". The walker uses this to derive a PROGRAM module's
// binary name (e.g. "tools/archiver" → "archiver").
func lastPathComponent(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}

	return p
}

// composeLDCmdVcsInfo composes cmd[0]: invokes
// `build/scripts/vcs_info.py` to materialise `__vcs_version__.c` from
// the upstream VCS state (`vcs.json`) and the C-template stub
// `svn_interface.c`. 5 args, fixed.
func composeLDCmdVcsInfo(vcsCPath string) []string {
	return []string{
		// TODO(portability): python3 path is captured from the
		// reference build host.
		"/ix/realm/pg/bin/python3",
		"$(SOURCE_ROOT)/build/scripts/vcs_info.py",
		"$(VCS)/vcs.json",
		vcsCPath,
		"$(SOURCE_ROOT)/build/scripts/c_templates/svn_interface.c",
	}
}

// composeLDCmdVcsCompile composes cmd[1]: clang compile of
// `__vcs_version__.c` → `__vcs_version__.c.o`. 94 args.
//
// Differs from the regular target CC bundle in two structural ways:
//
//   - The include block is a single `-I$(SOURCE_ROOT)` instead of
//     the 4-element `ccIncludes` set used by user-source CC.
//   - The two `noLibcUndebugBlock` copies are flanked by `-D_musl_=1`
//     and `-D_musl_` sentinels respectively, instead of being bare.
//     Real ymake emits this for vcs_version compiles inside a musl
//     PROGRAM closure to mark the .o as participating in the musl
//     build (`_musl_=1` for the first half, the bare `_musl_` for the
//     second half — verified entry-by-entry against
//     `tools/archiver/__vcs_version__.c.o`).
//
// PR-32 D10: the musl-specific D-flag pair (`-D_musl_=1` and
// `-D_musl_`) is now driven by the CLI's `--define MUSL=...` value
// instead of unconditional injection. The `muslOn` parameter
// reflects `cliMuslOn(ctx)` from the walker; when MUSL=no the two
// sentinels collapse to a bare double-`noLibcUndebugBlock`. The
// flag-bundle data (the literal `-D_musl_=1` / `-D_musl_` strings)
// stays here as the documented musl-internal vcs-compile shape.
//
// Output and input are passed in (output `__vcs_version__.c.o`, input
// `__vcs_version__.c`).
func composeLDCmdVcsCompile(vcsCPath, vcsOPath string, muslOn bool) []string {
	cmdArgs := make([]string, 0, 94)
	cmdArgs = append(cmdArgs,
		ccCompilerPath,
		"--target="+targetTriple,
		"-march="+archFlag,
		"-B"+binPath,
		"-c",
		"-o",
		vcsOPath,
		vcsCPath,
	)
	cmdArgs = append(cmdArgs, "-I$(SOURCE_ROOT)")
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, commonCFlags...)
	cmdArgs = append(cmdArgs, warningFlags...)
	cmdArgs = append(cmdArgs, commonDefines...)

	if muslOn {
		cmdArgs = append(cmdArgs, ldVcsMuslSelfDefine)
	}

	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)

	if muslOn {
		cmdArgs = append(cmdArgs, muslConsumerSentinel)
	}

	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)

	return cmdArgs
}

// ldVcsMuslSelfDefine is the `-D_musl_=1` flag the LD vcs_version
// compile injects between commonDefines and the first
// noLibcUndebugBlock copy when MUSL=yes (PR-32 D10). The =1 form
// matches `muslExtraDefines`'s musl-self CFLAG; the bare `-D_musl_`
// (consumer-side sentinel) is `muslConsumerSentinel` defined in
// gen.go and shared with the EmitCC AutoPeerCFlags path.
const ldVcsMuslSelfDefine = "-D_musl_=1"

// composeLDCmdLinkExe composes cmd[2]: the link_exe.py invocation
// that runs clang++ over the assembled object/archive set. Layout:
//
//	prologue (python3 + link_exe.py)              2 args
//	--start-plugins / paths / --end-plugins       2 + len(plugins) args  (omitted if empty)
//	--clang-ver / --source-root / --build-root    6 args
//	--arch=LINUX                                  1 arg
//	--objcopy-exe / llvm-objcopy                  2 args
//	clang++                                       1 arg
//	-Wl,--whole-archive                           1 arg
//	--ya-start-command-file / globals /           1 + len(globals) + 1 args  (block always present;
//	--ya-end-command-file                                                     globals slice may be empty)
//	-Wl,--no-whole-archive                        1 arg
//	__vcs_version__.c.o + ccPaths                 1 + len(ccPaths) args
//	-o / outputPath                               2 args
//	--target / -march / -B/usr/bin                3 args
//	-Wl,--start-group / peerLibs / -Wl,--end-group  1 + len(peerLibs) + 1 args
//	trailing static-musl flags                    12 args
//
// For tools/archiver: 2 + (3) + 6 + 1 + 2 + 1 + 1 + 3 + 1 + 2 + 2 + 3 + 34 + 12 = 73 args. ✓
func composeLDCmdLinkExe(outputPath, vcsOPath string, ccPaths, peerLibPaths, pluginPaths, globalPaths []string) []string {
	// Capacity hint matches the reference graph's structure plus the
	// caller-supplied slices.
	argCap := 2 + 6 + 1 + 2 + 1 + 1 + 3 + 1 + 2 + 2 + 3 + 12 + 1 + len(ccPaths) + len(peerLibPaths) + len(globalPaths)

	if len(pluginPaths) > 0 {
		argCap += 2 + len(pluginPaths)
	}

	cmdArgs := make([]string, 0, argCap)

	cmdArgs = append(cmdArgs,
		"/ix/realm/pg/bin/python3",
		"$(SOURCE_ROOT)/build/scripts/link_exe.py",
	)

	if len(pluginPaths) > 0 {
		cmdArgs = append(cmdArgs, "--start-plugins")
		cmdArgs = append(cmdArgs, pluginPaths...)
		cmdArgs = append(cmdArgs, "--end-plugins")
	}

	cmdArgs = append(cmdArgs,
		"--clang-ver", "21",
		"--source-root", "$(SOURCE_ROOT)",
		"--build-root", "$(BUILD_ROOT)",
		"--arch=LINUX",
		"--objcopy-exe", "/ix/realm/boot/bin/llvm-objcopy",
		"/ix/realm/boot/bin/clang++",
		"-Wl,--whole-archive",
		"--ya-start-command-file",
	)
	cmdArgs = append(cmdArgs, globalPaths...)
	cmdArgs = append(cmdArgs,
		"--ya-end-command-file",
		"-Wl,--no-whole-archive",
		vcsOPath,
	)
	cmdArgs = append(cmdArgs, ccPaths...)
	cmdArgs = append(cmdArgs,
		"-o", outputPath,
		"--target="+targetTriple,
		"-march="+archFlag,
		"-B"+binPath,
		"-Wl,--start-group",
	)
	cmdArgs = append(cmdArgs, peerLibPaths...)
	cmdArgs = append(cmdArgs, "-Wl,--end-group")
	cmdArgs = append(cmdArgs, ldStaticMuslTrailingFlags...)

	return cmdArgs
}

// composeLDCmdLinkOrCopy composes cmd[3]: invokes fs_tools.py
// `link_or_copy_to_dir` to drop the linked binary into its containing
// directory. 5 args, fixed.
func composeLDCmdLinkOrCopy(modulePath string) []string {
	return []string{
		"/ix/realm/pg/bin/python3",
		"$(SOURCE_ROOT)/build/scripts/fs_tools.py",
		"link_or_copy_to_dir",
		"--no-check",
		"$(BUILD_ROOT)/" + modulePath,
	}
}

// composeLDInputs composes the `inputs` array for an LD node. PR-35b
// closure of PR-31-D09: the BUILD_ROOT block now interleaves peer-
// archive paths with plugins, global archives, and own .o files, all
// sorted alphabetically as one block (matching the sg.json shape).
//
// Layout:
//
//  1. BUILD_ROOT block (alphabetically sorted as one set):
//     - peer LIBRARY archive paths (BUILD_ROOT-relative, prefixed
//     with $(BUILD_ROOT)/)
//     - plugin paths (already $(BUILD_ROOT)-rooted by caller)
//     - global archive paths (BUILD_ROOT-relative, prefixed)
//     - own .cpp.o files (already $(BUILD_ROOT)-rooted by caller)
//  2. The 7-script bundle in REGISTRATION ORDER (NOT alphabetical):
//     vcs_info.py, svn_interface.c, link_exe.py, thinlto_cache.py,
//     process_command_files.py, process_whole_archive_option.py,
//     fs_tools.py.
//  3. Caller appends member-CC inputs (source + headers) after this
//     function returns.
//
// Note that `__vcs_version__.c.o` is NOT in inputs even though it is
// consumed by cmd[2] — it is an intermediate produced by cmd[1]
// inside the same node, so the dependency is implicit. Likewise
// `__vcs_version__.c` is not in inputs — cmd[0] generates it
// in-place.
//
// The reference verification (tools/archiver) shows 35 entries in the
// BUILD_ROOT block: 32 peer .a + 1 plugin + 1 global .global.a + 1
// own main.cpp.o, all interleaved in alphabetical order.
func composeLDInputs(modulePath string, ccPaths, peerLibPaths, pluginPaths, globalPaths []string) []string {
	buildRootBlock := make([]string, 0, len(peerLibPaths)+len(pluginPaths)+len(globalPaths)+len(ccPaths))

	for _, p := range peerLibPaths {
		buildRootBlock = append(buildRootBlock, "$(BUILD_ROOT)/"+p)
	}

	buildRootBlock = append(buildRootBlock, pluginPaths...)

	for _, g := range globalPaths {
		buildRootBlock = append(buildRootBlock, "$(BUILD_ROOT)/"+g)
	}

	buildRootBlock = append(buildRootBlock, ccPaths...)
	sort.Strings(buildRootBlock)

	out := make([]string, 0, len(buildRootBlock)+len(ldScriptInputs))
	out = append(out, buildRootBlock...)
	out = append(out, ldScriptInputs...)

	_ = modulePath // reserved for future use (path-dependent inputs).

	return out
}

// ldSvnversionHInput is the c_template header consumed by vcs_info.py
// when it generates __vcs_version__.c. ymake appends it as the last
// entry of every PROGRAM LD node's inputs slice, after the member-CC
// input union. PR-35v adds this static injection (R9 closure).
const ldSvnversionHInput = "$(SOURCE_ROOT)/build/scripts/c_templates/svnversion.h"

// ldStaticMuslTrailingFlags is the 12-flag trailer the reference
// `tools/archiver/archiver` LD cmd[2] emits AFTER `-Wl,--end-group`.
// The shape encodes a static-musl Linux executable: no PIE, no
// dynamic linker, hand-rolled libc/libdl/libm linkage, and explicit
// section gc.
//
// `-nostdlib` appears TWICE in the reference (once after `-fno-pie`
// at index 70 of the original 73-arg slice, again at index 70-after-
// reindex — verified by direct probe). The duplication is part of the
// reference output; do not deduplicate.
var ldStaticMuslTrailingFlags = []string{
	"-rdynamic",
	"-Wl,--no-as-needed",
	"-static",
	"-Wl,--no-dynamic-linker",
	"-lrt",
	"-ldl",
	"-nostdlib",
	"-fno-pie",
	"-Wl,-no-pie",
	"-nostdlib",
	"-lm",
	"-Wl,--gc-sections",
}

// ldScriptInputs is the 7-script bundle that appears at the tail of
// every LD node's `inputs` array, in the exact NON-ALPHABETICAL order
// observed in the reference graph. The order encodes ymake's
// registration sequence for the link-script tool family; preserving
// it is required for byte-exact `inputs` matching (per PR-05's
// "inputs are NOT alphabetical for ~7 of 3730 nodes" finding).
var ldScriptInputs = []string{
	"$(SOURCE_ROOT)/build/scripts/vcs_info.py",
	"$(SOURCE_ROOT)/build/scripts/c_templates/svn_interface.c",
	"$(SOURCE_ROOT)/build/scripts/link_exe.py",
	"$(SOURCE_ROOT)/build/scripts/thinlto_cache.py",
	"$(SOURCE_ROOT)/build/scripts/process_command_files.py",
	"$(SOURCE_ROOT)/build/scripts/process_whole_archive_option.py",
	"$(SOURCE_ROOT)/build/scripts/fs_tools.py",
}
