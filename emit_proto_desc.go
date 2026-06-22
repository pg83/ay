package main

import (
	"crypto/md5"
	enchex "encoding/hex"
	"path/filepath"
	"strings"
)

// Proto-description (PD) producers:
//
//   - per .proto SRC, the DESC_PROTO submodule runs protoc → <proto>.desc + the hashed .rawproto;
//   - it merges those into <realprjname>.self.protodesc and .protosrc;
//   - a PROTO_DESCRIPTIONS module merges its peer closure's .self.protodesc into
//     <realprjname>.protodesc (what a BUNDLE moves) and .tar.

var (

	// protosFromProtocPeer is the builtin-proto peer NEED_GOOGLE_PROTO_PEERDIRS injects
	// into a DESC_PROTO submodule.
	protosFromProtocPeer = "contrib/libs/protobuf/builtin_proto/protos_from_protoc"
)

// DescProtoPeer names one DESC_PROTO submodule in a description closure: the merge
// node producing its .self.protodesc and that output's path.
type DescProtoPeer struct {
	SelfProtodesc VFS
	MergeRef      NodeRef
}

// DescPeerSpan is the result of resolving a DESC_PROTO module's peer chain: the
// merge-node closure plus the ordered _PROTO__INCLUDE set feeding this module's
// descriptor protoc command.
type DescPeerSpan struct {
	peers    []DescProtoPeer
	includes []VFS
}

// realPrjName is the REALPRJNAME for a module dir: the last ≤3 path components joined by "-".
func realPrjName(moduleDir string) string {
	return strings.TrimSuffix(archiveNameWithPrefix(moduleDir, ""), ".a")
}

// moddirHash is ${hash:MODDIR}: the lowercase md5 hex of the module dir.
func moddirHash(moduleDir string) string {
	sum := md5.Sum([]byte(moduleDir))

	return enchex.EncodeToString(sum[:])
}

// isProtoLibraryPeer reports whether the peer ya.make opens a PROTO_LIBRARY (the
// only module type with a DESC_PROTO submodule).
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

// descPeerClosure resolves a module's DESC_PROTO peer chain (builtin protos_from_protoc
// first, then declared proto-library PEERDIRs) in post-order, deduped by .self.protodesc path.
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

		// Aggregate the peer's _PROTO__INCLUDE set in entry order so the descriptor protoc
		// command renders the same span the cpp/py commands get.
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

// descProtoOutputRel computes the .desc output for a DESC_PROTO producer via the
// output-name policy (composeSrcDirOutputRel). resolvedRel is the physical path
// protoSourceRelPath produced.
func descProtoOutputRel(instancePath, srcRel, resolvedRel string) string {
	_ = srcRel

	return instancePath + "/" + composeSrcDirOutputRel(instancePath, resolvedRel) + ".desc"
}

// emitDescProtoSubmodule emits the DESC_PROTO submodule of a PROTO_LIBRARY: a PD
// producer per .proto SRC plus the merge node. The result exposes the DescClosure
// (peer closure with itself appended) and the merge node as the primary output.
func emitDescProtoSubmodule(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *ModuleEmitResult {
	span := descPeerClosure(ctx, instance, d.peerdirs, d.needGoogleProtoPeerdirs)

	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)

	cppOutRoot := protoCPPOutRoot(d)

	// The own PROTO_NAMESPACE is the import-closure search base, keyed per module not per .proto.
	var protoSearchPaths []VFS

	if cppOutRoot != "" {
		protoSearchPaths = []VFS{source(cppOutRoot)}
	}

	// _PROTO__INCLUDE peer band (encounter order); own namespace renders structurally as cppOutRoot.
	mid := descProtocIncludes(span.includes, cppOutRoot)

	// Module-stable -I set: build it once, not per source.
	scanCfg := protoWalkInputs(ctx.parsers, protoSearchPaths, instance.Path.rel()).ScanCfg
	scanner := ctx.scannerFor(instance)

	hash := moddirHash(instance.Path.rel())

	var producerRefs []NodeRef
	var descOutputs []VFS
	var rawprotoOutputs []VFS

	// Flatten each producer's source/script closure onto the merge node as direct
	// inputs, collecting the deduped union while iterating.
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
		if !strings.HasSuffix(src.string(), ".proto") {
			continue
		}

		srcRel := src.string()
		protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcRel)
		protoVFS := source(protoRelPath)
		imports := walkClosureTail(scanner, protoVFS, scanCfg)

		descOut := build(descProtoOutputRel(instance.Path.rel(), srcRel, protoRelPath))
		rawprotoOut := build(protoRelPath + "." + hash + ".rawproto")

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
	selfProtodesc := build(instance.Path.rel() + "/" + prj + ".self.protodesc")
	protosrc := build(instance.Path.rel() + "/" + prj + ".protosrc")

	mergeRef := emitDescProtoMerge(ctx, instance, selfProtodesc, protosrc, descOutputs, rawprotoOutputs, producerSourceInputs, producerRefs)

	closure := append(span.peers, DescProtoPeer{SelfProtodesc: selfProtodesc, MergeRef: mergeRef})

	selfPath := selfProtodesc

	// Own PROTO_NAMESPACE contribution unioned with the peers', so a parent DESC
	// submodule that PEERDIRs this one aggregates transitively.
	return &ModuleEmitResult{
		ARRef:        mergeRef,
		ARPath:       &selfPath,
		DescClosure:  closure,
		ProtoInclude: dedupVFS(protoNamespaceContribs(d), span.includes),
	}
}

