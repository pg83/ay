package main

import (
	"path/filepath"
	"slices"
	"strings"
)

func protoPythonResourceKey(instance ModuleInstance, d *ModuleData, src, suffix string) string {
	base := strings.TrimSuffix(src, ".proto")

	if d.pyNamespace == nil {
		return instance.Path.rel() + "/" + base + suffix
	}

	if d.pyNamespace.string() == "." {
		return base + suffix
	}

	nsPath := strings.ReplaceAll(d.pyNamespace.string(), ".", "/")

	// Upstream preserves the module-local proto subdirectory under the Python
	// namespace (gen_py_protos.py keeps the input's relative path), so a nested
	// SRC chunk_client/proto/data_statistics.proto under PY_NAMESPACE(yt_proto.yt.client)
	// becomes yt_proto/yt/client/chunk_client/proto/data_statistics_pb2.py — do
	// not collapse to filepath.Base.
	return filepath.ToSlash(filepath.Clean(nsPath + "/" + base + suffix))
}

func moduleExcludesTag(d *ModuleData, tag string) bool {
	return d != nil && d.excludeTags != nil && d.excludeTags[internStr(tag)]
}

func protoPythonNamespaceArg(d *ModuleData) string {
	if d.protoNamespace == nil {
		return "/"
	}

	return "/" + filepath.ToSlash(filepath.Clean(d.protoNamespace.string()))
}

func emitPyProtoSrcs(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerContribs PeerGlobalContribs, protoSrcs, evSrcs []string) *ProtoSrcsResult {
	if len(evSrcs) > 0 {
		throwFmt("gen: py-addressed PROTO_LIBRARY %s with .ev sources is not modelled", instance.Path.rel())
	}

	if len(protoSrcs) == 0 {
		return nil
	}

	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)

	// A peer that re-contributes the module's own PROTO_NAMESPACE build root
	// ($(B)/<ns>) makes upstream render the source-root include -I=$(S)/<ns> a
	// second time inside _PROTO__INCLUDE — exactly the C++ duplicateOutputRootInclude
	// rule (emit_proto.go). On the py side the CPP_PROTO self-sibling (peered by
	// walkPeersForGlobalAddIncl for optimized PROTO_LIBRARYs) carries that GLOBAL
	// $(B)/<ns> addincl, so a PROTO_NAMESPACE py module duplicates even with no real
	// proto peer; NO_OPTIMIZE_PY_PROTOS modules (no sibling, e.g. protos_from_protoc)
	// do not.
	duplicateOutputRootInclude := false

	if protoRoot := protoPythonOutputRoot(d); protoRoot != "" {
		duplicateOutputRootInclude = containsVFS(peerContribs.addIncl, build(protoRoot))
	}

	pe := newPyPBModuleEmission(ctx, d, instance, protocBinary, peerContribs.protoInclude, duplicateOutputRootInclude)

	var cppSibling *ModuleEmitResult

	if !moduleExcludesTag(d, "CPP_PROTO") {
		cppInstance := instance
		cppInstance.Language = LangCPP
		cppSibling = genModule(ctx, cppInstance)
	}

	var pyProtoRefs []NodeRef
	var pyProtoOutputs []VFS
	var auxEntries []PyProtoAuxEntry

	for _, src := range protoSrcs {
		auxEntries = append(auxEntries, emitPyProtoSrc(ctx, instance, d, src, protocLDRef, protocBinary, pe)...)
	}

	auxRes := emitPyProtoAuxChunks(ctx, instance, d, peerContribs, auxEntries, cppSibling)

	if auxRes != nil {
		pyProtoRefs = append(pyProtoRefs, auxRes.Refs...)
		pyProtoOutputs = append(pyProtoOutputs, auxRes.Outputs...)
	}

	if len(pyProtoRefs) == 0 {
		return nil
	}

	pyInstance := instance
	pyInstance.Language = LangPy
	globalBaseName := globalArchiveNameWithPrefixOrName(instance.Path.rel(), "libpy3", "")
	gRef := emitARGlobalNamedTagged(pyInstance, globalBaseName, tagPy3ProtoGlobal, pyProtoRefs, pyProtoOutputs, d.tc, ctx.host, ctx.emit)

	globalPath := build(instance.Path.rel() + "/" + globalBaseName)
	result := &ProtoSrcsResult{
		GlobalRef:  &gRef,
		GlobalPath: &globalPath,
	}

	if cppSibling != nil && cppSibling.ARPath != nil {
		// The CPP sibling's archive is whole-archived here; it enters the regular
		// link closure as a proper peer (walkPeersForGlobalAddIncl peers the CPP
		// instance first), so it is not re-adopted as this module's own ARPath.
		result.WholeArchiveRefs = append(result.WholeArchiveRefs, cppSibling.ARRef)
		result.WholeArchivePaths = append(result.WholeArchivePaths, *cppSibling.ARPath)
	} else if moduleExcludesTag(d, "CPP_PROTO") {
		result.WholeArchiveCmdPaths = append(result.WholeArchiveCmdPaths, build(instance.Path.rel()+"/"+archiveName(instance.Path.rel())))
	}

	return result
}

