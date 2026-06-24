package main

import (
	encb64 "encoding/base64"
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
	var genEntries []GenProtoResEntry

	for _, src := range protoSrcs {
		aux, gen := emitPyProtoSrc(ctx, instance, d, src, protocLDRef, protocBinary, pe)
		auxEntries = append(auxEntries, aux...)
		genEntries = append(genEntries, gen...)
	}

	auxRes := emitPyProtoAuxChunks(ctx, instance, d, peerContribs, auxEntries, cppSibling)

	if auxRes != nil {
		pyProtoRefs = append(pyProtoRefs, auxRes.Refs...)
		pyProtoOutputs = append(pyProtoOutputs, auxRes.Outputs...)
	}

	if objRes := emitGeneratedPyProtoObjcopy(ctx, instance, d, genEntries); objRes != nil {
		pyProtoRefs = append(pyProtoRefs, objRes.Refs...)
		pyProtoOutputs = append(pyProtoOutputs, objRes.Outputs...)
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

	globalPath := build(instance.Path.rel() + "/" + globalBaseName)
	result := &ProtoSrcsResult{
		GlobalRef:  &gRef,
		GlobalPath: &globalPath,
	}

	if cppSibling != nil && cppSibling.ARPath != nil {
		result.WholeArchiveRefs = append(result.WholeArchiveRefs, cppSibling.ARRef)
		result.WholeArchivePaths = append(result.WholeArchivePaths, *cppSibling.ARPath)
	} else if moduleExcludesTag(d, "CPP_PROTO") {
		result.WholeArchiveCmdPaths = append(result.WholeArchiveCmdPaths, build(instance.Path.rel()+"/"+archiveName(instance.Path.rel())))
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

	if d.useCommonGoogleAPIs {
		mid = append(mid, strISContribLibsGoogleapisCommonProtos)
	}

	if protoRoot != "" {
		mid = append(mid, internStr("-I=$(S)/"+protoRoot))

		if duplicateOutputRootInclude {
			mid = append(mid, internStr("-I=$(S)/"+protoRoot))
		}
	}

	for _, p := range protoInclude {
		token := internStr("-I=" + p.string())

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
		internStr("--python_out=$(B)/"+protoRoot),
	)

	pe.mid = appendArgStr(pe.mid, d.protocFlags)

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

func emitPyProtoSrc(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src string, protocLDRef NodeRef, protocBinary VFS, pe *PyPBModuleEmission) ([]PyProtoAuxEntry, []GenProtoResEntry) {
	na := ctx.na

	if d.moduleStmt.Name != tokProtoLibrary {
		return nil, nil
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

	protoSrcVFS := source(protoRelPath)
	protoCwd := strS

	var producerDeps []NodeRef
	var producerSourceInputs []VFS
	generated := false

	if info := codegenRegForInstance(ctx, instance).lookup(build(protoRelPath)); info != nil {
		protoSrcVFS = build(protoRelPath)
		protoCwd = strB
		producerDeps = []NodeRef{info.ProducerRef}
		generated = true

		producerSourceInputs = info.SourceInputs

		if len(info.ProducerSourceClosure) > 0 {
			producerSourceInputs = info.ProducerSourceClosure
		}
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

	yapyRes := emitGeneratedPyProtoYapyc(ctx, instance, pyYapyc, yapycTokens, pyPBRef, pyProtoSourceInputs(inputs))

	if yapyRes == nil {
		yapyRes = &GeneratedPyProtoYapycResult{}
	}

	if generated {
		return nil, genProtoResEntriesForSource(instance, d, src, yapycTokens, pyPBRef, outputs, yapyRes.Refs, yapyRes.Outputs)
	}

	return pyProtoAuxEntriesForSource(instance, d, src, pyPBRef, pyProtoSourceInputs(inputs), outputs, yapyRes.Refs, yapyRes.Outputs), nil
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

type GeneratedPyProtoYapycResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func emitGeneratedPyProtoYapyc(ctx *GenCtx, instance ModuleInstance, pyOutputs []VFS, tokens []string, pyPBRef NodeRef, sourceInputs []VFS) *GeneratedPyProtoYapycResult {
	na := ctx.na

	py3ccRef, py3ccSlowRef, py3ccBinary, py3ccSlowBin := py3ccToolRefs(ctx, instance)
	suffix := protoPySuffix(instance.Path.rel())
	res := &GeneratedPyProtoYapycResult{}

	for i, pyOut := range pyOutputs {
		uniq := ""

		if strings.Contains(tokens[i], "/") {
			uniq = "." + suffix
		}

		out := build(pyOut.rel() + uniq + ".yapyc3")
		cmdArgs := []STR{
			(py3ccBinary).str(),
			argSlowPy3cc.str(),
			(py3ccSlowBin).str(),
			internStr(tokens[i] + "-"),
			(pyOut).str(),
			(out).str(),
		}
		deps := []NodeRef{pyPBRef}
		toolRefs := depRefs(py3ccRef, py3ccSlowRef)

		nodeInputs := na.inputList(na.vfsList(py3ccBinary, py3ccSlowBin, pyOut), sourceInputs)

		if i > 0 {
			nodeInputs = append(nodeInputs, []VFS{pyOutputs[0]})
		}

		node := &Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envPYTHONHASHSEED, Value: strZero}}}),
			Env:          EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envPYTHONHASHSEED, Value: strZero}},
			Inputs:       nodeInputs,
			Outputs:      na.vfsList(out),
			KV:           &pyProtoKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			DepRefs:      deps,
			Resources:    usesPython3,
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

type GenProtoResEntry struct {
	token    string
	key      string
	output   VFS
	producer NodeRef
}

func genProtoResEntriesForSource(instance ModuleInstance, d *ModuleData, src string, tokens []string, pyPBRef NodeRef, pyOutputs []VFS, yapyRefs []NodeRef, yapyOuts []VFS) []GenProtoResEntry {
	var entries []GenProtoResEntry

	add := func(token, suffix string, output VFS, producer NodeRef) {
		entries = append(entries, GenProtoResEntry{
			token:    token,
			key:      "resfs/file/py/" + protoPythonResourceKey(instance, d, src, suffix),
			output:   output,
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

func emitGeneratedPyProtoObjcopy(ctx *GenCtx, instance ModuleInstance, d *ModuleData, entries []GenProtoResEntry) *ObjcopyEmitResult {
	if len(entries) == 0 {
		return nil
	}

	na := ctx.na
	oc := newObjcopyEmitCtx(ctx, d, instance.Platform)
	res := &ObjcopyEmitResult{}

	hashTag := stringPtr("PY3_PROTO")

	type chunk struct {
		paths   []string
		keysB64 []string
		kvsHash []string
		kvsCmd  []string
		inputs  []VFS
		deps    []NodeRef
		cmdLen  int
	}

	cur := chunk{}
	depSeen := map[NodeRef]struct{}{}

	flush := func() {
		if cur.cmdLen == 0 {
			return
		}

		hash := objcopyHash(cur.paths, cur.keysB64, cur.kvsHash, instance.Path.rel(), hashTag)
		outputObj := build(instance.Path.rel() + "/objcopy_" + hash + ".o")

		payload := make([]STR, 0, 2+len(cur.inputs)+len(cur.keysB64)+1+len(cur.kvsCmd))
		payload = append(payload, argInputs.str())

		for _, p := range cur.inputs {
			payload = append(payload, (p).str())
		}

		payload = append(payload, argKeys.str())
		payload = appendInternStrs(payload, cur.keysB64)
		payload = append(payload, argKvs.str())
		payload = appendInternStrs(payload, cur.kvsCmd)

		cmdArgs := objcopyCmdArgs(oc, outputObj, payload)
		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		node := &Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
			Env:          env,
			Inputs:       na.inputList(rescompilersChunk, cur.inputs, objcopyScriptChunk),
			Outputs:      na.vfsList(outputObj),
			KV:           &pyProtoKV2,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:    instance.Platform.UsesPython3Clang,
		}

		node.DepRefs = append(node.DepRefs, depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)...)
		node.DepRefs = append(node.DepRefs, cur.deps...)

		r := ctx.emit.emit(node)
		res.Refs = append(res.Refs, r)
		res.Outputs = append(res.Outputs, outputObj)
		cur = chunk{}
		depSeen = map[NodeRef]struct{}{}
	}

	for _, e := range entries {
		kvHash := "resfs/src/" + e.key + "=${rootrel;context=TEXT;input=TEXT:\"" + e.token + "\"}"
		kvCmd := "resfs/src/" + e.key + "=" + e.output.rel()
		kb64 := encb64.StdEncoding.EncodeToString([]byte(e.key))

		cur.paths = append(cur.paths, e.token)
		cur.keysB64 = append(cur.keysB64, kb64)
		cur.kvsHash = append(cur.kvsHash, kvHash)
		cur.kvsCmd = append(cur.kvsCmd, kvCmd)
		cur.inputs = append(cur.inputs, e.output)

		if e.producer != NodeRef(0) {
			if _, ok := depSeen[e.producer]; !ok {
				depSeen[e.producer] = struct{}{}
				cur.deps = append(cur.deps, e.producer)
			}
		}

		cur.cmdLen += rootCmdLen + len(e.token) + len(kb64) + len(kvHash) + len(kvCmd)

		if cur.cmdLen >= maxCmdLen {
			flush()
		}
	}

	flush()

	return res
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

		deduper.reset()

		for _, p := range ch.inputs {
			deduper.add(p)
		}

		tail := make([]VFS, 0, 1+len(auxClosure))

		if deduper.add(rescompilerBinVFS) {
			tail = append(tail, rescompilerBinVFS)
		}

		for _, p := range auxClosure {
			if p == aux {
				continue
			}

			if deduper.add(p) {
				tail = append(tail, p)
			}
		}

		ctx.emit.emitReserved(&Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
			Env:          env,
			Inputs:       na.inputList(ch.inputs, tail),
			Outputs:      na.vfsList(aux),
			KV:           &pyProtoKV3,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			DepRefs:      deps,
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

var (
	pyProtoKV  = KV{P: pkPY, PC: pcYellow}
	pyProtoKV2 = KV{P: pkPY, PC: pcYellow, ShowOut: true}
	pyProtoKV3 = KV{P: pkPR, PC: pcYellow, ShowOut: true}
)
