package main

// ld.go — emitter for LD (link executable) nodes.
//
// An LD node has ONE Node with FOUR Cmds:
//   cmd[0]: vcs_info.py — generates `__vcs_version__.c` (5 args).
//   cmd[1]: clang compile of `__vcs_version__.c` → `__vcs_version__.c.o`.
//           94 args. Same shape as CC but with single `-I$(S)` and
//           `-D_musl_=1` / `-D_musl_` sentinels around the two
//           `noLibcUndebugBlock` copies.
//   cmd[2]: link_exe.py — link invocation (73 args). Carries
//           `cwd: $(B)` because emitted command-file paths are
//           BUILD_ROOT-relative.
//   cmd[3]: fs_tools.py link_or_copy_to_dir — drops the linked binary
//           into its module dir's output slot (5 args).
//
// `TestEmitLD_ToolsArchiver_ByteExact` pins each cmd_args slice
// entry-by-entry against the reference `tools/archiver/archiver` LD.
//
// inputs[]: BUILD_ROOT block (peer .a + pyplugin + .global.a + own
// .cpp.o files) emitted as one alphabetically-sorted set, then the
// 7-script bundle in REGISTRATION ORDER (NOT alphabetical: vcs_info.py,
// svn_interface.c, link_exe.py, thinlto_cache.py,
// process_command_files.py, process_whole_archive_option.py,
// fs_tools.py), then union of every member CC's inputs in DFS order
// (1052 entries for reference tools/archiver).

import (
	"sort"
)

