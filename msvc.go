package main

import "strings"

var (
	msvcARKV = KV{P: pkAR, PC: pcLightRed, Soe: true}
	msvcCCKV = KV{P: pkCC, PC: pcYellow, Soe: true}
	msvcLDKV = KV{P: pkLD, PC: pcBlue, Soe: true}
)

type MSVCTools struct {
	CXX      ANY
	C        ANY
	Lib      ANY
	Link     ANY
	LibFlags []ANY
}

func newMSVCTools(env Environment) *MSVCTools {
	tool := func(name string) ANY {
		return internAny(strings.Trim(strings.TrimSpace(env.expand("$"+name)), `"`))
	}

	return &MSVCTools{
		CXX:      tool("CXX_COMPILER_UNQUOTED"),
		C:        tool("C_COMPILER_UNQUOTED"),
		Lib:      tool("_MSVC_LIB"),
		Link:     tool("_MSVC_LINK"),
		LibFlags: msvcConfWords(env, "$LINK_LIB_FLAGS"),
	}
}

func buildRootRel(v VFS) ANY {
	return v.rel().any()
}

func composeMSVCLibNode(instance ModuleInstance, archivePath VFS, objRefs []NodeRef, objPaths []VFS, tc ModuleToolchain, na *NodeArenas) NodeSpec {
	tools := instance.Platform.MSVCTools
	args := na.anys.alloc(8 + len(objPaths) + len(tools.LibFlags))[:0]

	args = append(args, tc.Python3, buildScriptsFixMsvcOutputPy.any(), strLib.any(), tools.Lib,
		internV("/OUT:", archivePath.relString()).any(), argYaStartCommandFile.any())

	for _, p := range objPaths {
		args = append(args, buildRootRel(p))
	}

	args = append(args, tools.LibFlags...)
	args = append(args, strYaEndCommandFile.any())
	na.anys.commit(len(args))

	return NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds:      na.cmdList(Cmd{CmdArgs: na.chunkList(args[:len(args):len(args)]), Cwd: bldRootDirVFS.cwd(), Env: envVarsVCS}),
		Env:       envVarsVCS,
		Inputs:    na.rootInputList(na.vfsList(objPaths...), na.vfsList(buildScriptsFixMsvcOutputPy, buildScriptsProcessCommandFilesPy)),
		KV:        &msvcARKV,
		Outputs:   na.vfsList(archivePath),
		DepRefs:   na.refList(na.objectBuildRefs(objRefs, objPaths)...),
		Resources: instance.Platform.UsesPython3Clang},
	}
}

func msvcIncludeArgs(na *NodeArenas, includes []ANY) []ANY {
	out := na.anys.alloc(2 * len(includes))[:0]

	for _, include := range includes {
		dir, _ := strings.CutPrefix(include.string(), "-I")

		out = append(out, strSlashI.any(), internAny(dir))
	}

	na.anys.commit(len(out))

	return out[:len(out):len(out)]
}

func msvcCompilerFlags(in *ModuleCompileEnv, cxx bool) ([]ANY, []STR) {
	text := "$CFLAGS $CONLYFLAGS"

	if cxx {
		text = "$CXXFLAGS"
	}

	return msvcModuleFlags(in, text)
}

func msvcModuleFlags(in *ModuleCompileEnv, text string) ([]ANY, []STR) {
	var uses []STR

	env := in.CompilerEnv

	env.resourceUses = &uses

	user := []struct {
		name string
		args []ANY
	}{
		{"USER_CFLAGS", in.CFlags},
		{"USER_CFLAGS_GLOBAL", concat(in.PeerCFlagsGlobal, in.OwnCFlagsGlobal)},
		{"USER_CXXFLAGS", in.CXXFlags},
		{"USER_CXXFLAGS_GLOBAL", composeOwnAndPeerGlobalBucket(in)},
		{"USER_CONLYFLAGS", in.COnlyFlags},
		{"USER_CONLYFLAGS_GLOBAL", concat(in.PeerCOnlyFlagsGlobal, in.OwnCOnlyFlagsGlobal)},
	}

	for i, u := range user {
		env.setString(internEnv(u.name), msvcPlaceholder(i))
	}

	var out []ANY

	for _, word := range splitShellWords(env.expand(text)) {
		switch {
		case strings.HasPrefix(word, "${"):
		case strings.HasPrefix(word, "\x01"):
			i := int(word[len(word)-1] - '0')

			out = append(out, user[i].args...)
		default:
			out = append(out, internAny(word))
		}
	}

	return out, uses
}

func msvcPlaceholder(i int) string {
	return "\x01" + string(rune('0'+i))
}

func msvcCompileArgs(na *NodeArenas, tools *MSVCTools, blocks *CcModuleArgBlocks, cxx bool, src, out VFS, perSource, langFlags []ANY) ArgChunks {
	compiler := tools.C

	if cxx {
		compiler = tools.CXX
	}

	head := na.anyList(compiler, strSlashC.any(), internV("/Fo", out.string()).any(), src.any())

	return na.chunkList(head, blocks.extraCFlags, blocks.msvcIncludes, blocks.msvcFlags, perSource, langFlags)
}

func msvcConfWords(env Environment, text string, locals ...confMacroBinding) []ANY {
	var out []ANY

	constant := true

	for _, word := range splitShellWords(substConfMacroArgs(text, locals, env, &constant)) {
		out = append(out, internAny(word))
	}

	return out
}

