package main

import (
	"path/filepath"
	"strings"
)

func protoPythonResourceKey(instance ModuleInstance, d *moduleData, src, suffix string) string {
	base := strings.TrimSuffix(src, ".proto")

	if d.pyNamespace == nil {
		return instance.Path.Rel() + "/" + base + suffix
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
		ThrowFmt("gen: py-addressed PROTO_LIBRARY %s with .ev sources is not modelled", instance.Path.Rel())
	}

	if len(protoSrcs) == 0 {
		return nil
	}

	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)

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
	globalBaseName := globalArchiveNameWithPrefixOrName(instance.Path.Rel(), "libpy3", "")
	gRef := EmitARGlobalNamedTagged(pyInstance, globalBaseName, tagPy3ProtoGlobal, pyProtoRefs, pyProtoOutputs, d.tc, ctx.host, ctx.emit)

	globalPath := Build(instance.Path.Rel() + "/" + globalBaseName)
	result := &protoSrcsResult{
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
		result.WholeArchiveCmdPaths = append(result.WholeArchiveCmdPaths, Build(instance.Path.Rel()+"/"+ArchiveName(instance.Path.Rel())))
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

	// A grpc module without an explicit PROTO_NAMESPACE emits at the build root
	// ("" — protoPythonOutputRoot's instance.Path default does not apply to grpc).
	// An explicit PROTO_NAMESPACE (e.g. contrib/proto/grpc on grpc reflection) wins.
	if d.grpc && d.protoNamespace == nil {
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
		grpcPyRef, grpcPyBinary = ctx.tool(argContribToolsProtocPluginsGrpcPython)
	}

	if !d.noMypy {
		mypyRef, mypyBinary = ctx.tool(argContribPythonMypyProtobufBinProtocGenMypy)
	}

	cmdArgs := []STR{
		d.tc.Python3,
		internStr(pbPyWrapperPath),
		argPyVer.str(), argPy3.str(),
		argSuffixes.str(),
	}
	cmdArgs = appendInternStrs(cmdArgs, suffixes)
	cmdArgs = append(cmdArgs,
		argInput.str(), internStr(protoRelPath),
		argNs.str(), internStr(protoPythonNamespaceArg(d)),
		arg2.str(),
		(protocBinary).str(),
		internStr("-I=./"+protoRoot),
		internStr("-I=$(S)/"+protoRoot),
		argIB2.str(),
		argIS3.str(),
	)

	if protoRoot != "" && protoRoot != "contrib/libs/protobuf/src" {
		cmdArgs = append(cmdArgs, internStr("-I=$(S)/"+protoRoot))

		// A GLOBAL PROTO_NAMESPACE on a grpc module re-contributes its output root
		// as an addincl, so protoc receives the -I twice (mirrors EmitPB's
		// duplicateOutputRootInclude for the cpp side).
		if d.grpc && d.protoNamespaceGlobal {
			cmdArgs = append(cmdArgs, internStr("-I=$(S)/"+protoRoot))
		}
	}

	cmdArgs = append(cmdArgs, argISContribLibsProtobufSrc.str())

	if d.grpc {
		cmdArgs = append(cmdArgs, argISContribLibsProtocSrc.str())
	}

	cmdArgs = append(cmdArgs,
		argIB2.str(),
		argISContribLibsProtobufSrc.str(),
		internStr("--python_out=$(B)/"+protoRoot),
		internStr(protoRelPath),
	)

	if d.grpc {
		cmdArgs = append(cmdArgs,
			internStr("--plugin=protoc-gen-grpc_py="+grpcPyBinary.String()),
			internStr("--grpc_py_out=$(B)/"+protoRoot),
		)
	}

	if !d.noMypy {
		cmdArgs = append(cmdArgs,
			internStr("--plugin=protoc-gen-mypy="+mypyBinary.String()),
			internStr("--mypy_out=$(B)/"+protoRoot),
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
		Platform:         instance.Platform,
		Cmds:             []Cmd{{CmdArgs: cmdArgs, Cwd: strS, Env: EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}}},
		Env:              EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}},
		Inputs:           inputChunks{inputs},
		Outputs:          outputs,
		KV:               pbKV,
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel(), ModuleTag: tagPy3Proto},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          toolRefs,
		usesResources:    []string{resourcePatternYMakePython3},
	}

	if len(toolRefs) > 0 {
		pyPBNode.ForeignDepRefs = toolRefs
	}

	pyPBRef := ctx.emit.Emit(pyPBNode)
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

	return instance.Path.Rel()
}

type generatedPyProtoYapycResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func emitGeneratedPyProtoYapyc(ctx *genCtx, instance ModuleInstance, pyOutputs []VFS, pyPBRef NodeRef, sourceInputs []VFS) *generatedPyProtoYapycResult {
	py3ccRef, py3ccSlowRef, py3ccBinary, py3ccSlowBin := py3ccToolRefs(ctx, instance)
	suffix := protoPySuffix(instance.Path.Rel())
	res := &generatedPyProtoYapycResult{}

	for i, pyOut := range pyOutputs {
		out := Build(pyOut.Rel() + "." + suffix + ".yapyc3")
		cmdArgs := []STR{
			(py3ccBinary).str(),
			argSlowPy3cc.str(),
			(py3ccSlowBin).str(),
			internStr(pyOut.Rel() + "-"),
			(pyOut).str(),
			(out).str(),
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

		// sourceInputs is shared across the pyOutputs loop — its own chunk,
		// referenced, not copied per node.
		nodeInputs := inputChunks{{py3ccBinary, py3ccSlowBin, pyOut}, sourceInputs}

		if i > 0 {
			nodeInputs = append(nodeInputs, []VFS{pyOutputs[0]})
		}

		node := &Node{
			Platform:         instance.Platform,
			Cmds:             []Cmd{{CmdArgs: cmdArgs, Env: EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envPYTHONHASHSEED, Value: strZero}}}},
			Env:              EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envPYTHONHASHSEED, Value: strZero}},
			Inputs:           nodeInputs,
			Outputs:          []VFS{out},
			KV:               KV{P: pkPY, PC: pcYellow},
			TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel(), ModuleTag: tagPy3Proto},
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			DepRefs:          deps,
			usesResources:    []string{resourcePatternYMakePython3},
		}

		if len(toolRefs) > 0 {
			node.ForeignDepRefs = toolRefs
		}

		res.Refs = append(res.Refs, ctx.emit.Emit(node))
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
	deduper.reset()

	for _, input := range inputs {
		if !input.IsSource() {
			continue
		}

		if !deduper.add(input) {
			continue
		}

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
		aux := Build(instance.Path.Rel() + "/" + protoResourceHash(ch.hashInputs, "$S/"+instance.Path.Rel(), "PY3_PROTO") + "_raw.auxcpp")
		auxClosure := pyProtoAuxInputClosure(ctx, instance, d, aux, ch.inputs, peerAddIncl)
		cmdArgs := []STR{internStr(rescompilerBinPath), (aux).str()}
		cmdArgs = appendInternStrs(cmdArgs, ch.cmdArgs)

		deps := append([]NodeRef(nil), ch.deps...)

		if rescompilerRef != (NodeRef(0)) {
			deps = append(deps, rescompilerRef)
		}

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

		ref := ctx.emit.Emit(&Node{
			Platform:         instance.Platform,
			Cmds:             []Cmd{{CmdArgs: cmdArgs, Env: env}},
			Env:              env,
			Inputs:           inputChunks{ch.inputs, tail},
			Outputs:          []VFS{aux},
			KV:               KV{P: pkPR, PC: pcYellow, ShowOut: true},
			TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel(), ModuleTag: tagPy3Proto},
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			DepRefs:          deps,
		})

		ccIn := ModuleCCInputs{
			TC:                   d.tc,
			InclArgs:             ctx.inclArgs,
			Flags:                d.flags,
			AddIncl:              d.addIncl,
			PeerAddInclGlobal:    peerAddIncl,
			PeerCFlagsGlobal:     peerContribs.cFlags,
			PeerCXXFlagsGlobal:   peerContribs.cxxFlags,
			PeerCOnlyFlagsGlobal: peerContribs.cOnlyFlags,
			ModuleScopeCFlags:    d.moduleScopeCFlags,
			PerSourceCFlags:      []ARG{argX, argC},
			SourceRoot:           ctx.sourceRoot,
			FS:                   ctx.fs,
			ExtraDepRefs:         []NodeRef{ref},
			Py3Suffix:            true,
			ForceCxx:             true,
			ModuleTag:            tagPy3Proto,
			IncludeInputs:        auxClosure,
		}
		ccRef, ccOut, _ := EmitCC(instance, aux.Rel()[strings.LastIndex(aux.Rel(), "/")+1:], aux, ccIn, ctx.host, ctx.emit)
		res.Refs = append(res.Refs, ccRef)
		res.Outputs = append(res.Outputs, ccOut)
	}

	return res
}
