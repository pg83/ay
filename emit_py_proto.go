package main

import (
	"path/filepath"
	"slices"
	"strings"
)

var pbPyWrapperPath = pbPyWrapperVFS.string()

func protoPythonResourceKeyBase(instance ModuleInstance, d *ModuleData, src string) string {
	base := strings.TrimSuffix(src, ".proto")

	if d.pyNamespace == nil {
		return instance.Path.rel() + "/" + base
	}

	if d.pyNamespace.string() == "." {
		return base
	}

	nsPath := strings.ReplaceAll(d.pyNamespace.string(), ".", "/")

	return filepath.ToSlash(filepath.Clean(nsPath + "/" + base))
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

type PyPBModuleEmission struct {
	grpcPyRef    NodeRef
	mypyRef      NodeRef
	grpcPyBinary VFS
	mypyBinary   VFS
	head         []STR
	mid          []STR
	tail         []STR
}

func (e *EmitContext) newPyPBModuleEmission(protocBinary VFS, protoInclude []VFS, duplicateOutputRootInclude bool) *PyPBModuleEmission {
	ctx, _, d := e.ctx, e.instance, e.d
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
		internV("-I=./", protoRoot),
		internV("-I=$(S)/", protoRoot),
		argIB2.str(),
		argIS3.str(),
	)

	if d.useCommonGoogleAPIs {
		mid = append(mid, strISContribLibsGoogleapisCommonProtos)
	}

	if protoRoot != "" {
		mid = append(mid, internV("-I=$(S)/", protoRoot))

		if duplicateOutputRootInclude {
			mid = append(mid, internV("-I=$(S)/", protoRoot))
		}
	}

	for _, p := range protoInclude {
		token := internV("-I=", p.string())

		if slices.Contains(mid, token) {
			continue
		}

		mid = append(mid, token)
	}

	if d.needGoogleProtoPeerdirs && !slices.Contains(mid, argISContribLibsProtocSrc.str()) {
		mid = append(mid, argISContribLibsProtocSrc.str())
	}

	pe.mid = append(mid,
		argIB2.str(),
		argISContribLibsProtobufSrc.str(),
		internV("--python_out=$(B)/", protoRoot),
	)

	pe.mid = appendArgStr(pe.mid, d.protocFlags)

	if d.grpc {
		pe.tail = append(pe.tail,
			internV("--plugin=protoc-gen-grpc_py=", pe.grpcPyBinary.string()),
			internV("--grpc_py_out=$(B)/", protoRoot),
		)
	}

	if !d.noMypy {
		pe.tail = append(pe.tail,
			internV("--plugin=protoc-gen-mypy=", pe.mypyBinary.string()),
			internV("--mypy_out=$(B)/", protoRoot),
		)
	}

	return pe
}

func (e *EmitContext) emitPyProtoSource(srcTok STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	src := srcTok.string()
	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
	duplicateOutputRootInclude := false

	if protoRoot := protoPythonOutputRoot(d); protoRoot != "" {
		duplicateOutputRootInclude = containsVFS(e.peers.SelfAddInclGlobal, build(protoRoot))
	}

	pe := e.newPyPBModuleEmission(protocBinary, e.peers.ProtoInclude, duplicateOutputRootInclude)
	protoRelPath := protoSourceRelPath(ctx.fs, instance, d, src)
	protoBase := strings.TrimSuffix(protoRelPath, ".proto")
	pyOut := build(protoBase, "__intpy3___pb2.py")
	pyiOut := build(protoBase, "__intpy3___pb2.pyi")

	var grpcPyOut VFS

	outputs := []VFS{pyOut}
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

	relChunk := []STR{internStr(protoRelPath)}
	cmdArgs := na.chunkList(pe.head, relChunk, pe.mid, relChunk)

	if len(pe.tail) > 0 {
		cmdArgs = append(cmdArgs, pe.tail)
	}

	toolRefs := depRefs(protocLDRef, pe.grpcPyRef)

	if !d.noMypy {
		toolRefs = append(toolRefs, depRefs(pe.mypyRef)...)
	}

	protoSrcVFS := source(protoRelPath)
	protoCwd := strS
	generatedProto := false

	var producerDeps []NodeRef
	var producerSourceInputs []VFS

	if info := e.codegen.lookup(build(protoRelPath)); info != nil {
		protoSrcVFS = build(protoRelPath)
		protoCwd = strB
		producerDeps = []NodeRef{info.ProducerRef}
		producerSourceInputs = info.SourceInputs
		generatedProto = true
	}

	inputs := []VFS{protocBinary, pbPyWrapperVFS, protoSrcVFS}
	transitive := walkClosure(e.scanner, source(protoRelPath), protoWalkInputs(ctx.parsers, nil, instance.Path.rel()))

	inputs = append(inputs, producerSourceInputs...)

	if d.grpc {
		inputs = append(inputs, pe.grpcPyBinary)
	}

	if !d.noMypy {
		inputs = append(inputs, pe.mypyBinary)
	}

	pbNodeKV := KV{P: pkPB, PC: pcYellow}
	protoBaseName := filepath.Base(protoBase)

	for i, out := range outputs {
		pbNodeKV.ExtOut = append(pbNodeKV.ExtOut, KVExt{
			Key: "ext_out_name_for_" + filepath.Base(out.rel()),
			Val: protoBaseName + suffixes[i],
		})
	}

	pyPBNode := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: cmdArgs, Cwd: protoCwd, Env: EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}}),
		Env:          EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}},
		Inputs:       na.inputList(inputs, transitive.buckets...),
		Outputs:      outputs,
		KV:           &pbNodeKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      producerDeps,
		Resources:    usesPython3,
	}

	if len(toolRefs) > 0 {
		pyPBNode.ForeignDepRefs = toolRefs
	}

	pyPBRef := ctx.emit.emitNode(pyPBNode)
	sourceInputs := pyProtoSourceInputs(inputs, transitive.buckets)
	keyBase := protoPythonResourceKeyBase(instance, d, src)

	tokenFor := func(out VFS) STR {
		if generatedProto {
			return internStr(trimModulePrefix(out.rel(), instance.Path.rel()))
		}

		return internV("${ARCADIA_BUILD_ROOT}/", out.rel())
	}

	e.codegen.register(&GeneratedFileInfo{OutputPath: pyOut, ProducerRef: pyPBRef, SourceInputs: sourceInputs})
	e.pySrcsReg = append(e.pySrcsReg, PySrc{Path: pyOut, Module: internV(keyBase, "_pb2.py"), Token: tokenFor(pyOut), Group: pyGroupProto})

	if d.grpc {
		e.codegen.register(&GeneratedFileInfo{OutputPath: grpcPyOut, ProducerRef: pyPBRef, SourceInputs: sourceInputs})
		e.pySrcsReg = append(e.pySrcsReg, PySrc{Path: grpcPyOut, Module: internV(keyBase, "_pb2_grpc.py"), Token: tokenFor(grpcPyOut), Group: pyGroupProto})
	}
}

