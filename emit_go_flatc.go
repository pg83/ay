package main

import "strings"

var goFlatcKV = KV{P: pkFG, PC: pcLightGreen}

type FbsNamespaceVar struct {
	Value STR
	Args  []ANY
}

func collectFbsNamespaceVars(ctx *GenCtx, own []ANY, peers []ResolvedPeer) []FbsNamespaceVar {
	var vars []FbsNamespaceVar

	dedupers.with(func(deduper *DeDuper) {
		for _, peer := range peers {
			for _, v := range peer.result.FbsNamespaces {
				if deduper.add(v.Value.strID()) {
					vars = append(vars, v)
				}
			}
		}
	})

	if len(own) > 0 {
		value := internStr(strings.Join(anyStrs(own), " "))

		vars = append(vars, FbsNamespaceVar{Value: value, Args: ctx.argSlices.internCopy(own)})
	}

	return vars
}

func fbsNamespaceArgs(na *NodeArenas, vars []FbsNamespaceVar) []ANY {
	count := 0

	for _, v := range vars {
		count += len(v.Args)
	}

	args := na.anys.alloc(count)[:0]

	for _, v := range vars {
		args = append(args, v.Args...)
	}

	na.anys.commit(len(args))

	return args[:len(args):len(args)]
}

func (e *EmitContext) emitGoFlatcSource(meta SrcMeta) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	src := e.moduleSourceVFS(meta.Source)
	toolRef, tool := ctx.tool(argContribLibsFlatbuffersFlatc)
	imports := e.scanner.walkClosure(src, d.scanCtx, scanDomainFlatc)
	module := instance.Path.relString()
	out := build(module, "/", strings.TrimSuffix(e.moduleSourceName(meta.Source), ".fbs"), ".fbs.gosrc")
	bindir := build(module)
	generated := build(module, "/_generated")
	postprocess := buildScriptsPostprocessGoFbsPy
	tarSources := buildScriptsTarSourcesPy
	head := na.anyList(tool.any(), strGo2.any(), argGenMutable.any(), strGoNamespace.any(), internStr(goPackageName(module, d)).any())
	tail := na.anyList(argDashO.any(), generated.any(), src.any())
	post := na.anyList(d.tc.Python3.any(), postprocess.any(), strArcadiaPrefix.any(), internStr(goArcPrefix).any(), strInputDir.any(), bindir.any(), strMap.any())
	tar := na.anyList(d.tc.Python3.any(), tarSources.any(), strFlat.any(), strInput.any(), generated.any(), strOutput.any(), out.any(), strExts.any(), strGo3.any())

	node := Node{
		Platform: instance.Platform, Env: envVarsVCS,
		Cmds: na.cmdList(
			Cmd{CmdArgs: na.chunkList(head, d.flatcFlags, flatcIOLeadArgs[:len(flatcIOLeadArgs)-1], e.peers.FlatcIncludeArgs, tail), Cwd: bldRootDirVFS, Env: envVarsVCS},
			Cmd{CmdArgs: na.chunkList(post, e.peers.FbsNamespaces), Env: envVarsVCS},
			Cmd{CmdArgs: na.chunkList(tar), Env: envVarsVCS},
		),
		Inputs: NodeInputs{files: na.inputList(na.vfsList(tool, src, postprocess, tarSources), ctx.scripts[postprocess.rel()], ctx.scripts[tarSources.rel()])},
		KV:     &goFlatcKV, KVExts: []KVExt{{Key: "tared_kind", Val: "nodir"}},
		ForeignDepRefs: na.refList(toolRef), Outputs: na.vfsList(out), Resources: usesPython3,
	}

	node.Inputs.files = append(node.Inputs.files, imports.bucketList()...)
	node.Inputs.roots = na.inputList(node.Inputs.files[0][1:], node.Inputs.files[1:]...)

	ref := e.emitNode(node)

	e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: ref})
	e.collectGoAutoSource(out, meta.Seq)
}