// EmitLD emits the 4-cmd LD node for a PROGRAM module.
//
// Caller-supplied inputs:
//   - `instance`: PROGRAM ModuleInstance; `instance.Path` names the
//     module dir. Target binary at `$(B)/<path>/<binaryName>`.
//   - `binaryName`: linker output basename — `PROGRAM(name)`'s parsed
//     argument; when empty falls back to `lastPathComponent(instance.Path)`.
//     Most PROGRAMs match the directory's trailing component; divergent
//     case is `contrib/tools/ragel6/bin/ya.make` declaring `PROGRAM(ragel6)`
//     (binary is `bin/ragel6`, not `bin/bin`).
//   - `ccRefs` / `ccPaths`: module's own .cpp.o files, one per source.
//     Order matters for cmd[2] argv: emitted between whole-archive
//     block and `-o` flag in the order supplied.
//   - `peerLDRefs` / `peerLibPaths`: peer LIBRARY archives in PEERDIR
//     walk order (non-alphabetical). Paths are BUILD_ROOT-relative
//     (link_exe.py uses cwd=$(B)). peerLDRefs wire DepRefs.
//   - `pluginRefs` / `pluginPaths`: plugins for `--start-plugins ...
//     --end-plugins` (e.g. musl pyplugin). `pluginPaths` are full
//     `$(B)/...`. Pass nil when none.
//   - `globalRefs` / `globalPaths`: peer `.global.a` archives wrapped
//     in `-Wl,--whole-archive ... -Wl,--no-whole-archive`. Paths
//     BUILD_ROOT-relative. Pass nil when none.
//
// Returns the LD NodeRef. Output path is `$(B)/<instance.Path>/<binaryName>`;
// callers can re-derive via `LDOutputPath(instance, binaryName)`.
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
	wantsSplitDwarf bool,
	hostP *Platform,
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

	// Fall back to last path component when caller omits binaryName
	// (synthetic tests constructing ModuleInstance directly).
	if binaryName == "" {
		binaryName = lastPathComponent(instance.Path)
	}

	// RECURSE-driven LD output-dir lift for ragel6 et al. (see ldBinaryDir).
	// PROGRAM(ragel6) lives in contrib/tools/ragel6/bin/ya.make but REF
	// attributes the LD output to the parent contrib/tools/ragel6.
	// TODO: remove shim when a general RECURSE-driven BinaryDir lift lands.
	binaryDir := ldBinaryDir(instance)
	hostBuild := instance.Platform.IsHost

	binPrefix := binaryDir + "/"
	outputVFS := Build(binPrefix + binaryName)
	vcsCVFS := Build(binPrefix + "__vcs_version__.c")

	// Host PROGRAM nodes use `.pic.o` for vcs_version compile output;
	// target nodes use plain `.o`.
	vcsOSuffix := ".o"
	if hostBuild {
		vcsOSuffix = ".pic.o"
	}

	vcsOVFS := Build(binPrefix + "__vcs_version__.c" + vcsOSuffix)

	// Pre-materialise the three .String() forms — vcsCVFS / vcsOVFS
	// flow into multiple cmd composers; .String() them once each.
	vcsCPath := vcsCVFS.String()
	vcsOPath := vcsOVFS.String()
	outputPath := outputVFS.String()

	tools := instance.Platform.Tools
	cmd0 := composeLDCmdVcsInfo(tools, vcsCPath)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, vcsCPath, vcsOPath, muslOn, moduleCFlags, peerCFlagsGlobal, usePython3, hostBuild, instance.Flags.NoCompilerWarnings)
	cmd2 := composeLDCmdLinkExe(instance.Platform, outputPath, vcsOPath, ccPaths, peerLibPaths, pluginPaths, globalPaths, objcopyPaths, hostBuild, wantsStrip)
	cmd3 := composeLDCmdLinkOrCopy(tools, binaryDir)
	splitDwarfCmds := composeLDSplitDwarfCmds(tools, outputPath, wantsSplitDwarf)

	// vcs_info.py and fs_tools.py only carry ARCADIA_ROOT_DISTBUILD;
	// the clang compile and link_exe.py invocations both carry the
	// full target-CC env (matches the reference cmd-level env on
	// each cmd).
	envVcsOnly := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}
	envFull := hostP.ToolEnv()

	cmds := []Cmd{
		{CmdArgs: cmd0, Env: envVcsOnly},
		{CmdArgs: cmd1, Env: envFull},
		{CmdArgs: cmd2, Cwd: "$(B)", Env: envFull},
		{CmdArgs: cmd3, Env: envVcsOnly},
	}
	cmds = append(cmds, splitDwarfCmds...)

	inputs := composeLDInputs(instance.Path, ccPaths, peerLibPaths, pluginPaths, globalPaths, objcopyPaths)

	// Append per-CC member inputs (source + headers) after the script
	// bundle, deduplicated. Matches sg.json LD shape: BUILD_ROOT block
	// + 7 scripts + UNION-of-CC-inputs.
	inputSet := map[VFS]struct{}{}
	for _, p := range inputs {
		inputSet[p] = struct{}{}
	}

	for _, pV := range memberInputs {
		if _, dup := inputSet[pV]; dup {
			continue
		}

		// Drop BUILD_ROOT-rooted codegen products (.pb.h, .pb.cc,
		// _serialized.*, ANTLR outputs) from LD inputs: BUILD_ROOT
		// entries on LD's `inputs` are .o / .a / .pyplugin only;
		// codegen artefacts wire solely through CC's own `inputs`.
		if pV.IsBuild() && isBuildRootCodegenProductRel(pV.Rel) {
			continue
		}

		inputSet[pV] = struct{}{}
		inputs = append(inputs, pV)
	}

	// svnversion.h is a c_template consumed by vcs_info.py when
	// generating __vcs_version__.c. ymake injects it as a static input
	// on every PROGRAM LD node (last position). Not seen by the CC
	// include scanner (no user source #includes it), so injected here.
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

	outputs := []VFS{outputVFS}
	if wantsSplitDwarf {
		outputs = append(outputs, Build(binPrefix+binaryName+".debug"))
	}

	n := &Node{
		Cmds:    cmds,
		Env:     envFull,
		Inputs:  inputs,
		Outputs: outputs,
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
// `binaryName` mirrors EmitLD's contract.
func LDOutputPath(instance ModuleInstance, binaryName string) string {
	if binaryName == "" {
		binaryName = lastPathComponent(instance.Path)
	}

	return Build(ldBinaryDir(instance) + "/" + binaryName).String()
}

// ldBinaryDir returns the effective BUILD_ROOT-relative directory for
// the LD node's output, vcs artifacts, and target_properties.module_dir.
// For most PROGRAMs this equals instance.Path; for bin-subdir PROGRAMs
// declaring SRCDIR(parent) the reference graph lifts all paths to the
// parent dir (ragel6, py3cc, event2cpp, rescompiler, rescompressor).
// TODO: replace with a general RECURSE-driven BinaryDir field on
// ModuleInstance.
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
func composeLDCmdVcsInfo(tools Toolchain, vcsCPath string) []string {
	return []string{
		tools.Python3,
		ldVcsInfoPath,
		"$(VCS)/vcs.json",
		vcsCPath,
		ldSvnInterfacePath,
	}
}

// composeLDCmdVcsCompile composes cmd[1]: clang compile of
// `__vcs_version__.c` → `__vcs_version__.c.o` (target, 94 args) or
// `__vcs_version__.c.pic.o` (host).
//
// Target (hostBuild=false): target triple, -march=, commonCFlags,
// warningFlags, commonDefines. `moduleCFlags` unused.
//
// Host (hostBuild=true): host triple (no -march), hostCFlags, the
// warning bundle picked by `pickWarningFlags(noCompilerWarnings)`,
// hostDefines, then moduleCFlags (carries own CFLAGS + `-D_musl_=1`
// when muslOn), then ndebugPicBlock × 2 with catboostOpenSourceDefine
// + muslConsumerSentinel + hostSseFeatures between them.
//
// The musl D-flag pair (`-D_musl_=1` and `-D_musl_`) is driven by the
// CLI's `--define MUSL=...`; when MUSL=no the sentinels collapse to
// bare double-`noLibcUndebugBlock` (target) or no muslConsumerSentinel
// between catboost and SSE (host).
func composeLDCmdVcsCompile(p *Platform, vcsCPath, vcsOPath string, muslOn bool, moduleCFlags, peerCFlagsGlobal []string, usePython3 bool, hostBuild bool, noCompilerWarnings bool) []string {
	if hostBuild {
		return composeLDCmdVcsCompileHost(p, vcsCPath, vcsOPath, muslOn, moduleCFlags, peerCFlagsGlobal, usePython3, noCompilerWarnings)
	}

	bundle := compileFlagBundleFor(p)
	cmdArgs := make([]string, 0, 94+len(peerCFlagsGlobal))
	cmdArgs = append(cmdArgs,
		p.Tools.CC,
		"--target="+p.Triple,
	)
	cmdArgs = append(cmdArgs, bundle.ArchArgs...)
	cmdArgs = append(cmdArgs,
		"-B"+binPath,
		"-c",
		"-o",
		vcsOPath,
		vcsCPath,
	)
	cmdArgs = append(cmdArgs, "-I$(S)")
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, bundle.CFlags...)
	cmdArgs = append(cmdArgs, warningFlags...)
	cmdArgs = append(cmdArgs, bundle.Defines...)

	if muslOn && !flagsContain(peerCFlagsGlobal, ldVcsMuslSelfDefine) {
		cmdArgs = append(cmdArgs, ldVcsMuslSelfDefine)
	}

	// PEERDIR-derived GLOBAL CFLAGS between musl-self sentinel and the
	// first `noLibcUndebugBlock`. Anchor: devtools/ymake/bin/ymake
	// cmd[1] ref:48..58.
	cmdArgs = append(cmdArgs, peerCFlagsGlobal...)

	cmdArgs = append(cmdArgs, bundle.NoLibcBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)

	var autoPeerCFlags []string
	if muslOn {
		autoPeerCFlags = append(autoPeerCFlags, muslConsumerSentinel)
	}
	if usePython3 {
		autoPeerCFlags = append(autoPeerCFlags, "-DUSE_PYTHON3")
	}

	cmdArgs = appendAutoPeerAndCPUFeatures(cmdArgs, bundle, autoPeerCFlags)
	cmdArgs = append(cmdArgs, bundle.NoLibcBlock...)

	return cmdArgs
}

