package main

import (
	"path/filepath"
	"strings"
)

func protoPythonResourceKey(instance ModuleInstance, d *moduleData, src, suffix string) string {
	base := strings.TrimSuffix(src, ".proto")

	if d.pyNamespace == nil {
		return instance.Path + "/" + base + suffix
	}

	if *d.pyNamespace == "." {
		return base + suffix
	}

	nsPath := strings.ReplaceAll(*d.pyNamespace, ".", "/")
	return filepath.ToSlash(filepath.Clean(nsPath + "/" + filepath.Base(base) + suffix))
}

func moduleExcludesTag(d *moduleData, tag string) bool {
	return d != nil && d.excludeTags != nil && d.excludeTags[tag]
}

func protoPythonNamespaceArg(d *moduleData) string {
	if d == nil || d.protoNamespace == nil {
		return "/"
	}

	return "/" + filepath.ToSlash(filepath.Clean(*d.protoNamespace))
}

func emitPyProtoSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs, protoSrcs, evSrcs []string) *protoSrcsResult {
	if len(evSrcs) > 0 {
		ThrowFmt("gen: py-addressed PROTO_LIBRARY %s with .ev sources is not modelled", instance.Path)
	}

	if len(protoSrcs) == 0 {
		return nil
	}

	protocLDRef, protocBinary := ctx.tool(pbProtocModule)

	var cppSibling *moduleEmitResult

	if !moduleExcludesTag(d, "CPP_PROTO") {
		cppInstance := instance
		cppInstance.Language = LangCPP
		cppSibling = genModule(ctx, cppInstance)
	}

	var pyProtoRefs []NodeRef
	var pyProtoOutputs []VFS
	var auxEntries []pyProtoAuxEntry

	for _, src := range protoSrcs {
		auxEntries = append(auxEntries, emitPyProtoSrc(ctx, instance, d, src, protocLDRef, protocBinary)...)
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
	globalBaseName := globalArchiveNameWithPrefixOrName(instance.Path, "libpy3", "")
	gRef := EmitARGlobalNamedTagged(pyInstance, globalBaseName, "py3_proto_global", pyProtoRefs, pyProtoOutputs, ctx.host, ctx.emit)

	globalPath := Build(instance.Path + "/" + globalBaseName)
	result := &protoSrcsResult{
		GlobalRef:  &gRef,
		GlobalPath: &globalPath,
	}

	if cppSibling != nil && cppSibling.ARPath != nil {
		result.WholeArchiveRefs = append(result.WholeArchiveRefs, cppSibling.ARRef)
		result.WholeArchivePaths = append(result.WholeArchivePaths, *cppSibling.ARPath)

		if d.optimizePyProtos {
			result.ARRef = cppSibling.ARRef
			result.ARPath = cppSibling.ARPath
		}
	} else if moduleExcludesTag(d, "CPP_PROTO") {
		result.WholeArchiveCmdPaths = append(result.WholeArchiveCmdPaths, Build(instance.Path+"/"+ArchiveName(instance.Path)))
	}

	return result
}

func emitPyProtoSrc(ctx *genCtx, instance ModuleInstance, d *moduleData, src string, protocLDRef NodeRef, protocBinary VFS) []pyProtoAuxEntry {
	if d.moduleStmt == nil || d.moduleStmt.Name != tokProtoLibrary {
		return nil
	}

	protoRelPath := protoSourceRelPath(ctx.fs, instance, d, src)
	protoBase := strings.TrimSuffix(protoRelPath, ".proto")
	protoRoot := protoPythonOutputRoot(instance, d)

	if d.grpc {
		protoRoot = ""
	}

	pyBase := protoBase + "__intpy3___pb2.py"
	pyOut := Build(pyBase)
	pyiOut := Build(protoBase + "__intpy3___pb2.pyi")
	var grpcPyOut VFS
	outputs := []VFS{pyOut}
	suffixes := []string{"_pb2.py"}

	if d.grpc {
		grpcPyOut = Build(protoBase + "__intpy3___pb2_grpc.py")
		outputs = append(outputs, grpcPyOut)
		suffixes = append(suffixes, "_pb2_grpc.py")
	}

	if !d.noMypy {
		outputs = append(outputs, pyiOut)
		suffixes = append(suffixes, "_pb2.pyi")
	}

	var grpcPyBinary, mypyBinary VFS
	var grpcPyRef, mypyRef NodeRef

	if d.grpc {
		grpcPyRef, grpcPyBinary = ctx.tool(pbGrpcPyModule)
	}

	if !d.noMypy {
		mypyRef, mypyBinary = ctx.tool(pbMypyModule)
	}

	cmdArgs := []ANY{
		stringAny(instance.Platform.Tools.Python3),
		stringAny(pbPyWrapperPath),
		stringAny("--py_ver"), stringAny("py3"),
		stringAny("--suffixes"),
	}
	cmdArgs = appendStringAny(cmdArgs, suffixes)
	cmdArgs = append(cmdArgs,
		stringAny("--input"), stringAny(protoRelPath),
		stringAny("--ns"), stringAny(protoPythonNamespaceArg(d)),
		stringAny("--"),
		vfsAny(protocBinary),
		stringAny("-I=./"+protoRoot),
		stringAny("-I=$(S)/"+protoRoot),
		stringAny("-I=$(B)"),
		stringAny("-I=$(S)"),
	)

	if !d.grpc && protoRoot != "contrib/libs/protobuf/src" {
		cmdArgs = append(cmdArgs, stringAny("-I=$(S)/"+protoRoot))
	}

	cmdArgs = append(cmdArgs, stringAny("-I=$(S)/contrib/libs/protobuf/src"))

	if d.grpc {
		cmdArgs = append(cmdArgs, stringAny("-I=$(S)/contrib/libs/protoc/src"))
	}

	cmdArgs = append(cmdArgs,
		stringAny("-I=$(B)"),
		stringAny("-I=$(S)/contrib/libs/protobuf/src"),
		stringAny("--python_out=$(B)/"+protoRoot),
		stringAny(protoRelPath),
	)

	if d.grpc {
		cmdArgs = append(cmdArgs,
			stringAny("--plugin=protoc-gen-grpc_py="+grpcPyBinary.String()),
			stringAny("--grpc_py_out=$(B)/"+protoRoot),
		)
	}

	if !d.noMypy {
		cmdArgs = append(cmdArgs,
			stringAny("--plugin=protoc-gen-mypy="+mypyBinary.String()),
			stringAny("--mypy_out=$(B)/"+protoRoot),
		)
	}

	toolRefs := make([]NodeRef, 0, 3)

	if protocLDRef != (NodeRef(0)) {
		toolRefs = append(toolRefs, protocLDRef)
	}

	if grpcPyRef != (NodeRef(0)) {
		toolRefs = append(toolRefs, grpcPyRef)
	}

	if !d.noMypy && mypyRef != (NodeRef(0)) {
		toolRefs = append(toolRefs, mypyRef)
	}

	inputs := []VFS{protocBinary, pbPyWrapperVFS, Source(protoRelPath)}
	transitive, hasDescriptor := protoTransitiveImports(ctx.parsers, ctx.fs, protoRelPath, nil)

	if hasDescriptor {
		inputs = append(inputs, pbDescriptorVFS)
	}

	inputs = append(inputs, transitive...)

	if d.grpc {
		inputs = append(inputs, grpcPyBinary)
	}

	if !d.noMypy {
		inputs = append(inputs, mypyBinary)
	}

	pbKV := KV{P: pkPB, PC: pcYellow}
	protoBaseName := filepath.Base(protoBase)

	for i, out := range outputs {
		pbKV.ExtOut = append(pbKV.ExtOut, KVExt{
			Key: "ext_out_name_for_" + filepath.Base(out.Rel()),
			Val: protoBaseName + suffixes[i],
		})
	}

	pyPBNode := &Node{
		Cmds:             []Cmd{{CmdArgs: cmdArgs, Cwd: "$(S)", Env: EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}}},
		Env:              EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}},
		Inputs:           inputs,
		Outputs:          outputs,
		KV:               pbKV,
		Tags:             instance.Platform.Tags,
		TargetProperties: TargetProperties{ModuleDir: instance.Path, ModuleTag: "py3_proto"},
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:          toolRefs,
	}

	if len(toolRefs) > 0 {
		pyPBNode.ForeignDepRefs = toolRefs
	}

	pyPBRef := ctx.emit.Emit(bindNodePlatform(withResources(pyPBNode, resourcePatternYMakePython3), instance.Platform))
	pyYapyc := []VFS{pyOut}

	if d.grpc {
		pyYapyc = append(pyYapyc, grpcPyOut)
	}

	yapyRes := emitGeneratedPyProtoYapyc(ctx, instance, pyYapyc, pyPBRef, pyProtoSourceInputs(inputs))

	if yapyRes == nil {
		yapyRes = &generatedPyProtoYapycResult{}
	}

	return pyProtoAuxEntriesForSource(instance, d, src, pyPBRef, pyProtoSourceInputs(inputs), outputs, yapyRes.Refs, yapyRes.Outputs)
}

