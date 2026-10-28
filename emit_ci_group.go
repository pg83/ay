package main

import "strings"

var ciGroupKV = KV{P: pkCI, PC: pcLightRed}

func (e *EmitContext) packagePeer(dir string) (*ModuleEmitResult, []VFS) {
	instance := e.derivePeerInstance(strings.TrimPrefix(dir, "/"))

	instance.Demand = demandLinked

	name := e.ctx.moduleName(instance.Path.relString())

	if name == tokPackage {
		instance.Peer |= peerPackageContents
		instance.Language = LangCPP
	}

	if units := javaModuleUnits[name]; len(units) > 0 {
		for _, unit := range units {
			if unit.tag == "JAR_RUNNABLE" || unit.tag == "FULL_JAR_COMPILATION" || len(units) == 1 {
				instance.Kind = unit.sub
				instance.Language = LangJava

				break
			}
		}
	}

	return e.completePackagePeer(instance)
}

func (e *EmitContext) completePackagePeer(instance ModuleInstance) (*ModuleEmitResult, []VFS) {
	if isLinkingModuleType(e.ctx.moduleName(instance.Path.relString())) {
		instance.Demand = demandSelf
	}

	result := e.genPeerModule(instance)

	var inputs []VFS

	seen := map[*ModuleEmitResult]bool{}

	var visit func(*ModuleEmitResult)

	visit = func(res *ModuleEmitResult) {
		if seen[res] {
			return
		}

		seen[res] = true

		if res.IncludedDeps != nil {
			inputs = append(inputs, res.IncludedDeps.Inputs...)
			e.refs = append(e.refs, res.IncludedDeps.Refs...)
		}

		if res.ARPath != nil {
			inputs = append(inputs, *res.ARPath)
			e.refs = append(e.refs, res.ARRef)
		} else if res.LDPath != nil {
			inputs = append(inputs, *res.LDPath)
			e.refs = append(e.refs, res.LDRef)
		}

		if info := e.codegen.lookup(moduleOutputPath(res)); info != nil && info.collectionPeers() != nil {
			for _, peer := range info.collectionPeers().Modules {
				peer.Demand = demandLinked

				if isLinkingModuleType(e.ctx.moduleName(peer.Path.relString())) {
					peer.Demand = demandSelf
				}

				visit(e.genPeerModule(peer))
			}
		}
	}

	visit(result)

	return result, inputs
}

func (e *EmitContext) appendPackagePeer(out []VFS, res *ModuleEmitResult, seen map[*ModuleEmitResult]bool, globalOuts *[]VFS) []VFS {
	if seen[res] {
		return out
	}

	seen[res] = true

	if info := e.codegen.lookup(moduleOutputPath(res)); info != nil && info.collectionPeers() != nil && info.collectionPeers().PassPeers {
		for _, peer := range info.collectionPeers().Modules {
			peer.Demand = demandLinked

			if isLinkingModuleType(e.ctx.moduleName(peer.Path.relString())) {
				peer.Demand = demandSelf
			}

			out = e.appendPackagePeer(out, e.genPeerModule(peer), seen, globalOuts)
		}
	}

	out = append(out, res.JavaClosure...)

	if jm := res.java; jm != nil {
		for _, peer := range e.ctx.javaManagedPeersClosure(jm) {
			if peer.java != nil {
				out = e.appendPackagePeer(out, peer.java.result, seen, globalOuts)
			} else if peer.foreign != nil {
				out = e.appendPackagePeer(out, peer.foreign, seen, globalOuts)
			}
		}

		if jm.outRef != 0 {
			out = append(out, jm.target)
		}

		return out
	}

	if res.LDPath != nil && (res.isPROGRAM || isLinkingModuleType(res.ModuleStmtName)) {
		e.codegen.forwardModuleIncludedDeps(*res.LDPath, res.IncludedDeps)
		e.refs = append(e.refs, res.LDRef)

		return append(out, *res.LDPath)
	}

	e.refs = append(e.refs, res.PeerArchiveClosureRefs...)
	e.refs = append(e.refs, res.PeerGlobalClosureRefs...)
	e.refs = append(e.refs, res.PeerDynamicClosureRefs...)

	out = append(out, res.PeerArchiveClosurePaths...)
	out = append(out, res.PeerGlobalClosurePaths...)

	if globalOuts != nil {
		*globalOuts = append(*globalOuts, res.PeerGlobalClosurePaths...)
	}

	out = append(out, res.PeerDynamicClosurePaths...)

	if res.ARPath != nil {
		e.refs = append(e.refs, res.ARRef)
		out = append(out, *res.ARPath)
	}

	if res.GlobalPath != nil {
		e.refs = append(e.refs, *res.GlobalRef)
		out = append(out, *res.GlobalPath)

		if globalOuts != nil {
			*globalOuts = append(*globalOuts, *res.GlobalPath)
		}
	}

	return out
}

func (e *EmitContext) genCiGroup() *ModuleEmitResult {
	var peers, construction []VFS
	var globals []ResourceDecl

	seen := map[*ModuleEmitResult]bool{}

	for _, dir := range e.d.peerdirs {
		res, built := e.packagePeer(dir.string())

		construction = append(construction, built...)
		peers = e.appendPackagePeer(peers, res, seen, nil)
		globals = append(globals, res.ResourceGlobalClosure...)
	}

	globals = e.scriptModuleResources(globals, "build/platform/python/ymake_python3")
	e.emitDeclaredProducers(nil)

	module := e.instance.Path.relString()
	name := realPrjName(module)

	if len(e.d.moduleStmt.Args) > 0 {
		name = e.d.moduleStmt.Args[0].string()
	}

	out := build(module, "/", name, ".ci.pkg.fake")
	inputs := append([]VFS{buildScriptsTouchPy}, peers...)

	inputs = append(inputs, construction...)
	inputs = append(inputs, e.outs...)

	na := e.ctx.na

	node := NodeSpec{Node: Node{Module: true, Platform: e.instance.Platform, KV: &ciGroupKV,
		KVExts: []KVExt{{Key: "package", Val: "PACKAGE"}}, Env: envVarsVCS, Inputs: na.rootInputList(na.vfsList(dedup(inputs)...)), Outputs: na.vfsList(out), Resources: usesPython3,
		DepRefs: na.refList(dedupRefs(e.refs)...),
		Cmds:    na.cmdList(e.pythonScriptCmd("touch.py", out.string()))},
	}

	ref := e.emitLazyModuleNode(node, nil)
	result := e.scriptModuleResult(ref, out, dedup(peers), globals)

	result.TestSuites = e.styleSuites(globals)

	return result
}
