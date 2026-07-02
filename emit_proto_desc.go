package main

import (
	"crypto/md5"
	enchex "encoding/hex"
	"path/filepath"
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
	if !peerYaMakeExists(ctx.fs, peerPath) {
		return false
	}

	for _, s := range moduleStmts(ctx, peerPath) {
		if m, ok := s.(*ModuleStmt); ok {
			return m.Name == tokProtoLibrary
		}
	}

	return false
}

func descPeerClosure(ctx *GenCtx, instance ModuleInstance, peerdirs []STR, injectBuiltins bool) DescPeerSpan {
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

	if injectBuiltins && instance.Path.rel() != protosFromProtocPeer {
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
	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
	cppOutRoot := protoCPPOutRoot(d)

	var protoSearchPaths []VFS

	if cppOutRoot != "" {
		protoSearchPaths = []VFS{source(cppOutRoot)}
	}

	mid := descProtocIncludes(span.includes, cppOutRoot)
	scanCfg := protoWalkInputs(ctx.parsers, protoSearchPaths, instance.Path.rel())
	scanner := e.scanner
	hash := moddirHash(instance.Path.rel())

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

	for _, src := range d.srcs {
		if !extIsProto(src.string()) {
			continue
		}

		srcRel := src.string()
		protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcRel)
		protoVFS := source(protoRelPath)
		imports := walkClosureTail(scanner, protoVFS, scanCfg)
		descOut := build(descProtoOutputRel(instance.Path.rel(), srcRel, protoRelPath))
		rawprotoOut := build(protoRelPath, ".", hash, ".rawproto")

		ref := emitProtoDescProducer(ctx, instance, protoRelPath, descOut, rawprotoOut,
			protocLDRef, protocBinary, mid, imports)

		producerRefs = append(producerRefs, ref)
		descOutputs = append(descOutputs, descOut)
		rawprotoOutputs = append(rawprotoOutputs, rawprotoOut)

		addSourceInput(descRawprotoWrapperVFS)
		addSourceInput(protoVFS)

		for _, im := range imports {
			addSourceInput(im)
		}
	}

	prj := realPrjName(instance.Path.rel())
	selfProtodesc := build(instance.Path.rel(), "/", prj, ".self.protodesc")
	protosrc := build(instance.Path.rel(), "/", prj, ".protosrc")
	mergeRef := emitDescProtoMerge(ctx, instance, selfProtodesc, protosrc, descOutputs, rawprotoOutputs, producerSourceInputs, producerRefs)
	closure := append(span.peers, DescProtoPeer{SelfProtodesc: selfProtodesc, MergeRef: mergeRef})
	selfPath := selfProtodesc

	return &ModuleEmitResult{
		ARRef:        mergeRef,
		ARPath:       &selfPath,
		DescClosure:  closure,
		ProtoInclude: dedup(protoNamespaceContribs(d), span.includes),
	}
}

func protoNamespaceContribs(d *ModuleData) []VFS {
	var own []VFS

	if d.protoNamespace != nil {
		own = []VFS{source(filepath.ToSlash(filepath.Clean(d.protoNamespace.string())))}
	}

	return append(own, d.protoAddInclGlobal...)
}

func descProtocIncludes(peerProtoAddIncl []VFS, cppOutRoot string) []STR {
	out := make([]STR, 0, 8+len(peerProtoAddIncl))

	out = append(out,
		internV("-I=./", cppOutRoot),
		internV("-I=$(S)/", cppOutRoot),
		argIB2.str(),
		argIS3.str(),
	)

	if cppOutRoot != "" {
		out = append(out, internV("-I=$(S)/", cppOutRoot))
	}

	for _, p := range peerProtoAddIncl {
		out = append(out, internV("-I=", p.string()))
	}

	out = append(out,
		argIB2.str(),
		argISContribLibsProtobufSrc.str(),
		strIncludeSourceInfo,
	)

	return out
}