func protoPythonOutputRoot(instance ModuleInstance, d *moduleData) string {
	if d != nil && d.protoNamespace != nil {
		root := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(*d.protoNamespace)), "/")

		if root != "." && root != "" {
			return root
		}
	}

	return instance.Path
}

type generatedPyProtoYapycResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func emitGeneratedPyProtoYapyc(ctx *genCtx, instance ModuleInstance, pyOutputs []VFS, pyPBRef NodeRef, sourceInputs []VFS) *generatedPyProtoYapycResult {
	py3ccRef, py3ccSlowRef, py3ccBinary, py3ccSlowBin := py3ccToolRefs(ctx, instance)
	suffix := protoPySuffix(instance.Path)
	res := &generatedPyProtoYapycResult{}

	for i, pyOut := range pyOutputs {
		out := Build(pyOut.Rel() + "." + suffix + ".yapyc3")
		cmdArgs := []ANY{
			vfsAny(py3ccBinary),
			stringAny("--slow-py3cc"),
			vfsAny(py3ccSlowBin),
			stringAny(pyOut.Rel() + "-"),
			vfsAny(pyOut),
			vfsAny(out),
		}
		deps := []NodeRef{pyPBRef}
		var toolRefs []NodeRef

		if py3ccRef != (NodeRef(0)) {
			deps = append(deps, py3ccRef)
			toolRefs = append(toolRefs, py3ccRef)
		}

		if py3ccSlowRef != (NodeRef(0)) {
			deps = append(deps, py3ccSlowRef)
			toolRefs = append(toolRefs, py3ccSlowRef)
		}

		nodeInputs := append([]VFS{py3ccBinary, py3ccSlowBin, pyOut}, sourceInputs...)

		if i > 0 {
			nodeInputs = append(nodeInputs, pyOutputs[0])
		}

		node := &Node{
			Cmds:             []Cmd{{CmdArgs: cmdArgs, Env: EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}, {Name: "PYTHONHASHSEED", Value: "0"}}}},
			Env:              EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}, {Name: "PYTHONHASHSEED", Value: "0"}},
			Inputs:           nodeInputs,
			Outputs:          []VFS{out},
			KV:               KV{P: pkPY, PC: pcYellow},
			Tags:             instance.Platform.Tags,
			TargetProperties: TargetProperties{ModuleDir: instance.Path, ModuleTag: "py3_proto"},
			Platform:         string(instance.Platform.Target),
			Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
			DepRefs:          deps,
		}

		if len(toolRefs) > 0 {
			node.ForeignDepRefs = toolRefs
		}

		res.Refs = append(res.Refs, ctx.emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3), instance.Platform)))
		res.Outputs = append(res.Outputs, out)
	}

	return res
}

