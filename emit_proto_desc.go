package main

import (
	"crypto/md5"
	enchex "encoding/hex"
	"strings"
)

var (
	protosFromProtocPeer = "contrib/libs/protobuf/builtin_proto/protos_from_protoc"
	protoDescKV          = KV{P: pkPD, PC: pcLightCyan}
)

type DescProtoPeer struct {
	SelfProtodesc VFS
	MergeRef      NodeRef
}

type DescPeerSpan struct {
	peers    []DescProtoPeer
	includes []VFS
}

func realPrjName(moduleDir string) string {
	return strings.TrimSuffix(archiveNameWithPrefix(moduleDir, ""), ".a")
}

func moddirHash(moduleDir string) string {
	sum := md5.Sum([]byte(moduleDir))

	return enchex.EncodeToString(sum[:])
}

func isProtoLibraryPeer(ctx *GenCtx, peerPath string) bool {
	if !peerYaMakeExists(ctx.fs, dirKey(peerPath).source()) {
		return false
	}

	for _, s := range moduleStmts(ctx, peerPath) {
		if m, ok := s.(*ModuleStmt); ok {
			return m.Name == tokProtoLibrary
		}
	}

	return false
}

func descPeerClosure(ctx *GenCtx, instance ModuleInstance, peerdirs []ANY, injectBuiltins bool) DescPeerSpan {
	var span DescPeerSpan

	seen := make(map[VFS]struct{})
	includesSeen := make(map[VFS]struct{})

	add := func(peers []DescProtoPeer) {
		for _, p := range peers {
			if _, dup := seen[p.SelfProtodesc]; dup {
				continue
			}

			seen[p.SelfProtodesc] = struct{}{}
			span.peers = append(span.peers, p)
		}
	}

	enter := func(peerPath string) {
		if !isProtoLibraryPeer(ctx, peerPath) {
			return
		}

		peerInstance := ModuleInstance{
			Path:     source(peerPath),
			Kind:     KindLib,
			Language: LangDescProto,
			Platform: instance.Platform,
		}

		res := genModule(ctx, peerInstance)

		add(res.DescClosure)

		for _, g := range res.ProtoInclude {
			if _, dup := includesSeen[g]; dup {
				continue
			}

			includesSeen[g] = struct{}{}
			span.includes = append(span.includes, g)
		}
	}

	if injectBuiltins && instance.Path.relString() != protosFromProtocPeer {
		enter(protosFromProtocPeer)
	}

	for _, p := range peerdirs {
		enter(p.string())
	}

	return span
}

func descProtoOutputRel(instancePath, srcRel, resolvedRel string) string {
	_ = srcRel

	return instancePath + "/" + composeSrcDirOutputRel(instancePath, resolvedRel) + ".desc"
}

func (e *EmitContext) emitDescProtoSubmodule() *ModuleEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	span := descPeerClosure(ctx, instance, d.peerdirs, d.needGoogleProtoPeerdirs)
	protoInclude := dedup(protoNamespaceContribs(d), span.includes)
	d.scanCtx = newModuleScanContext(ctx, instance, d, dedup(d.addIncl, d.addInclGlobal), nil, nil, protoInclude)
	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
	cppOutRoot := protoCPPOutRoot(d)

	mid := descProtocIncludes(ctx.na, span.includes, cppOutRoot)
	hash := moddirHash(instance.Path.relString())

	var producerRefs []NodeRef
	var descOutputs []VFS
	var rawprotoOutputs []VFS

	var producerSourceInputs []VFS

	sourceInputSeen := make(map[VFS]struct{})

	addSourceInput := func(v VFS) {
		if _, dup := sourceInputSeen[v]; dup {
			return
		}

		sourceInputSeen[v] = struct{}{}
		producerSourceInputs = append(producerSourceInputs, v)
	}

	for _, meta := range d.srcs {
		src := meta.Source

		if !extIsProto(src.string()) {
			continue
		}

		srcRel := src.string()
		protoRelPath := e.protoSourceRelPath(srcRel)
		protoVFS := source(protoRelPath)
		imports := e.scanner.walkClosure(protoVFS, d.scanCtx, scanDomainProto)
		descOut := build(descProtoOutputRel(instance.Path.relString(), srcRel, protoRelPath))
		rawprotoOut := build(protoRelPath, ".", hash, ".rawproto")

		ref := e.emitProtoDescProducer(protoRelPath, descOut, rawprotoOut,
			protocLDRef, protocBinary, mid, imports)

		producerRefs = append(producerRefs, ref)
		descOutputs = append(descOutputs, descOut)
		rawprotoOutputs = append(rawprotoOutputs, rawprotoOut)

		addSourceInput(descRawprotoWrapperVFS)
		addSourceInput(protoVFS)

		for _, bucket := range imports.bucketList() {
			for _, im := range bucket {
				addSourceInput(im)
			}
		}
	}

	prj := realPrjName(instance.Path.relString())
	selfProtodesc := build(instance.Path.relString(), "/", prj, ".self.protodesc")
	protosrc := build(instance.Path.relString(), "/", prj, ".protosrc")
	mergeRef := e.emitDescProtoMerge(selfProtodesc, protosrc, descOutputs, rawprotoOutputs, producerSourceInputs, producerRefs)
	closure := append(span.peers, DescProtoPeer{SelfProtodesc: selfProtodesc, MergeRef: mergeRef})
	selfPath := selfProtodesc

	return &ModuleEmitResult{
		ARRef:        mergeRef,
		ARPath:       &selfPath,
		DescClosure:  closure,
		ProtoInclude: protoInclude,
	}
}

