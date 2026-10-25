package main

import (
	"slices"
)

var (
	ldLinkDynLibPath = ldLinkDynLibVFS.string()
	dynamicLibraryKV = KV{P: pkLD, PC: pcLightBlue, ShowOut: true}
)

type DllModuleArgs struct {
	name              ANY
	prefix            *ANY
	version           string
	exports           *ANY
	hasName           bool
	deprecatedExports bool
}

func parseDllModuleArgs(args []ANY) DllModuleArgs {
	var out DllModuleArgs

	for i := 0; i < len(args); i++ {
		switch args[i].string() {
		case "PREFIX", "EXPORTS":
			keyword := args[i].string()

			if keyword == "EXPORTS" {
				out.deprecatedExports = true
			}

			if i+1 < len(args) {
				i++

				if keyword == "PREFIX" {
					out.prefix = &args[i]
				} else {
					out.exports = &args[i]
				}
			}
		default:
			if !out.hasName {
				out.name = args[i]
				out.hasName = true
			} else if out.version == "" {
				out.version = "." + args[i].string()
			}
		}
	}

	return out
}

func bindDllModulePrefix(stmt *ModuleStmt, env Environment) {
	args := parseDllModuleArgs(stmt.Args)

	if args.prefix != nil {
		bound := append([]ANY(nil), stmt.Args...)

		for i := range stmt.Args {
			if args.prefix == &stmt.Args[i] {
				bound[i] = expandStmtTokenAny(*args.prefix, env)
			}
		}

		stmt.Args = bound
	}
}

func dllOutputName(instance ModuleInstance, stmt *ModuleStmt) string {
	args := parseDllModuleArgs(stmt.Args)
	prefix := "lib"

	if isPythonDllModule(stmt.Name) {
		prefix = ""
	}

	if args.prefix != nil {
		prefix = args.prefix.string()
	}

	name := javaRealPrjName(instance.Path.relString(), javaNameTwoDirs)

	if args.hasName {
		name = args.name.string()
	}

	return prefix + name + ".so"
}

func dllModuleVersion(stmt *ModuleStmt) string {
	return parseDllModuleArgs(stmt.Args).version
}

func dllExportsArg(stmt *ModuleStmt) *ANY {
	return parseDllModuleArgs(stmt.Args).exports
}

