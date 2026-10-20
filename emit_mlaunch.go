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
	node := Node{Platform: e.instance.Platform, KV: kv, Resources: usesPython3, Env: envVarsVCS, Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(args), Stdout: out, Env: envVarsVCS}), Inputs: na.inputList(na.vfsList(inputs...)), Outputs: na.vfsList(out)}
	pending := e.pendingEmit(func() { e.emitReservedNode(node, ref) })

	e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: ref, OnUse: pending})
	e.collectGoAutoSource(out, 0)
}

func (e *EmitContext) genPackageFinal() *ModuleEmitResult {
	var peers []VFS
	var globals []ResourceDecl

	seen := map[*ModuleEmitResult]bool{}

	for _, language := range e.d.selfPeers {
		self := e.instance

		self.Language = language
		self.Demand = max(e.instance.Demand, demandSelf)

		result := genModule(e.ctx, self)

		peers = e.appendPackagePeer(peers, result, seen)
		globals = append(globals, result.ResourceGlobalClosure...)
	}

	for _, dir := range e.d.peerdirs {
		peer := e.derivePeerInstance(dir.string())

		peer.Demand = demandLinked

		if e.ctx.moduleName(dir.string()) == tokMlaunchLibrary {
			peer.Language = LangPackage
		}

		result := genModule(e.ctx, peer)

		peers = e.appendPackagePeer(peers, result, seen)
		e.globalSources = append(e.globalSources, result.GlobalSources...)
		globals = append(globals, result.ResourceGlobalClosure...)
	}

	globals = e.scriptModuleResources(globals, "build/platform/python/ymake_python3")
	e.emitFromSandboxes()
	e.emitBundles()
	e.emitDeclaredProducers(nil)

	module := e.instance.Path.relString()
	name := programBinaryName(e.instance, e.d.moduleStmt)
	out := build(module, "/", name, ".final.pkg.fake")
	na := e.ctx.na
	copyScript := buildScriptsCopyToDirPy
	inputs := []VFS{buildScriptsTouchPy, copyScript, buildScriptsProcessCommandFilesPy}

	var lateOuts []VFS

	for _, peer := range peers {
		if info := e.codegen.lookup(peer); info != nil {
			lateOuts = append(lateOuts, info.LateOutputs...)
		}
	}

	lateOuts = dedup(lateOuts)

	inputs = append(inputs, lateOuts...)
	inputs = append(inputs, peers...)
	inputs = append(inputs, e.outs...)

	args := []ANY{e.d.tc.Python3.any(), copyScript.any(), strYaStartCommandFile.any(), strDestDir.any(), build(module).any(), strBuildRoot2.any(), bldRootDirVFS.any()}

	for _, file := range concat(lateOuts, peers) {
		if !strings.HasSuffix(file.relString(), ".pkg.fake") {
			args = append(args, file.any())
		}
	}

	args = append(args, strYaEndCommandFile.any())

	node := Node{Module: true, Platform: e.instance.Platform, KV: &packageFinalKV, KVExts: []KVExt{{Key: "package", Val: "PACKAGE"}}, Env: envVarsVCS, Resources: usesPython3,
		Inputs: na.inputList(na.vfsList(dedup(inputs)...)), Outputs: na.vfsList(out), DepRefs: na.refList(dedupRefs(e.refs)...),
		Cmds: na.cmdList(e.pythonScriptCmd("touch.py", out.string()), Cmd{CmdArgs: na.chunkList(args), Env: envVarsVCS})}

	for _, file := range concat(lateOuts, peers) {
		if !strings.HasSuffix(file.relString(), ".pkg.fake") {
			node.Outputs = append(node.Outputs, build(module, "/", file.relString()))
		}
	}

	suffix := ""

	if e.d.mlaunch != nil {
		suffix = e.d.mlaunch.Suffix
	}

	e.appendSdcMeta(&node, name+suffix)

	ref := e.emitLazyModuleNode(node, peers)

	return e.scriptModuleResult(ref, out, dedup(peers), globals)
}
