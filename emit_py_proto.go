package main

import (
	"path/filepath"
	"slices"
	"strings"
)

func protoPythonResourceKeyParts(instance ModuleInstance, d *ModuleData, src string) (dir, sep, base string) {
	base = strings.TrimSuffix(src, ".proto")

	if d.pyNamespace == nil {
		return instance.Path.relString(), "/", base
	}

	if d.pyNamespace.string() == "." {
		return "", "", base
	}

	nsPath := strings.ReplaceAll(d.pyNamespace.string(), ".", "/")

	return "", "", filepath.ToSlash(filepath.Clean(nsPath + "/" + base))
}

func protoPythonNamespaceArg(d *ModuleData) string {
	if d.protoNamespace == nil {
		return "/"
	}

	return "/" + filepath.ToSlash(filepath.Clean(d.protoNamespace.string()))
}

type PyPBModuleEmission struct {
	grpcPyRef    NodeRef
	mypyRef      NodeRef
	grpcPyBinary VFS
	mypyBinary   VFS
	head         []ANY
	mid          []ANY
	tail         []ANY
	scanCfg      ScanContext
}

func (e *EmitContext) newPyPBModuleEmission(protocBinary VFS, protoInclude []VFS, duplicateOutputRootInclude bool) *PyPBModuleEmission {
	if e.pyPBOk {
		return &e.pyPBEmission
	}

	e.pyPBOk = true

	ctx, _, d := e.ctx, e.instance, e.d
	pe := &e.pyPBEmission

	*pe = PyPBModuleEmission{}

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
	na := ctx.na
	head := na.anys.alloc(6 + len(suffixes))[:0]

	head = append(head,
		d.tc.Python3.any(),
		pbPyWrapperVFS.any(),
		argPyVer.any(), argPy3.any(),
		argSuffixes.any(),
	)

	head = appendInternAnys(head, suffixes)
	head = append(head, argInput.any())
	na.anys.commit(len(head))
	pe.head = head[:len(head):len(head)]

	mid := na.anys.alloc(15 + len(protoInclude) + len(d.protocFlags))[:0]

	mid = append(mid,
		argNs.any(), internStr(protoPythonNamespaceArg(d)).any(),
		arg2.any(),
		(protocBinary).any(),
		internV("-I=./", protoRoot).any(),
		internV("-I=$(S)/", protoRoot).any(),
		argIB2.any(),
		argIS3.any(),
	)

	if d.useCommonGoogleAPIs {
		mid = append(mid, strISContribLibsGoogleapisCommonProtos.any())
	}

	if protoRoot != "" {
		mid = append(mid, internV("-I=$(S)/", protoRoot).any())

		if duplicateOutputRootInclude {
			mid = append(mid, internV("-I=$(S)/", protoRoot).any())
		}
	}

	for _, p := range protoInclude {
		token := internV("-I=", p.prefix(), p.relString()).any()

		if slices.Contains(mid, token) {
			continue
		}

		mid = append(mid, token)
	}

	if d.needGoogleProtoPeerdirs && !slices.Contains(mid, argISContribLibsProtocSrc.any()) {
		mid = append(mid, argISContribLibsProtocSrc.any())
	}

	mid = append(mid,
		argIB2.any(),
		argISContribLibsProtobufSrc.any(),
		internV("--python_out=$(B)/", protoRoot).any(),
	)

	mid = appendAnyLists(mid, d.protocFlags)
	na.anys.commit(len(mid))
	pe.mid = mid[:len(mid):len(mid)]

	tail := na.anys.alloc(4)[:0]

	if d.grpc {
		tail = append(tail,
			internV("--plugin=protoc-gen-grpc_py=", pe.grpcPyBinary.prefix(), pe.grpcPyBinary.relString()).any(),
			internV("--grpc_py_out=$(B)/", protoRoot).any(),
		)
	}

	if !d.noMypy {
		tail = append(tail,
			internV("--plugin=protoc-gen-mypy=", pe.mypyBinary.prefix(), pe.mypyBinary.relString()).any(),
			internV("--mypy_out=$(B)/", protoRoot).any(),
		)
	}

	na.anys.commit(len(tail))
	pe.tail = tail[:len(tail):len(tail)]
	pe.scanCfg = protoWalkInputs(ctx.parsers, nil, e.instance.Path.relString())

	return pe
}