// pyPBModuleEmission is the per-module py-proto emission context: the
// resolved plugin tools and the stable spans of the protoc py command line.
// Built once per module (emitPyProtoSrcs, before its source loop):
//
//	head: [python3, protoc_wrapper.py, --py-ver py3, --suffixes <…>, --input]
//	mid:  [--ns <ns>, --, protoc, the -I set, --python_out] (follows the
//	      per-source input path)
//	tail: the grpc / mypy plugin blocks (they follow the source token)
type PyPBModuleEmission struct {
	grpcPyRef    NodeRef
	mypyRef      NodeRef
	grpcPyBinary VFS
	mypyBinary   VFS

	head []STR
	mid  []STR
	tail []STR
}

func newPyPBModuleEmission(ctx *GenCtx, d *ModuleData, instance ModuleInstance, protocBinary VFS, protoInclude []VFS, duplicateOutputRootInclude bool) *PyPBModuleEmission {
	pe := &PyPBModuleEmission{}

	if d.grpc {
		pe.grpcPyRef, pe.grpcPyBinary = ctx.tool(argContribToolsProtocPluginsGrpcPython)
	}

	if !d.noMypy {
		pe.mypyRef, pe.mypyBinary = ctx.tool(argContribPythonMypyProtobufBinProtocGenMypy)
	}

	suffixes := []string{"_pb2.py"}

	if d.grpc {
		suffixes = append(suffixes, "_pb2_grpc.py")
	}

	if !d.noMypy {
		suffixes = append(suffixes, "_pb2.pyi")
	}

	protoRoot := protoPythonOutputRoot(d)

	head := []STR{
		d.tc.Python3,
		internStr(pbPyWrapperPath),
		argPyVer.str(), argPy3.str(),
		argSuffixes.str(),
	}
	head = appendInternStrs(head, suffixes)
	pe.head = append(head, argInput.str())

	mid := make([]STR, 0, 16)
	mid = append(mid,
		argNs.str(), internStr(protoPythonNamespaceArg(d)),
		arg2.str(),
		(protocBinary).str(),
		internStr("-I=./"+protoRoot),
		internStr("-I=$(S)/"+protoRoot),
		argIB2.str(),
		argIS3.str(),
	)

	// USE_COMMON_GOOGLE_APIS PEERDIRs contrib/libs/googleapis-common-protos, whose
	// PROTO_ADDINCL(GLOBAL) injects -I=$(S)/contrib/libs/googleapis-common-protos
	// into the proto cmdline ahead of the protobuf runtime include. The cpp variant
	// picks this up through the proto-addincl closure; the py cmdline adds it here.
	if d.useCommonGoogleAPIs {
		mid = append(mid, strISContribLibsGoogleapisCommonProtos)
	}

	if protoRoot != "" && protoRoot != "contrib/libs/protobuf/src" {
		mid = append(mid, internStr("-I=$(S)/"+protoRoot))

		// A peer re-contributing $(B)/<protoRoot> renders the source-root include a
		// second time (mirrors EmitPB's duplicateOutputRootInclude for the cpp side).
		if duplicateOutputRootInclude {
			mid = append(mid, internStr("-I=$(S)/"+protoRoot))
		}
	}

	mid = append(mid, argISContribLibsProtobufSrc.str())

	// The transitive _PROTO__INCLUDE set trails the protobuf-src include and
	// precedes the NEED_GOOGLE_PROTO_PEERDIRS protoc-src include, in encounter
	// order. Upstream's PROTO_NAMESPACE always expands to a `GLOBAL FOR proto
	// $(S)/<ns>` addincl (proto.conf:136 PROTO_ADDINCL), so bare and GLOBAL
	// namespaces both ride the proto peer closure into every transitive consumer's
	// _PROTO__INCLUDE — py3_proto consumers included (sg7: brandformance carries
	// -I=$(S)/yt; caesar carries -I=$(S)/contrib/libs/googleapis-common-protos).
	// protos_from_protoc is a PY-only implicit peer added after the real proto
	// peers, so its protoc-src include trails this band. _PROTO__INCLUDE is a set:
	// a namespace already rendered (the module's own protoRoot, the structural
	// protobuf-src, or a USE_COMMON_GOOGLE_APIS googleapis emitted above) is
	// skipped here.
	for _, p := range protoInclude {
		token := internStr("-I=" + p.string())
		if slices.Contains(mid, token) {
			continue
		}

		mid = append(mid, token)
	}

	// NEED_GOOGLE_PROTO_PEERDIRS (default yes) peers the PY-only protos_from_protoc,
	// whose GLOBAL PROTO_NAMESPACE(contrib/libs/protoc/src) adds protoc/src to the
	// py-proto include path. The protobuf builtins DISABLE it (proto.conf:717,857).
	// When protos_from_protoc is reached through the proto peer closure, protoc/src
	// is already a member of the rendered _PROTO__INCLUDE set above — _PROTO__INCLUDE
	// is a set, so do not re-emit it (sg3 devtools/ya/bin py protos).
	if d.needGoogleProtoPeerdirs && !slices.Contains(mid, argISContribLibsProtocSrc.str()) {
		mid = append(mid, argISContribLibsProtocSrc.str())
	}

	pe.mid = append(mid,
		argIB2.str(),
		argISContribLibsProtobufSrc.str(),
		internStr("--python_out=$(B)/"+protoRoot),
	)

	if d.grpc {
		pe.tail = append(pe.tail,
			internStr("--plugin=protoc-gen-grpc_py="+pe.grpcPyBinary.string()),
			internStr("--grpc_py_out=$(B)/"+protoRoot),
		)
	}

	if !d.noMypy {
		pe.tail = append(pe.tail,
			internStr("--plugin=protoc-gen-mypy="+pe.mypyBinary.string()),
			internStr("--mypy_out=$(B)/"+protoRoot),
		)
	}

	return pe
}