type pyProtoAuxEntry struct {
	path     VFS
	key      string
	producer NodeRef
	inputs   []VFS
}

func pyProtoAuxEntriesForSource(instance ModuleInstance, d *moduleData, src string, pyPBRef NodeRef, producerInputs []VFS, pyOutputs []VFS, yapyRefs []NodeRef, yapyOuts []VFS) []pyProtoAuxEntry {
	var entries []pyProtoAuxEntry
	addResource := func(srcPath VFS, key string, producer NodeRef) {
		entries = append(entries, pyProtoAuxEntry{path: srcPath, key: key, producer: producer, inputs: producerInputs})
	}
	addResource(pyOutputs[0], protoPythonResourceKey(instance, d, src, "_pb2.py"), pyPBRef)

	if len(yapyOuts) > 0 {
		addResource(yapyOuts[0], protoPythonResourceKey(instance, d, src, "_pb2.py.yapyc3"), yapyRefs[0])
	}

	if d.grpc && len(pyOutputs) > 2 && pyOutputs[1].Rel() != "" {
		addResource(pyOutputs[1], protoPythonResourceKey(instance, d, src, "_pb2_grpc.py"), pyPBRef)

		if len(yapyOuts) > 1 {
			addResource(yapyOuts[1], protoPythonResourceKey(instance, d, src, "_pb2_grpc.py.yapyc3"), yapyRefs[1])
		}
	}

	return entries
}

