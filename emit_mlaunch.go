package main

import "strings"

var (
	mlaunchKV      = KV{P: pkML, PC: pcCyan}
	golaunchKV     = KV{P: pkGL, PC: pcCyan}
	packageFinalKV = KV{P: pkPK, PC: pcLightRed}
)

func (e *EmitContext) emitMlaunch(resolved []ResolvedPeer, peerArchivePaths []VFS, deps []string) {
	if e.d.mlaunch == nil {
		return
	}

	if e.d.mlaunch.Generate == "composite" {
		script := buildInternalScriptsGenCompositeGo
		args := append([]ANY{e.d.tc.Python3.any(), script.any(), strLocalPeers.any()}, e.d.mlaunch.Components...)

		e.emitGolaunchSource(&golaunchKV, script, args, build(e.instance.Path.relString(), "/main.go"))

		return
	}

	if e.d.mlaunch.Generate == "" {
		return
	}

	script := buildInternalScriptsGenMlaunchPy
	args := []ANY{e.d.tc.Python3.any(), script.any(), strType.any(), internStr(e.d.mlaunch.Generate).any(), strLocalPeers.any()}

	dedupers.with(func(deduper *DeDuper) {
		for _, peer := range resolved {
			if peer.result.ARPath != nil {
				deduper.add(peer.result.ARPath.strID())
			}
		}

		for _, peer := range peerArchivePaths {
			if deduper.has(peer.strID()) {
				args = append(args, peer.rel().any())
			}
		}
	})

	args = append(args, strDeps.any())
	args = append(args, internAnys(deps)...)
	args = append(args, internAnys(e.d.mlaunch.Args)...)

	e.emitGolaunchSource(&mlaunchKV, script, args, build(e.instance.Path.relString(), "/launch.go"))
}

func (e *EmitContext) emitGolaunchSource(kv *KV, script VFS, args []ANY, out VFS) {
	na := e.ctx.na
	ref := e.ctx.emit.reserve()
	inputs := append([]VFS{script}, e.ctx.scripts[script.rel()]...)
	node := Node{Platform: e.instance.Platform, KV: kv, Resources: usesPython3, Env: envVarsVCS, Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(args), Stdout: out, Env: envVarsVCS}), Inputs: NodeInputs{files: na.rootInputList(na.vfsList(inputs...))}, Outputs: na.vfsList(out)}
	pending := e.pendingEmit(func() { e.emitReservedNode(node, ref) })

	e.register(GeneratedFileSpec{OutputPath: out, ProducerRef: ref, OnUse: pending})
	e.collectGoAutoSource(out, 0)
}

