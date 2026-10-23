package main

import (
	"path/filepath"
	"strings"
)

func isPythonProtoModule(d *ModuleData) bool {
	return d.unit.Tag == unitTagPy3Proto || d.unit.Tag == strPyProto
}

func protoPythonVersion(d *ModuleData) string {
	if d.usePython2 {
		return "py2"
	}

	return "py3"
}

type pyPBPending struct {
	emit         *EmitContext
	ctx          *GenCtx
	instance     ModuleInstance
	scanner      *IncludeScanner
	scanCtx      *ScanContext
	cmdArgs      ArgChunks
	protoCwd     VFS
	toolInputs   []VFS
	sourceInputs []VFS
	protoInput   VFS
	outputs      []VFS
	extOut       []KVExt
	toolRefs     []NodeRef
	pyPBRef      NodeRef
}

func (p *pyPBPending) emitPending() {
	s := *p

	*p = pyPBPending{}

	imports := s.scanner.walkClosure(s.protoInput, s.scanCtx, scanDomainProto)
	buckets := imports.bucketList()
	na := s.ctx.na
	inputChunks := na.inputs.alloc(3 + len(buckets))[:0]

	inputChunks = append(inputChunks, s.toolInputs, s.sourceInputs, na.vfsList(s.protoInput))
	inputChunks = append(inputChunks, buckets...)
	na.inputs.commit(len(inputChunks))

	node := Node{
		Platform:      s.instance.Platform,
		Cmds:          na.cmdList(Cmd{CmdArgs: s.cmdArgs, Cwd: s.protoCwd, Env: envVarsVCS}),
		Env:           envVarsVCS,
		Inputs:        InputChunks(inputChunks[:len(inputChunks):len(inputChunks)]),
		InputClosures: []Closure{imports},
		Outputs:       s.outputs,
		KV:            &pbKV,
		KVExts:        s.extOut,
		Resources:     usesPython3,
	}

	if len(s.toolRefs) > 0 {
		node.ForeignDepRefs = s.toolRefs
	}

	s.emit.emitReservedNode(node, s.pyPBRef)
}

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

func pyProtoLogicalModule(module, src string, scope PyScope) string {
	if scope.ModuleName != nil {
		return scope.ModuleName.string()
	}

	prefix := strings.ReplaceAll(module, "/", ".") + "."

	if scope.Namespace != nil {
		prefix = scope.Namespace.string() + "."
	}

	if scope.TopLevel {
		prefix = ""
	}

	return prefix + strings.ReplaceAll(strings.TrimSuffix(src, ".proto"), "/", ".")
}

func pyProtoOutputToken(instance ModuleInstance, out VFS, generatedProto bool) ANY {
	if generatedProto {
		return internStr(trimModulePrefix(out.relString(), instance.Path.relString())).any()
	}

	return internV("${ARCADIA_BUILD_ROOT}/", out.relString()).any()
}

func (e *EmitContext) enqueuePyProtoOutputs(src string, srcGroup int, outputs []VFS, generatedProto bool, logicalModule STR) {
	instance, d := e.instance, e.d
	keyDir, keySep, keyBase := protoPythonResourceKeyParts(instance, d, src)

	for i, suffix := range pyProtoSuffixes(d) {
		key := keyDir + keySep + keyBase + suffix

		if logicalModule != 0 {
			key = logicalModule.string() + strings.TrimSuffix(suffix, filepath.Ext(suffix))

			if !d.usePython2 {
				key += ".py"
			}
		} else {
			key = strings.ReplaceAll(key, "-", "_")

			if d.usePython2 {
				key = strings.ReplaceAll(strings.TrimSuffix(key, ".py"), "/", ".")
			}
		}

		out := outputs[i]

		e.enqueueSrc(SrcMeta{
			Source: out.any(), Prio: stmtPrioDefault,
			PyMeta: e.addPyMeta(PySourceMeta{
				Module: internStr(key),
				Token:  pyProtoOutputToken(instance, out, generatedProto),
				Group:  srcGroup,
				Kind:   pySourceProto,
			}),
		})
	}
}

