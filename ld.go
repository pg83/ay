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
//           CC node but with a single `-I$(S)` (no full
//           ccIncludes set) and `-D_musl_=1` / `-D_musl_` sentinels
//           wrapping the two `noLibcUndebugBlock` copies instead of
//           CC's bare `noLibcUndebugBlock × 2`.
//   cmd[2]: link_exe.py — the actual link invocation. 73 args. Carries
//           a `cwd: $(B)` because the emitted command-file
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
//     `$(B)/<path>/<binaryName>`; PR-24 does not handle host
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
//     NOT prefixed with `$(B)/` — link_exe.py interprets the
//     argv strings relative to its `cwd`. The `peerLDRefs` are wired
//     as DepRefs so the Merkle hash captures the link-time inputs.
//   - `pluginRefs` / `pluginPaths`: plugin script paths for the
//     `--start-plugins ... --end-plugins` block (e.g. the musl
//     pyplugin). `pluginPaths` are full `$(B)/...` paths
//     because they appear verbatim in cmd[2] and in `inputs`. Pass nil
//     when the module has no plugins.
//   - `globalRefs` / `globalPaths`: peer `.global.a` archives that
//     wrap into the `-Wl,--whole-archive ... -Wl,--no-whole-archive`
//     block. `globalPaths` are BUILD_ROOT-relative (same convention
//     as peerLibPaths). Pass nil when none.
//
// Returns the LD NodeRef. The output path is
// `$(B)/<instance.Path>/<binaryName>`; the caller can
// re-derive it via `LDOutputPath(instance, binaryName)` if needed.
func EmitLD(
	instance ModuleInstance,
	binaryName string,
	ccRefs []NodeRef,
	ccPaths []VFS,
	peerLDRefs []NodeRef,
	peerLibPaths []string,
	pluginRefs []NodeRef,
	pluginPaths []string,
	globalRefs []NodeRef,
	globalPaths []string,
	objcopyRefs []NodeRef,
	objcopyPaths []VFS,
	memberInputs []VFS, //nolint:vfs-stay // cascade from gen.go member-inputs bucket
	muslOn bool,
	moduleCFlags []string,
	peerCFlagsGlobal []string,
	usePython3 bool,
	wantsStrip bool,
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

	if len(objcopyRefs) != len(objcopyPaths) {
		ThrowFmt("EmitLD: objcopyRefs/objcopyPaths length mismatch (%d vs %d)", len(objcopyRefs), len(objcopyPaths))
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

	// PR-40 Fix D: RECURSE-driven LD output-dir lift for ragel6.
	// The PROGRAM(ragel6) macro lives in contrib/tools/ragel6/bin/ya.make
	// but the reference graph attributes the LD output and all
	// BUILD_ROOT-relative paths to the PARENT directory
	// contrib/tools/ragel6. Use ldBinaryDir to resolve the effective
	// output directory so every path formula uses the lifted parent.
	// TODO: remove this shim when a general RECURSE-driven BinaryDir
	// lift lands in M3+.
	binaryDir := ldBinaryDir(instance)
	// PR-M3-platform-pair-step11: compose-flavor dispatch on
	// instance.Platform.Target. The local is named `targetX8664` to make the
	// per-platform-identity question explicit (rather than the
	// "am I a host build?" framing of the prior `hostBuild`). The
	// downstream composers still see the same boolean.
	targetX8664 := instance.Platform.Target == PlatformDefaultLinuxX8664
	hostBuild := targetX8664

	binPrefix := binaryDir + "/"
	outputVFS := Build(binPrefix + binaryName)
	vcsCVFS := Build(binPrefix + "__vcs_version__.c")

	// PR-38: host PROGRAM nodes use `.pic.o` for the vcs_version
	// compile output to match the reference graph shape; target nodes
	// use plain `.o`.
	vcsOSuffix := ".o"
	if hostBuild {
		vcsOSuffix = ".pic.o"
	}

	vcsOVFS := Build(binPrefix + "__vcs_version__.c" + vcsOSuffix)

	cmd0 := composeLDCmdVcsInfo(vcsCVFS.String())
	cmd1 := composeLDCmdVcsCompile(vcsCVFS.String(), vcsOVFS.String(), muslOn, moduleCFlags, peerCFlagsGlobal, usePython3, hostBuild, instance.Flags.NoCompilerWarnings)
	cmd2 := composeLDCmdLinkExe(outputVFS.String(), vcsOVFS.String(), ccPaths, peerLibPaths, pluginPaths, globalPaths, objcopyPaths, hostBuild, wantsStrip)
	cmd3 := composeLDCmdLinkOrCopy(binaryDir)

	// vcs_info.py and fs_tools.py only carry ARCADIA_ROOT_DISTBUILD;
	// the clang compile and link_exe.py invocations both carry the
	// full target-CC env (matches the reference cmd-level env on
	// each cmd).
	envVcsOnly := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}
	envFull := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	cmds := []Cmd{
		{CmdArgs: cmd0, Env: envVcsOnly},
		{CmdArgs: cmd1, Env: envFull},
		{CmdArgs: cmd2, Cwd: "$(B)", Env: envFull},
		{CmdArgs: cmd3, Env: envVcsOnly},
	}

	inputs := composeLDInputs(instance.Path, ccPaths, peerLibPaths, pluginPaths, globalPaths, objcopyPaths)

	// PR-31 D11 + PR-35b: append the per-CC member inputs (source +
	// headers) after the script bundle, deduplicated against the
	// existing set. Matches the sg.json LD shape: BUILD_ROOT block
	// (peers + plugins + globals + own .o, alphabetically sorted by
	// composeLDInputs) + 7 scripts + UNION-of-CC-inputs.
	inputSet := map[VFS]struct{}{}
	for _, p := range inputs {
		inputSet[p] = struct{}{}
	}

	for _, pV := range memberInputs {
		if _, dup := inputSet[pV]; dup {
			continue
		}

		// PR-M3-final-codegen-registry-expansion: drop BUILD_ROOT-rooted
		// codegen products (generated `.pb.h`, `.pb.cc`, `_serialized.*`,
		// ANTLR outputs, etc.) from LD inputs. Same shape as AR: BUILD_ROOT
		// entries on an LD's `inputs` slot are .o objects and .a archives
		// (plus the rare .pyplugin); generated source/header artifacts are
		// wired solely through the constituent CC's own `inputs`. Verified
		// in REF on tools/event2cpp/event2cpp.
		if pV.IsBuild() && isBuildRootCodegenProductRel(pV.Rel) {
			continue
		}

		inputSet[pV] = struct{}{}
		inputs = append(inputs, pV)
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
	if _, dup := inputSet[ldSvnversionHVFS]; !dup {
		inputs = append(inputs, ldSvnversionHVFS)
	}

	// DepRefs capture every node whose UID flows into the LD's
	// content hash: own .cpp.o files, plugin inputs, global
	// archives, and peer LIBRARY archives.
	depRefs := make([]NodeRef, 0, len(ccRefs)+len(pluginRefs)+len(globalRefs)+len(peerLDRefs)+len(objcopyRefs))
	depRefs = append(depRefs, ccRefs...)
	depRefs = append(depRefs, pluginRefs...)
	depRefs = append(depRefs, globalRefs...)
	depRefs = append(depRefs, peerLDRefs...)
	depRefs = append(depRefs, objcopyRefs...)

	n := &Node{
		Cmds:    cmds,
		Env:     envFull,
		Inputs:  inputs,
		Outputs: []VFS{outputVFS},
		KV: map[string]string{
			"p":        "LD",
			"pc":       "light-blue",
			"show_out": "yes",
		},
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags: instance.Platform.Tags,
		TargetProperties: map[string]string{
			"module_dir":  binaryDir,
			"module_lang": ldModuleLang(instance),
			"module_type": "bin",
		},
		DepRefs: depRefs,
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

	return Build(ldBinaryDir(instance) + "/" + binaryName).String()
}

// ldBinaryDir returns the effective BUILD_ROOT-relative directory for
// the LD node's output, vcs artifacts, and target_properties.module_dir.
// For most PROGRAMs this equals instance.Path; for contrib/tools/ragel6/bin
// the reference graph lifts all paths to the parent contrib/tools/ragel6.
//
// PR-40 Fix D: narrow shim — the only PROGRAM in the M2 closure that
// exercises RECURSE-driven output-dir lifting is ragel6/bin. A general
// BinaryDir field on ModuleInstance (set by the RECURSE walker) would
// replace this check; deferred to M3+.
// TODO: remove this shim when RECURSE-driven BinaryDir lift lands in M3+.
//
// PR-M3-F-1: extend the table with M3-closure bin-subdir PROGRAMs that
// declare SRCDIR(parent) so their binary lands in the parent dir, not
// the /bin subdir that we walk.
func ldBinaryDir(instance ModuleInstance) string {
	switch instance.Path {
	case "contrib/tools/ragel6/bin":
		return "contrib/tools/ragel6"
	case "tools/py3cc/bin":
		return "tools/py3cc"
	case "tools/event2cpp/bin":
		return "tools/event2cpp"
	case "tools/rescompiler/bin":
		return "tools/rescompiler"
	case "tools/rescompressor/bin":
		return "tools/rescompressor"
	case "tools/py3cc/slow/bin":
		return "tools/py3cc/slow"
	}

	return instance.Path
}

// ldModuleLang returns the value for the `module_lang` target_property
// of an LD node. Python program modules (PY3_PROGRAM_BIN) emit `py3`;
// all other PROGRAM modules emit `cpp`.
func ldModuleLang(instance ModuleInstance) string {
	if instance.Language == LangPy {
		return "py3"
	}

	return "cpp"
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
		"$(S)/build/scripts/vcs_info.py",
		"$(VCS)/vcs.json",
		vcsCPath,
		"$(S)/build/scripts/c_templates/svn_interface.c",
	}
}

// composeLDCmdVcsCompile composes cmd[1]: clang compile of
// `__vcs_version__.c` → `__vcs_version__.c.o` (target) or
// `__vcs_version__.c.pic.o` (host). Target: 94 args.
//
// Target build (hostBuild=false): uses the target toolchain triple,
// -march=, commonCFlags, warningFlags, commonDefines. The
// `moduleCFlags` parameter is unused on the target path.
//
// Host build (hostBuild=true): uses hostTriple (no -march), hostCFlags,
// the warning bundle picked by `pickWarningFlags(noCompilerWarnings)`
// (6-arg `-Werror`/`-Wall`/`-Wextra` + 3× `-Wno-*` for normal modules;
// 1-arg `-Wno-everything` for NO_COMPILER_WARNINGS modules), hostDefines, then moduleCFlags
// (which carries the module's own CFLAGS plus -D_musl_=1 when muslOn),
// then ndebugPicBlock × 2 with catboostOpenSourceDefine +
// muslConsumerSentinel + hostSseFeatures between them. Pinned byte-exact
// against the reference contrib/tools/ragel6/ragel6 LD cmd[1] (PR-38).
//
// PR-32 D10: the musl-specific D-flag pair (`-D_musl_=1` and
// `-D_musl_`) is now driven by the CLI's `--define MUSL=...` value
// instead of unconditional injection. The `muslOn` parameter
// reflects `cliMuslOn(ctx)` from the walker; when MUSL=no the two
// sentinels collapse to a bare double-`noLibcUndebugBlock` (target)
// or no muslConsumerSentinel between catboost and SSE (host).
func composeLDCmdVcsCompile(vcsCPath, vcsOPath string, muslOn bool, moduleCFlags, peerCFlagsGlobal []string, usePython3 bool, hostBuild bool, noCompilerWarnings bool) []string {
	if hostBuild {
		return composeLDCmdVcsCompileHost(vcsCPath, vcsOPath, muslOn, moduleCFlags, peerCFlagsGlobal, usePython3, noCompilerWarnings)
	}

	cmdArgs := make([]string, 0, 94+len(peerCFlagsGlobal))
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
	cmdArgs = append(cmdArgs, "-I$(S)")
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, commonCFlags...)
	cmdArgs = append(cmdArgs, warningFlags...)
	cmdArgs = append(cmdArgs, commonDefines...)

	if muslOn {
		cmdArgs = append(cmdArgs, ldVcsMuslSelfDefine)
	}

	// PR-M3-final-LD-trailer-and-cflags Cluster B: PEERDIR-derived GLOBAL
	// CFLAGS land here between the musl-self sentinel and the first
	// `noLibcUndebugBlock`. Empirical anchor (devtools/ymake/bin/ymake
	// cmd[1] ref:48..58): -DLZMA_API_STATIC, -DOPENSSL_RENAME_SYMBOLS=1,
	// -DFFI_STATIC_BUILD, -DUSE_PYTHON3, -DASIO_STANDALONE,
	// -DASIO_SEPARATE_COMPILATION, -DFMT_EXPORT, -DPCRE_STATIC,
	// -DANTLR4CPP_STATIC, -DANTLR4_USE_THREAD_LOCAL_CACHE,
	// -DANTLR4CPP_USING_ABSEIL precede -UNDEBUG / -mno-outline-atomics.
	cmdArgs = append(cmdArgs, peerCFlagsGlobal...)

	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)

	if muslOn {
		cmdArgs = append(cmdArgs, muslConsumerSentinel)
	}

	// PR-M3-final-LD-trailer-and-cflags Cluster B: USE_PYTHON3
	// (defaultPeerCFlags slot for target LD vcs compile) lands between
	// muslConsumerSentinel and the second noLibcUndebugBlock copy.
	// Anchor: devtools/ymake/bin/ymake cmd[1] ref:83 (`-DUSE_PYTHON3`
	// after `-D_musl_`, before second `-UNDEBUG`).
	if usePython3 {
		cmdArgs = append(cmdArgs, "-DUSE_PYTHON3")
	}

	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)

	return cmdArgs
}