func (e *EmitContext) emitPyProtoSource(srcTok ANY, srcGroup int) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	src := srcTok.string()
	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
	duplicateOutputRootInclude := false

	if protoRoot := protoPythonOutputRoot(d); protoRoot != "" {
		duplicateOutputRootInclude = slices.Contains(e.peers.PeerAddInclGlobal, build(protoRoot))
	}

	pe := e.newPyPBModuleEmission(protocBinary, e.peers.ProtoInclude, duplicateOutputRootInclude)
	protoRelPath := protoSourceRelPath(ctx.fs, instance, d, src)
	protoBase := strings.TrimSuffix(protoRelPath, ".proto")
	pyOut := build(protoBase, "__intpy3___pb2.py")

	if e.codegen.lookup(pyOut) != nil {
		return
	}

	pyiOut := build(protoBase, "__intpy3___pb2.pyi")

	var grpcPyOut VFS

	outputs := na.vfs.alloc(3)[:0]

	outputs = append(outputs, pyOut)

	suffixes := []string{"_pb2.py"}

	if d.grpc {
		grpcPyOut = build(protoBase, "__intpy3___pb2_grpc.py")
		outputs = append(outputs, grpcPyOut)
		suffixes = append(suffixes, "_pb2_grpc.py")
	}

	if !d.noMypy {
		outputs = append(outputs, pyiOut)
		suffixes = append(suffixes, "_pb2.pyi")
	}

	na.vfs.commit(len(outputs))

	outputs = outputs[:len(outputs):len(outputs)]

	relChunk := na.anyList(internStr(protoRelPath).any())
	chunks := na.chunks.alloc(5)[:0]

	chunks = append(chunks, pe.head, relChunk, pe.mid, relChunk)

	if len(pe.tail) > 0 {
		chunks = append(chunks, pe.tail)
	}

	na.chunks.commit(len(chunks))

	cmdArgs := ArgChunks(chunks[:len(chunks):len(chunks)])
	toolRefs := na.noderefs.alloc(3)[:0]

	for _, r := range [2]NodeRef{protocLDRef, pe.grpcPyRef} {
		if r != 0 {
			toolRefs = append(toolRefs, r)
		}
	}

	if !d.noMypy && pe.mypyRef != 0 {
		toolRefs = append(toolRefs, pe.mypyRef)
	}

	na.noderefs.commit(len(toolRefs))

	toolRefs = toolRefs[:len(toolRefs):len(toolRefs)]

	protoSrcVFS := source(protoRelPath)
	protoCwd := srcRootDirVFS
	generatedProto := false

	var producerDeps []NodeRef
	var producerSourceInputs []VFS

	if info := e.codegen.lookup(build(protoRelPath)); info != nil {
		protoSrcVFS = build(protoRelPath)
		protoCwd = bldRootDirVFS
		producerDeps = na.refList(info.ProducerRef)
		producerSourceInputs = info.SourceInputs
		generatedProto = true
	}

	transitive := walkClosure(e.scanner, source(protoRelPath), pe.scanCfg)
	inputs := na.vfs.alloc(5 + len(producerSourceInputs))[:0]

	inputs = append(inputs, protocBinary, pbPyWrapperVFS, protoSrcVFS)
	inputs = append(inputs, producerSourceInputs...)

	if d.grpc {
		inputs = append(inputs, pe.grpcPyBinary)
	}

	if !d.noMypy {
		inputs = append(inputs, pe.mypyBinary)
	}

	na.vfs.commit(len(inputs))

	inputs = inputs[:len(inputs):len(inputs)]

	pbNodeKV := na.kvs.one()

	*pbNodeKV = KV{P: pkPB, PC: pcYellow}

	protoBaseName := filepath.Base(protoBase)
	extOut := na.exts.alloc(len(outputs))[:0]

	for i, out := range outputs {
		extOut = append(extOut, KVExt{
			Key: internStr("ext_out_name_for_" + filepath.Base(out.relString())).string(),
			Val: internStr(protoBaseName + suffixes[i]).string(),
		})
	}

	na.exts.commit(len(outputs))
	pbNodeKV.ExtOut = extOut[:len(outputs):len(outputs)]

	pyPBNode := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: cmdArgs, Cwd: protoCwd, Env: envVarsVCS}),
		Env:          envVarsVCS,
		Inputs:       na.inputList(inputs, transitive.buckets...),
		Outputs:      outputs,
		KV:           pbNodeKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      producerDeps,
		Resources:    usesPython3,
	}

	if len(toolRefs) > 0 {
		pyPBNode.ForeignDepRefs = toolRefs
	}

	pyPBRef := ctx.emit.emitNode(pyPBNode)
	sourceInputs := dedupSourceVFS(na, inputs, transitive.buckets)
	keyDir, keySep, keyBase := protoPythonResourceKeyParts(instance, d, src)

	tokenFor := func(out VFS) STR {
		if generatedProto {
			return internStr(trimModulePrefix(out.relString(), instance.Path.relString()))
		}

		return internV("${ARCADIA_BUILD_ROOT}/", out.relString())
	}

	e.codegen.register(&GeneratedFileInfo{OutputPath: pyOut, ProducerRef: pyPBRef, SourceInputs: sourceInputs})
	e.pySrcsReg = append(e.pySrcsReg, PySrc{Path: pyOut, Module: internV(keyDir, keySep, keyBase, "_pb2.py"), Token: tokenFor(pyOut).any(), Group: pyGroupProto, SrcGroup: srcGroup})

	if d.grpc {
		e.codegen.register(&GeneratedFileInfo{OutputPath: grpcPyOut, ProducerRef: pyPBRef, SourceInputs: sourceInputs})
		e.pySrcsReg = append(e.pySrcsReg, PySrc{Path: grpcPyOut, Module: internV(keyDir, keySep, keyBase, "_pb2_grpc.py"), Token: tokenFor(grpcPyOut).any(), Group: pyGroupProto, SrcGroup: srcGroup})
	}
}

