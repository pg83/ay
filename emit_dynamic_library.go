package main

import (
	"slices"
)

var (
	ldLinkDynLibPath = ldLinkDynLibVFS.string()
	dynamicLibraryKV = KV{P: pkLD, PC: pcLightBlue, ShowOut: true}
)

func dllOutputName(instance ModuleInstance, stmt *ModuleStmt) string {
	prefix := "lib"
	name := ""

	if len(stmt.Args) > 0 {
		name = stmt.Args[0].string()
	} else {
		name = javaRealPrjName(instance.Path.relString(), javaNameTwoDirs)
	}

	for i := 1; i+1 < len(stmt.Args); i++ {
		if stmt.Args[i].string() == "PREFIX" {
			prefix = stmt.Args[i+1].string()
		}
	}

	return prefix + name + ".so"
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
	resolved := make([]*ModuleEmitResult, 0, len(peerPaths))

	for _, p := range peerPaths {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}

		peerInstance := e.derivePeerInstance(p)

		resolved = append(resolved, genModule(ctx, peerInstance))
	}

	rpathOnly := make([]*ModuleEmitResult, 0, len(dynLibRPathHelperPeers))

	for _, p := range dynLibRPathHelperPeers {
		peerInstance := e.derivePeerInstance(p)

		rpathOnly = append(rpathOnly, genModule(ctx, peerInstance))
	}

	var resourceGlobals []ResourceDecl

	var (
		peerArchiveRefs, pluginRefs                                                      []NodeRef
		peerArchivePaths, peerAddInclGlobal, pluginPaths                                 []VFS
		peerCFlagsGlobal, peerCXXFlagsGlobal, peerCOnlyFlagsGlobal, peerRPathFlagsGlobal []ANY
	)

	dedupers.with(func(deduper *DeDuper) {
		for _, pr := range resolved {
			for _, decl := range pr.ResourceGlobalClosure {
				if deduper.add(decl.GlobalVar.strID()) {
					resourceGlobals = append(resourceGlobals, decl)
				}
			}
		}

		peerArchiveRefs = make([]NodeRef, 0, len(resolved))
		peerArchivePaths = make([]VFS, 0, len(resolved))

		for _, pr := range resolved {
			if pr.ARPath != nil {
				peerArchiveRefs = append(peerArchiveRefs, pr.ARRef)
				peerArchivePaths = append(peerArchivePaths, *pr.ARPath)
			}
		}

		deduper.reset()

		for _, pr := range resolved {
			for _, p := range pr.AddInclGlobal {
				if deduper.add(p.strID()) {
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}
			}
		}

		deduper.reset()

		for _, pr := range resolved {
			for _, a := range pr.CFlagsGlobal {
				if deduper.add(a.strID()) {
					peerCFlagsGlobal = append(peerCFlagsGlobal, a)
				}
			}
		}

		deduper.reset()

		for _, pr := range resolved {
			for _, a := range pr.CXXFlagsGlobal {
				if deduper.add(a.strID()) {
					peerCXXFlagsGlobal = append(peerCXXFlagsGlobal, a)
				}
			}
		}

		deduper.reset()

		for _, pr := range resolved {
			for _, a := range pr.COnlyFlagsGlobal {
				if deduper.add(a.strID()) {
					peerCOnlyFlagsGlobal = append(peerCOnlyFlagsGlobal, a)
				}
			}
		}

		deduper.reset()

		for _, pr := range resolved {
			for _, a := range pr.RPathFlagsGlobal {
				if deduper.add(a.strID()) {
					peerRPathFlagsGlobal = append(peerRPathFlagsGlobal, a)
				}
			}
		}

		for _, pr := range rpathOnly {
			for _, a := range pr.RPathFlagsGlobal {
				if deduper.add(a.strID()) {
					peerRPathFlagsGlobal = append(peerRPathFlagsGlobal, a)
				}
			}
		}

		pluginRefs = []NodeRef{}
		pluginPaths = []VFS{}

		deduper.reset()

		for _, pr := range resolved {
			for i, pp := range pr.LDPluginPaths {
				if deduper.add(pp.strID()) {
					pluginRefs = append(pluginRefs, pr.LDPluginRefs[i])
					pluginPaths = append(pluginPaths, pp)
				}
			}
		}
	})

	d.tc = resolveModuleToolchain(ctx, resourceGlobals, instance.Platform.ClangVer)

	fixElfRef, fixElfPath := ctx.tool(argToolsFixElf)
	outputName := "lib" + d.moduleStmt.Args[0].string() + ".so"
	outputVFS := build(instance.Path.relString(), "/", outputName)
	vcsCVFS := build(instance.Path.relString(), "/__vcs_version__.c")
	vcsOVFS := build(instance.Path.relString(), "/__vcs_version__.c.pic.o")
	cmd0 := composeLDCmdVcsInfo(na, d.tc, vcsCVFS)
	cmd1 := composeLDCmdVcsCompile(na, instance.Platform, d.tc, vcsCVFS, vcsOVFS, d.cFlags, nil, d.moduleScopeCFlags, d.flags.NoCompilerWarnings, d.noOptimize)
	cmd2 := composeDynLibCmd(na, instance.Platform, d.tc, instance.Path.relString(), outputVFS, outputName, vcsOVFS, nil, peerArchivePaths, pluginPaths, anyStrs(d.dynamicLibraryFrom), d.exportsScript.string(), fixElfPath)
	cmd3 := composeLDCmdLinkOrCopy(na, d.tc, instance.Path.relString())
	envVcsOnly := envVarsVCS
	envFull := ctx.host.toolEnv()
	inputs := composeDynLibInputs(na, peerArchivePaths, pluginPaths, fixElfPath, instance.Path.relString(), d.exportsScript.string(), ctx.scripts)
	deps := na.noderefs.alloc(len(peerArchiveRefs) + len(pluginRefs) + 1)[:0]

	deps = append(deps, peerArchiveRefs...)
	deps = append(deps, pluginRefs...)
	deps = append(deps, ctx.vcsRef)
	na.noderefs.commit(len(deps))

	deps = deps[:len(deps):len(deps)]

	n := Node{
		Platform:  instance.Platform,
		Cmds:      na.cmdList(Cmd{CmdArgs: na.chunkList(cmd0), Env: envVcsOnly}, Cmd{CmdArgs: na.chunkList(cmd1), Env: envFull}, Cmd{CmdArgs: na.chunkList(cmd2), Cwd: bldRootDirVFS, Env: envFull}, Cmd{CmdArgs: na.chunkList(cmd3), Env: envVcsOnly}),
		Env:       envFull,
		Inputs:    inputs,
		Outputs:   na.vfsList(build(instance.Path.relString(), "/", outputName)),
		KV:        &dynamicLibraryKV,
		DepRefs:   deps,
		Resources: instance.Platform.UsesLinkResources,
	}

	n.ForeignDepRefs = na.refList(fixElfRef)

	ref := e.emitNode(n)
	addInclGlobal := concat(d.addInclGlobal, peerAddInclGlobal)
	cFlagsGlobal := concat(d.cFlagsGlobal, peerCFlagsGlobal)
	cxxFlagsGlobal := concat(d.cxxFlagsGlobal, peerCXXFlagsGlobal)
	cOnlyFlagsGlobal := concat(d.cOnlyFlagsGlobal, peerCOnlyFlagsGlobal)

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
		RPathFlagsGlobal:             concat(peerRPathFlagsGlobal, d.rpathFlagsGlobal),
		PeerArchiveClosureRefs:       nil,
		PeerArchiveClosurePaths:      nil,
		isPyLibrary:                  false,
		PeerGlobalClosureRefs:        nil,
		PeerGlobalClosurePaths:       nil,
		PeerWholeArchiveClosureRefs:  nil,
		PeerWholeArchiveClosurePaths: nil,
		LDPluginRefs:                 pluginRefs,
		LDPluginPaths:                pluginPaths,
		InducedDeps:                  d.inducedDeps,
		ModuleStmtName:               d.moduleStmt.Name,
	}
}

