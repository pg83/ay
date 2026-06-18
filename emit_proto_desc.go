package main

import (
	"crypto/md5"
	enchex "encoding/hex"
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
	descRawprotoWrapperVFS = source("build/scripts/desc_rawproto_wrapper.py")
	mergeFilesVFS          = source("build/scripts/merge_files.py")
	collectRawprotoVFS     = source("build/scripts/collect_rawproto.py")
	mergeProtosrcVFS       = source("build/scripts/merge_protosrc.py")

	strDescProtoTag = internStr("desc_proto")

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
func descPeerClosure(ctx *GenCtx, instance ModuleInstance, peerdirs []STR, injectBuiltins bool) []DescProtoPeer {
	var out []DescProtoPeer
	seen := make(map[VFS]struct{})

	add := func(peers []DescProtoPeer) {
		for _, p := range peers {
			if _, dup := seen[p.SelfProtodesc]; dup {
				continue
			}

			seen[p.SelfProtodesc] = struct{}{}
			out = append(out, p)
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
		add(genModule(ctx, peerInstance).DescClosure)
	}

	if injectBuiltins && instance.Path.rel() != protosFromProtocPeer {
		enter(protosFromProtocPeer)
	}

	for _, p := range peerdirs {
		enter(p.string())
	}

	return out
}

// emitDescProtoSubmodule emits the DESC_PROTO submodule of a PROTO_LIBRARY: a PD
// producer per .proto SRC plus the .self.protodesc / .protosrc merge node. The
// returned result exposes this module's DescClosure (its DESC peer closure with
// itself appended) for a PROTO_DESCRIPTIONS consumer, and the merge node as the
// module's primary output.
func emitDescProtoSubmodule(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *ModuleEmitResult {
	closure := descPeerClosure(ctx, instance, d.peerdirs, d.needGoogleProtoPeerdirs)

	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)

	cppOutRoot := protoCPPOutRoot(d)

	// The DESC submodule's own PROTO_NAMESPACE plus the protobuf runtime src is
	// the proto-import search base. We deliberately do NOT walk the CPP peer
	// closure here: the genModule memo key ignores Platform, so genModule-ing a
	// shared contrib module through the DESC traversal (which may carry a
	// host/tool platform) would poison its canonical (target) build-type result.
	// The full _PROTO__INCLUDE peer chain is byte-exact protoc-include
	// propagation, which the ticket scopes out; here we only need the producer's
	// outputs and command shape.
	var protoSearchPaths []VFS

	if cppOutRoot != "" {
		protoSearchPaths = []VFS{source(cppOutRoot)}
	}

	mid := descProtocIncludes(nil, cppOutRoot)

	// The proto import-closure search config is module-stable (the -I set does
	// not depend on the individual .proto) — build it once, not per source.
	scanCfg := protoWalkInputs(ctx.parsers, protoSearchPaths, instance.Path.rel()).ScanCfg
	scanner := ctx.scannerFor(instance)

	hash := moddirHash(instance.Path.rel())

	var producerRefs []NodeRef
	var descOutputs []VFS
	var rawprotoOutputs []VFS

	for _, src := range d.srcs {
		if !strings.HasSuffix(src.string(), ".proto") {
			continue
		}

		protoRelPath := protoSourceRelPath(ctx.fs, instance, d, src.string())
		protoVFS := source(protoRelPath)
		imports := walkClosureTail(scanner, protoVFS, scanCfg)

		descOut := build(protoRelPath + ".desc")
		rawprotoOut := build(protoRelPath + "." + hash + ".rawproto")

		ref := emitProtoDescProducer(ctx, instance, protoRelPath, descOut, rawprotoOut,
			protocLDRef, protocBinary, mid, imports)

		producerRefs = append(producerRefs, ref)
		descOutputs = append(descOutputs, descOut)
		rawprotoOutputs = append(rawprotoOutputs, rawprotoOut)
	}

	prj := realPrjName(instance.Path.rel())
	selfProtodesc := build(instance.Path.rel() + "/" + prj + ".self.protodesc")
	protosrc := build(instance.Path.rel() + "/" + prj + ".protosrc")

	mergeRef := emitDescProtoMerge(ctx, instance, selfProtodesc, protosrc, descOutputs, rawprotoOutputs, producerRefs)

	closure = append(closure, DescProtoPeer{SelfProtodesc: selfProtodesc, MergeRef: mergeRef})

	selfPath := selfProtodesc

	return &ModuleEmitResult{
		ARRef:       mergeRef,
		ARPath:      &selfPath,
		DescClosure: closure,
	}
}

// descProtocIncludes builds the protoc -I span of a PD command: the own
// namespace, the peer _PROTO__INCLUDE chain, then -I=$(B) / protobuf-src and
// --include_source_info. (The exact peer-addincl set is the DESC submodule's
// _PROTO__INCLUDE; per the ticket, byte-exact protoc include propagation is a
// separate concern — this reproduces the command shape and the output set.)
func descProtocIncludes(peerProtoAddIncl []VFS, cppOutRoot string) []STR {
	out := make([]STR, 0, 6+len(peerProtoAddIncl))
	out = append(out,
		internStr("-I=./"+cppOutRoot),
		internStr("-I=$(S)/"+cppOutRoot),
	)

	for _, p := range peerProtoAddIncl {
		out = append(out, internStr("-I="+p.string()))
	}

	out = append(out,
		argIB2.str(),
		argISContribLibsProtobufSrc.str(),
		internStr("--include_source_info"),
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
		internStr("--desc-output"),
		descOut.str(),
		internStr("--rawproto-output"),
		rawprotoOut.str(),
		internStr("--proto-file"),
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
	descOutputs, rawprotoOutputs []VFS, producerRefs []NodeRef) NodeRef {
	na := ctx.emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	merge := make([]STR, 0, 3+len(descOutputs))
	merge = append(merge, wrapccPython3STR, mergeFilesVFS.str(), selfProtodesc.str())

	for _, d := range descOutputs {
		merge = append(merge, d.str())
	}

	collect := make([]STR, 0, 4+len(rawprotoOutputs))
	collect = append(collect, wrapccPython3STR, collectRawprotoVFS.str(), internStr("--output"), protosrc.str())

	for _, r := range rawprotoOutputs {
		collect = append(collect, internStr(r.rel()))
	}

	inputs := make([]VFS, 0, len(descOutputs)+len(rawprotoOutputs))
	inputs = append(inputs, descOutputs...)
	inputs = append(inputs, rawprotoOutputs...)

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
	closure := descPeerClosure(ctx, instance, d.peerdirs, false)

	na := ctx.emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	prj := realPrjName(instance.Path.rel())
	protodesc := build(instance.Path.rel() + "/" + prj + ".protodesc")
	tar := build(instance.Path.rel() + "/" + prj + ".tar")

	merge := make([]STR, 0, 3+len(closure))
	merge = append(merge, wrapccPython3STR, mergeFilesVFS.str(), protodesc.str())

	collect := make([]STR, 0, 4+len(closure))
	collect = append(collect, wrapccPython3STR, mergeProtosrcVFS.str(), internStr("--output"), tar.str())

	inputs := make([]VFS, 0, len(closure))
	deps := make([]NodeRef, 0, len(closure))

	for _, p := range closure {
		merge = append(merge, p.SelfProtodesc.str())
		collect = append(collect, internStr(p.SelfProtodesc.rel()))
		inputs = append(inputs, p.SelfProtodesc)
		deps = append(deps, p.MergeRef)
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