func (e *EmitContext) flushPyProtoSrcs() *ProtoSrcsResult {
	ctx, instance, d := e.ctx, e.instance, e.d

	var entries []PyGenResEntry

	for _, ps := range e.pySrcsReg {
		if ps.Group != pyGroupProto {
			continue
		}

		entries = append(entries, e.pyResEntriesFor(ps)...)
	}

	if len(entries) == 0 {
		return nil
	}

	var cppSibling *ModuleEmitResult

	if !moduleExcludesTag(d, "CPP_PROTO") {
		cppInstance := instance

		cppInstance.Language = LangCPP
		cppSibling = genModule(ctx, cppInstance)
	}

	peerAddIncl := e.peers.SelfAddInclGlobal

	if cppSibling != nil {
		peerAddIncl = dedup(cppSibling.AddInclGlobal, e.peers.SelfAddInclGlobal)
	}

	genRefs, genOuts := e.packResources(ResourcePack{Tag: d.unit.Tag, Items: pyGenResourceItems(entries), RawClosure: func(aux VFS, inputs []VFS, ref NodeRef) Closure {
		return e.pyProtoAuxInputClosure(aux, inputs, ref, peerAddIncl)
	}})

	if len(genRefs) == 0 {
		return nil
	}

	protoLibName := ""

	if len(d.moduleStmt.Args) > 0 {
		protoLibName = d.moduleStmt.Args[0].string()
	}

	globalBaseName := globalArchiveNameWithPrefixOrName(instance.Path.rel(), d.unit.ARPrefix, protoLibName)
	gRef := emitARGlobalNamedTagged(instance, globalBaseName, d.unit.GlobalARTag, genRefs, genOuts, d.tc, ctx.host, ctx.emit)
	globalPath := build(instance.Path.rel(), "/", globalBaseName)

	result := &ProtoSrcsResult{
		GlobalRef:  &gRef,
		GlobalPath: &globalPath,
	}

	if cppSibling != nil && cppSibling.ARPath != nil {
		result.WholeArchiveRefs = append(result.WholeArchiveRefs, cppSibling.ARRef)
		result.WholeArchivePaths = append(result.WholeArchivePaths, *cppSibling.ARPath)
	} else if moduleExcludesTag(d, "CPP_PROTO") {
		result.WholeArchiveCmdPaths = append(result.WholeArchiveCmdPaths, build(instance.Path.rel(), "/", archiveNameWithPrefixOrName(instance.Path.rel(), "lib", "")))
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

func (e *EmitContext) pyProtoAuxInputClosure(aux VFS, seed []VFS, ref NodeRef, peerAddIncl []VFS) Closure {
	ctx, instance, d := e.ctx, e.instance, e.d
	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	emits := make([]IncludeDirective, 0, len(seed))

	for _, in := range seed {
		if in.isSource() {
			emits = append(emits, IncludeDirective{kind: includeQuoted, target: internStr(in.rel())})
		}
	}

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     aux,
		ProducerRef:    ref,
		GeneratorRefs:  []NodeRef{rescompilerRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: emits},
		Compile:        &CompileSpec{ForceCxx: true, Py3Suffix: true, CFlags: []ARG{argX, argC}},
	})

	scanCfg := newScanContext(ctx.parsers, d.addIncl, peerAddIncl, includeScannerBasePaths(), instance.Path.rel())

	return walkClosure(e.scanner, aux, scanCfg)
}