func (e *EmitContext) hasProtoPySrcs() bool {
	for _, ps := range e.pySrcsReg {
		if ps.Group == pyGroupProto {
			return true
		}
	}

	return false
}

func (e *EmitContext) pyProtoYapycOut(ps PySrc) VFS {
	rel := ps.Path.relString()
	token := strings.TrimPrefix(ps.Token.string(), "${ARCADIA_BUILD_ROOT}/")

	if strings.Contains(token, "/") {
		return build(rel, ".", pySrcYapycSuffix(e.instance.Path.relString()), ".yapyc3")
	}

	return build(rel, ".yapyc3")
}

func (e *EmitContext) appendPyProtoResEntries(out []PyGenResEntry, ps PySrc) []PyGenResEntry {
	rel := ps.Path.relString()
	grpc := strings.HasSuffix(rel, "__intpy3___pb2_grpc.py")
	info := e.codegen.mustInfo(ps.Path, "appendPyProtoResEntries")
	token := ps.Token.string()
	yapycOut := e.pyProtoYapycOut(ps)

	if !strings.HasPrefix(token, "${ARCADIA_BUILD_ROOT}/") {
		return append(out,
			PyGenResEntry{token: token, key: ps.Module, path: ps.Path},
			PyGenResEntry{token: e.resStr2(token, strings.TrimPrefix(yapycOut.relString(), rel)), key: ps.Module, yapyc: true, path: yapycOut})
	}

	entryInputs := info.SourceInputs

	if grpc {
		siblingPy := build(strings.TrimSuffix(rel, "__intpy3___pb2_grpc.py"), "__intpy3___pb2.py")

		entryInputs = concat(info.SourceInputs, []VFS{siblingPy})
	}

	return append(out,
		PyGenResEntry{token: token, key: ps.Module, path: ps.Path, inputs: entryInputs},
		PyGenResEntry{token: e.resStr2("${ARCADIA_BUILD_ROOT}/", yapycOut.relString()), key: ps.Module, yapyc: true, path: yapycOut, inputs: info.SourceInputs})
}

