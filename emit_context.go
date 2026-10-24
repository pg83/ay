package main

import (
	"slices"
	"strings"
)

type PeerContext struct {
	Modules           []VFS
	GlobalSources     []VFS
	SelfAddInclGlobal []VFS
	PeerAddInclGlobal []VFS
	ResourceGlobals   []ResourceDecl
	ProtoInclude      []VFS
	FbsNamespaces     []ANY
	FlatcIncludeArgs  []ANY
	CythonInclude     []VFS
	LDFlagsGlobal     []ANY
	LDResources       []STR
}

type EmitContext struct {
	moduleIncludeRoots []VFS
	moduleGlobals      []VFS
	moduleGlobalInputs []VFS
	globalSources      []VFS
	ctx                *GenCtx
	frame              *ModuleFrame
	instance           ModuleInstance
	d                  *ModuleData
	peers              *PeerContext
	scanner            *IncludeScanner
	codegen            *CodegenRegistry
	srcs               []SrcMeta
	commandHead        int
	commandQueueActive bool
	producerOutputs    map[VFS]int
	deferCC            bool
	ccPending          []CCPending
	filePending        []func()
	srcsClosed         bool
	refs               []NodeRef
	outs               []VFS
	metas              []SrcMeta
	objcopyRes         *ObjcopyEmitResult
	protoRes           *ProtoSrcsResult
	goRes              *GoSrcsResult
	goInclJoined       []ANY
	pyMetas            []PySourceMeta
	pySrcsReg          []PySrc
	resources          []ResourceEntry
	localObjs          CollectedObjs
	globalObjs         CollectedObjs
	prodPos            []ProducerPos
	prodBacking        []VFS
	prodSrcs           []SrcMeta
	scInputs           []VFS
	objScratch         []ResourceItem
	rawScratch         []ResourceItem
	resItems           []ResourceItem
	dirScratch         []IncludeDirective
	resEntries         []PyGenResEntry
	resStrBuf          []byte
	prodVFS            []VFS
	cvScratch          []Closure
	peerScratch        []string
	sbomOrder          []ResolvedPeer
	orderedCC          []VFS
	prodOrder          []int
	protoPaths         []protoPathEntry
	protoPathPos       int
	pbEmission         [3]PbModuleEmission
	pbEmissionOk       [3]bool
	pyPBEmission       [2]PyPBModuleEmission
	pyPBOk             [2]bool
	objcopyCtx         ObjcopyEmitCtx
	objcopyOk          bool
	protoResVal        ProtoSrcsResult
}

type CollectedObjs struct {
	refs  []NodeRef
	outs  []VFS
	metas []SrcMeta
}

func (c CollectedObjs) reset() CollectedObjs {
	return CollectedObjs{refs: c.refs[:0], outs: c.outs[:0], metas: c.metas[:0]}
}

func (e *EmitContext) partitionCollected() (local, global CollectedObjs) {
	local = e.localObjs
	global = e.globalObjs

	firstGlobal := -1

	for i, m := range e.metas {
		if m.Global {
			firstGlobal = i

			break
		}
	}

	if firstGlobal < 0 {
		e.localObjs = local.reset()
		e.globalObjs = global.reset()

		return CollectedObjs{refs: e.refs, outs: e.outs, metas: e.metas}, global
	}

	if n := len(e.metas); cap(local.refs) < n {
		local.refs = make([]NodeRef, 0, n)
		local.outs = make([]VFS, 0, n)
		local.metas = make([]SrcMeta, 0, n)
	}

	local.refs = append(local.refs, e.refs[:firstGlobal]...)
	local.outs = append(local.outs, e.outs[:firstGlobal]...)
	local.metas = append(local.metas, e.metas[:firstGlobal]...)

	global.refs = append(global.refs, e.refs[firstGlobal])
	global.outs = append(global.outs, e.outs[firstGlobal])
	global.metas = append(global.metas, e.metas[firstGlobal])

	for i := firstGlobal + 1; i < len(e.metas); i++ {
		m := e.metas[i]
		dst := &local

		if m.Global {
			dst = &global
		}

		dst.refs = append(dst.refs, e.refs[i])
		dst.outs = append(dst.outs, e.outs[i])
		dst.metas = append(dst.metas, m)
	}

	e.localObjs = local.reset()
	e.globalObjs = global.reset()

	return local, global
}