func (e *EmitContext) emitDynamicLibrary() *ModuleEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na

	if len(d.moduleStmt.Args) == 0 {
		throwFmt("gen: %s DYNAMIC_LIBRARY requires a basename argument", instance.Path.relString())
	}

	if len(d.dynamicLibraryFrom) == 0 {
		throwFmt("gen: %s DYNAMIC_LIBRARY requires DYNAMIC_LIBRARY_FROM(...)", instance.Path.relString())
	}

	if d.exportsScript == nil {
		throwFmt("gen: %s DYNAMIC_LIBRARY requires EXPORTS_SCRIPT(...)", instance.Path.relString())
	}

	dynLibRPathHelperPeers := []string{"build/platform/local_so"}
	rpathHelperSet := make(map[string]struct{}, len(dynLibRPathHelperPeers))

	for _, p := range dynLibRPathHelperPeers {
		rpathHelperSet[p] = struct{}{}
	}

	peerPaths := make([]string, 0, 5+len(d.dynamicLibraryFrom))

	for _, p := range e.appendPlatformPeerdirs(nil, instance) {
		if peerYaMakeExists(ctx.fs, source(p)) {
			peerPaths = append(peerPaths, p)
		}
	}

	if !effectiveNoPlatform(d.flags) {
		peerPaths = append(peerPaths, e.defaultCommonLinkPeerdirs()...)
	}

	for _, p := range d.dynamicLibraryFrom {
		if _, helper := rpathHelperSet[p.string()]; helper {
			continue
		}

		peerPaths = append(peerPaths, p.string())
	}

	seen := make(map[string]struct{}, len(peerPaths))
	resolved := make([]ResolvedPeer, 0, len(peerPaths))

	for _, p := range peerPaths {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}

		peerInstance := e.derivePeerInstance(p)

		resolved = append(resolved, ResolvedPeer{path: p, result: e.genPeerModule(peerInstance)})
	}

	rpathOnly := make([]*ModuleEmitResult, 0, len(dynLibRPathHelperPeers))

	for _, p := range dynLibRPathHelperPeers {
		peerInstance := e.derivePeerInstance(p)

		rpathOnly = append(rpathOnly, e.genPeerModule(peerInstance))
	}

	var cFlagsGlobal, cxxFlagsGlobal, cOnlyFlagsGlobal []CompilerFlagValue

	cflagsCap, cxxflagsCap, conlyflagsCap := 0, 0, 0

	var resourceGlobals []ResourceDecl
	var peerLinkRpath []ANY

	var (
		peerArchiveRefs                                                                  []NodeRef
		peerArchivePaths, peerAddInclGlobal, pluginPaths                                 []VFS
		peerCFlagsGlobal, peerCXXFlagsGlobal, peerCOnlyFlagsGlobal, peerRPathFlagsGlobal []ANY
	)

	dedupers.with(func(deduper *DeDuper) {
		for _, rp := range resolved {
			pr := rp.result

			cflagsCap += len(pr.CFlagsGlobal)
			cxxflagsCap += len(pr.CXXFlagsGlobal)
			conlyflagsCap += len(pr.COnlyFlagsGlobal)

			for _, decl := range pr.ResourceGlobalClosure {
				if deduper.add(decl.GlobalVar.strID()) {
					resourceGlobals = append(resourceGlobals, decl)
				}
			}
		}

		peerArchiveRefs = make([]NodeRef, 0, len(resolved))
		peerArchivePaths = make([]VFS, 0, len(resolved))

		for _, rp := range resolved {
			pr := rp.result

			if pr.ARPath != nil {
				peerArchiveRefs = append(peerArchiveRefs, pr.ARRef)
				peerArchivePaths = append(peerArchivePaths, *pr.ARPath)
			}
		}

		deduper.reset()

		for _, rp := range resolved {
			pr := rp.result

			for _, p := range pr.AddInclGlobal {
				if deduper.add(p.strID()) {
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}
			}
		}

		e.bindCompilerFlagResources(resourceGlobals)

		cFlagsGlobal, peerCFlagsGlobal = e.compilerFlagExports(deduper, resolved, compilerFlagsCommon, cflagsCap, d.cFlagsGlobal, peerCFlagsGlobal)
		cxxFlagsGlobal, peerCXXFlagsGlobal = e.compilerFlagExports(deduper, resolved, compilerFlagsCxx, cxxflagsCap, d.cxxFlagsGlobal, peerCXXFlagsGlobal)
		cOnlyFlagsGlobal, peerCOnlyFlagsGlobal = e.compilerFlagExports(deduper, resolved, compilerFlagsCOnly, conlyflagsCap, d.cOnlyFlagsGlobal, peerCOnlyFlagsGlobal)

		deduper.reset()

		for _, rp := range resolved {
			pr := rp.result

			for _, a := range pr.RPathFlagsGlobal {
				if deduper.add(a.strID()) {
					peerRPathFlagsGlobal = append(peerRPathFlagsGlobal, a)
				}
			}
		}

		peerLinkRpath = peerRPathFlagsGlobal

		for _, pr := range rpathOnly {
			for _, a := range pr.RPathFlagsGlobal {
				if deduper.add(a.strID()) {
					peerRPathFlagsGlobal = append(peerRPathFlagsGlobal, a)
				}
			}
		}
	})

	d.tc = resolveModuleToolchain(ctx, resourceGlobals, instance.Platform.ClangVer)
	var modulePeers []VFS

	for _, peer := range resolved {
		modulePeers = e.appendModulePeer(modulePeers, peer.result, len(resolved))
	}

	if modulePeers != nil {
		na.vfs.commit(len(modulePeers))
	}

	e.frame.peerCtx = PeerContext{Modules: modulePeers}
	e.peers = &e.frame.peerCtx
	e.emitLDPlugins()
	e.collectPeerModuleGlobals()
	pluginPaths = e.globalPlugins()

	fixElfRef, fixElfPath := ctx.tool(argToolsFixElf)
	outputName := "lib" + d.moduleStmt.Args[0].string() + ".so"
	outputVFS := build(instance.Path.relString(), "/", outputName)
	vcsCVFS := build(instance.Path.relString(), "/__vcs_version__.c")
	vcsOVFS := build(instance.Path.relString(), "/__vcs_version__.c.pic.o")
	cmd0 := composeLDCmdVcsInfo(na, d.tc, vcsCVFS)

	var vcsResources []STR

	commandEnv := Environment{s: &e.frame.ifEnv, resourceUses: &vcsResources}
	cmd1 := composeLDCmdVcsCompile(na, instance.Platform, d.tc, vcsCVFS, vcsOVFS, compilerCommandFlags(d.cFlags, commandEnv).Args, nil, d.moduleScopeCFlags, commandEnv, d.flags.NoCompilerWarnings, d.flags.NoWShadow, d.noOptimize)
	envVcsOnly := envVarsVCS
	envFull := ctx.host.toolEnv()
	moduleEnv := Environment{s: &e.frame.ifEnv}
	soOutputs := envSoOutputs
	copySO := moduleEnv.string(soOutputs) == "yes"

	if !moduleEnv.hasBindingID(soOutputs) {
		sanitizer := moduleEnv.string(envSANITIZER_TYPE)

		copySO = moduleEnv.bool(envOPENSOURCE) || instance.Platform.OS == OSDarwin && moduleEnv.bool(envCLANG) && sanitizer != "" && sanitizer != "no"
	}

	rpathGlobal := concat(peerRPathFlagsGlobal, d.rpathFlagsGlobal)

	var linkRpath []ANY

	if copySO {
		linkRpath = concat(peerLinkRpath, d.rpathFlagsGlobal)
	}

	cmd2 := composeDynLibCmd(na, instance.Platform, d.tc, instance.Path.relString(), outputVFS, outputName, vcsOVFS, peerArchivePaths, pluginPaths, anyStrs(d.dynamicLibraryFrom), d.exportsScript.string(), fixElfPath, linkRpath)
	cmds := na.cmds.alloc(4)[:0]

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(cmd0), Env: envVcsOnly}, Cmd{CmdArgs: na.chunkList(cmd1), Env: envFull, Resources: vcsResources}, Cmd{CmdArgs: na.chunkList(cmd2), Cwd: bldRootDirVFS, Env: envFull})

	if copySO {
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(composeLDCmdLinkOrCopy(na, d.tc, instance.Path.relString())), Env: envVcsOnly})
	}

	na.cmds.commit(len(cmds))

	inputs := composeDynLibInputs(na, peerArchivePaths, pluginPaths, fixElfPath, instance.Path.relString(), d.exportsScript.string(), ctx.scripts, copySO)
	deps := na.noderefs.alloc(len(peerArchiveRefs) + 1)[:0]

	deps = append(deps, peerArchiveRefs...)
	deps = append(deps, ctx.vcsRef)
	na.noderefs.commit(len(deps))

	deps = deps[:len(deps):len(deps)]

	n := Node{
		Platform: instance.Platform,
		Cmds:     cmds[:len(cmds):len(cmds)],
		Env:      envFull,
		Inputs:   NodeInputs{files: inputs, roots: appendInputChunks(na, inputs[:2], inputs[3:])},

		Outputs:   na.vfsList(build(instance.Path.relString(), "/", outputName)),
		KV:        &dynamicLibraryKV,
		DepRefs:   deps,
		Resources: instance.Platform.UsesLinkResources,
	}

	n.ForeignDepRefs = na.refList(fixElfRef)

	ref := e.emitModuleNode(n, true)
	addInclGlobal := concat(d.addInclGlobal, peerAddInclGlobal)

	return &ModuleEmitResult{
		ARPath:                       nil,
		isPROGRAM:                    false,
		LDRef:                        ref,
		LDPath:                       ptr(build(instance.Path.relString(), "/", outputName)),
		AddInclGlobal:                addInclGlobal,
		OwnAddInclGlobal:             slices.Clone(d.addInclGlobal),
		CFlagsGlobal:                 cFlagsGlobal,
		CXXFlagsGlobal:               cxxFlagsGlobal,
		COnlyFlagsGlobal:             cOnlyFlagsGlobal,
		RPathFlagsGlobal:             rpathGlobal,
		PeerArchiveClosureRefs:       nil,
		PeerArchiveClosurePaths:      nil,
		isPyLibrary:                  false,
		PeerGlobalClosureRefs:        nil,
		PeerGlobalClosurePaths:       nil,
		PeerWholeArchiveClosureRefs:  nil,
		PeerWholeArchiveClosurePaths: nil,
		InducedDeps:                  d.inducedDeps,
		ModuleStmtName:               d.moduleStmt.Name,
	}
}