func (e *EmitContext) emitPyProtoBytecode() {
	ctx := e.ctx

	if !e.hasProtoPySrcs() {
		return
	}

	py3ccLDRef, py3ccBinary := ctx.tool(argToolsPy3cc)
	py3ccSlowLDRef, py3ccSlowBin := ctx.tool(argToolsPy3ccSlow)

	for _, ps := range e.pySrcsReg {
		if ps.Group != pyGroupProto {
			continue
		}

		e.emitPyProtoYapyc(ps, py3ccLDRef, py3ccSlowLDRef, py3ccBinary, py3ccSlowBin)
	}
}

func (e *EmitContext) emitPyProtoYapyc(ps PySrc, py3ccRef, py3ccSlowRef NodeRef, py3ccBinary, py3ccSlowBin VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	rel := ps.Path.relString()
	info := e.codegen.mustInfo(ps.Path, "emitPyProtoYapyc")
	token := strings.TrimPrefix(ps.Token.string(), "${ARCADIA_BUILD_ROOT}/")
	yapycOut := e.pyProtoYapycOut(ps)
	yapycTail := na.anyList(internV(token, "-").any(), (ps.Path).any(), (yapycOut).any())
	inputsHead := na.vfsList(py3ccBinary, py3ccSlowBin, ps.Path)

	var nodeInputs InputChunks

	if strings.HasSuffix(rel, "__intpy3___pb2_grpc.py") {
		siblingPy := build(strings.TrimSuffix(rel, "__intpy3___pb2_grpc.py"), "__intpy3___pb2.py")

		nodeInputs = na.inputList(inputsHead, info.SourceInputs, na.srcChunk(siblingPy))
	} else {
		nodeInputs = na.inputList(inputsHead, info.SourceInputs)
	}

	yapycEnv := envVarsVCSPyHash

	yapycNode := Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: na.chunkList(e.ctx.py3ccHead(py3ccBinary, py3ccSlowBin), yapycTail), Env: yapycEnv}),
		Env:            yapycEnv,
		Inputs:         nodeInputs,
		Outputs:        na.vfsList(yapycOut),
		KV:             &pyCodegenKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:        na.refList(info.ProducerRef),
		ForeignDepRefs: na.refList(py3ccRef, py3ccSlowRef),
		Resources:      usesPython3,
	}

	yapycRef := ctx.emit.emitNode(yapycNode)

	e.codegen.register(&GeneratedFileInfo{OutputPath: yapycOut, ProducerRef: yapycRef})
}

func (e *EmitContext) flushPyProtoGroup(srcGroup int) ([]NodeRef, []VFS) {
	d := e.d
	entries := e.resEntries[:0]

	for _, ps := range e.pySrcsReg {
		if ps.Group != pyGroupProto || ps.SrcGroup != srcGroup {
			continue
		}

		entries = e.appendPyProtoResEntries(entries, ps)
	}

	e.resEntries = entries

	if len(entries) == 0 {
		return nil, nil
	}

	peerAddIncl := e.peers.PeerAddInclGlobal

	return e.packResources(ResourcePack{Tag: d.unit.HashTag, Items: e.pyGenResourceItems(entries), RawClosure: func(aux VFS, inputs []VFS, ref NodeRef) Closure {
		return e.pyProtoAuxInputClosure(aux, inputs, ref, peerAddIncl)
	}})
}