func newEmitContextIn(frame *ModuleFrame, ctx *GenCtx, instance ModuleInstance, d *ModuleData, peers *PeerContext) *EmitContext {
	scanner := ctx.scannerFor(instance)

	if d.dontResolveIncludes {
		scanner = scanner.withoutIncludeResolution()
	}

	k := len(d.resources)
	prev := &frame.emitCtx

	frame.emitCtx = EmitContext{
		ctx: ctx, frame: frame, instance: instance, d: d, peers: peers, scanner: scanner, codegen: scanner.codegen,
		resources: d.resources[:k:k],

		ccPending:   scrub(prev.ccPending),
		filePending: scrub(prev.filePending),
		srcs:        prev.srcs[:0],
		refs:        prev.refs[:0],
		outs:        prev.outs[:0],
		metas:       prev.metas[:0],
		pyMetas:     scrub(prev.pyMetas),
		pySrcsReg:   prev.pySrcsReg[:0],
		localObjs:   prev.localObjs.reset(),
		globalObjs:  prev.globalObjs.reset(),
		prodPos:     scrub(prev.prodPos),
		prodBacking: prev.prodBacking[:0],
		prodSrcs:    prev.prodSrcs[:0],
		scInputs:    prev.scInputs[:0],
		objScratch:  scrub(prev.objScratch),
		rawScratch:  scrub(prev.rawScratch),
		resItems:    scrub(prev.resItems),
		dirScratch:  prev.dirScratch[:0],
		resEntries:  scrub(prev.resEntries),
		resStrBuf:   prev.resStrBuf[:0],
		prodVFS:     prev.prodVFS[:0],
		cvScratch:   scrub(prev.cvScratch),
		peerScratch: scrub(prev.peerScratch),
		sbomOrder:   scrub(prev.sbomOrder),
		orderedCC:   prev.orderedCC[:0],
		prodOrder:   prev.prodOrder[:0],
		protoPaths:  prev.protoPaths[:0],
	}

	return &frame.emitCtx
}

func (e *EmitContext) emitNode(node Node) NodeRef {
	e.resolveNodeCodegenDeps(&node)

	return e.ctx.emit.emitNodePtr(&node)
}

func (e *EmitContext) emitReservedNode(node Node, ref NodeRef) {
	e.resolveNodeCodegenDeps(&node)
	e.ctx.emit.emitReservedNodePtr(&node, ref)
}

func (e *EmitContext) resolveNodeCodegenDeps(node *Node) {
	e.includeNodeInputs(node)
	e.resolveNodeCodegenRefs(node)
}

func (e *EmitContext) resolveNodeCodegenRefs(node *Node) {
	if len(node.Resources) > 0 && e.peers != nil {
		node.ResourceGlobals = e.ctx.declSlices.internCopy(e.peers.ResourceGlobals)
	}

	var refs []NodeRef
	var mains []VFS

	for _, chunk := range node.Inputs {
		for _, input := range chunk {
			if input.isSource() {
				continue
			}

			info := e.codegen.lookupSTR(input.rel())

			if info == nil {
				continue
			}

			if refs == nil {
				refs = nodeRefScratches.get()
			}

			if info.OnUse != nil {
				fireGenerated(info)
			}

			refs = append(refs, info.ProducerRef)

			if main := info.ProducerMainOut; main != 0 && main != input {
				if mains == nil {
					mains = vfsScratches.get()
				}

				mains = append(mains, main)
			}
		}
	}

	if len(mains) > 0 {
		dedupers.with(func(deduper *DeDuper) {
			for _, chunk := range node.Inputs {
				for _, input := range chunk {
					deduper.addStable(input.strID())
				}
			}

			extra := e.ctx.na.vfs.alloc(len(mains))[:0]

			for _, main := range mains {
				if deduper.addStable(main.strID()) {
					extra = append(extra, main)
				}
			}

			e.ctx.na.vfs.commit(len(extra))

			if len(extra) > 0 {
				node.Inputs = appendInputChunks(e.ctx.na, node.Inputs, e.ctx.na.inputList(extra))
			}
		})

		vfsScratches.put(mains)
	}

	if len(refs) == 0 {
		return
	}

	if len(node.DepRefs)+len(refs) <= 16 {
		out := e.ctx.na.noderefs.alloc(len(node.DepRefs) + len(refs))
		k := copy(out, node.DepRefs)

		for _, ref := range refs {
			seen := false

			for _, previous := range out[:k] {
				if previous == ref {
					seen = true

					break
				}
			}

			if !seen {
				out[k] = ref
				k++
			}
		}

		e.ctx.na.noderefs.commit(k)
		node.DepRefs = out[:k]
		nodeRefScratches.put(refs)

		return
	}

	var result []NodeRef

	dedupers.with(func(deduper *DeDuper) {
		out := e.ctx.na.noderefs.alloc(len(node.DepRefs) + len(refs))
		k := 0

		for _, ref := range node.DepRefs {
			deduper.addStable(ref.strID())
			out[k] = ref
			k++
		}

		for _, ref := range refs {
			if !deduper.addStable(ref.strID()) {
				continue
			}

			out[k] = ref
			k++
		}

		e.ctx.na.noderefs.commit(k)
		result = out[:k]
	})

	node.DepRefs = result
	nodeRefScratches.put(refs)
}