func emitPyProtoSrc(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src string, protocLDRef NodeRef, protocBinary VFS, pe *PyPBModuleEmission) []PyProtoAuxEntry {
	na := ctx.na

	if d.moduleStmt.Name != tokProtoLibrary {
		return nil
	}

	protoRelPath := protoSourceRelPath(ctx.fs, instance, d, src)
	protoBase := strings.TrimSuffix(protoRelPath, ".proto")

	pyBase := protoBase + "__intpy3___pb2.py"
	pyOut := build(pyBase)
	pyiOut := build(protoBase + "__intpy3___pb2.pyi")
	var grpcPyOut VFS
	outputs := []VFS{pyOut}
	suffixes := []string{"_pb2.py"}

	if d.grpc {
		grpcPyOut = build(protoBase + "__intpy3___pb2_grpc.py")
		outputs = append(outputs, grpcPyOut)
		suffixes = append(suffixes, "_pb2_grpc.py")
	}

	if !d.noMypy {
		outputs = append(outputs, pyiOut)
		suffixes = append(suffixes, "_pb2.pyi")
	}

	relChunk := []STR{internStr(protoRelPath)}
	cmdArgs := na.chunkList(pe.head, relChunk, pe.mid, relChunk)

	if len(pe.tail) > 0 {
		cmdArgs = append(cmdArgs, pe.tail)
	}

	toolRefs := depRefs(protocLDRef, pe.grpcPyRef)

	if !d.noMypy {
		toolRefs = append(toolRefs, depRefs(pe.mypyRef)...)
	}

	// SRCS(X.proto) may name a build-generated .proto (e.g. an IF(GEN_PROTO)
	// RUN_PROGRAM/RUN_ANTLR STDOUT — irt/bannerland/proto/banner_flags/
	// banner_flags.proto). Mirror the C++ PB override (emit_proto.go / emitPB):
	// swap the proto input to the $(B) build path, pin the producer as a dep,
	// fold in the producer's $(S) source inputs, and run protoc from $(B).
	// Without this the py PB node lists a nonexistent $(S) .proto and finalize
	// content-hashing faults on it. The codegen registry is shared per platform,
	// and emitPyProtoSrcs generates the C++ sibling first, so the JV producer is
	// already registered by the time this lookup runs.
	protoSrcVFS := source(protoRelPath)
	protoCwd := strS

	var producerDeps []NodeRef
	var producerSourceInputs []VFS

	if info := codegenRegForInstance(ctx, instance).lookup(build(protoRelPath)); info != nil {
		protoSrcVFS = build(protoRelPath)
		protoCwd = strB
		producerDeps = []NodeRef{info.ProducerRef}
		producerSourceInputs = info.SourceInputs
	}

	inputs := []VFS{protocBinary, pbPyWrapperVFS, protoSrcVFS}
	transitive := walkClosureTail(ctx.scannerFor(instance), source(protoRelPath), protoWalkInputs(ctx.parsers, nil, instance.Path.rel()).ScanCfg)

	inputs = append(inputs, transitive...)
	inputs = append(inputs, producerSourceInputs...)

	if d.grpc {
		inputs = append(inputs, pe.grpcPyBinary)
	}

	if !d.noMypy {
		inputs = append(inputs, pe.mypyBinary)
	}

	pbKV := KV{P: pkPB, PC: pcYellow}
	protoBaseName := filepath.Base(protoBase)

	for i, out := range outputs {
		pbKV.ExtOut = append(pbKV.ExtOut, KVExt{
			Key: "ext_out_name_for_" + filepath.Base(out.rel()),
			Val: protoBaseName + suffixes[i],
		})
	}

	pyPBNode := &Node{
		Platform:         instance.Platform,
		Cmds:             na.cmdList(Cmd{CmdArgs: cmdArgs, Cwd: protoCwd, Env: EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}}),
		Env:              EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}},
		Inputs:           na.inputList(inputs),
		Outputs:          outputs,
		KV:               pbKV,
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel(), ModuleTag: tagPy3Proto},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          producerDeps,
		Resources:        usesPython3,
	}

	if len(toolRefs) > 0 {
		pyPBNode.ForeignDepRefs = toolRefs
	}

	pyPBRef := ctx.emit.emit(pyPBNode)
	pyYapyc := []VFS{pyOut}

	if d.grpc {
		pyYapyc = append(pyYapyc, grpcPyOut)
	}

	yapyRes := emitGeneratedPyProtoYapyc(ctx, instance, pyYapyc, pyPBRef, pyProtoSourceInputs(inputs))

	if yapyRes == nil {
		yapyRes = &GeneratedPyProtoYapycResult{}
	}

	return pyProtoAuxEntriesForSource(instance, d, src, pyPBRef, pyProtoSourceInputs(inputs), outputs, yapyRes.Refs, yapyRes.Outputs)
}

