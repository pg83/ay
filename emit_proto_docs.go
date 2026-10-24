package main

import "strings"

var protoDocsKV = KV{P: pkPD, PC: pcYellow}

func (e *EmitContext) emitDocsProtoSubmodule() *ModuleEmitResult {
	ctx, d, instance := e.ctx, e.d, e.instance
	na := ctx.na

	if d.moduleVariants&(1<<LangDocsProto) == 0 {
		return &ModuleEmitResult{ModuleStmtName: tokProtoLibrary}
	}

	var includes, peers []VFS

	peerDirs := []string{}

	if d.useCommonGoogleAPIs {
		peerDirs = append(peerDirs, "contrib/libs/googleapis-common-protos")
	}

	if d.needGoogleProtoPeerdirs {
		peerDirs = append(peerDirs, protosFromProtocPeer)
	}

	peerDirs = append(peerDirs, anyStrings(d.peerdirs)...)

	for _, peer := range peerDirs {
		if peer == instance.Path.relString() || !isProtoLibraryPeer(ctx, peer) {
			continue
		}

		res := e.genPeerModule(ModuleInstance{Path: source(peer), Language: LangDocsProto, Kind: KindLib, Platform: instance.Platform})

		includes = append(includes, res.ProtoInclude...)
		peers = append(peers, res.DocsClosure...)
	}

	includes = dedup(includes)

	protoIncludes := dedup(d.protoAddInclGlobal, includes)

	e.scriptModuleResources(nil, "build/platform/python/ymake_python3")
	d.scanCtx = newModuleScanContext(ctx, instance, d, nil, nil, nil, protoIncludes)

	protocRef, protoc := ctx.tool(argContribToolsProtoc)
	docRef, docTool := ctx.tool(argVendorGithubComPseudomutoProtocGenDocCmdProtocGenDoc)
	wrapper := buildScriptsDocsProtoWrapperPy
	template := buildScriptsDocsProtoMarkdownTmpl
	namespace := protoCPPOutRoot(d)

	var generated []VFS

	for _, meta := range d.srcs {
		if !extIsProto(meta.Source.string()) {
			continue
		}

		rel := e.protoSourceRel(meta.Source.string()).string()
		src := source(rel)
		out := build(strings.TrimSuffix(rel, ".proto"), ".md")

		args := []ANY{
			d.tc.Python3.any(), wrapper.any(), strDocsOutput.any(), out.any(),
			strTemplate.any(), template.any(), arg2.any(), protoc.any(),
			internV("-I=./", namespace).any(), internV("-I=$(S)/", namespace).any(), argIB2.any(), argIS3.any(),
		}

		for _, inc := range protoIncludes {
			args = append(args, internV("-I=", inc.string()).any())
		}

		args = append(args, argIB2.any(), argISContribLibsProtobufSrc.any())
		args = append(args, d.protocFlags...)
		args = append(args, internV("--plugin=protoc-gen-doc=", docTool.string()).any(), internStr(rel).any())

		view := e.scanner.walkClosure(src, d.scanCtx, scanDomainProto)
		inputs := view.bucketList()

		ref := e.emitNode(Node{
			Platform: instance.Platform,
			Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...)), Cwd: srcRootDirVFS, Env: envVarsVCS}),
			Env:      envVarsVCS, Inputs: na.inputList(na.vfsList(src, wrapper, template, protoc, docTool), inputs...),
			BuildFromInputs: na.inputList(na.vfsList(src, wrapper, template), inputs...),
			InputClosures:   []Closure{view}, Outputs: na.vfsList(out), KV: &protoDocsKV, ForeignDepRefs: na.refList(protocRef, docRef), Resources: usesPython3,
		})

		e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: ref})
		generated = append(generated, out)
	}

	d.docs = &DocsData{dirs: []string{"__dummy_dir__"}}

	out := build(instance.Path.relString(), "/", realPrjName(instance.Path.relString()), ".docslib")
	ref := e.docsLibrary(out, generated, false)

	return &ModuleEmitResult{
		ARRef: ref, ARPath: ptr(out), LDRef: ref, LDPath: ptr(out),
		ProtoInclude: protoIncludes, DocsClosure: append(peers, out), ModuleStmtName: tokProtoLibrary,
	}
}