func (e *EmitContext) includeNodeInputs(node *Node) {
	if node.InputClosure.self != 0 || node.InputClosure.buckets != nil {
		return
	}

	var extra []Closure
	var included InputChunks
	var templates, textTemplates []VFS

	for _, chunk := range node.Inputs {
		for _, input := range chunk {
			if info := e.codegen.lookup(input); info != nil && len(info.IncludedBuildInputs) > 0 {
				included = append(included, info.IncludedBuildInputs)
			}
		}
	}

	for _, chunk := range node.buildFromInputs() {
		for _, input := range chunk {
			if deps := e.codegen.moduleIncludedDeps(input); deps != nil {
				included = append(included, deps.Inputs)
				node.DepRefs = concat(node.DepRefs, deps.Refs)
			}

			if info := e.codegen.lookup(input); info != nil && info.IncludedInputs != nil {
				included = append(included, info.IncludedInputs.sourceInputs(e.ctx.na))
			}

			template := strings.HasSuffix(input.relString(), ".in")
			text := input.isSource() && slices.Contains(node.TextInputs, input)

			if template {
				if text {
					textTemplates = append(textTemplates, input)
				} else {
					templates = append(templates, input)
				}
			}

			if input.isBuild() && e.codegen.lookup(input) != nil || input.isSource() && !text {
				if cl := e.inputClosure(input); cl.buckets != nil {
					extra = append(extra, cl)
				}
			}
		}
	}

	if len(templates)+len(textTemplates) > 0 && len(node.Outputs) == 1 {
		e.codegen.forwardTemplateInputs(node.Outputs[0], e.ctx.na.vfsList(templates...), e.ctx.na.vfsList(textTemplates...))
	}

	if len(extra)+len(included) == 0 {
		return
	}

	dedupers.with(func(dd *DeDuper) {
		bound := len(node.Inputs) + len(included)

		for _, cl := range extra {
			bound += len(cl.bucketList())
		}

		chunks := e.ctx.na.inputs.alloc(bound)[:0]

		for _, chunk := range node.Inputs {
			chunks = append(chunks, chunk)

			for _, input := range chunk {
				dd.addStable(input.strID())
			}
		}

		for _, bucket := range included {
			if unseen := e.ctx.na.filterSeen(dd, bucket); len(unseen) > 0 {
				chunks = append(chunks, unseen)
			}
		}

		for _, cl := range extra {
			for _, bucket := range cl.bucketList() {
				if unseen := e.ctx.na.filterSeen(dd, bucket); len(unseen) > 0 {
					chunks = append(chunks, unseen)
				}
			}
		}

		e.ctx.na.inputs.commit(len(chunks))
		node.Inputs = chunks[:len(chunks):len(chunks)]
	})
}

func (e *EmitContext) textInputSources(inputs []VFS, parsedCount int) []VFS {
	text := e.ctx.na.vfs.alloc(len(inputs) - parsedCount)[:0]

	for _, input := range inputs[parsedCount:] {
		if input.isSource() && !slices.Contains(inputs[:parsedCount], input) {
			text = append(text, input)
		}
	}

	e.ctx.na.vfs.commit(len(text))

	return text[:len(text):len(text)]
}

func (e *EmitContext) inputClosure(input VFS) Closure {
	cl := e.includeClosure(input)

	e.scanner.reportClosureDiagnostics(input, cl)

	return cl
}

func (e *EmitContext) includeClosure(input VFS) Closure {
	if e.d.scanCtx == nil {
		e.d.scanCtx = newScanContext(e.ctx.na, e.ctx.parsers, nil, nil, nil, includeScannerBasePaths())
	}

	cl, cached := e.scanner.closure(input)

	if !cached {
		cl = e.scanner.scanClosureWithDefaultParser(input, e.d.scanCtx, scanDomainCC, emptyParser)
	}

	return cl
}

func (e *EmitContext) resStr2(a, b string) string {
	start := len(e.resStrBuf)

	e.resStrBuf = append(e.resStrBuf, a...)
	e.resStrBuf = append(e.resStrBuf, b...)

	return bytesString(e.resStrBuf[start:])
}