func (e *EmitContext) genPackageFinal() *ModuleEmitResult {
	var peers []VFS
	var directPeers, construction, globalOuts []VFS
	var globals []ResourceDecl

	seen := map[*ModuleEmitResult]bool{}

	for _, language := range e.d.selfPeers {
		self := e.instance

		self.Language = language
		self.Demand = max(e.instance.Demand, demandSelf)

		result, built := e.completePackagePeer(self)

		construction = append(construction, built...)

		if out := moduleOutputPath(result); out != 0 {
			directPeers = append(directPeers, out)
		}

		peers = e.appendPackagePeer(peers, result, seen, &globalOuts)
		globals = append(globals, result.ResourceGlobalClosure...)
	}

	for _, dir := range e.d.peerdirs {
		peer := e.derivePeerInstance(dir.string())

		peer.Demand = demandLinked

		if e.ctx.moduleName(dir.string()) == tokMlaunchLibrary {
			peer.Language = LangPackage
		}

		result, built := e.completePackagePeer(peer)

		construction = append(construction, built...)

		if out := moduleOutputPath(result); out != 0 {
			directPeers = append(directPeers, out)
		}

		peers = e.appendPackagePeer(peers, result, seen, &globalOuts)
		e.globalSources = append(e.globalSources, result.GlobalSources...)
		globals = append(globals, result.ResourceGlobalClosure...)
	}

	globals = e.scriptModuleResources(globals, "build/platform/python/ymake_python3")
	e.emitDeclaredProducers(nil)

	module := e.instance.Path.relString()
	name := realPrjName(module)

	if len(e.d.moduleStmt.Args) > 0 {
		name = programBinaryName(e.instance, e.d.moduleStmt)
	}

	out := build(module, "/", name, ".final.pkg.fake")
	na := e.ctx.na
	copyScript := buildScriptsCopyToDirPy
	inputs := []VFS{buildScriptsTouchPy, copyScript, buildScriptsProcessCommandFilesPy}
	buildFrom := na.inputList(na.vfsList(inputs...), na.vfsList(directPeers...), na.vfsList(e.outs...))

	var lateOuts []VFS

	for _, peer := range peers {
		if info := e.codegen.lookup(peer); info != nil {
			lateOuts = append(lateOuts, info.LateOutputs...)
		}
	}

	lateOuts = dedup(lateOuts)
	globalOuts = dedup(e.globalSources, globalOuts)

	globalSet := make(map[VFS]bool, len(globalOuts))
	copyFiles := make([]VFS, 0, len(globalOuts)+len(lateOuts)+len(peers))

	for _, file := range globalOuts {
		globalSet[file] = true
		copyFiles = append(copyFiles, file)
	}

	copyFiles = append(copyFiles, lateOuts...)

	for _, file := range peers {
		if !globalSet[file] {
			copyFiles = append(copyFiles, file)
		}
	}

	inputs = append(inputs, peers...)
	inputs = append(inputs, construction...)
	inputs = append(inputs, e.outs...)

	args := []ANY{e.d.tc.Python3.any(), copyScript.any(), strYaStartCommandFile.any(), strDestDir.any(), build(module).any(), strBuildRoot2.any(), bldRootDirVFS.any()}
	outputs := []VFS{out}

	if env := (Environment{s: &e.frame.ifEnv}); env.string(envPackedPackageExt) != "" {
		archive := build(module, "/", name, ".", env.string(envPackedPackageExt))

		args = append(args, strDestArch.any(), archive.any())
		outputs = append(outputs, archive)
	}

	for _, file := range copyFiles {
		if !strings.HasSuffix(file.relString(), ".pkg.fake") {
			args = append(args, file.any())
		}
	}

	args = append(args, strYaEndCommandFile.any())

	node := Node{Module: true, Platform: e.instance.Platform, KV: &packageFinalKV, KVExts: []KVExt{{Key: "package", Val: "PACKAGE"}}, Env: envVarsVCS, Resources: usesPython3,
		Inputs: NodeInputs{files: na.rootInputList(na.vfsList(dedup(inputs)...)), roots: buildFrom}, Outputs: na.vfsList(append(outputs, e.outs...)...), DepRefs: na.refList(dedupRefs(e.refs)...),
		Cmds: na.cmdList(e.pythonScriptCmd("touch.py", out.string()), Cmd{CmdArgs: na.chunkList(args), Env: envVarsVCS})}

	suffix := ""

	if e.d.mlaunch != nil {
		suffix = e.d.mlaunch.Suffix
	}

	if !e.appendSdcMeta(&node, name+suffix) && len(e.globalSources) > 0 {
		globalInputs := make([]VFS, 0, 2*len(e.globalSources))

		for _, global := range e.globalSources {
			globalInputs = append(globalInputs, global, e.codegen.lookup(global).ProducerMainOut)
		}

		node.Inputs.files = appendInputChunks(na, node.Inputs.files, na.inputList(na.vfsList(dedup(globalInputs)...)))
	}

	for _, file := range copyFiles {
		if !strings.HasSuffix(file.relString(), ".pkg.fake") {
			node.Outputs = append(node.Outputs, build(module, "/", file.relString()))
		}
	}

	packagePeers := dedup(peers)

	e.setModulePeerRoots(&node, packagePeers)
	node.Inputs.files = e.appendModuleInputs(node.Inputs.files, packagePeers, false)

	ref := e.emitLazyModuleNode(node, peers, outputs...)

	return e.scriptModuleResult(ref, out, packagePeers, globals)
}