func protoPythonOutputRoot(d *ModuleData) string {
	if d != nil && d.protoNamespace != nil {
		root := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(d.protoNamespace.string())), "/")

		if root != "." && root != "" {
			return root
		}
	}

	// Without a PROTO_NAMESPACE the python protos compile against the arcadia
	// root: --python_out=$(B)/ and -I=$(S)/ (not the module dir).
	return ""
}

type GeneratedPyProtoYapycResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func emitGeneratedPyProtoYapyc(ctx *GenCtx, instance ModuleInstance, pyOutputs []VFS, pyPBRef NodeRef, sourceInputs []VFS) *GeneratedPyProtoYapycResult {
	na := ctx.na

	py3ccRef, py3ccSlowRef, py3ccBinary, py3ccSlowBin := py3ccToolRefs(ctx, instance)
	suffix := protoPySuffix(instance.Path.rel())
	res := &GeneratedPyProtoYapycResult{}

	for i, pyOut := range pyOutputs {
		out := build(pyOut.rel() + "." + suffix + ".yapyc3")
		cmdArgs := []STR{
			(py3ccBinary).str(),
			argSlowPy3cc.str(),
			(py3ccSlowBin).str(),
			internStr(pyOut.rel() + "-"),
			(pyOut).str(),
			(out).str(),
		}
		deps := []NodeRef{pyPBRef}
		toolRefs := depRefs(py3ccRef, py3ccSlowRef)

		// sourceInputs is shared across the pyOutputs loop — its own chunk,
		// referenced, not copied per node.
		nodeInputs := na.inputList(na.vfsList(py3ccBinary, py3ccSlowBin, pyOut), sourceInputs)

		if i > 0 {
			nodeInputs = append(nodeInputs, []VFS{pyOutputs[0]})
		}

		node := &Node{
			Platform:         instance.Platform,
			Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envPYTHONHASHSEED, Value: strZero}}}),
			Env:              EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envPYTHONHASHSEED, Value: strZero}},
			Inputs:           nodeInputs,
			Outputs:          na.vfsList(out),
			KV:               KV{P: pkPY, PC: pcYellow},
			TargetProperties: TargetProperties{ModuleDir: instance.Path.rel(), ModuleTag: tagPy3Proto},
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			DepRefs:          deps,
			Resources:        usesPython3,
		}

		if len(toolRefs) > 0 {
			node.ForeignDepRefs = toolRefs
		}

		res.Refs = append(res.Refs, ctx.emit.emit(node))
		res.Outputs = append(res.Outputs, out)
	}

	return res
}