// composeLDCmdVcsCompileHost composes the HOST variant of cmd[1]:
// x86_64 toolchain, hostCFlags, warning bundle, hostDefines, then
// `moduleCFlags` (own + `-D_musl_=1` when muslOn), then ndebugPicBlock
// twice with catboostOpenSourceDefine + muslConsumerSentinel +
// hostSseFeatures between them. Matches ragel6/yasm host LD cmd[1].
//
// Warning bundle selection (mirror of `pickWarningFlags`):
// NO_COMPILER_WARNINGS modules (vendored upstream contrib/tools)
// get the single-arg `-Wno-everything`; regular host PROGRAMs get the
// 6-arg standard bundle. Canonical composition at
// `ymake_conf.py:1550-1556` and `gnu_compiler.conf:124-140`.
func composeLDCmdVcsCompileHost(p *Platform, vcsCPath, vcsOPath string, muslOn bool, moduleCFlags, peerCFlagsGlobal []string, usePython3 bool, noCompilerWarnings bool) []string {
	cmdArgs := make([]string, 0, 94+len(moduleCFlags)+len(peerCFlagsGlobal))
	cmdArgs = append(cmdArgs,
		p.Tools.CC,
		"--target="+p.Triple,
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
	// PEERDIR-derived GLOBAL CFLAGS between moduleCFlags (terminating
	// with -D_musl_=1 when MUSL=yes) and the first ndebugPicBlock.
	// Anchor: tools/py3cc/py3cc cmd[1] ref:45..47.
	cmdArgs = append(cmdArgs, peerCFlagsGlobal...)
	cmdArgs = append(cmdArgs, ndebugPicBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)

	if muslOn {
		cmdArgs = append(cmdArgs, muslConsumerSentinel)
	}

	cmdArgs = append(cmdArgs, hostSseFeatures...)

	// USE_PYTHON3 (defaultPeerCFlags slot for host LD vcs compile)
	// between hostSseFeatures and the second ndebugPicBlock.
	// Anchor: tools/py3cc/slow/py3cc cmd[1] ref:78.
	if usePython3 {
		cmdArgs = append(cmdArgs, "-DUSE_PYTHON3")
	}

	cmdArgs = append(cmdArgs, ndebugPicBlock...)

	return cmdArgs
}

// ldVcsMuslSelfDefine is `-D_musl_=1`, injected between commonDefines
// and the first noLibcUndebugBlock when MUSL=yes. The =1 form matches
// `muslExtraDefines`'s musl-self CFLAG; the bare `-D_musl_` consumer
// sentinel lives in `muslConsumerSentinel`.
const ldVcsMuslSelfDefine = "-D_musl_=1"

// composeLDCmdLinkExe composes cmd[2]: link_exe.py invocation running
// clang++ over the assembled object/archive set. Layout:
//
//	prologue (python3 + link_exe.py)              2 args
//	--start-plugins / paths / --end-plugins       2 + len(plugins) args  (omitted if empty)
//	--clang-ver / --source-root / --build-root    6 args
//	--arch=LINUX                                  1 arg
//	--objcopy-exe / llvm-objcopy                  2 args
//	clang++                                       1 arg
//	-Wl,--whole-archive                           1 arg
//	--ya-start-command-file / globals /           1 + len(globals) + 1 args
//	--ya-end-command-file
//	-Wl,--no-whole-archive                        1 arg
//	__vcs_version__.c[.pic].o + ccPaths           1 + len(ccPaths) args
//	-o / outputPath                               2 args
//	--target / [-march] / -B/usr/bin              2-3 args (host omits -march)
//	-Wl,--start-group / peerLibs / -Wl,--end-group  1 + len(peerLibs) + 1 args
//	trailing static flags                         12 args
//
// `hostBuild` selects host triple (x86_64, no -march) and
// `ldHostStaticTrailingFlags` (no -lrt/-ldl) in place of target's
// `ldStaticMuslTrailingFlags`.
//
// `wantsStrip` controls insertion of `-Wl,--strip-all` between the
// trailer's `-lm` and `-Wl,--gc-sections`. Set true for PY3_PROGRAM_BIN
// (STRIP() in _BASE_PY3_PROGRAM, python.conf:884).
func composeLDCmdLinkExe(p *Platform, outputPath, vcsOPath string, ccPaths []VFS, peerLibPaths, pluginPaths, globalPaths []string, objcopyPaths []VFS, hostBuild, wantsStrip bool) []string {
	// Capacity hint matches the reference graph's structure plus the
	// caller-supplied slices.
	argCap := 2 + 6 + 1 + 2 + 1 + 1 + 3 + 1 + 2 + 2 + 3 + 12 + 1 + len(ccPaths) + len(peerLibPaths) + len(globalPaths) + len(objcopyPaths)

	if len(pluginPaths) > 0 {
		argCap += 2 + len(pluginPaths)
	}

	cmdArgs := make([]string, 0, argCap)

	cmdArgs = append(cmdArgs,
		p.Tools.Python3,
		ldLinkExePath,
	)

	if len(pluginPaths) > 0 {
		cmdArgs = append(cmdArgs, "--start-plugins")
		cmdArgs = append(cmdArgs, pluginPaths...)
		cmdArgs = append(cmdArgs, "--end-plugins")
	}

	cmdArgs = append(cmdArgs,
		"--clang-ver", p.ClangVer,
		"--source-root", "$(S)",
		"--build-root", "$(B)",
		"--arch=LINUX",
		"--objcopy-exe", p.Tools.Objcopy,
		p.Tools.CXX,
		"-Wl,--whole-archive",
		"--ya-start-command-file",
	)
	cmdArgs = append(cmdArgs, globalPaths...)
	cmdArgs = append(cmdArgs,
		"--ya-end-command-file",
		"-Wl,--no-whole-archive",
	)
	// SRCS_GLOBAL .o slot goes BEFORE $VCS_C_OBJ (upstream ld.conf:229-230
	// / 266-267 / 294-295). Paths emit bare (BUILD_ROOT-relative) —
	// `${rootrel;ext=.o:SRCS_GLOBAL}` strips the $(B)/ prefix; VFS.Rel
	// gives that form natively. The `inputs` slot retains the prefix.
	for _, op := range objcopyPaths {
		cmdArgs = append(cmdArgs, op.Rel)
	}

	cmdArgs = append(cmdArgs, vcsOPath)
	for _, cp := range ccPaths {
		cmdArgs = append(cmdArgs, cp.String())
	}
	cmdArgs = append(cmdArgs, "-o", outputPath)

	if hostBuild {
		cmdArgs = append(cmdArgs,
			"--target="+p.Triple,
			"-B"+binPath,
		)
	} else {
		bundle := compileFlagBundleFor(p)
		cmdArgs = append(cmdArgs, "--target="+p.Triple)
		cmdArgs = append(cmdArgs, bundle.ArchArgs...)
		cmdArgs = append(cmdArgs, "-B"+binPath)
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
	trailer = p.WithLinkerSelectionFlags(trailer)

	// When STRIP() is set on the module (PY3_PROGRAM_BIN per
	// `_BASE_PY3_PROGRAM`), splice `-Wl,--strip-all` between the
	// trailer's `-lm` and its terminating `-Wl,--gc-sections`.
	if wantsStrip {
		gcIdx := -1

		for i, arg := range trailer {
			if arg == ldGcSectionsFlag {
				gcIdx = i
			}
		}

		if gcIdx < 0 {
			ThrowFmt("composeLDCmdLinkExe: trailer must contain %q for strip insertion", ldGcSectionsFlag)
		}

		cmdArgs = append(cmdArgs, trailer[:gcIdx]...)
		cmdArgs = append(cmdArgs, ldStripAllFlag)
		cmdArgs = append(cmdArgs, trailer[gcIdx:]...)
	} else {
		cmdArgs = append(cmdArgs, trailer...)
	}

	return cmdArgs
}

// ldStripAllFlag is the linker flag injected when STRIP() is in effect
// (PY3_PROGRAM_BIN via `_BASE_PY3_PROGRAM`, python.conf:884). Upstream
// `LD_STRIP_FLAG=-Wl,--strip-all` (linkers/ld.conf:22, Linux/Android).
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
func composeLDCmdLinkOrCopy(tools Toolchain, modulePath string) []string {
	return []string{
		tools.Python3,
		ldFsToolsPath,
		"link_or_copy_to_dir",
		"--no-check",
		Build(modulePath).String(),
	}
}

func composeLDSplitDwarfCmds(tools Toolchain, outputPath string, enabled bool) []Cmd {
	if !enabled {
		return nil
	}

	debugPath := outputPath + ".debug"

	return []Cmd{
		{CmdArgs: []string{tools.Objcopy, "--only-keep-debug", outputPath, debugPath}},
		{CmdArgs: []string{tools.Strip, "--strip-debug", outputPath}},
		{CmdArgs: []string{tools.Objcopy, "--remove-section=.gnu_debuglink", "--add-gnu-debuglink", debugPath, outputPath}},
	}
}

// composeLDInputs composes the `inputs` array for an LD node. The
// BUILD_ROOT block interleaves peer-archives, plugins, globals, and
// own .o files, alphabetically sorted as one set, followed by the
// 7-script bundle in REGISTRATION ORDER (vcs_info.py, svn_interface.c,
// link_exe.py, thinlto_cache.py, process_command_files.py,
// process_whole_archive_option.py, fs_tools.py). Caller appends
// member-CC inputs afterwards.
//
// `__vcs_version__.c.o` / `__vcs_version__.c` are NOT in inputs:
// produced by cmd[0]/cmd[1] inside the same node (implicit deps).
//
// Reference tools/archiver: 35 BUILD_ROOT entries (32 peer .a + 1
// plugin + 1 global + 1 own main.cpp.o).
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
	// objcopy `.o` paths arrive $(B)-rooted; they belong in the
	// BUILD_ROOT block alongside own .cpp.o and peer .a entries.
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
// Encodes static-musl Linux: no PIE, no dynamic linker, hand-rolled
// libc/libdl/libm linkage, explicit section gc.
//
// `-nostdlib` appears TWICE; the duplication is part of the reference
// output — do not deduplicate.
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
// Differs from `ldStaticMuslTrailingFlags`: `-lrt`/`-ldl` absent
// (host binaries skip glibc-compat shims), replaced by two `-fPIC`
// flags satisfying the static-PIC link invariant.
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

// ldHostStaticRTTrailingFlags is the 14-flag trailer for host PROGRAM
// LD cmd[2] when the binary links `contrib/libs/libc_compat` (and
// typically `contrib/libs/linuxvdso`). Differs from
// `ldHostStaticTrailingFlags` by the inserted `-lrt`/`-ldl` pair
// between `--no-dynamic-linker` and the first `-nostdlib`.
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
// the `-lrt`/`-ldl` host LD trailer. Host PROGRAMs that PEERDIR
// `contrib/libs/libc_compat` (archiver, protoc, py3cc, stage0pycc, ...)
// pull in glibc-compat shims requiring `-lrt`/`-ldl`; ragel6/yasm omit.
const libcCompatPeerPath = "contrib/libs/libc_compat/libcontrib-libs-libc_compat.a"

// ldScriptInputs is the 7-script bundle appended at the tail of every
// LD node's `inputs` array, in the NON-ALPHABETICAL registration order
// observed in the reference graph. Order is load-bearing for byte-exact
// `inputs` matching.
var ldScriptInputs = []VFS{
	Source("build/scripts/vcs_info.py"),
	Source("build/scripts/c_templates/svn_interface.c"),
	Source("build/scripts/link_exe.py"),
	Source("build/scripts/thinlto_cache.py"),
	Source("build/scripts/process_command_files.py"),
	Source("build/scripts/process_whole_archive_option.py"),
	Source("build/scripts/fs_tools.py"),
}

// LD-script VFS constants. cmd_args use the cached .String() form
// (`…Path` shim) and stitch the same VFS into the inputs slot.
// ldSvnversionHVFS is input-only; the rest flow into both.
var (
	ldVcsInfoVFS      = Source("build/scripts/vcs_info.py")
	ldSvnInterfaceVFS = Source("build/scripts/c_templates/svn_interface.c")
	ldLinkExeVFS      = Source("build/scripts/link_exe.py")
	ldFsToolsVFS      = Source("build/scripts/fs_tools.py")
	ldSvnversionHVFS  = Source("build/scripts/c_templates/svnversion.h")

	ldVcsInfoPath      = ldVcsInfoVFS.String()
	ldSvnInterfacePath = ldSvnInterfaceVFS.String()
	ldLinkExePath      = ldLinkExeVFS.String()
	ldFsToolsPath      = ldFsToolsVFS.String()
)
