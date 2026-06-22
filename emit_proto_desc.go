package main

import (
	"crypto/md5"
	enchex "encoding/hex"
	"path/filepath"
	"strings"
)

// Proto-description (PD) producers. Upstream (build/conf/proto.conf):
//
//   - per .proto SRC, the DESC_PROTO submodule runs _PROTO_DESC_RAWPROTO_CMD
//     (desc_rawproto_wrapper.py around protoc) → <proto>.desc + the hashed
//     <proto>.<md5(MODDIR)>.rawproto (kv p=PD, module_tag desc_proto);
//   - the DESC_PROTO submodule itself (_DESC_PROTO) merges those into
//     <realprjname>.self.protodesc (merge_files.py) and <realprjname>.protosrc
//     (collect_rawproto.py);
//   - a PROTO_DESCRIPTIONS module merges its DESC_PROTO peer closure's
//     .self.protodesc into <realprjname>.protodesc (merge_files.py) and
//     <realprjname>.tar (merge_protosrc.py); its .protodesc primary output is
//     what a BUNDLE(<dir>) moves.

var (

	// protosFromProtocPeer is the builtin-proto peer NEED_GOOGLE_PROTO_PEERDIRS
	// injects into a DESC_PROTO submodule (proto.conf: PEERDIR +=
	// contrib/libs/protobuf/builtin_proto/protos_from_protoc); it transitively
	// pulls in protos_from_protobuf.
	protosFromProtocPeer = "contrib/libs/protobuf/builtin_proto/protos_from_protoc"
)

// DescProtoPeer names one DESC_PROTO submodule in a description closure: the
// merge node that produces its .self.protodesc and that output's path.
type DescProtoPeer struct {
	SelfProtodesc VFS
	MergeRef      NodeRef
}

// DescPeerSpan is the result of resolving a DESC_PROTO module's peer chain: the
// merge-node closure (for the .protosrc/.protodesc merges) plus the transitive
// proto-include contributions the peers feed into this module's descriptor
// protoc command — the single ordered _PROTO__INCLUDE set, aggregated the same
// way the C++ proto path does (gen.go).
type DescPeerSpan struct {
	peers    []DescProtoPeer
	includes []VFS
}

// realPrjName is upstream's REALPRJNAME for a module dir: the last ≤3 path
// components joined by "-" (the same stem archiveNameWithPrefix builds).
func realPrjName(moduleDir string) string {
	return strings.TrimSuffix(archiveNameWithPrefix(moduleDir, ""), ".a")
}

// moddirHash is ${hash:MODDIR}: the lowercase md5 hex of the module dir.
func moddirHash(moduleDir string) string {
	sum := md5.Sum([]byte(moduleDir))

	return enchex.EncodeToString(sum[:])
}

// isProtoLibraryPeer reports whether the peer ya.make opens a PROTO_LIBRARY (the
// only module type with a DESC_PROTO submodule, so the only one a DESC_PROTO
// peer tag enters).
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

// descPeerClosure resolves a module's DESC_PROTO peer chain (the builtin
// protos_from_protoc first, then declared proto-library PEERDIRs), genModule-ing
// each as a LangDescProto instance and concatenating their closures in
// post-order, deduped by .self.protodesc path.
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

		// The peer reports its transitive PROTO_NAMESPACE contributions (own +
		// its own peers') as one ordered _PROTO__INCLUDE set; aggregate in entry
		// order so the descriptor protoc command renders the same span the cpp/py
		// proto commands get. genModule is already memoized for this (path, kind,
		// language, platform) — reading the cached result adds no new traversal.
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

// descProtoOutputRel is upstream's ${output;suf=.desc:File} for a DESC_PROTO
// producer: the .desc output goes through ymake's output-name policy (the same
// rule emit_cc.go applies via composeSrcDirOutputRel) — a module-local source
// nested in a subdirectory rebases to <module>/_/<sub>, a source reached through
// SRCDIR outside the module rebases with the relative ascent mapped to `__`
// segments, and a flat in-module source keeps its rootrel path. The paired
// .rawproto carries ${norel;output:File} (no rebasing) and stays at resolvedRel,
// so it is composed separately. srcRel is the SRCS spelling; resolvedRel is the
// physical path protoSourceRelPath produced.
func descProtoOutputRel(instancePath, srcRel, resolvedRel string) string {
	_ = srcRel

	return instancePath + "/" + composeSrcDirOutputRel(instancePath, resolvedRel) + ".desc"
}