// composeLDCmdVcsCompileHost composes the HOST variant of cmd[1]: uses
// the x86_64 toolchain, hostCFlags, the warning bundle, hostDefines,
// then `moduleCFlags` (module-own CFLAGS + `-D_musl_=1` when muslOn),
// then ndebugPicBlock twice with catboostOpenSourceDefine +
// muslConsumerSentinel + hostSseFeatures between them. Matches the
// reference shape for contrib/tools/ragel6 and contrib/tools/yasm host
// LD cmd[1].
//
// Warning bundle selection follows the module's NO_COMPILER_WARNINGS
// attribute (mirror of `pickWarningFlags`): modules that declare
// NO_COMPILER_WARNINGS (contrib/tools/* vendored upstream tools — yasm,
// ragel5, ragel6, protoc, cpp_styleguide) get the single-arg
// `-Wno-everything` (muslWarningFlags); regular host PROGRAMs
// (tools/archiver, tools/enum_parser, tools/event2cpp, tools/py3cc,
// tools/rescompiler, tools/rescompressor, tools/struct2fieldcalc,
// library/python/runtime_py3/stage0pycc) get the 6-arg standard
// bundle (`-Werror -Wall -Wextra -Wno-parentheses
// -Wno-implicit-const-int-float-conversion -Wno-unknown-warning-option`).
// Canonical bundle composition: `ymake_conf.py:1550-1556` and
// `gnu_compiler.conf:124-140`.
func composeLDCmdVcsCompileHost(vcsCPath, vcsOPath string, muslOn bool, moduleCFlags, peerCFlagsGlobal []string, usePython3 bool, noCompilerWarnings bool) []string {
	cmdArgs := make([]string, 0, 94+len(moduleCFlags)+len(peerCFlagsGlobal))
	cmdArgs = append(cmdArgs,
		ccCompilerPath,
		"--target="+hostTriple,
		"-B"+binPath,
		"-c",
		"-o",
		vcsOPath,
		vcsCPath,
	)
	cmdArgs = append(cmdArgs, "-I$(S)")
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, hostCFlags...)
	cmdArgs = append(cmdArgs, pickWarningFlags(noCompilerWarnings)...)
	cmdArgs = append(cmdArgs, hostDefines...)
	cmdArgs = append(cmdArgs, moduleCFlags...)
	// PR-M3-final-LD-trailer-and-cflags Cluster B: PEERDIR-derived GLOBAL
	// CFLAGS land here between moduleCFlags (terminating with -D_musl_=1
	// when MUSL=yes) and the first ndebugPicBlock. Empirical anchor
	// (tools/py3cc/py3cc cmd[1] ref:45..47): -DLZMA_API_STATIC,
	// -DOPENSSL_RENAME_SYMBOLS=1, -DFFI_STATIC_BUILD precede -DNDEBUG /
	// -fPIC.
	cmdArgs = append(cmdArgs, peerCFlagsGlobal...)
	cmdArgs = append(cmdArgs, ndebugPicBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)

	if muslOn {
		cmdArgs = append(cmdArgs, muslConsumerSentinel)
	}

	cmdArgs = append(cmdArgs, hostSseFeatures...)

	// PR-M3-final-LD-trailer-and-cflags Cluster B: USE_PYTHON3
	// (defaultPeerCFlags slot for host LD vcs compile) lands between
	// hostSseFeatures and the second ndebugPicBlock copy. Anchor:
	// tools/py3cc/slow/py3cc cmd[1] ref:78 (`-DUSE_PYTHON3` after the
	// `-mcx16` SSE tail, before the second `-DNDEBUG`).
	if usePython3 {
		cmdArgs = append(cmdArgs, "-DUSE_PYTHON3")
	}

	cmdArgs = append(cmdArgs, ndebugPicBlock...)

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
//	__vcs_version__.c[.pic].o + ccPaths           1 + len(ccPaths) args
//	-o / outputPath                               2 args
//	--target / [-march] / -B/usr/bin              2-3 args (target has -march; host does not)
//	-Wl,--start-group / peerLibs / -Wl,--end-group  1 + len(peerLibs) + 1 args
//	trailing static flags                         12 args
//
// For tools/archiver (target): 2 + (3) + 6 + 1 + 2 + 1 + 1 + 3 + 1 + 2 + 2 + 3 + 34 + 12 = 73 args. ✓
// For ragel6 (host): 2 + (3) + 6 + 1 + 2 + 1 + 1 + 3 + 1 + 10 + 2 + 2 + 12 + 12 = 58 args (varies by peer count).
//
// PR-38: `hostBuild` selects the host toolchain triple (x86_64, no
// -march) and `ldHostStaticTrailingFlags` in place of the target's
// `ldStaticMuslTrailingFlags` (which carries -lrt/-ldl absent on host).
//
// PR-M3-py3-program-bin-strip-all: `wantsStrip` controls insertion of
// `-Wl,--strip-all` between the trailer's `-lm` and `-Wl,--gc-sections`.
// Set true for PY3_PROGRAM_BIN (STRIP() in _BASE_PY3_PROGRAM, python.conf:884).
func composeLDCmdLinkExe(outputPath, vcsOPath string, ccPaths []VFS, peerLibPaths, pluginPaths, globalPaths []string, objcopyPaths []VFS, hostBuild, wantsStrip bool) []string {
	// Capacity hint matches the reference graph's structure plus the
	// caller-supplied slices.
	argCap := 2 + 6 + 1 + 2 + 1 + 1 + 3 + 1 + 2 + 2 + 3 + 12 + 1 + len(ccPaths) + len(peerLibPaths) + len(globalPaths) + len(objcopyPaths)

	if len(pluginPaths) > 0 {
		argCap += 2 + len(pluginPaths)
	}

	cmdArgs := make([]string, 0, argCap)

	cmdArgs = append(cmdArgs,
		"/ix/realm/pg/bin/python3",
		"$(S)/build/scripts/link_exe.py",
	)

	if len(pluginPaths) > 0 {
		cmdArgs = append(cmdArgs, "--start-plugins")
		cmdArgs = append(cmdArgs, pluginPaths...)
		cmdArgs = append(cmdArgs, "--end-plugins")
	}

	cmdArgs = append(cmdArgs,
		"--clang-ver", "21",
		"--source-root", "$(S)",
		"--build-root", "$(B)",
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
	)
	// PR-M3-py3cc-objcopy-shape: SRCS_GLOBAL .o slot goes BEFORE
	// $VCS_C_OBJ (upstream ld.conf:229-230 / 266-267 / 294-295). Paths
	// emit bare (BUILD_ROOT-relative) — upstream uses
	// `${rootrel;ext=.o:SRCS_GLOBAL}` which strips the $(B)/
	// prefix. The `inputs` slot retains the prefix via composeLDInputs.
	for _, p := range objcopyPaths {
		// SRCS_GLOBAL bare-relative rendering: `${rootrel;ext=.o:SRCS_GLOBAL}`
		// strips the $(B)/ prefix; VFS.Rel gives that form natively.
		cmdArgs = append(cmdArgs, p.Rel)
	}

	cmdArgs = append(cmdArgs, vcsOPath)
	for _, p := range ccPaths {
		cmdArgs = append(cmdArgs, p.String())
	}
	cmdArgs = append(cmdArgs, "-o", outputPath)

	if hostBuild {
		cmdArgs = append(cmdArgs,
			"--target="+hostTriple,
			"-B"+binPath,
		)
	} else {
		cmdArgs = append(cmdArgs,
			"--target="+targetTriple,
			"-march="+archFlag,
			"-B"+binPath,
		)
	}

	cmdArgs = append(cmdArgs, "-Wl,--start-group")
	cmdArgs = append(cmdArgs, peerLibPaths...)
	cmdArgs = append(cmdArgs, "-Wl,--end-group")

	var trailer []string
	if hostBuild {
		if peersIncludeLibcCompat(peerLibPaths) {
			trailer = ldHostStaticRTTrailingFlags
		} else {
			trailer = ldHostStaticTrailingFlags
		}
	} else {
		trailer = ldStaticMuslTrailingFlags
	}

	// PR-M3-py3-program-bin-strip-all: when STRIP() was called on the
	// module (PY3_PROGRAM_BIN per `_BASE_PY3_PROGRAM` in upstream
	// python.conf), splice `-Wl,--strip-all` between the trailer's
	// `-lm` and its terminating `-Wl,--gc-sections`. The trailer
	// constants are pinned to end with `-Wl,--gc-sections`; we
	// preserve that postfix and insert before it.
	if wantsStrip {
		n := len(trailer)
		if n == 0 || trailer[n-1] != ldGcSectionsFlag {
			ThrowFmt("composeLDCmdLinkExe: trailer must end with %q for strip insertion", ldGcSectionsFlag)
		}

		cmdArgs = append(cmdArgs, trailer[:n-1]...)
		cmdArgs = append(cmdArgs, ldStripAllFlag)
		cmdArgs = append(cmdArgs, trailer[n-1])
	} else {
		cmdArgs = append(cmdArgs, trailer...)
	}

	return cmdArgs
}

