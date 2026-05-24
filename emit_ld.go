package main

// ld.go — emitter for LD (link executable) nodes.
//
// One Node, four Cmds: vcs_info.py, clang compile of __vcs_version__.c,
// link_exe.py (carries `cwd: $(B)` because command-file paths are
// BUILD_ROOT-relative), fs_tools.py link_or_copy_to_dir.
//
// inputs[] = BUILD_ROOT block (peer archives, plugins, globals, own .o,
// objcopy .o), then the 7-script bundle, then the svnversion.h c_template.
// No member-CC source/header closure: the link command reads only the
// objects/archives it bundles plus its own scripts. Node-input order is
// normalized away (the gate sorts inputs), so the block is built in
// first-occurrence order, not sorted. __vcs_version__.c{,.o} are produced
// in-node and not listed.

// EmitLD emits the 4-cmd LD node for a PROGRAM module.
//
// `binaryName` is PROGRAM(name)'s parsed argument; when empty falls back
// to lastPathComponent(instance.Path). Divergent case: ragel6 lives in
// contrib/tools/ragel6/bin/ya.make but declares PROGRAM(ragel6).
//
// `ccPaths` order is load-bearing for cmd[2] argv (between whole-archive
// block and `-o`). `peerLDRefs`/`peerLibPaths` arrive in PEERDIR walk
// order (non-alphabetical). `peerLinkCmdPaths` preserves the combined
// static/dynamic discovery order for the `--start-group ... --end-group`
// block. Pass nil for plugin/global slices when empty.
//
// Output path: $(B)/ldBinaryDir(instance)/binaryName; LDOutputPath
// re-derives it for callers.
func EmitLD(
	instance ModuleInstance,
	binaryName string,
	ccRefs []NodeRef,
	ccPaths []VFS,
	peerLDRefs []NodeRef,
	peerLibPaths []VFS,
	peerLinkCmdPaths []VFS,
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
	moduleCFlags []string,
	peerCFlagsGlobal []string,
	autoPeerCFlags []string,
	peerLDFlagsGlobal []string,
	ownLDFlags []string,
	ownRPathFlags []string,
	peerRPathFlagsGlobal []string,
	objAddLibsGlobal []string,
	noCompilerWarnings bool,
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
	cmd1 := composeLDCmdVcsCompile(instance.Platform, vcsCPath, vcsOPath, moduleCFlags, peerCFlagsGlobal, autoPeerCFlags, noCompilerWarnings)
	cmd2 := composeLDCmdLinkExe(instance.Platform, outputPath, vcsOPath, ccPaths, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths, dynamicPaths, objcopyPaths, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal, wantsStrip)
	cmd3 := composeLDCmdLinkOrCopy(tools, binaryDir, dynamicPaths...)
	splitDwarfCmds := composeLDSplitDwarfCmds(tools, outputPath, wantsSplitDwarf)

	// vcs_info.py, fs_tools.py, and the split-dwarf tail only carry
	// ARCADIA_ROOT_DISTBUILD; the clang compile and link_exe.py
	// invocations both carry the full target-CC env (matches the
	// reference cmd-level env on each cmd).
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
	for i := range splitDwarfCmds {
		splitDwarfCmds[i].Env = envVcsOnly
	}
	cmds = append(cmds, splitDwarfCmds...)

	inputs := composeLDInputs(instance.Path, ccPaths, peerLibPaths, pluginPaths, globalPaths, wholeArchivePaths, dynamicPaths, objcopyPaths)

	// svnversion.h is a c_template consumed by vcs_info.py (cmd[0]) when
	// generating __vcs_version__.c. ymake injects it as a static input on
	// every PROGRAM LD node (last position). It is never produced by the
	// link nor part of composeLDInputs, so it is appended here.
	inputs = append(inputs, ldSvnversionHVFS)

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
		KV: map[string]interface{}{
			"p":        "LD",
			"pc":       "light-blue",
			"show_out": "yes",
		},
		Platform: string(instance.Platform.Target),
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

	// PY3_PROGRAM LDs carry module_tag=py3_bin (mirror of _BASE_PY3_PROGRAM
	// in python.conf). PY3_PROGRAM_BIN (wantsStrip) and C++ PROGRAMs emit
	// no module_tag.
	if instance.Language == LangPy && !wantsStrip {
		n.TargetProperties["module_tag"] = "py3_bin"
	}

	return emit.Emit(bindNodePlatform(n, instance.Platform))
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

	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, pickWarningFlags(noCompilerWarnings, false), bundle.Defines, preNoLibcExtras, autoPeerCFlags)

	return cmdArgs
}

