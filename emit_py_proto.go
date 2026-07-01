package main

import (
	"path/filepath"
	"slices"
	"strings"
)

var (
	pyProtoKV  = KV{P: pkPY, PC: pcYellow}
	pyProtoKV2 = KV{P: pkPY, PC: pcYellow, ShowOut: true}
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

func (e *EmitContext) emitPyProtoSrcs(peerContribs PeerGlobalContribs, protoSrcs, evSrcs []string) *ProtoSrcsResult {
	ctx, instance, d := e.ctx, e.instance, e.d

	if len(evSrcs) > 0 {
		throwFmt("gen: py-addressed PROTO_LIBRARY %s with .ev sources is not modelled", instance.Path.rel())
	}

	if len(protoSrcs) == 0 {
		return nil
	}

	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
	duplicateOutputRootInclude := false

	if protoRoot := protoPythonOutputRoot(d); protoRoot != "" {
		duplicateOutputRootInclude = containsVFS(peerContribs.addIncl, build(protoRoot))
	}

	pe := e.newPyPBModuleEmission(protocBinary, peerContribs.protoInclude, duplicateOutputRootInclude)

	var cppSibling *ModuleEmitResult

	if !moduleExcludesTag(d, "CPP_PROTO") {
		cppInstance := instance

		cppInstance.Language = LangCPP
		cppSibling = genModule(ctx, cppInstance)
	}

	var pyProtoRefs []NodeRef
	var pyProtoOutputs []VFS
	var entries []PyGenResEntry

	for _, src := range protoSrcs {
		entries = append(entries, e.emitPyProtoSrc(src, protocLDRef, protocBinary, pe)...)
	}

	peerAddIncl := peerContribs.addIncl

	if cppSibling != nil {
		peerAddIncl = dedup(cppSibling.AddInclGlobal, peerContribs.addIncl)
	}

	genRes := e.emitPyGenResources(entries, "PY3_PROTO", &pyProtoKV2, func(aux VFS, inputs []VFS, ref NodeRef) []VFS {
		return e.pyProtoAuxInputClosure(aux, inputs, ref, peerAddIncl)
	})

	if genRes != nil {
		pyProtoRefs = append(pyProtoRefs, genRes.Refs...)
		pyProtoOutputs = append(pyProtoOutputs, genRes.Outputs...)
	}

	if len(pyProtoRefs) == 0 {
		return nil
	}

	pyInstance := instance

	pyInstance.Language = LangPy

	protoLibName := ""

	if len(d.moduleStmt.Args) > 0 {
		protoLibName = d.moduleStmt.Args[0].string()
	}

	globalBaseName := globalArchiveNameWithPrefixOrName(instance.Path.rel(), "libpy3", protoLibName)
	gRef := emitARGlobalNamedTagged(pyInstance, globalBaseName, tagPy3ProtoGlobal, pyProtoRefs, pyProtoOutputs, d.tc, ctx.host, ctx.emit)
	globalPath := build(instance.Path.rel(), "/", globalBaseName)

	result := &ProtoSrcsResult{
		GlobalRef:  &gRef,
		GlobalPath: &globalPath,
	}

	if cppSibling != nil && cppSibling.ARPath != nil {
		result.WholeArchiveRefs = append(result.WholeArchiveRefs, cppSibling.ARRef)
		result.WholeArchivePaths = append(result.WholeArchivePaths, *cppSibling.ARPath)
	} else if moduleExcludesTag(d, "CPP_PROTO") {
		result.WholeArchiveCmdPaths = append(result.WholeArchiveCmdPaths, build(instance.Path.rel(), "/", archiveName(instance.Path.rel())))
	}

	return result
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

func (e *EmitContext) emitPyProtoSrc(src string, protocLDRef NodeRef, protocBinary VFS, pe *PyPBModuleEmission) []PyGenResEntry {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	protoRelPath := protoSourceRelPath(ctx.fs, instance, d, src)
	protoBase := strings.TrimSuffix(protoRelPath, ".proto")
	pyBase := protoBase + "__intpy3___pb2.py"
	pyOut := build(pyBase)
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

	var producerDeps []NodeRef
	var producerSourceInputs []VFS

	generated := false

	if info := e.codegen.lookup(build(protoRelPath)); info != nil {
		protoSrcVFS = build(protoRelPath)
		protoCwd = strB
		producerDeps = []NodeRef{info.ProducerRef}
		generated = true
		producerSourceInputs = info.SourceInputs
	}

	inputs := []VFS{protocBinary, pbPyWrapperVFS, protoSrcVFS}
	transitive := walkClosureTail(e.scanner, source(protoRelPath), protoWalkInputs(ctx.parsers, nil, instance.Path.rel()))

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
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: cmdArgs, Cwd: protoCwd, Env: EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}}),
		Env:          EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}},
		Inputs:       na.inputList(inputs),
		Outputs:      outputs,
		KV:           &pbKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      producerDeps,
		Resources:    usesPython3,
	}

	if len(toolRefs) > 0 {
		pyPBNode.ForeignDepRefs = toolRefs
	}

	pyPBRef := ctx.emit.emit(pyPBNode)
	pyYapyc := []VFS{pyOut}
	pyBuildBase := protoBase

	if generated {
		pyBuildBase = strings.TrimSuffix(src, ".proto")
	}

	yapycTokens := []string{pyBuildBase + "__intpy3___pb2.py"}

	if d.grpc {
		pyYapyc = append(pyYapyc, grpcPyOut)
		yapycTokens = append(yapycTokens, pyBuildBase+"__intpy3___pb2_grpc.py")
	}

	yapyRes := emitPyGenYapyc(ctx, instance, pyYapyc, yapycTokens, pyPBRef, pyProtoSourceInputs(inputs))

	return pyProtoResEntriesForSource(instance, d, src, generated, yapycTokens, pyPBRef, pyProtoSourceInputs(inputs), outputs, yapyRes.Refs, yapyRes.Outputs)
}