func protoNamespaceContribs(d *ModuleData) []VFS {
	var own []VFS

	if d.protoNamespace != nil {
		own = []VFS{sourceClean(d.protoNamespace.string())}
	}

	return append(own, d.protoAddInclGlobal...)
}

func descProtocIncludes(na *NodeArenas, peerProtoAddIncl []VFS, cppOutRoot string) []ANY {
	out := na.anys.alloc(8 + len(peerProtoAddIncl))[:0]

	out = append(out,
		internV("-I=./", cppOutRoot).any(),
		internV("-I=$(S)/", cppOutRoot).any(),
		argIB2.any(),
		argIS3.any(),
	)

	if cppOutRoot != "" {
		out = append(out, internV("-I=$(S)/", cppOutRoot).any())
	}

	for _, p := range peerProtoAddIncl {
		out = append(out, internV("-I=", p.prefix(), p.relString()).any())
	}

	out = append(out,
		argIB2.any(),
		argISContribLibsProtobufSrc.any(),
		strIncludeSourceInfo.any(),
	)

	na.anys.commit(len(out))

	return out[:len(out):len(out)]
}

func (e *EmitContext) emitProtoDescProducer(protoRelPath string,
	descOut, rawprotoOut VFS, protocLDRef NodeRef, protocBinary VFS, mid []ANY, imports Closure) NodeRef {
	na := e.ctx.na

	head := na.anyList(
		wrapccPython3STR.any(),
		descRawprotoWrapperVFS.any(),
		strDescOutput.any(),
		descOut.any(),
		strRawprotoOutput.any(),
		rawprotoOut.any(),
		strProtoFile.any(),
		internStr(protoRelPath).any(),
		arg2.any(),
		protocBinary.any(),
	)

	cmdArgs := na.chunkList(head, mid)
	env := envVarsVCS
	importBuckets := imports.bucketList()
	inputChunks := na.inputs.alloc(2 + len(importBuckets))[:2+len(importBuckets)]

	inputChunks[0] = na.vfsList(protocBinary)
	inputChunks[1] = na.vfsList(source(protoRelPath), descRawprotoWrapperVFS)
	copy(inputChunks[2:], importBuckets)
	na.inputs.commit(len(inputChunks))

	node := Node{
		Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: srcRootDirVFS,
			Env: env}),
		Env:            env,
		Inputs:         InputChunks(inputChunks[:len(inputChunks):len(inputChunks)]),
		KV:             &protoDescKV,
		Outputs:        na.vfsList(descOut, rawprotoOut),
		ForeignDepRefs: na.refList(protocLDRef),
		Resources:      usesPython3,
	}

	return e.emitNode(node)
}

