package main

import (
	"slices"
	"strings"
)

var unionKV = KV{P: pkUN, PC: pcLightCyan}

func isUnionModule(t TOK) bool {
	return t == tokUnion || t == tokSdcUnion
}

func (d *ModuleData) addFiles(files []ANY) {
	for _, file := range files {
		name := file.string()

		if strings.HasPrefix(name, "$(B)/") || strings.HasPrefix(name, "${ARCADIA_BUILD_ROOT}") {
			continue
		}

		d.copyFiles = append(d.copyFiles, CopyFileEntry{Src: name, Dst: name, DeclSeq: d.nextDeclSeq()})
	}
}

func (e *EmitContext) genUnion() *ModuleEmitResult {
	var peers []VFS
	var globals []ResourceDecl
	var peerIncludes, peerProtoIncludes []VFS
	var peerContexts []ModuleInstance

	for _, p := range e.d.peerdirs {
		peer := e.derivePeerInstance(p.string())

		peer.Demand = demandNone
		peerContexts = append(peerContexts, peer)

		res := e.genPeerModule(peer)

		peers = append(peers, res.JavaClosure...)
		globals = append(globals, res.ResourceGlobalClosure...)
		peerIncludes = append(peerIncludes, res.AddInclGlobal...)
		peerProtoIncludes = append(peerProtoIncludes, res.ProtoInclude...)
		e.globalSources = append(e.globalSources, res.GlobalSources...)
	}

	globals = e.scriptModuleResources(globals, "build/platform/python/ymake_python3")

	e.d.scanCtx.ccPeer = e.ctx.na.vfsList(dedup(peerIncludes)...)
	e.d.scanCtx.protoInclude = e.ctx.na.vfsList(dedup(e.d.scanCtx.protoInclude, peerProtoIncludes)...)

	own := e.emitSdcMetas(false)

	e.globalSources = dedup(e.globalSources, own)
	e.emitDeclaredProducers(nil)

	generated := slices.DeleteFunc(dedup(e.outs), func(out VFS) bool { return slices.Contains(own, out) })

	if len(e.d.unionExplicitOutputs) > 0 {
		generated = nil

		for _, file := range e.d.unionExplicitOutputs {
			generated = append(generated, e.moduleSourceVFS(file))
		}

		generated = dedup(generated)
	}

	generated = orderSdcInstallOutputs(generated, e.d.sdcInstallOutputs)

	module := e.instance.Path.relString()
	name := realPrjName(module)

	if len(e.d.moduleStmt.Args) > 0 {
		name = e.d.moduleStmt.Args[0].string()
	}

	out := build(module, "/", name, Environment{s: &e.frame.ifEnv}.string(envMODULE_SUFFIX))
	na := e.ctx.na

	node := NodeSpec{Node: Node{Module: true, Platform: e.instance.Platform, KV: &unionKV,
		KVExts: []KVExt{{Key: "package", Val: "UNION"}}, Env: envVarsVCS, Inputs: na.rootInputList(na.vfsList(own...), na.vfsList(generated...)), Outputs: na.vfsList(append([]VFS{out}, generated...)...), Resources: usesPython3,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(e.d.tc.Python3.any(), strC2.any(), internStr("open('"+out.string()+"','w').close()").any())), Env: envVarsVCS})},
	}

	e.appendSdcMeta(&node, name)

	ref := e.emitLazyModuleNode(node, peers, out)

	e.codegen.mustInfo(out).LateOutputs = e.ctx.na.vfsList(generated...)

	passPeers := e.d.resourcePassPeers
	info := e.codegen.mustInfo(out)

	info.attributes.collectionPeers.put(info.id, &CollectionPeerInfo{Modules: peerContexts, PassPeers: passPeers})

	if !passPeers {
		peers = nil
	}

	res := e.scriptModuleResult(ref, out, dedup(peers), globals)

	res.GlobalSources = e.globalSources
	res.TestSuites = e.styleSuites(globals)

	return res
}

func orderSdcInstallOutputs(generated, declared []VFS) []VFS {
	if len(declared) == 0 {
		return generated
	}

	present := make(map[VFS]bool, len(generated))

	for _, out := range generated {
		present[out] = true
	}

	ordered := make([]VFS, 0, len(declared))
	seen := make(map[VFS]bool, len(declared))

	for _, out := range declared {
		if present[out] && !seen[out] {
			ordered = append(ordered, out)
			seen[out] = true
		}
	}

	for i, next := 0, 0; i < len(generated); i++ {
		if seen[generated[i]] {
			generated[i] = ordered[next]
			next++
		}
	}

	return generated
}
