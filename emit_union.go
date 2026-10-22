package main

import "strings"

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

		d.copyFiles = append(d.copyFiles, CopyFileEntry{Src: name, Dst: name})
	}
}

func (e *EmitContext) genUnion() *ModuleEmitResult {
	var peers []VFS
	var globals []ResourceDecl
	var peerIncludes, peerProtoIncludes []VFS

	for _, p := range e.d.peerdirs {
		peer := e.derivePeerInstance(p.string())

		peer.Demand = demandNone

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
	e.emitFromSandboxes()
	e.emitBundles()
	e.globalSources = dedup(e.globalSources, e.emitSdcMetas())
	e.emitDeclaredProducers(nil)

	generated := dedup(e.outs)

	generated = orderSdcInstallOutputs(generated, e.d.sdcInstallOutputs)

	module := e.instance.Path.relString()
	name := realPrjName(module)

	if len(e.d.moduleStmt.Args) > 0 {
		name = e.d.moduleStmt.Args[0].string()
	}

	out := build(module, "/", name, ".pkg.fake")
	na := e.ctx.na

	node := Node{
		Module: true, Platform: e.instance.Platform, KV: &unionKV,
		KVExts: []KVExt{{Key: "package", Val: "UNION"}}, Env: envVarsVCS,
		Inputs:  na.inputList(na.vfsList(generated...)),
		Outputs: na.vfsList(append([]VFS{out}, generated...)...), Resources: usesPython3,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(e.d.tc.Python3.any(), strC2.any(), internStr("open('"+out.string()+"','w').close()").any())), Env: envVarsVCS}),
	}

	e.appendSdcMeta(&node, name)

	ref := e.emitLazyModuleNode(node, peers, out)

	e.codegen.mustInfo(out).LateOutputs = e.ctx.na.vfsList(generated...)

	res := e.scriptModuleResult(ref, out, dedup(peers), globals)

	res.GlobalSources = e.globalSources

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
