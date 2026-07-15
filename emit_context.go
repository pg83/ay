package main

type PeerContext struct {
	SelfAddInclGlobal []VFS
	PeerAddInclGlobal []VFS
	ResourceGlobals   []ResourceDecl
	ProtoInclude      []VFS
}

type EmitContext struct {
	ctx          *GenCtx
	instance     ModuleInstance
	d            *ModuleData
	peers        *PeerContext
	scanner      *IncludeScanner
	codegen      *CodegenRegistry
	srcs         []SrcMeta
	srcHead      int
	srcsClosed   bool
	refs         []NodeRef
	outs         []VFS
	metas        []SrcMeta
	objcopyRes   *ObjcopyEmitResult
	protoRes     *ProtoSrcsResult
	goRes        *GoSrcsResult
	goInclJoined []ANY
	goInclSplit  []ANY
	pyMetas      []PySourceMeta
	pySrcsReg    []PySrc
	resources    []ResourceEntry
	localObjs    CollectedObjs
	globalObjs   CollectedObjs
	prodPos      []ProducerPos
	prodBacking  []VFS
	prodSrcs     []SrcMeta
	objScratch   []ResourceItem
	rawScratch   []ResourceItem
	resItems     []ResourceItem
	dirScratch   []IncludeDirective
	resEntries   []PyGenResEntry
	resStrBuf    []byte
	resVFSBuf    []VFS
	prodVFS      []VFS
	cvScratch    []Closure
	peerScratch  []string
	arMembers    []ARMember
	sbomOrder    []ResolvedPeer
	orderedCC    []VFS
	prodOrder    []int
	protoPaths   []protoPathEntry
	protoPathPos int
	pbEmission   [2]PbModuleEmission
	pbEmissionOk [2]bool
	pyPBEmission PyPBModuleEmission
	pyPBOk       bool
	objcopyCtx   ObjcopyEmitCtx
	objcopyOk    bool
	protoResVal  ProtoSrcsResult
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

func newEmitContext(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peers *PeerContext) *EmitContext {
	scanner := ctx.scannerFor(instance)
	k := len(d.resources)

	return &EmitContext{ctx: ctx, instance: instance, d: d, peers: peers, scanner: scanner, codegen: scanner.codegen, resources: d.resources[:k:k]}
}

func newEmitContextIn(frame *ModuleFrame, ctx *GenCtx, instance ModuleInstance, d *ModuleData, peers *PeerContext) *EmitContext {
	scanner := ctx.scannerFor(instance)
	k := len(d.resources)
	prev := &frame.emitCtx

	frame.emitCtx = EmitContext{
		ctx: ctx, instance: instance, d: d, peers: peers, scanner: scanner, codegen: scanner.codegen,
		resources: d.resources[:k:k],

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
		objScratch:  scrub(prev.objScratch),
		rawScratch:  scrub(prev.rawScratch),
		resItems:    scrub(prev.resItems),
		dirScratch:  prev.dirScratch[:0],
		resEntries:  scrub(prev.resEntries),
		resStrBuf:   prev.resStrBuf[:0],
		resVFSBuf:   prev.resVFSBuf[:0],
		prodVFS:     prev.prodVFS[:0],
		cvScratch:   scrub(prev.cvScratch),
		peerScratch: scrub(prev.peerScratch),
		arMembers:   prev.arMembers[:0],
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
	var refs []NodeRef

	for _, chunk := range node.Inputs {
		if len(chunk) == 0 || chunk[0].isSource() {
			continue
		}

		for _, input := range chunk {
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
		}
	}

	if len(refs) == 0 {
		if refs != nil {
			nodeRefScratches.put(refs)
		}

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

func (e *EmitContext) resStr2(a, b string) string {
	start := len(e.resStrBuf)

	e.resStrBuf = append(e.resStrBuf, a...)
	e.resStrBuf = append(e.resStrBuf, b...)

	return bytesString(e.resStrBuf[start:])
}

func (e *EmitContext) resVFS1(v VFS) []VFS {
	e.resVFSBuf = append(e.resVFSBuf, v)

	n := len(e.resVFSBuf)

	return e.resVFSBuf[n-1 : n : n]
}

func (e *EmitContext) collectObj(ref NodeRef, out VFS, meta SrcMeta) {
	e.refs = append(e.refs, ref)
	e.outs = append(e.outs, out)
	e.metas = append(e.metas, meta)
}

func (e *EmitContext) at(instance ModuleInstance) *EmitContext {
	return newEmitContext(e.ctx, instance, e.d, e.peers)
}

func (e *EmitContext) enqueueSrc(meta SrcMeta) {
	if e.srcsClosed {
		throwFmt("enqueueSrc after source queue closed for %q", meta.Source.string())
	}

	if cap(e.srcs) == 0 {
		e.srcs = make([]SrcMeta, 0, len(e.d.srcs)+8)
	}

	e.srcs = append(e.srcs, meta)
}

func (e *EmitContext) producersOnly() bool {
	return e.instance.Demand == demandNone
}

func (e *EmitContext) register(info GeneratedFileInfo) *GeneratedFileInfo {
	info.OwnerModule = e.instance.Path

	return e.codegen.register(info)
}

func (e *EmitContext) emit() {
	d := e.d
	fsMemberRefs, fsMemberPaths := e.emitFromSandboxes()

	e.emitBundles()

	cythonPlans := e.planCythonCpp()

	e.emitDeclaredProducers(cythonPlans)

	for _, meta := range d.srcs {
		if !isCodegenProducingSrcID(meta.Source) {
			e.enqueueSrc(meta)
		}
	}

	if !e.producersOnly() {
		e.registerCollectPySrcs()

		regCCPy3Suffix := d.moduleStmt.Name == tokPy23NativeLibrary || d.moduleStmt.Name == tokPy23Library

		e.emitPyRegister(regCCPy3Suffix)
	}

	finalized := e.producersOnly()

	for {
		for e.srcHead < len(e.srcs) {
			meta := e.srcs[e.srcHead]

			e.srcs[e.srcHead] = SrcMeta{}
			e.srcHead++
			e.emitOneSource(meta)
		}

		if finalized {
			break
		}

		finalized = true

		if !isProgramModuleType(d.moduleStmt.Name) {
			e.emitPyBytecode(true)

			genPyAuxRefs, genPyAuxOuts := e.emitGeneratedPyAuxChunks()

			for i, ref := range genPyAuxRefs {
				e.collectObj(ref, genPyAuxOuts[i], SrcMeta{Prio: stmtPrioDefault, Global: true})
			}

			if d.moduleStmt.Name == tokProtoLibrary {
				if pyRes := e.emitPyProtoLibraryResult(); pyRes != nil {
					e.protoRes = pyRes
				}
			}
		} else if pyModuleTypeUsesPython3(d.moduleStmt.Name) {
			e.emitPyBytecode(false)
		}

		if !isProgramModuleType(d.moduleStmt.Name) || d.unit.Tag != 0 || len(e.resources) > 0 {
			e.objcopyRes = e.emitResourceObjcopy()
		}

		for i, ref := range fsMemberRefs {
			e.collectObj(ref, fsMemberPaths[i], SrcMeta{Prio: stmtPrioDefault})
		}

		e.flushGoCgo2()
		e.flushGoSrcs()
	}

	e.srcsClosed = true
}