func (e *EmitContext) flushPyProtoSrcs() *ProtoSrcsResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	entries := e.resEntries[:0]

	for _, ps := range e.pySrcsReg {
		if ps.Group != pyGroupProto {
			continue
		}

		entries = e.appendPyProtoResEntries(entries, ps)
	}

	e.resEntries = entries

	if len(entries) == 0 {
		return nil
	}

	var cppSibling *ModuleEmitResult

	if d.moduleStmt.Name == tokProtoLibrary && !moduleExcludesTag(d, "CPP_PROTO") {
		cppInstance := instance

		cppInstance.Language = LangCPP
		cppSibling = genModule(ctx, cppInstance)
	}

	peerAddIncl := e.peers.PeerAddInclGlobal

	if cppSibling != nil {
		peerAddIncl = dedup(cppSibling.AddInclGlobal, e.peers.PeerAddInclGlobal)
	}

	genRefs, genOuts := e.packResources(ResourcePack{Tag: d.unit.HashTag, Items: e.pyGenResourceItems(entries), RawClosure: func(aux VFS, inputs []VFS, ref NodeRef) Closure {
		return e.pyProtoAuxInputClosure(aux, inputs, ref, peerAddIncl)
	}})

	if len(genRefs) == 0 {
		return nil
	}

	protoLibName := ""

	if len(d.moduleStmt.Args) > 0 {
		protoLibName = d.moduleStmt.Args[0].string()
	}

	globalBaseName := globalArchiveNameWithPrefixOrName(instance.Path.relString(), d.unit.ARPrefix, protoLibName)
	gRef := emitARGlobalNamedTagged(instance, globalBaseName, d.unit.GlobalARTag, genRefs, genOuts, d.tc, ctx.host, ctx.emit)
	globalPath := build(instance.Path.relString(), "/", globalBaseName)

	result := &ProtoSrcsResult{
		GlobalRef:  &gRef,
		GlobalPath: &globalPath,
	}

	if cppSibling != nil && cppSibling.ARPath != nil {
		result.WholeArchiveRefs = append(result.WholeArchiveRefs, cppSibling.ARRef)
		result.WholeArchivePaths = append(result.WholeArchivePaths, *cppSibling.ARPath)
	} else if moduleExcludesTag(d, "CPP_PROTO") {
		result.WholeArchiveCmdPaths = append(result.WholeArchiveCmdPaths, build(instance.Path.relString(), "/", archiveNameWithPrefixOrName(instance.Path.relString(), "lib", "")))
	}

	return result
}

func protoPythonOutputRoot(d *ModuleData) string {
	if d != nil && d.protoNamespace != nil {
		root := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(d.protoNamespace.string())), "/")

		if root != "." && root != "" {
			return root
		}
	}

	return ""
}

func pyProtoAuxPy3Suffix(d *ModuleData) bool {
	return d.unit.Tag == unitTagPy3Proto || d.moduleStmt.Name == tokPy23Library || d.moduleStmt.Name == tokPy23NativeLibrary
}

func (e *EmitContext) pyProtoAuxInputClosure(aux VFS, seed []VFS, ref NodeRef, peerAddIncl []VFS) Closure {
	ctx, instance, d := e.ctx, e.instance, e.d
	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	emits := make([]IncludeDirective, 0, len(seed))

	for _, in := range seed {
		if in.isSource() {
			emits = append(emits, IncludeDirective{kind: includeQuoted, target: includeTarget(in.rel().any())})
		}
	}

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     aux,
		ProducerRef:    ref,
		GeneratorRefs:  []NodeRef{rescompilerRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: emits},
		Compile:        &CompileSpec{ForceCxx: true, Py3Suffix: pyProtoAuxPy3Suffix(d), CFlags: []ANY{argX.any(), argC.any()}},
	})

	scanCfg := newScanContext(ctx.parsers, d.addIncl, peerAddIncl, includeScannerBasePaths(), instance.Path.relString())

	return walkClosure(e.scanner, aux, scanCfg)
}