func (e *EmitContext) collectObj(ref NodeRef, out VFS, meta SrcMeta) {
	if isPythonProtoModule(e.d) && meta.Global {
		if e.objcopyRes == nil {
			e.objcopyRes = &ObjcopyEmitResult{}
		}

		e.objcopyRes.Refs = append(e.objcopyRes.Refs, ref)
		e.objcopyRes.Outputs = append(e.objcopyRes.Outputs, out)

		return
	}

	e.refs = append(e.refs, ref)
	e.outs = append(e.outs, out)
	e.metas = append(e.metas, meta)
}

func (e *EmitContext) enqueueSrc(meta SrcMeta) {
	assert(!e.srcsClosed, "enqueueSrc after source queue closed")

	if isPythonProtoModule(e.d) {
		meta.Stmt = -len(e.d.srcs) - len(e.srcs) - 1
	}

	if cap(e.srcs) == 0 {
		e.srcs = make([]SrcMeta, 0, len(e.d.srcs)+8)
	}

	e.srcs = append(e.srcs, meta)

	if e.commandQueueActive && e.deferCC {
		e.queueSource(len(e.srcs) - 1)
	}
}

func (e *EmitContext) producersOnly() bool {
	return e.instance.Demand == demandNone && (e.instance.Kind == KindForeignDescent || e.instance.Language == LangFbsJavaNative)
}

func (e *EmitContext) register(info GeneratedFileInfo) *GeneratedFileInfo {
	info.OwnerModule = e.instance.Path
	info.OwnerSerial = e.frame.serial

	e.collectScriptOutput(info.OutputPath)

	return e.codegen.register(info)
}

func (e *EmitContext) collectScriptOutput(output VFS) {
	if e.d.docs != nil || isUnionModule(e.d.moduleStmt.Name) || e.d.moduleStmt.Name == tokPackageFinal || e.d.moduleStmt.Name == tokCiGroup || e.d.moduleStmt.Name == tokGenLibrary {
		e.outs = append(e.outs, output)
	}
}

func (e *EmitContext) appendModuleOutputs(node *Node) {
	added := false

	for _, meta := range e.metas {
		if src := meta.Source.vfs(); src != 0 && src.isBuild() {
			if info := e.codegen.lookup(src); info != nil && info.OwnerModule == e.instance.Path && len(info.ModuleOutputs) > 0 {
				node.Outputs = append(node.Outputs, info.ModuleOutputs...)
				node.Inputs = appendInputChunks(e.ctx.na, node.Inputs, e.ctx.na.inputList(e.ctx.na.vfsList(src)))
				node.DepRefs = append(node.DepRefs, info.ProducerRef)

				added = true
			}
		}
	}

	if added {
		node.DepRefs = e.ctx.na.uniqueRefs(node.DepRefs)
	}
}

func (e *EmitContext) pendingEmit(fn func()) *PendingEmit {
	p := e.ctx.na.pending.one()

	*p = PendingEmit{fn: fn, frame: e.frame, publication: e.ctx.emit.pendingPublication()}
	e.frame.pending++

	return p
}

func (e *EmitContext) pendingEmitter(emitter pendingEmitter) *PendingEmit {
	p := e.ctx.na.pending.one()

	*p = PendingEmit{emitter: emitter, frame: e.frame, publication: e.ctx.emit.pendingPublication()}
	e.frame.pending++

	return p
}

func (e *EmitContext) emit() {
	d := e.d

	e.globalSources = dedup(e.peers.GlobalSources, e.emitSdcMetas())
	e.deferCC = true

	cythonPlans := e.planCythonCpp()

	if d.ros != nil && d.ros.Language == LangGo {
		e.emitRosEmpty()
	}

	e.emitPyFbsGroups()

	e.emitDeclaredProducers(cythonPlans)
	e.registerSCInputs()

	var pyProtoResources *ObjcopyEmitResult

	finalized := e.producersOnly()

	for {
		e.drainCommands(cythonPlans, e.prodSrcs)

		if finalized {
			break
		}

		finalized = true

		if d.moduleStmt.Name == tokProtoLibrary {
			pyProtoResources = e.preparePyProtoLibraryResources()
		}

		e.flushGoCgo2()
		e.flushGoSrcs()
	}

	e.srcsClosed = true
	e.commandQueueActive = false
	e.deferCC = false

	for _, complete := range e.filePending {
		complete()
	}

	e.filePending = scrub(e.filePending)

	for _, cc := range e.ccPending {
		e.emitCCWith(cc.src, cc.inputs, cc.ref)
	}

	e.ccPending = scrub(e.ccPending)

	if pyProtoResources != nil {
		e.protoRes = e.emitPyProtoLibraryResult(pyProtoResources)
	}
}