func pyProtoSourceInputs(inputs []VFS) []VFS {
	out := make([]VFS, 0, len(inputs))
	seen := map[VFS]struct{}{}

	for _, input := range inputs {
		if !input.IsSource() {
			continue
		}

		if _, ok := seen[input]; ok {
			continue
		}

		seen[input] = struct{}{}
		out = append(out, input)
	}

	return out
}

type pyProtoAuxChunksResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func emitPyProtoAuxChunks(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs, entries []pyProtoAuxEntry, cppSibling *moduleEmitResult) *pyProtoAuxChunksResult {
	if len(entries) == 0 {
		return nil
	}

	rescompilerRef, _ := ctx.tool("tools/rescompiler/bin")
	type chunk struct {
		hashInputs []string
		cmdArgs    []string
		inputs     []VFS
		deps       []NodeRef
	}

	var chunks []chunk
	cur := chunk{}
	cmdLen := 0
	inputSeen := map[VFS]struct{}{}
	depSeen := map[NodeRef]struct{}{}
	addInput := func(v VFS) {
		if _, ok := inputSeen[v]; ok {
			return
		}

		inputSeen[v] = struct{}{}
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
		inputSeen = map[VFS]struct{}{}
		depSeen = map[NodeRef]struct{}{}
	}

	for _, e := range entries {
		key := "resfs/file/py/" + e.key
		arcBuildPath := "${ARCADIA_BUILD_ROOT}/" + e.path.Rel()
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + arcBuildPath + "\"}"
		kvCmd := "resfs/src/" + key + "=" + e.path.Rel()

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
		cur.cmdArgs = append(cur.cmdArgs, e.path.String(), "-"+key)
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

	res := &pyProtoAuxChunksResult{}

	for _, ch := range chunks {
		aux := Build(instance.Path + "/" + protoResourceHash(ch.hashInputs, "$S/"+instance.Path, "PY3_PROTO") + "_raw.auxcpp")
		auxClosure := pyProtoAuxInputClosure(ctx, instance, d, aux, ch.inputs, peerAddIncl)
		cmdArgs := []ANY{stringAny(rescompilerBinPath), vfsAny(aux)}
		cmdArgs = appendStringAny(cmdArgs, ch.cmdArgs)

		deps := append([]NodeRef(nil), ch.deps...)

		if rescompilerRef != (NodeRef(0)) {
			deps = append(deps, rescompilerRef)
		}

		env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}
		inputs := append([]VFS(nil), ch.inputs...)
		inputs = append(inputs, rescompilerBinVFS)
		inputs = append(inputs, auxClosure...)
		inputs = dedupVFS(inputs)
		ref := ctx.emit.Emit(bindNodePlatform(&Node{
			Cmds:             []Cmd{{CmdArgs: cmdArgs, Env: env}},
			Env:              env,
			Inputs:           inputs,
			Outputs:          []VFS{aux},
			KV:               KV{P: pkPR, PC: pcYellow, ShowOut: "yes"},
			Tags:             instance.Platform.Tags,
			TargetProperties: TargetProperties{ModuleDir: instance.Path, ModuleTag: "py3_proto"},
			Platform:         string(instance.Platform.Target),
			Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
			DepRefs:          deps,
		}, instance.Platform))

		ccIn := ModuleCCInputs{
			InclArgs:             ctx.inclArgs,
			Flags:                d.flags,
			AddIncl:              d.addIncl,
			PeerAddInclGlobal:    peerAddIncl,
			PeerCFlagsGlobal:     peerContribs.cFlags,
			PeerCXXFlagsGlobal:   peerContribs.cxxFlags,
			PeerCOnlyFlagsGlobal: peerContribs.cOnlyFlags,
			ModuleScopeCFlags:    d.moduleScopeCFlags,
			PerSourceCFlags:      []ARG{internArg("-x"), internArg("c++")},
			SourceRoot:           ctx.sourceRoot,
			FS:                   ctx.fs,
			ExtraDepRefs:         []NodeRef{ref},
			Py3Suffix:            true,
			ForceCxx:             true,
			ModuleTag:            stringPtr("py3_proto"),
			IncludeInputs:        auxClosure,
		}
		ccRef, ccOut, _ := EmitCC(instance, aux.Rel()[strings.LastIndex(aux.Rel(), "/")+1:], aux, ccIn, ctx.host, ctx.emit)
		res.Refs = append(res.Refs, ccRef)
		res.Outputs = append(res.Outputs, ccOut)
	}

	return res
}