// protoNamespaceContribs builds a module's own _PROTO__INCLUDE contribution: the
// PROTO_NAMESPACE entry (bare and GLOBAL ride identically) plus parsed PROTO_ADDINCL GLOBAL paths.
func protoNamespaceContribs(d *ModuleData) []VFS {
	var own []VFS

	if d.protoNamespace != nil {
		own = []VFS{source(filepath.ToSlash(filepath.Clean(d.protoNamespace.string())))}
	}

	return append(own, d.protoAddInclGlobal...)
}

// descProtocIncludes builds the protoc -I span of a PD command: structural -I=$(B)
// -I=$(S), own cppOutRoot, then the peer PROTO_NAMESPACE span.
func descProtocIncludes(peerProtoAddIncl []VFS, cppOutRoot string) []STR {
	out := make([]STR, 0, 8+len(peerProtoAddIncl))
	out = append(out,
		internStr("-I=./"+cppOutRoot),
		internStr("-I=$(S)/"+cppOutRoot),
		argIB2.str(),
		argIS3.str(),
	)

	if cppOutRoot != "" {
		out = append(out, internStr("-I=$(S)/"+cppOutRoot))
	}

	for _, p := range peerProtoAddIncl {
		out = append(out, internStr("-I="+p.string()))
	}

	out = append(out,
		argIB2.str(),
		argISContribLibsProtobufSrc.str(),
		strIncludeSourceInfo,
	)

	return out
}

// emitProtoDescProducer emits one per-proto PD producer producing <proto>.desc and the
// hashed <proto>.rawproto.
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
		Env:              env,
		Inputs:           na.inputList(inputs, imports),
		KV:               KV{P: pkPD, PC: pcLightCyan},
		Outputs:          na.vfsList(descOut, rawprotoOut),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel(), ModuleTag: strDescProtoTag},
		ForeignDepRefs:   depRefs(protocLDRef),
		Resources:        usesPython3,
	}

	return ctx.emit.emit(node)
}

// emitDescProtoMerge emits the DESC_PROTO submodule merge node: the .desc into
// .self.protodesc, then the .rawproto into .protosrc.
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

	inputs := make([]VFS, 0, len(descOutputs)+len(rawprotoOutputs)+len(producerSourceInputs))
	inputs = append(inputs, descOutputs...)
	inputs = append(inputs, rawprotoOutputs...)
	inputs = append(inputs, producerSourceInputs...)

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(
			Cmd{CmdArgs: na.chunkList(merge), Env: env},
			Cmd{CmdArgs: na.chunkList(collect), Cwd: strB, Env: env},
		),
		Env:              env,
		Inputs:           na.inputList(inputs, ctx.scripts[mergeFilesVFS], ctx.scripts[collectRawprotoVFS]),
		KV:               KV{P: pkPD, PC: pcLightCyan},
		Outputs:          na.vfsList(selfProtodesc, protosrc),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel(), ModuleLang: mlDescProto, ModuleType: mtLib, ModuleTag: strDescProtoTag},
		DepRefs:          producerRefs,
		Resources:        usesPython3,
	}

	return ctx.emit.emit(node)
}

// emitProtoDescriptions emits a PROTO_DESCRIPTIONS module: merges its peer closure's
// .self.protodesc into <realprjname>.protodesc (backing a BUNDLE move), then into .tar.
func emitProtoDescriptions(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *ModuleEmitResult {
	closure := descPeerClosure(ctx, instance, d.peerdirs, false).peers

	na := ctx.emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	prj := realPrjName(instance.Path.rel())
	protodesc := build(instance.Path.rel() + "/" + prj + ".protodesc")
	tar := build(instance.Path.rel() + "/" + prj + ".tar")

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

	// PROTO_DESCRIPTIONS keeps SBOM info (unlike DESC_PROTO), so it materializes the python
	// toolchain peer's toolchain.component.sbom as a direct input+dep.
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
		Env:              env,
		Inputs:           na.inputList(inputs, ctx.scripts[mergeFilesVFS], ctx.scripts[mergeProtosrcVFS]),
		KV:               KV{P: pkPD, PC: pcLightCyan},
		Outputs:          na.vfsList(protodesc, tar),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel(), ModuleLang: mlProtoDescriptions, ModuleType: mtLib},
		DepRefs:          deps,
		Resources:        usesPython3,
	}

	mergeRef := ctx.emit.emit(node)
	primary := protodesc

	return &ModuleEmitResult{
		ARRef:       mergeRef,
		ARPath:      &primary,
		DescClosure: closure,
	}
}