type PyProtoAuxEntry struct {
	path     VFS
	key      string
	producer NodeRef
	inputs   []VFS
}

func pyProtoAuxEntriesForSource(instance ModuleInstance, d *ModuleData, src string, pyPBRef NodeRef, producerInputs []VFS, pyOutputs []VFS, yapyRefs []NodeRef, yapyOuts []VFS) []PyProtoAuxEntry {
	var entries []PyProtoAuxEntry
	addResource := func(srcPath VFS, key string, producer NodeRef) {
		entries = append(entries, PyProtoAuxEntry{path: srcPath, key: key, producer: producer, inputs: producerInputs})
	}
	addResource(pyOutputs[0], protoPythonResourceKey(instance, d, src, "_pb2.py"), pyPBRef)

	if len(yapyOuts) > 0 {
		addResource(yapyOuts[0], protoPythonResourceKey(instance, d, src, "_pb2.py.yapyc3"), yapyRefs[0])
	}

	if d.grpc && len(pyOutputs) > 2 && pyOutputs[1].rel() != "" {
		// The _pb2_grpc.py and _pb2.py share one protoc producer (pyPBRef). Bundling
		// _pb2_grpc.py pulls that producer's sibling output _pb2.py into the chunk's
		// inputs (deduped against the _pb2.py resource when co-located).
		entries = append(entries, PyProtoAuxEntry{
			path:     pyOutputs[1],
			key:      protoPythonResourceKey(instance, d, src, "_pb2_grpc.py"),
			producer: pyPBRef,
			inputs:   append(append([]VFS(nil), producerInputs...), pyOutputs[0]),
		})

		if len(yapyOuts) > 1 {
			addResource(yapyOuts[1], protoPythonResourceKey(instance, d, src, "_pb2_grpc.py.yapyc3"), yapyRefs[1])
		}
	}

	return entries
}

func pyProtoSourceInputs(inputs []VFS) []VFS {
	out := make([]VFS, 0, len(inputs))
	deduper.reset()

	for _, input := range inputs {
		if !input.isSource() {
			continue
		}

		if !deduper.add(input) {
			continue
		}

		out = append(out, input)
	}

	return out
}

type PyProtoAuxChunksResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func emitPyProtoAuxChunks(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerContribs PeerGlobalContribs, entries []PyProtoAuxEntry, cppSibling *ModuleEmitResult) *PyProtoAuxChunksResult {
	na := ctx.na

	if len(entries) == 0 {
		return nil
	}

	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	type chunk struct {
		hashInputs []string
		cmdArgs    []string
		inputs     []VFS
		deps       []NodeRef
	}

	var chunks []chunk
	cur := chunk{}
	cmdLen := 0
	// Chunk accumulation runs no deduper user (the dedupVFS call / input tail
	// filter below follow the final flush), so the input set lives on the
	// deduper, reset per flush. depSeen stays a local map: it is live
	// simultaneously with the input set.
	deduper.reset()
	depSeen := map[NodeRef]struct{}{}
	addInput := func(v VFS) {
		if !deduper.add(v) {
			return
		}

		cur.inputs = append(cur.inputs, v)
	}
	addDep := func(ref NodeRef) {
		if ref == (NodeRef(0)) {
			return
		}

		if _, ok := depSeen[ref]; ok {
			return
		}

		depSeen[ref] = struct{}{}
		cur.deps = append(cur.deps, ref)
	}
	flush := func() {
		if cmdLen == 0 {
			return
		}

		chunks = append(chunks, cur)
		cur = chunk{}
		cmdLen = 0
		deduper.reset()
		depSeen = map[NodeRef]struct{}{}
	}

	for _, e := range entries {
		key := "resfs/file/py/" + e.key
		arcBuildPath := "${ARCADIA_BUILD_ROOT}/" + e.path.rel()
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + arcBuildPath + "\"}"
		kvCmd := "resfs/src/" + key + "=" + e.path.rel()

		cur.hashInputs = append(cur.hashInputs, "-", kvHash)
		cur.cmdArgs = append(cur.cmdArgs, "-", kvCmd)
		addInput(e.path)

		for _, input := range e.inputs {
			addInput(input)
		}

		addDep(e.producer)
		cmdLen += rootCmdLen + len("-") + len(kvHash)

		if cmdLen >= maxCmdLen {
			flush()
		}

		cur.hashInputs = append(cur.hashInputs, arcBuildPath, "-"+key)
		cur.cmdArgs = append(cur.cmdArgs, e.path.string(), "-"+key)
		addInput(e.path)

		for _, input := range e.inputs {
			addInput(input)
		}

		addDep(e.producer)
		cmdLen += rootCmdLen + len(arcBuildPath) + len(key)

		if cmdLen >= maxCmdLen {
			flush()
		}
	}

	flush()

	peerAddIncl := peerContribs.addIncl

	if cppSibling != nil {
		peerAddIncl = dedupVFS(cppSibling.AddInclGlobal, peerContribs.addIncl)
	}

	res := &PyProtoAuxChunksResult{}

	for _, ch := range chunks {
		aux := build(instance.Path.rel() + "/" + protoResourceHash(ch.hashInputs, "$S/"+instance.Path.rel(), "PY3_PROTO") + "_raw.auxcpp")
		auxRef := ctx.emit.reserve()
		auxClosure := pyProtoAuxInputClosure(ctx, instance, d, aux, ch.inputs, auxRef, peerAddIncl)
		cmdArgs := []STR{internStr(rescompilerBinPath), (aux).str()}
		cmdArgs = appendInternStrs(cmdArgs, ch.cmdArgs)

		deps := append(append([]NodeRef(nil), ch.deps...), depRefs(rescompilerRef)...)

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		// ch.inputs is internally deduped already (deduper-gated accumulation),
		// so it survives a whole-list dedup intact — reference it as a chunk and
		// filter only the rescompiler + closure tail against it.
		deduper.reset()

		for _, p := range ch.inputs {
			deduper.add(p)
		}

		tail := make([]VFS, 0, 1+len(auxClosure))

		if deduper.add(rescompilerBinVFS) {
			tail = append(tail, rescompilerBinVFS)
		}

		// auxClosure is the aux window (root-led: aux is a build output); the
		// PR node's own output never joins its inputs, so skip the root.
		for _, p := range auxClosure {
			if p == aux {
				continue
			}

			if deduper.add(p) {
				tail = append(tail, p)
			}
		}

		ctx.emit.emitReserved(&Node{
			Platform:         instance.Platform,
			Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
			Env:              env,
			Inputs:           na.inputList(ch.inputs, tail),
			Outputs:          na.vfsList(aux),
			KV:               KV{P: pkPR, PC: pcYellow, ShowOut: true},
			TargetProperties: TargetProperties{ModuleDir: instance.Path.rel(), ModuleTag: tagPy3Proto},
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			DepRefs:          deps,
		}, auxRef)

		ccIn := ModuleCCInputs{
			TC:                   d.tc,
			InclArgs:             ctx.inclArgs,
			Flags:                d.flags,
			AddIncl:              d.addIncl,
			PeerAddInclGlobal:    peerAddIncl,
			ScanCfg:              newScanContext(ctx.parsers, d.addIncl, peerAddIncl, includeScannerBasePaths(), instance.Path.rel()),
			PeerCFlagsGlobal:     peerContribs.cFlags,
			PeerCXXFlagsGlobal:   peerContribs.cxxFlags,
			PeerCOnlyFlagsGlobal: peerContribs.cOnlyFlags,
			ModuleScopeCFlags:    d.moduleScopeCFlags,
			ClangWarnings:        d.clangWarnings,
			PerSourceCFlags:      []ARG{argX, argC},
			FS:                   ctx.fs,
			ExtraDepRefs:         []NodeRef{auxRef},
			Py3Suffix:            true,
			ForceCxx:             true,
			ModuleTag:            tagPy3Proto,
			IncludeInputs:        auxClosure,
		}
		ccIn.CCBlocks = composeCCModuleArgBlocks(na, instance.Platform, &ccIn)
		ccRef, ccOut, _ := emitCC(instance, aux.rel()[strings.LastIndex(aux.rel(), "/")+1:], aux, ccIn, ctx.host, ctx.emit)
		res.Refs = append(res.Refs, ccRef)
		res.Outputs = append(res.Outputs, ccOut)
	}

	return res
}