func (e *EmitContext) emitDescProtoMerge(selfProtodesc, protosrc VFS,
	descOutputs, rawprotoOutputs, producerSourceInputs []VFS, producerRefs []NodeRef) NodeRef {
	ctx := e.ctx
	na := ctx.na
	env := envVarsVCS
	merge := na.anys.alloc(3 + len(descOutputs))[:0]

	merge = append(merge, wrapccPython3STR.any(), mergeFilesVFS.any(), selfProtodesc.any())

	for _, d := range descOutputs {
		merge = append(merge, d.any())
	}

	na.anys.commit(len(merge))

	merge = merge[:len(merge):len(merge)]

	collect := na.anys.alloc(4 + len(rawprotoOutputs))[:0]

	collect = append(collect, wrapccPython3STR.any(), collectRawprotoVFS.any(), strOutput.any(), protosrc.any())

	for _, r := range rawprotoOutputs {
		collect = append(collect, r.rel().any())
	}

	na.anys.commit(len(collect))

	collect = collect[:len(collect):len(collect)]

	buildInputs := na.vfs.alloc(len(descOutputs) + len(rawprotoOutputs))
	ni := copy(buildInputs, descOutputs)

	ni += copy(buildInputs[ni:], rawprotoOutputs)
	na.vfs.commit(ni)

	buildInputs = buildInputs[:ni:ni]

	node := Node{
		Platform: e.instance.Platform,
		Cmds: na.cmdList(
			Cmd{CmdArgs: na.chunkList(merge), Env: env},
			Cmd{CmdArgs: na.chunkList(collect), Cwd: bldRootDirVFS, Env: env},
		),
		Env:       env,
		Inputs:    na.inputList(buildInputs, producerSourceInputs, ctx.scripts[mergeFilesVFS.rel()], ctx.scripts[collectRawprotoVFS.rel()]),
		KV:        &protoDescKV,
		Outputs:   na.vfsList(selfProtodesc, protosrc),
		DepRefs:   na.noderefs.list(producerRefs...),
		Resources: usesPython3,
	}

	return e.emitNode(node)
}

func (e *EmitContext) emitProtoDescriptions() *ModuleEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	closure := descPeerClosure(ctx, instance, d.peerdirs, false).peers
	na := ctx.emit.nodeArenas()
	env := envVarsVCS
	prj := realPrjName(instance.Path.relString())
	protodesc := build(instance.Path.relString(), "/", prj, ".protodesc")
	tar := build(instance.Path.relString(), "/", prj, ".tar")
	merge := na.anys.alloc(3 + len(closure))[:0]

	merge = append(merge, wrapccPython3STR.any(), mergeFilesVFS.any(), protodesc.any())

	for _, p := range closure {
		merge = append(merge, p.SelfProtodesc.any())
	}

	na.anys.commit(len(merge))

	merge = merge[:len(merge):len(merge)]

	collect := na.anys.alloc(4 + len(closure))[:0]

	collect = append(collect, wrapccPython3STR.any(), mergeProtosrcVFS.any(), strOutput.any(), tar.any())

	for _, p := range closure {
		collect = append(collect, p.SelfProtodesc.rel().any())
	}

	na.anys.commit(len(collect))

	collect = collect[:len(collect):len(collect)]

	sbomRef, sbomPath := (*NodeRef)(nil), (*VFS)(nil)

	if sbomActive(ctx, instance) {
		sbomRef, sbomPath = pythonToolchainSbomComponent(ctx, instance.Platform)
	}

	inputs := na.vfs.alloc(len(closure) + 1)[:0]
	deps := na.noderefs.alloc(len(closure) + 1)[:0]

	for _, p := range closure {
		inputs = append(inputs, p.SelfProtodesc)
		deps = append(deps, p.MergeRef)
	}

	if sbomRef != nil {
		inputs = append(inputs, *sbomPath)
		deps = append(deps, *sbomRef)
	}

	na.vfs.commit(len(inputs))
	na.noderefs.commit(len(deps))

	inputs = inputs[:len(inputs):len(inputs)]
	deps = deps[:len(deps):len(deps)]

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(
			Cmd{CmdArgs: na.chunkList(merge), Env: env},
			Cmd{CmdArgs: na.chunkList(collect), Cwd: bldRootDirVFS, Env: env},
		),
		Env:       env,
		Inputs:    na.inputList(inputs, ctx.scripts[mergeFilesVFS.rel()], ctx.scripts[mergeProtosrcVFS.rel()]),
		KV:        &protoDescKV,
		Outputs:   na.vfsList(protodesc, tar),
		DepRefs:   deps,
		Resources: usesPython3,
	}

	mergeRef := e.emitNode(node)
	primary := protodesc

	return &ModuleEmitResult{
		ARRef:       mergeRef,
		ARPath:      &primary,
		DescClosure: closure,
	}
}