type PyPBModuleEmission struct {
	mypyRef        NodeRef
	mypyBinary     VFS
	pluginRefs     []NodeRef
	pluginBinaries []VFS
	suffixes       []string
	head           []ANY
	mid            []ANY
	tail           []ANY
}

func (e *EmitContext) pyProtoIncludePlan() []VFS {
	return concat(dedup(e.d.protoIncludeLocal), e.peers.ProtoInclude)
}

func (e *EmitContext) newPyPBModuleEmission(protocBinary VFS) *PyPBModuleEmission {
	if e.pyPBOk {
		return &e.pyPBEmission
	}

	e.pyPBOk = true

	ctx, _, d := e.ctx, e.instance, e.d
	pe := &e.pyPBEmission
	protoInclude := e.pyProtoIncludePlan()

	*pe = PyPBModuleEmission{}

	for _, plugin := range d.pyProtoPlugins {
		ref, binary := ctx.tool(internArg(plugin.ToolPath))

		pe.pluginRefs = append(pe.pluginRefs, ref)
		pe.pluginBinaries = append(pe.pluginBinaries, binary)
	}

	if !d.noMypy {
		pe.mypyRef, pe.mypyBinary = ctx.tool(argContribPythonMypyProtobufBinProtocGenMypy)
	}

	suffixes := pyProtoSuffixes(d)

	if !d.noMypy {
		suffixes = append(suffixes, "_pb2.pyi")
	}

	pe.suffixes = suffixes

	protoRoot := protoPythonOutputRoot(d)
	na := ctx.na
	head := na.anys.alloc(6 + len(suffixes))[:0]

	head = append(head,
		d.tc.Python3.any(),
		pbPyWrapperVFS.any(),
		argPyVer.any(), internStr(protoPythonVersion(d)).any(),
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
		protocBinary.any(),
		internV("-I=./", protoRoot).any(),
		internV("-I=$(S)/", protoRoot).any(),
		argIB2.any(),
		argIS3.any(),
	)

	for _, p := range protoInclude {
		mid = append(mid, internV("-I=", p.string()).any())
	}

	mid = append(mid,
		argIB2.any(),
		argISContribLibsProtobufSrc.any(),
		internV("--python_out=$(B)/", protoRoot).any(),
	)

	mid = appendAnyLists(mid, d.protocFlags)
	na.anys.commit(len(mid))
	pe.mid = mid[:len(mid):len(mid)]

	tail := na.anys.alloc(2 * (len(d.pyProtoPlugins) + 1))[:0]

	for i, plugin := range d.pyProtoPlugins {
		binary := pe.pluginBinaries[i]

		tail = append(tail,
			internV("--plugin=protoc-gen-", plugin.Name, "=", binary.prefix(), binary.relString()).any(),
			internV("--", plugin.Name, "_out=$(B)/", protoRoot).any(),
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

	return pe
}

func (e *EmitContext) emitPyProtoSource(srcTok ANY, srcGroup int, logicalModule STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	src := e.moduleSourceName(srcTok)
	protoc := argContribToolsProtoc

	if d.usePython2 {
		protoc = argContribToolsProtocOld
	}

	protocLDRef, protocBinary := ctx.tool(protoc)
	pe := e.newPyPBModuleEmission(protocBinary)
	scanCtx := d.scanCtx
	protoSrcVFS := resolveSourceOrGeneratedVFS(ctx, instance, srcTok.string(), d.srcDirs)
	protoRel := e.protoSourceRel(protoSrcVFS.string())
	protoRelPath := protoRel.string()
	protoBase := strings.TrimSuffix(protoRelPath, ".proto")
	internal := "__int" + protoPythonVersion(d) + "__"
	pyOut := build(protoBase, internal, "_pb2.py")
	outputs := na.vfs.alloc(len(pe.suffixes))[:0]

	for _, suffix := range pe.suffixes {
		outputs = append(outputs, build(protoBase, internal, suffix))
	}

	na.vfs.commit(len(outputs))
	outputs = outputs[:len(outputs):len(outputs)]

	if e.codegen.lookup(pyOut) != nil {
		e.enqueuePyProtoOutputs(src, srcGroup, outputs, protoSrcVFS.isBuild(), logicalModule)

		return
	}

	relChunk := na.anyList(protoRel.any())
	chunks := na.chunks.alloc(5)[:0]

	chunks = append(chunks, pe.head, relChunk, pe.mid, relChunk)

	if len(pe.tail) > 0 {
		chunks = append(chunks, pe.tail)
	}

	na.chunks.commit(len(chunks))

	cmdArgs := ArgChunks(chunks[:len(chunks):len(chunks)])
	toolRefs := na.noderefs.alloc(1 + len(pe.pluginRefs) + 1)[:0]

	for _, r := range append([]NodeRef{protocLDRef}, pe.pluginRefs...) {
		if r != 0 {
			toolRefs = append(toolRefs, r)
		}
	}

	if !d.noMypy && pe.mypyRef != 0 {
		toolRefs = append(toolRefs, pe.mypyRef)
	}

	na.noderefs.commit(len(toolRefs))

	toolRefs = toolRefs[:len(toolRefs):len(toolRefs)]

	protoCwd := srcRootDirVFS
	generatedProto := protoSrcVFS.isBuild()

	if generatedProto {
		e.codegen.use(protoSrcVFS)
		protoCwd = bldRootDirVFS
	}

	toolInputs := na.vfs.alloc(1 + len(pe.pluginBinaries) + 1)[:0]

	toolInputs = append(toolInputs, protocBinary)
	toolInputs = append(toolInputs, pe.pluginBinaries...)

	if !d.noMypy {
		toolInputs = append(toolInputs, pe.mypyBinary)
	}

	na.vfs.commit(len(toolInputs))
	toolInputs = toolInputs[:len(toolInputs):len(toolInputs)]

	sourceInputsHead := na.vfs.alloc(2)[:0]

	sourceInputsHead = append(sourceInputsHead, pbPyWrapperVFS)

	if !generatedProto {
		sourceInputsHead = append(sourceInputsHead, protoSrcVFS)
	}

	na.vfs.commit(len(sourceInputsHead))
	sourceInputsHead = sourceInputsHead[:len(sourceInputsHead):len(sourceInputsHead)]

	protoBaseName := filepath.Base(protoBase)
	extOut := na.exts.alloc(len(outputs))[:0]

	for i, out := range outputs {
		keyStart := len(e.resStrBuf)

		e.resStrBuf = append(e.resStrBuf, "ext_out_name_for_"...)
		e.resStrBuf = append(e.resStrBuf, filepath.Base(out.relString())...)

		key := internBytes(e.resStrBuf[keyStart:])
		valStart := len(e.resStrBuf)

		e.resStrBuf = append(e.resStrBuf, protoBaseName...)
		e.resStrBuf = append(e.resStrBuf, pe.suffixes[i]...)

		val := internBytes(e.resStrBuf[valStart:])

		e.resStrBuf = e.resStrBuf[:keyStart]
		extOut = append(extOut, KVExt{Key: key.string(), Val: val.string()})
	}

	na.exts.commit(len(outputs))
	extOut = extOut[:len(outputs):len(outputs)]

	pyPBRef := ctx.emit.reserve()
	pending := na.pyPB.one()

	*pending = pyPBPending{
		emit: e, ctx: ctx, instance: instance, scanner: e.scanner, scanCtx: scanCtx,
		cmdArgs: cmdArgs, protoCwd: protoCwd,
		toolInputs: toolInputs, sourceInputs: sourceInputsHead, protoInput: protoSrcVFS,
		outputs: outputs, extOut: extOut,
		toolRefs: toolRefs, pyPBRef: pyPBRef,
	}

	pyPBPE := e.pendingEmitter(pending)

	for _, out := range outputs {
		e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: pyPBRef, OnUse: pyPBPE})
	}

	e.codegen.setMainOut(pyPBRef, outputs)

	e.enqueuePyProtoOutputs(src, srcGroup, outputs, generatedProto, logicalModule)
}

func protoPythonOutputRoot(d *ModuleData) string {
	if d.protoNamespace != nil {
		root := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(d.protoNamespace.string())), "/")

		if root != "." && root != "" {
			return root
		}
	}

	return ""
}
