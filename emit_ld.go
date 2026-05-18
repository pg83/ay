package main

// ld.go — emitter for LD (link executable) nodes.
//
// An LD node has ONE Node with FOUR Cmds:
//   cmd[0]: vcs_info.py — generates `__vcs_version__.c` (5 args).
//   cmd[1]: clang compile of `__vcs_version__.c` → `__vcs_version__.c.o`.
//           Same shape as CC but with single `-I$(S)`.
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
//     walk order (non-alphabetical). peerLDRefs wire DepRefs.
//   - `pluginRefs` / `pluginPaths`: plugins for `--start-plugins ...
//     --end-plugins` (e.g. musl pyplugin). Pass nil when none.
//   - `globalRefs` / `globalPaths`: peer `.global.a` archives wrapped
//     in `-Wl,--whole-archive ... -Wl,--no-whole-archive`. Pass nil
//     when none.
//
// Returns the LD NodeRef. Output path is `$(B)/<instance.Path>/<binaryName>`;
// callers can re-derive via `LDOutputPath(instance, binaryName)`.
func EmitLD(
	instance ModuleInstance,
	binaryName string,
	ccRefs []NodeRef,
	ccPaths []VFS,
	peerLDRefs []NodeRef,
	peerLibPaths []VFS,
	pluginRefs []NodeRef,
	pluginPaths []VFS,
	globalRefs []NodeRef,
	globalPaths []VFS,
	wholeArchiveRefs []NodeRef,
	wholeArchivePaths []VFS,
	wholeArchiveCmdPaths []VFS,
	dynamicRefs []NodeRef,
	dynamicPaths []VFS,
	objcopyRefs []NodeRef,
	objcopyPaths []VFS,
	memberInputs []VFS, //nolint:vfs-stay // cascade from gen.go member-inputs bucket
	moduleCFlags []string,
	peerCFlagsGlobal []string,
	autoPeerCFlags []string,
	peerLDFlagsGlobal []string,
	objAddLibsGlobal []string,
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
	if len(wholeArchiveRefs) != len(wholeArchivePaths) {
		ThrowFmt("EmitLD: wholeArchiveRefs/wholeArchivePaths length mismatch (%d vs %d)", len(wholeArchiveRefs), len(wholeArchivePaths))
	}

	if len(objcopyRefs) != len(objcopyPaths) {
		ThrowFmt("EmitLD: objcopyRefs/objcopyPaths length mismatch (%d vs %d)", len(objcopyRefs), len(objcopyPaths))
	}
	if len(dynamicRefs) != len(dynamicPaths) {
		ThrowFmt("EmitLD: dynamicRefs/dynamicPaths length mismatch (%d vs %d)", len(dynamicRefs), len(dynamicPaths))
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

	binPrefix := binaryDir + "/"
	outputVFS := Build(binPrefix + binaryName)
	vcsCVFS := Build(binPrefix + "__vcs_version__.c")
	vcsOVFS := Build(binPrefix + "__vcs_version__.c" + instance.Platform.ObjectSuffix())

	// Pre-materialise the three .String() forms — vcsCVFS / vcsOVFS
	// flow into multiple cmd composers; .String() them once each.
	vcsCPath := vcsCVFS.String()
	vcsOPath := vcsOVFS.String()
	outputPath := outputVFS.String()

	tools := instance.Platform.Tools
	cmd0 := composeLDCmdVcsInfo(tools, vcsCPath)
	cmd1 := composeLDCmdVcsCompile(instance.Platform, vcsCPath, vcsOPath, moduleCFlags, peerCFlagsGlobal, autoPeerCFlags, instance.Flags.NoCompilerWarnings)
	cmd2 := composeLDCmdLinkExe(instance.Platform, outputPath, vcsOPath, ccPaths, peerLibPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths, dynamicPaths, objcopyPaths, peerLDFlagsGlobal, objAddLibsGlobal, wantsStrip)
	cmd3 := composeLDCmdLinkOrCopy(tools, binaryDir, dynamicPaths...)
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

	inputs := composeLDInputs(instance.Path, ccPaths, peerLibPaths, pluginPaths, globalPaths, wholeArchivePaths, dynamicPaths, objcopyPaths)

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
	depRefs := make([]NodeRef, 0, len(ccRefs)+len(pluginRefs)+len(globalRefs)+len(peerLDRefs)+len(dynamicRefs)+len(objcopyRefs))
	depRefs = append(depRefs, ccRefs...)
	depRefs = append(depRefs, pluginRefs...)
	depRefs = append(depRefs, globalRefs...)
	depRefs = append(depRefs, wholeArchiveRefs...)
	depRefs = append(depRefs, peerLDRefs...)
	depRefs = append(depRefs, dynamicRefs...)
	depRefs = append(depRefs, objcopyRefs...)

	outputs := []VFS{outputVFS}
	for _, p := range dynamicPaths {
		outputs = append(outputs, Build(binaryDir+"/"+lastPathComponent(p.Rel)))
	}
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
func LDOutputPath(instance ModuleInstance, binaryName string) VFS {
	if binaryName == "" {
		binaryName = lastPathComponent(instance.Path)
	}

	return Build(ldBinaryDir(instance) + "/" + binaryName)
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
// `__vcs_version__.c` → `__vcs_version__.c.o`.
//
// Mirrors upstream `_SRC_C_NODEPS_CMD` (gnu_compiler.conf:328): the
// vcs compile is a regular C-compile that threads the per-module
// auto-peer CFLAGS. `autoPeerCFlags` is the pre-resolved slice
// produced by `defaultPeerCFlags` upstream of the caller; this
// composer is agnostic about its contents.
func composeLDCmdVcsCompile(p *Platform, vcsCPath, vcsOPath string, moduleCFlags, peerCFlagsGlobal, autoPeerCFlags []string, noCompilerWarnings bool) []string {
	bundle := compileFlagBundleFor(p)
	cmdArgs := make([]string, 0, 94+len(moduleCFlags)+len(peerCFlagsGlobal))
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

	// $CFLAGS in _SRC_C_NODEPS_CMD (gnu_compiler.conf:328) is the
	// per-module CFLAGS accumulator: module's own CFLAGS() first, then
	// peer-propagated GLOBAL CFLAGS.
	preNoLibcExtras := make([]string, 0, len(moduleCFlags)+len(peerCFlagsGlobal))
	preNoLibcExtras = append(preNoLibcExtras, moduleCFlags...)
	preNoLibcExtras = append(preNoLibcExtras, peerCFlagsGlobal...)

	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, pickWarningFlags(noCompilerWarnings), bundle.Defines, preNoLibcExtras, autoPeerCFlags)

	return cmdArgs
}

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
//	__vcs_version__.c.o + ccPaths                 1 + len(ccPaths) args
//	-o / outputPath                               2 args
//	--target / -march / -B/usr/bin                3 args
//	-Wl,--start-group / peerLibs / -Wl,--end-group  1 + len(peerLibs) + 1 args
//	trailing static flags                         12 args
//
// `wantsStrip` controls insertion of `-Wl,--strip-all` between the
// trailer's `-lm` and `-Wl,--gc-sections`. Set true for PY3_PROGRAM_BIN
// (STRIP() in _BASE_PY3_PROGRAM, python.conf:884).
func composeLDCmdLinkExe(p *Platform, outputPath, vcsOPath string, ccPaths []VFS, peerLibPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths, dynamicPaths []VFS, objcopyPaths []VFS, peerLDFlagsGlobal, objAddLibsGlobal []string, wantsStrip bool) []string {
	// Capacity hint matches the reference graph's structure plus the
	// caller-supplied slices.
	argCap := 2 + 6 + 1 + 2 + 1 + 1 + 3 + 1 + 2 + 2 + 3 + 12 + 1 + len(ccPaths) + len(peerLibPaths) + len(dynamicPaths) + len(globalPaths) + len(objcopyPaths)

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
		for _, p := range pluginPaths {
			cmdArgs = append(cmdArgs, p.String())
		}
		cmdArgs = append(cmdArgs, "--end-plugins")
	}

	cmdArgs = append(cmdArgs,
		"--clang-ver", p.ClangVer,
		"--source-root", "$(S)",
		"--build-root", "$(B)",
	)
	for _, p := range wholeArchiveCmdPaths {
		cmdArgs = append(cmdArgs, "--whole-archive-libs", p.Rel)
	}
	for _, p := range wholeArchivePaths {
		cmdArgs = append(cmdArgs, "--whole-archive-libs", p.Rel)
	}
	cmdArgs = append(cmdArgs,
		"--arch=LINUX",
		"--objcopy-exe", p.Tools.Objcopy,
		p.Tools.CXX,
		"-Wl,--whole-archive",
		"--ya-start-command-file",
	)
	for _, p := range globalPaths {
		cmdArgs = append(cmdArgs, p.Rel)
	}
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

	bundle := compileFlagBundleFor(p)
	cmdArgs = append(cmdArgs, "--target="+p.Triple)
	cmdArgs = append(cmdArgs, bundle.ArchArgs...)
	cmdArgs = append(cmdArgs, "-B"+binPath)

	cmdArgs = append(cmdArgs, "-Wl,--start-group")
	for _, p := range peerLibPaths {
		cmdArgs = append(cmdArgs, p.Rel)
	}
	for _, p := range dynamicPaths {
		cmdArgs = append(cmdArgs, p.Rel)
	}
	cmdArgs = append(cmdArgs, "-Wl,--end-group")

	// _EXE_FLAGS layout after `-Wl,--end-group` per build/conf/linkers/
	// ld.conf:158-179.

	// EXPORTS_VALUE (ld.conf:130-132): OS_LINUX → -rdynamic.
	trailer := []string{"-rdynamic"}

	// LDFLAGS = USER_LDFLAGS + _LD_FLAGS (ld.conf:1). _LD_FLAGS from
	// ymake_conf.py:1803-1812: MUSL=yes Linux → ["-Wl,--no-as-needed"].
	// Plus -fPIC pair from gnu_compiler.conf:43-46 when PIC=yes
	// (`CFLAGS+=-fPIC; LDFLAGS+=-fPIC` both surface in clang++ link cmd).
	trailer = append(trailer, "-Wl,--no-as-needed")
	if p.PIC {
		trailer = append(trailer, "-fPIC", "-fPIC")
	}

	// LDFLAGS_GLOBAL (ld.conf:168): peer-aggregated `LDFLAGS()`.
	// musl/ya.make contributes -static, -Wl,--no-dynamic-linker.
	trailer = append(trailer, peerLDFlagsGlobal...)

	// OBJADDE_LIB_GLOBAL (ld.conf:171): peer-aggregated `EXTRALIBS()`.
	// util/ya.make contributes -lrt -ldl (OS_LINUX); musl/ya.make
	// contributes -nostdlib -fno-pie -Wl,-no-pie.
	trailer = append(trailer, objAddLibsGlobal...)

	// C_SYSTEM_LIBRARIES (ld.conf:120): MUSL=yes → -nostdlib. Plus
	// -lm appended in ymake.core.conf:942 (USE_ARCADIA_LIBM=no).
	trailer = append(trailer, "-nostdlib", "-lm")

	// STRIP_FLAG (ld.conf:22): emitted before DCE_FLAG when STRIP() in
	// effect (PY3_PROGRAM_BIN via _BASE_PY3_PROGRAM, python.conf:884).
	if wantsStrip {
		trailer = append(trailer, "-Wl,--strip-all")
	}

	// DCE_FLAG (ld.conf:46): OS_LINUX → -Wl,--gc-sections.
	trailer = append(trailer, "-Wl,--gc-sections")

	// ICF_FLAG and _LD_NO_PIE_FLAG (ld.conf:73-78) are LLD-specific;
	// WithLinkerSelectionFlags adds them at the canonical slots
	// (prefix-LLD ICF + trailing -Wl,-no-pie when !PIC).
	trailer = p.WithLinkerSelectionFlags(trailer)

	cmdArgs = append(cmdArgs, trailer...)

	return cmdArgs
}

// composeLDCmdLinkOrCopy composes cmd[3]: invokes fs_tools.py
// `link_or_copy_to_dir` to drop the linked binary into its containing
// directory. 5 args, fixed.
func composeLDCmdLinkOrCopy(tools Toolchain, modulePath string, dynamicPaths ...VFS) []string {
	cmd := []string{
		tools.Python3,
		ldFsToolsPath,
		"link_or_copy_to_dir",
		"--no-check",
	}
	for _, p := range dynamicPaths {
		cmd = append(cmd, p.String())
	}
	cmd = append(cmd, Build(modulePath).String())

	return cmd
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
func composeLDInputs(modulePath string, ccPaths []VFS, peerLibPaths []VFS, pluginPaths []VFS, globalPaths []VFS, wholeArchivePaths []VFS, dynamicPaths []VFS, objcopyPaths []VFS) []VFS {
	// Lift every build-root participant into one VFS-typed block before
	// alphabetising.
	buildRootBlock := make([]VFS, 0, len(peerLibPaths)+len(pluginPaths)+len(globalPaths)+len(wholeArchivePaths)+len(dynamicPaths)+len(ccPaths)+len(objcopyPaths))

	buildRootBlock = append(buildRootBlock, peerLibPaths...)
	buildRootBlock = append(buildRootBlock, pluginPaths...)
	buildRootBlock = append(buildRootBlock, globalPaths...)
	buildRootBlock = append(buildRootBlock, wholeArchivePaths...)
	buildRootBlock = append(buildRootBlock, dynamicPaths...)
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