func composeDynLibCmd(na *NodeArenas, p *Platform, tc ModuleToolchain, modulePath string, output VFS, outputName string, vcsO VFS, ownObjects, peerLibPaths, pluginPaths []VFS, wholeArchivePeers []string, exportsScript string, fixElf VFS) []ANY {
	bound := 60 + len(pluginPaths) + 2*len(wholeArchivePeers) + len(ownObjects) +
		len(p.SysrootArgs) + len(peerLibPaths) + len(p.LinkPreludeExtra) + p.Linker.ArgCount

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

	for _, o := range ownObjects {
		cmdArgs = append(cmdArgs, o.any())
	}

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

	if !p.PIC && p.CompressDebugSections {
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

func composeDynLibInputs(na *NodeArenas, peerLibPaths, pluginPaths []VFS, fixElfPath VFS, modulePath, exportsScript string, scripts ScriptDeps) InputChunks {
	peerChunk := na.vfsList(peerLibPaths...)
	pluginChunk := na.vfsList(pluginPaths...)

	tailChunk := na.vfsList(
		ldSvnInterfaceVFS,
		ldSvnversionHVFS,
		source(modulePath, "/", exportsScript),
	)

	chunks := na.inputs.alloc(7)[:0]

	chunks = append(chunks, peerChunk, pluginChunk, na.srcChunk(fixElfPath))

	for _, w := range [3]VFS{ldVcsInfoVFS, ldLinkDynLibVFS, ldFsToolsVFS} {
		chunks = append(chunks, scripts[w.rel()])
	}

	chunks = append(chunks, tailChunk)
	na.inputs.commit(len(chunks))

	return InputChunks(chunks[:len(chunks):len(chunks)])
}
