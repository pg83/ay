package main

import "strings"

var ciGroupKV = KV{P: pkCI, PC: pcLightRed}

func (e *EmitContext) packagePeer(dir string) *ModuleEmitResult {
	instance := e.derivePeerInstance(strings.TrimPrefix(dir, "/"))

	instance.Demand = demandLinked

	name := e.ctx.moduleName(instance.Path.relString())

	if name == tokPackage {
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

	return genModule(e.ctx, instance)
}

func (e *EmitContext) appendPackagePeer(out []VFS, res *ModuleEmitResult, seen map[*ModuleEmitResult]bool) []VFS {
	if seen[res] {
		return out
	}

	seen[res] = true
	out = append(out, res.JavaClosure...)

	if jm := res.java; jm != nil {
		for _, peer := range e.ctx.javaManagedPeersClosure(jm) {
			if peer.java != nil {
				out = e.appendPackagePeer(out, peer.java.result, seen)
			} else if peer.foreign != nil {
				out = e.appendPackagePeer(out, peer.foreign, seen)
			}
		}

		if jm.outRef != 0 {
			out = append(out, jm.target)
		}

		return out
	}

	if res.LDPath != nil && (res.isPROGRAM || isLinkingModuleType(res.ModuleStmtName)) {
		e.refs = append(e.refs, res.LDRef)

		return append(out, *res.LDPath)
	}

	e.refs = append(e.refs, res.PeerArchiveClosureRefs...)
	e.refs = append(e.refs, res.PeerGlobalClosureRefs...)
	e.refs = append(e.refs, res.PeerDynamicClosureRefs...)

	out = append(out, res.PeerArchiveClosurePaths...)
	out = append(out, res.PeerGlobalClosurePaths...)
	out = append(out, res.PeerDynamicClosurePaths...)

	if res.ARPath != nil {
		e.refs = append(e.refs, res.ARRef)
		out = append(out, *res.ARPath)
	}

	if res.GlobalPath != nil {
		e.refs = append(e.refs, *res.GlobalRef)
		out = append(out, *res.GlobalPath)
	}

	return out
}

func (e *EmitContext) genCiGroup() *ModuleEmitResult {
	var peers []VFS
	var globals []ResourceDecl

	seen := map[*ModuleEmitResult]bool{}

	for _, dir := range e.d.peerdirs {
		res := e.packagePeer(dir.string())

		peers = e.appendPackagePeer(peers, res, seen)
		globals = append(globals, res.ResourceGlobalClosure...)
	}

	globals = e.scriptModuleResources(globals, "build/platform/python/ymake_python3")
	e.emitFromSandboxes()
	e.emitDeclaredProducers(nil)

	module := e.instance.Path.relString()
	name := realPrjName(module)

	if len(e.d.moduleStmt.Args) > 0 {
		name = e.d.moduleStmt.Args[0].string()
	}

	out := build(module, "/", name, ".ci.pkg.fake")
	inputs := append([]VFS{buildScriptsTouchPy}, peers...)

	inputs = append(inputs, e.outs...)

	na := e.ctx.na

	node := Node{
		Module: true, Platform: e.instance.Platform, KV: &ciGroupKV,
		KVExts: []KVExt{{Key: "package", Val: "PACKAGE"}}, Env: envVarsVCS,
		Inputs: na.inputList(na.vfsList(dedup(inputs)...)), Outputs: na.vfsList(out), Resources: usesPython3,
		DepRefs: na.refList(dedupRefs(e.refs)...),
		Cmds:    na.cmdList(e.pythonScriptCmd("touch.py", out.string())),
	}

	ref := e.emitLazyModuleNode(node, nil)

	return e.scriptModuleResult(ref, out, dedup(peers), globals)
}