// ldStripAllFlag is the linker flag injected when STRIP() is in effect
// for the module (PY3_PROGRAM_BIN — see `_BASE_PY3_PROGRAM` in
// `build/conf/python.conf:884`). Resolved upstream as
// `LD_STRIP_FLAG=-Wl,--strip-all` in `build/conf/linkers/ld.conf:22`
// for Linux/Android targets.
const ldStripAllFlag = "-Wl,--strip-all"

// ldGcSectionsFlag is the trailer-terminating linker flag every LD
// trailer ends with. The strip-all splice in composeLDCmdLinkExe
// asserts this postfix to keep the insertion order pinned.
const ldGcSectionsFlag = "-Wl,--gc-sections"

// peersIncludeLibcCompat reports whether the host LD's peer-archive
// slot includes `contrib/libs/libc_compat`. Used to select the
// `-lrt`/`-ldl`-augmented trailer.
func peersIncludeLibcCompat(peerLibPaths []string) bool {
	for _, p := range peerLibPaths {
		if p == libcCompatPeerPath {
			return true
		}
	}

	return false
}

// composeLDCmdLinkOrCopy composes cmd[3]: invokes fs_tools.py
// `link_or_copy_to_dir` to drop the linked binary into its containing
// directory. 5 args, fixed.
func composeLDCmdLinkOrCopy(modulePath string) []string {
	return []string{
		"/ix/realm/pg/bin/python3",
		"$(S)/build/scripts/fs_tools.py",
		"link_or_copy_to_dir",
		"--no-check",
		Build(modulePath).String(),
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
//     with $(B)/)
//     - plugin paths (already $(B)-rooted by caller)
//     - global archive paths (BUILD_ROOT-relative, prefixed)
//     - own .cpp.o files (already $(B)-rooted by caller)
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
func composeLDInputs(modulePath string, ccPaths []VFS, peerLibPaths []string, pluginPaths []string, globalPaths []string, objcopyPaths []VFS) []VFS {
	// peerLibPaths / globalPaths arrive BUILD_ROOT-relative (caller convention);
	// pluginPaths arrive as full $(B)/... strings. ccPaths and
	// objcopyPaths are already VFS-typed. Lift all into the VFS-typed
	// BUILD_ROOT block before alphabetising.
	buildRootBlock := make([]VFS, 0, len(peerLibPaths)+len(pluginPaths)+len(globalPaths)+len(ccPaths)+len(objcopyPaths))

	for _, p := range peerLibPaths {
		buildRootBlock = append(buildRootBlock, Build(p))
	}

	for _, p := range pluginPaths {
		buildRootBlock = append(buildRootBlock, ParseVFSOrSource(p))
	}

	for _, g := range globalPaths {
		buildRootBlock = append(buildRootBlock, Build(g))
	}

	buildRootBlock = append(buildRootBlock, ccPaths...)
	// PR-M3-py3cc-objcopy-shape: objcopy `.o` paths arrive as
	// $(B)/...-rooted; they belong in the BUILD_ROOT block of
	// the LD's `inputs` slot just like own .cpp.o and peer .a entries.
	buildRootBlock = append(buildRootBlock, objcopyPaths...)
	sort.Slice(buildRootBlock, func(i, j int) bool {
		return string(buildRootBlock[i].Rel) < string(buildRootBlock[j].Rel)
	})

	out := make([]VFS, 0, len(buildRootBlock)+len(ldScriptInputs))
	out = append(out, buildRootBlock...)
	out = append(out, ldScriptInputs...)

	_ = modulePath // reserved for future use (path-dependent inputs).

	return out
}

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

// ldHostStaticTrailingFlags is the 12-flag trailer the reference host
// PROGRAM LD cmd[2] (ragel6, yasm) emits AFTER `-Wl,--end-group`.
// Differs from `ldStaticMuslTrailingFlags` in two places: `-lrt` and
// `-ldl` are absent (host binaries do not need the glibc-compat rt/dl
// shims), replaced by two `-fPIC` flags that satisfy the static-PIC
// link invariant. Verified entry-by-entry against
// `contrib/tools/ragel6/ragel6` LD cmd[2] in the reference graph.
var ldHostStaticTrailingFlags = []string{
	"-rdynamic",
	"-Wl,--no-as-needed",
	"-fPIC",
	"-fPIC",
	"-static",
	"-Wl,--no-dynamic-linker",
	"-nostdlib",
	"-fno-pie",
	"-Wl,-no-pie",
	"-nostdlib",
	"-lm",
	"-Wl,--gc-sections",
}

// ldHostStaticRTTrailingFlags is the 14-flag trailer the reference host
// PROGRAM LD cmd[2] emits AFTER `-Wl,--end-group` for host binaries
// that link against `contrib/libs/libc_compat` (and typically
// `contrib/libs/linuxvdso`). Differs from `ldHostStaticTrailingFlags`
// by the inserted `-lrt`/`-ldl` pair between `--no-dynamic-linker` and
// the first `-nostdlib`. Verified entry-by-entry against
// `tools/archiver/archiver` LD cmd[2] in the reference graph (PR-M3-
// final-LD-trailer-and-cflags Cluster A).
var ldHostStaticRTTrailingFlags = []string{
	"-rdynamic",
	"-Wl,--no-as-needed",
	"-fPIC",
	"-fPIC",
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

// libcCompatPeerPath is the peer-library path whose presence triggers
// the `-lrt`/`-ldl` host LD trailer. Empirically (PR-M3-final-LD-
// trailer-and-cflags Cluster A): host PROGRAMs that PEERDIR
// `contrib/libs/libc_compat` (archiver, protoc, py3cc, stage0pycc, ...)
// pull in glibc-compat shims that require `-lrt`/`-ldl`; host PROGRAMs
// that do not (ragel6, yasm) omit them. The peer path appears in
// `peerLibPaths` for host LD nodes as
// `contrib/libs/libc_compat/libcontrib-libs-libc_compat.a`.
const libcCompatPeerPath = "contrib/libs/libc_compat/libcontrib-libs-libc_compat.a"

// ldScriptInputs is the 7-script bundle that appears at the tail of
// every LD node's `inputs` array, in the exact NON-ALPHABETICAL order
// observed in the reference graph. The order encodes ymake's
// registration sequence for the link-script tool family; preserving
// it is required for byte-exact `inputs` matching (per PR-05's
// "inputs are NOT alphabetical for ~7 of 3730 nodes" finding).
var ldScriptInputs = []VFS{
	Source("build/scripts/vcs_info.py"),
	Source("build/scripts/c_templates/svn_interface.c"),
	Source("build/scripts/link_exe.py"),
	Source("build/scripts/thinlto_cache.py"),
	Source("build/scripts/process_command_files.py"),
	Source("build/scripts/process_whole_archive_option.py"),
	Source("build/scripts/fs_tools.py"),
}

// ldSvnversionHVFS is the c_template header consumed by vcs_info.py
// when it generates __vcs_version__.c. ymake appends it as the last
// entry of every PROGRAM LD node's inputs slice, after the member-CC
// input union. PR-35v adds this static injection (R9 closure).
var ldSvnversionHVFS = Source("build/scripts/c_templates/svnversion.h")