func pyProtoResEntriesForSource(instance ModuleInstance, d *ModuleData, src string, generated bool, tokens []string, pyPBRef NodeRef, producerInputs []VFS, pyOutputs []VFS, yapyRefs []NodeRef, yapyOuts []VFS) []PyGenResEntry {
	var entries []PyGenResEntry

	if generated {
		add := func(token, suffix string, output VFS, producer NodeRef) {
			entries = append(entries, PyGenResEntry{
				token:    token,
				key:      protoPythonResourceKey(instance, d, src, suffix),
				path:     output,
				producer: producer,
			})
		}

		yapToken := func(i int) string {
			return tokens[i] + strings.TrimPrefix(yapyOuts[i].rel(), pyOutputs[i].rel())
		}

		add(tokens[0], "_pb2.py", pyOutputs[0], pyPBRef)

		if len(yapyOuts) > 0 {
			add(yapToken(0), "_pb2.py.yapyc3", yapyOuts[0], yapyRefs[0])
		}

		if d.grpc && len(pyOutputs) > 1 {
			add(tokens[1], "_pb2_grpc.py", pyOutputs[1], pyPBRef)

			if len(yapyOuts) > 1 {
				add(yapToken(1), "_pb2_grpc.py.yapyc3", yapyOuts[1], yapyRefs[1])
			}
		}

		return entries
	}

	add := func(path VFS, suffix string, producer NodeRef, inputs []VFS) {
		entries = append(entries, PyGenResEntry{
			token:    "${ARCADIA_BUILD_ROOT}/" + path.rel(),
			key:      protoPythonResourceKey(instance, d, src, suffix),
			path:     path,
			producer: producer,
			inputs:   inputs,
		})
	}

	add(pyOutputs[0], "_pb2.py", pyPBRef, producerInputs)

	if len(yapyOuts) > 0 {
		add(yapyOuts[0], "_pb2.py.yapyc3", yapyRefs[0], producerInputs)
	}

	if d.grpc && len(pyOutputs) > 2 && pyOutputs[1].rel() != "" {
		add(pyOutputs[1], "_pb2_grpc.py", pyPBRef, concat(producerInputs, []VFS{pyOutputs[0]}))

		if len(yapyOuts) > 1 {
			add(yapyOuts[1], "_pb2_grpc.py.yapyc3", yapyRefs[1], producerInputs)
		}
	}

	return entries
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