// composeLDCmdLinkExe composes cmd[2]: link_exe.py wrapping clang++ over
// the assembled object/archive set.
//
// `wantsStrip` inserts `-Wl,--strip-all` between the trailer's `-lm`
// and `-Wl,--gc-sections` (set true for PY3_PROGRAM_BIN via STRIP() in
// _BASE_PY3_PROGRAM, python.conf:884).
func composeLDCmdLinkExe(p *Platform, outputPath, vcsOPath string, ccPaths []VFS, peerLinkCmdPaths, pluginPaths, globalPaths, wholeArchivePaths, wholeArchiveCmdPaths, dynamicPaths []VFS, objcopyPaths []VFS, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal []string, wantsStrip bool) []string {
	// Capacity hint matches the reference graph's structure plus the
	// caller-supplied slices.
	argCap := 2 + 6 + 1 + 2 + 1 + 1 + 3 + 1 + 2 + 2 + 3 + 16 + 1 + len(ccPaths) + len(peerLinkCmdPaths) + len(globalPaths) + len(objcopyPaths) + len(peerLDFlagsGlobal) + len(ownLDFlags) + len(ownRPathFlags) + len(peerRPathFlagsGlobal) + len(objAddLibsGlobal)

	argCap += 2 + len(pluginPaths)

	cmdArgs := make([]string, 0, argCap)

	cmdArgs = append(cmdArgs,
		p.Tools.Python3,
		ldLinkExePath,
	)

	cmdArgs = append(cmdArgs, "--start-plugins")
	for _, p := range pluginPaths {
		cmdArgs = append(cmdArgs, p.String())
	}
	cmdArgs = append(cmdArgs, "--end-plugins")

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
	for _, p := range peerLinkCmdPaths {
		cmdArgs = append(cmdArgs, p.Rel)
	}
	cmdArgs = append(cmdArgs, "-Wl,--end-group")

	cmdArgs = append(cmdArgs, composeProgramLinkTrailer(p, dynamicPaths, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal, wantsStrip)...)

	return cmdArgs
}

func composeProgramLinkTrailer(p *Platform, dynamicPaths []VFS, peerLDFlagsGlobal, ownLDFlags, ownRPathFlags, peerRPathFlagsGlobal, objAddLibsGlobal []string, wantsStrip bool) []string {
	linkPrelude := []string{"-rdynamic"}
	if p != nil && !p.PIC && p.Flags["SANDBOXING"] == "yes" {
		linkPrelude = append(linkPrelude, "-Wl,--compress-debug-sections=zstd")
	}
	systemLibs := []string{"-nostdlib", "-lm"}
	if p == nil || p.Flags["MUSL"] != "yes" {
		linkPrelude = append(linkPrelude, "-ldl", "-lrt")
		systemLibs = []string{"-nodefaultlibs", "-lpthread", "-lc", "-lm"}
	}
	linkPrelude = append(linkPrelude, "-Wl,--no-as-needed")

	if len(ownRPathFlags) == 0 && len(peerRPathFlagsGlobal) == 0 {
		trailer := append([]string(nil), linkPrelude...)
		if p.PIC {
			trailer = append(trailer, "-fPIC", "-fPIC")
		}
		trailer = append(trailer, peerLDFlagsGlobal...)
		trailer = append(trailer, ownLDFlags...)
		trailer = append(trailer, objAddLibsGlobal...)
		trailer = append(trailer, systemLibs...)
		if wantsStrip {
			trailer = append(trailer, "-Wl,--strip-all")
		}
		trailer = append(trailer, "-Wl,--gc-sections")

		return p.WithLinkerSelectionFlags(trailer)
	}
	_ = dynamicPaths

	trailer := append([]string(nil), linkPrelude...)
	trailer = append(trailer, ownRPathFlags...)
	if p.PIC {
		trailer = append(trailer, "-fPIC")
	}
	trailer = append(trailer, p.LinkerSelectionGDBIndexFlags()...)
	trailer = append(trailer, peerRPathFlagsGlobal...)
	if p.PIC {
		trailer = append(trailer, "-fPIC")
	}
	trailer = append(trailer, p.LinkerSelectionTailFlags()...)
	trailer = append(trailer, peerLDFlagsGlobal...)
	trailer = append(trailer, ownLDFlags...)
	trailer = append(trailer, objAddLibsGlobal...)
	trailer = append(trailer, systemLibs...)
	if wantsStrip {
		trailer = append(trailer, "-Wl,--strip-all")
	}
	trailer = append(trailer, "-Wl,--gc-sections")
	trailer = append(trailer, p.LinkerSelectionNoPieFlags()...)

	return trailer
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

// composeLDInputs composes the `inputs` array for an LD node: the BUILD_ROOT
// block (peer archives, plugins, globals, own .o, objcopy .o) followed by
// ldScriptInputs in registration order. Caller appends svnversion.h
// afterwards. Node-input order is normalized away. __vcs_version__.c{,.o}
// are produced by cmd[0]/cmd[1] in-node and excluded.
func composeLDInputs(modulePath string, ccPaths []VFS, peerLibPaths []VFS, pluginPaths []VFS, globalPaths []VFS, wholeArchivePaths []VFS, dynamicPaths []VFS, objcopyPaths []VFS) []VFS {
	// Lift every build-root participant into one VFS-typed block in
	// first-occurrence order. Keep the first occurrence when multiple
	// categories contribute the same BUILD_ROOT path.
	buildRootBlock := make([]VFS, 0, len(peerLibPaths)+len(pluginPaths)+len(globalPaths)+len(wholeArchivePaths)+len(dynamicPaths)+len(ccPaths)+len(objcopyPaths))
	buildRootSeen := make(map[VFS]struct{}, cap(buildRootBlock))

	appendBuildRoot := func(paths []VFS) {
		for _, p := range paths {
			if _, dup := buildRootSeen[p]; dup {
				continue
			}

			buildRootSeen[p] = struct{}{}
			buildRootBlock = append(buildRootBlock, p)
		}
	}

	appendBuildRoot(peerLibPaths)
	appendBuildRoot(pluginPaths)
	appendBuildRoot(globalPaths)
	appendBuildRoot(wholeArchivePaths)
	appendBuildRoot(dynamicPaths)
	appendBuildRoot(ccPaths)
	// objcopy `.o` paths arrive $(B)-rooted; they belong in the
	// BUILD_ROOT block alongside own .cpp.o and peer .a entries.
	appendBuildRoot(objcopyPaths)

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