func (e *EmitContext) emitMSVCLink(instance ModuleInstance, binaryName string, ccRefs []NodeRef, ccPaths []VFS, peerLDRefs []NodeRef, peerLibPaths, peerLinkCmdPaths []VFS, globalRefs []NodeRef, globalPaths, wholeArchiveCmdPaths []VFS, ldFlags []ANY, tc ModuleToolchain, emit *StreamingEmitter, vcsRef NodeRef) NodeRef {
	na := emit.nodeArenas()
	tools := instance.Platform.MSVCTools
	env := Environment{s: &e.frame.ifEnv}
	binDir := instance.Path.relString()
	output := build(binDir, "/", binaryName)
	vcsC := build(binDir, "/__vcs_version__.c")
	vcsO := build(binDir, "/__vcs_version__.c", instance.Platform.objectSuffix())

	var vcsResources []STR

	vcsEnv := env

	vcsEnv.resourceUses = &vcsResources

	cFlags, cFlagUses := msvcModuleFlags(&e.d.cc, "$CFLAGS")
	vcsCompile := na.anyConcat([]ANY{tools.C, strSlashC.any(), internV("/Fo", vcsO.string()).any(), vcsC.any()}, compilerExtraCFlags(vcsEnv), []ANY{strSlashI.any(), srcRootDirVFS.any()}, cFlags)
	vfsCmd := na.anyConcat([]ANY{tc.Python3, buildScriptsGenerateWinVfsPy.any(), build(binDir).any()}, msvcConfWords(env, "$_FLAGS_COMMON_LIBPATHS"))
	plugins := e.globalPlugins()
	localLD := e.localLinkerFlagValue(nil)
	outputs := []VFS{output}
	link := []ANY{tc.Python3, buildScriptsFixMsvcOutputPy.any(), strLink.any(), tools.Link, internV("/OUT:", output.relString()).any()}

	for _, p := range wholeArchiveCmdPaths {
		link = append(link, argWholeArchiveLibs.any(), p.rel().any())
	}

	if !stringIsTruthy(env.string(envNO_DEBUGINFO)) {
		pdb := build(binDir, "/", strings.TrimSuffix(binaryName, instance.Platform.programSuffix()), ".pdb")

		outputs = append(outputs, pdb)
		link = append(link, internV("/PDB:", pdb.relString()).any())
	}

	link = append(link, argStartPlugins.any())

	for _, p := range plugins {
		link = append(link, p.any())
	}

	link = append(link, argEndPlugins.any(), strStartWa.any(), argYaStartCommandFile.any())

	for _, p := range globalPaths {
		if !isGlobalObject(p) {
			link = append(link, p.rel().any())
		}
	}

	link = append(link, strYaEndCommandFile.any(), strEndWa.any(), argYaStartCommandFile.any())

	for _, p := range globalPaths {
		if isGlobalObject(p) {
			link = append(link, p.rel().any())
		}
	}

	link = append(link, strYaEndCommandFile.any(), argYaStartCommandFile.any(), vcsO.rel().any())

	for _, p := range ccPaths {
		link = append(link, p.rel().any())
	}

	link = append(link, msvcConfWords(env, "$LINK_EXE_FLAGS $LINK_STDLIBS", confMacroBinding{name: envBINDIR.str(), values: []ANY{build(binDir).any()}, bound: true})...)
	link = append(link, localLD.Args...)
	link = append(link, ldFlags...)

	for _, p := range peerLinkCmdPaths {
		link = append(link, p.rel().any())
	}

	link = append(link, strYaEndCommandFile.any())

	cmds := na.cmdList(
		Cmd{CmdArgs: na.chunkList(composeLDCmdVcsInfo(na, tc, vcsC)), Env: envVarsVCS},
		Cmd{CmdArgs: na.chunkList(vcsCompile), Env: envVarsVCS, Resources: resourceKeyUnion(vcsResources, cFlagUses)},
		Cmd{CmdArgs: na.chunkList(vfsCmd), Env: envVarsVCS},
		Cmd{CmdArgs: na.chunkList(na.anyList(link...)), Cwd: bldRootDirVFS.cwd(), Env: envVarsVCS, Resources: localLD.Resources},
	)

	scripts := na.vfsList(ldSvnInterfaceVFS, ldSvnversionHVFS, ldVcsInfoVFS, buildScriptsFixMsvcOutputPy, buildScriptsGenerateWinVfsPy, buildScriptsProcessCommandFilesPy)
	deps := na.refList(concat(na.objectBuildRefs(ccRefs, ccPaths), na.objectBuildRefs(globalRefs, globalPaths), peerLDRefs, []NodeRef{vcsRef})...)

	n := NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds:      cmds,
		Env:       envVarsVCS,
		Inputs:    na.rootInputList(na.vfsList(concat(peerLibPaths, plugins, globalPaths, ccPaths)...), scripts, localLD.Inputs),
		Outputs:   na.vfsList(outputs...),
		KV:        &msvcLDKV,
		DepRefs:   deps,
		Resources: instance.Platform.UsesPython3Clang},
	}

	e.appendSdcMeta(&n, binaryName)

	return e.emitModuleNode(n, true)
}
