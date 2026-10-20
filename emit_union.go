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

	for _, p := range e.d.peerdirs {
		res := genModule(e.ctx, ModuleInstance{Path: source(p.string()), Kind: KindLib, Platform: e.instance.Platform})

		peers = append(peers, res.JavaClosure...)
		globals = append(globals, res.ResourceGlobalClosure...)
		e.globalSources = append(e.globalSources, res.GlobalSources...)
	}

	globals = e.scriptModuleResources(globals, "build/platform/python/ymake_python3")
	e.emitFromSandboxes()
	e.emitBundles()
	e.globalSources = dedup(e.globalSources, e.emitSdcMetas())
	e.emitDeclaredProducers(nil)

	generated := dedup(e.outs)
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
	res := e.scriptModuleResult(ref, out, dedup(peers), globals)

	res.GlobalSources = e.globalSources

	return res
}