func emitProtoDescProducer(ctx *GenCtx, instance ModuleInstance, protoRelPath string,
	descOut, rawprotoOut VFS, protocLDRef NodeRef, protocBinary VFS, mid []STR, imports []VFS) NodeRef {
	na := ctx.emit.nodeArenas()

	head := na.strList(
		wrapccPython3STR,
		descRawprotoWrapperVFS.str(),
		strDescOutput,
		descOut.str(),
		strRawprotoOutput,
		rawprotoOut.str(),
		strProtoFile,
		internStr(protoRelPath),
		arg2.str(),
		protocBinary.str(),
	)

	cmdArgs := na.chunkList(head, mid)
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	inputs := []VFS{
		protocBinary,
		source(protoRelPath),
		descRawprotoWrapperVFS,
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: strS,
			Env: env}),
		Env:            env,
		Inputs:         na.inputList(inputs, imports),
		KV:             &protoDescKV,
		Outputs:        na.vfsList(descOut, rawprotoOut),
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(protocLDRef),
		Resources:      usesPython3,
	}

	return ctx.emit.emit(node)
}

func emitDescProtoMerge(ctx *GenCtx, instance ModuleInstance, selfProtodesc, protosrc VFS,
	descOutputs, rawprotoOutputs, producerSourceInputs []VFS, producerRefs []NodeRef) NodeRef {
	na := ctx.emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	merge := make([]STR, 0, 3+len(descOutputs))

	merge = append(merge, wrapccPython3STR, mergeFilesVFS.str(), selfProtodesc.str())

	for _, d := range descOutputs {
		merge = append(merge, d.str())
	}

	collect := make([]STR, 0, 4+len(rawprotoOutputs))

	collect = append(collect, wrapccPython3STR, collectRawprotoVFS.str(), strOutput, protosrc.str())

	for _, r := range rawprotoOutputs {
		collect = append(collect, internStr(r.rel()))
	}

	inputs := concat(descOutputs, rawprotoOutputs, producerSourceInputs)

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(
			Cmd{CmdArgs: na.chunkList(merge), Env: env},
			Cmd{CmdArgs: na.chunkList(collect), Cwd: strB, Env: env},
		),
		Env:          env,
		Inputs:       na.inputList(inputs, ctx.scripts[mergeFilesVFS], ctx.scripts[collectRawprotoVFS]),
		KV:           &protoDescKV,
		Outputs:      na.vfsList(selfProtodesc, protosrc),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      producerRefs,
		Resources:    usesPython3,
	}

	return ctx.emit.emit(node)
}

func (e *EmitContext) emitProtoDescriptions() *ModuleEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	closure := descPeerClosure(ctx, instance, d.peerdirs, false).peers
	na := ctx.emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	prj := realPrjName(instance.Path.rel())
	protodesc := build(instance.Path.rel(), "/", prj, ".protodesc")
	tar := build(instance.Path.rel(), "/", prj, ".tar")
	merge := make([]STR, 0, 3+len(closure))

	merge = append(merge, wrapccPython3STR, mergeFilesVFS.str(), protodesc.str())

	collect := make([]STR, 0, 4+len(closure))

	collect = append(collect, wrapccPython3STR, mergeProtosrcVFS.str(), strOutput, tar.str())

	inputs := make([]VFS, 0, len(closure))
	deps := make([]NodeRef, 0, len(closure))

	for _, p := range closure {
		merge = append(merge, p.SelfProtodesc.str())
		collect = append(collect, internStr(p.SelfProtodesc.rel()))
		inputs = append(inputs, p.SelfProtodesc)
		deps = append(deps, p.MergeRef)
	}

	if sbomActive(ctx, instance) {
		if pyRef, pyPath := pythonToolchainSbomComponent(ctx, instance.Platform); pyRef != nil {
			inputs = append(inputs, *pyPath)
			deps = append(deps, *pyRef)
		}
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(
			Cmd{CmdArgs: na.chunkList(merge), Env: env},
			Cmd{CmdArgs: na.chunkList(collect), Cwd: strB, Env: env},
		),
		Env:          env,
		Inputs:       na.inputList(inputs, ctx.scripts[mergeFilesVFS], ctx.scripts[mergeProtosrcVFS]),
		KV:           &protoDescKV,
		Outputs:      na.vfsList(protodesc, tar),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      deps,
		Resources:    usesPython3,
	}

	mergeRef := ctx.emit.emit(node)
	primary := protodesc

	return &ModuleEmitResult{
		ARRef:       mergeRef,
		ARPath:      &primary,
		DescClosure: closure,
	}
}