func composeDynLibCmd(na *NodeArenas, p *Platform, tc ModuleToolchain, modulePath string, output VFS, outputName string, vcsO VFS, peerLibPaths, pluginPaths []VFS, wholeArchivePeers []string, exportsScript string, fixElf VFS, linkRpath []ANY) []ANY {
	bound := 60 + len(pluginPaths) + 2*len(wholeArchivePeers) +
		len(p.SysrootArgs) + len(peerLibPaths) + len(p.LinkPreludeExtra) + p.Linker.ArgCount + len(linkRpath)

	cmdArgs := na.anys.alloc(bound)[:0]

	cmdArgs = append(cmdArgs,
		tc.Python3.any(),
		ldLinkDynLibVFS.any(),
		argTarget.any(), output.any(),
	)

	cmdArgs = append(cmdArgs, argStartPlugins.any())

	for _, p := range pluginPaths {
		cmdArgs = append(cmdArgs, p.any())
	}

	cmdArgs = append(cmdArgs, argEndPlugins.any())

	for _, peer := range wholeArchivePeers {
		cmdArgs = append(cmdArgs, argWholeArchivePeers.any(), internStr(peer).any())
	}

	cmdArgs = append(cmdArgs, p.Linker.Roots...)

	cmdArgs = append(cmdArgs,
		argArchLinux.any(),
		argObjcopyExe.any(), tc.Objcopy.any(),
		argFixElf.any(), fixElf.any(),
		tc.CXX.any(),
	)

	cmdArgs = appendLinkerGlobals(cmdArgs, p.Linker, nil, nil)
	cmdArgs = append(cmdArgs, vcsO.any())
	cmdArgs = append(cmdArgs, argDashO.any(), output.any())
	cmdArgs = append(cmdArgs, p.Linker.Shared...)
	cmdArgs = appendLinkerTemplate(cmdArgs, p.Linker.Soname, "$_SONAME", outputName)
	cmdArgs = append(cmdArgs, p.TargetArg.any())
	cmdArgs = append(cmdArgs, p.SysrootArgs...)
	cmdArgs = appendAnyLists(cmdArgs, p.Linker.BeforePeers, p.Linker.StartGroup)

	for _, p := range peerLibPaths {
		cmdArgs = append(cmdArgs, p.rel().any())
	}

	cmdArgs = appendAnyLists(cmdArgs, p.Linker.EndGroup, p.Linker.AfterPeers)
	cmdArgs = appendLinkerTemplate(cmdArgs, p.Linker.Exports, "${input:EXPORTS_FILE}", "$(S)/"+modulePath+"/"+exportsScript)
	cmdArgs = append(cmdArgs, linkRpath...)

	if p.CompressDebugSections {
		cmdArgs = append(cmdArgs, argWlCompressDebugSectionsZstd.any())
	}

	cmdArgs = append(cmdArgs, p.LinkPreludeExtra...)
	cmdArgs = append(cmdArgs, argWlNoAsNeeded.any())

	if p.PIC {
		cmdArgs = append(cmdArgs, argFPIC.any())
	}

	cmdArgs = append(cmdArgs,
		argWlGdbIndex.any(),
		argWlZNotext.any(),
	)

	if p.PIC {
		cmdArgs = append(cmdArgs, argFPIC.any())
	}

	cmdArgs = append(cmdArgs,
		argFuseLdLld.any(),
		internV("--ld-path=", tc.LLD.prefix(), tc.LLD.relString()).any(),
		argWlNoRosegment.any(),
		argWlBuildIdSha1.any(),
	)

	cmdArgs = appendAnyLists(cmdArgs, p.Linker.Intercept, p.Linker.SystemLibs)
	cmdArgs = append(cmdArgs, argLm.any())
	cmdArgs = appendAnyLists(cmdArgs, p.Linker.DCE, p.Linker.ICF, p.Linker.NoPie)
	na.anys.commit(len(cmdArgs))

	return cmdArgs[:len(cmdArgs):len(cmdArgs)]
}

func composeDynLibInputs(na *NodeArenas, peerLibPaths, pluginPaths []VFS, fixElfPath VFS, modulePath, exportsScript string, scripts ScriptDeps, copySO bool) InputChunks {
	peerChunk := na.vfsList(peerLibPaths...)
	pluginChunk := na.vfsList(pluginPaths...)

	tailChunk := na.vfsList(
		ldSvnInterfaceVFS,
		ldSvnversionHVFS,
		source(modulePath, "/", exportsScript),
	)

	chunks := na.inputs.alloc(7)[:0]

	chunks = append(chunks, peerChunk, pluginChunk, na.srcChunk(fixElfPath))

	for _, w := range [2]VFS{ldVcsInfoVFS, ldLinkDynLibVFS} {
		chunks = append(chunks, scripts[w.rel()])
	}

	if copySO {
		chunks = append(chunks, scripts[ldFsToolsVFS.rel()])
	}

	chunks = append(chunks, tailChunk)
	na.inputs.commit(len(chunks))

	return InputChunks(chunks[:len(chunks):len(chunks)])
}