// emitDescProtoSubmodule emits the DESC_PROTO submodule of a PROTO_LIBRARY: a PD
// producer per .proto SRC plus the .self.protodesc / .protosrc merge node. The
// returned result exposes this module's DescClosure (its DESC peer closure with
// itself appended) for a PROTO_DESCRIPTIONS consumer, and the merge node as the
// module's primary output.
func emitDescProtoSubmodule(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *ModuleEmitResult {
	span := descPeerClosure(ctx, instance, d.peerdirs, d.needGoogleProtoPeerdirs)

	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)

	cppOutRoot := protoCPPOutRoot(d)

	// The DESC submodule's own PROTO_NAMESPACE plus the protobuf runtime src is
	// the proto-import search base. The _PROTO__INCLUDE peer span is computed
	// from the DESC peer closure's reported PROTO_NAMESPACE contributions (read
	// from genModule results already memoized for this platform — no new
	// traversal, no memo poisoning); the import-closure search config stays
	// keyed on the own namespace, which is context-free per .proto.
	var protoSearchPaths []VFS

	if cppOutRoot != "" {
		protoSearchPaths = []VFS{source(cppOutRoot)}
	}

	// _PROTO__INCLUDE peer band: the single ordered proto-include set (encounter
	// order). Own namespace is rendered structurally as cppOutRoot, not here.
	mid := descProtocIncludes(span.includes, cppOutRoot)

	// The proto import-closure search config is module-stable (the -I set does
	// not depend on the individual .proto) — build it once, not per source.
	scanCfg := protoWalkInputs(ctx.parsers, protoSearchPaths, instance.Path.rel()).ScanCfg
	scanner := ctx.scannerFor(instance)

	hash := moddirHash(instance.Path.rel())

	var producerRefs []NodeRef
	var descOutputs []VFS
	var rawprotoOutputs []VFS

	// Upstream flattens each producer's source/script closure onto the merge
	// node as reference-only direct inputs: desc_rawproto_wrapper.py, every
	// declared source proto, and the parsed proto import closure. Collect the
	// deduped union while iterating the producers (the producer command inputs
	// stay unchanged — this only mirrors them on the merge node).
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

	// This module's own PROTO_NAMESPACE contribution, unioned with the peers' —
	// the same GLOBAL-vs-bare split the C++ proto path uses (gen.go), so a
	// parent DESC submodule that PEERDIRs this one aggregates transitively.
	return &ModuleEmitResult{
		ARRef:        mergeRef,
		ARPath:       &selfPath,
		DescClosure:  closure,
		ProtoInclude: dedupVFS(protoNamespaceContribs(d), span.includes),
	}
}

// protoNamespaceContribs builds a module's own _PROTO__INCLUDE contribution: the
// PROTO_NAMESPACE entry (bare or GLOBAL — proto.conf:136 always emits a GLOBAL FOR
// proto addincl, so both ride identically) followed by any parsed PROTO_ADDINCL
// GLOBAL paths. Mirrors gen.go's ownProtoInclude.
func protoNamespaceContribs(d *ModuleData) []VFS {
	var own []VFS

	if d.protoNamespace != nil {
		own = []VFS{source(filepath.ToSlash(filepath.Clean(d.protoNamespace.string())))}
	}

	return append(own, d.protoAddInclGlobal...)
}

// descProtocIncludes builds the protoc -I span of a PD command, mirroring
// upstream `_PROTO_DESC_RAWPROTO_CMD` (proto.conf):
// `-I=./$PROTO_NAMESPACE -I=$ARCADIA_ROOT/$PROTO_NAMESPACE ${pre=-I=:_PROTO__INCLUDE}
//
//	-I=$ARCADIA_BUILD_ROOT -I=$PROTOBUF_INCLUDE_PATH --include_source_info`.
//
// The _PROTO__INCLUDE band has the same structure as the C++/PY proto commands
// (composePBArgBlocks): the structural -I=$(B) -I=$(S), the own cppOutRoot, then
// the peer PROTO_NAMESPACE span.
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

// emitProtoDescProducer emits one per-proto PD producer
// (_PROTO_DESC_RAWPROTO_CMD): desc_rawproto_wrapper.py around protoc producing
// <proto>.desc and the hashed <proto>.rawproto.
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

// emitDescProtoMerge emits the DESC_PROTO submodule merge node
// (_PROTO_DESC_MERGE_CMD): merge_files.py over the .desc into .self.protodesc,
// then collect_rawproto.py over the .rawproto into .protosrc.
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

// emitProtoDescriptions emits a PROTO_DESCRIPTIONS module
// (_PROTO_DESC_MERGE_PEERS_CMD): merge_files.py over its DESC_PROTO peer
// closure's .self.protodesc into <realprjname>.protodesc, then merge_protosrc.py
// into <realprjname>.tar. The .protodesc primary output backs a BUNDLE move.
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

	// PROTO_DESCRIPTIONS keeps _NEED_SBOM_INFO (unlike _DESC_PROTO, which
	// DISABLEs it) and is a _BARE_UNIT final target, so it materializes the
	// universal YMAKE_PYTHON3 toolchain peer's global toolchain.component.sbom as
	// a direct input+dep — the merge command renders only the .protodesc subset of
	// SRCS_GLOBAL, but the whole peer-closure global set attaches. The DESC_PROTO
	// peers contribute none (feature disabled). Off-contour/non-x86_64 returns nil.
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
