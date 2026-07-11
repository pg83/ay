package main

import "unsafe"

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

	if n := len(e.metas); cap(local.refs) < n {
		local.refs = make([]NodeRef, 0, n)
		local.outs = make([]VFS, 0, n)
		local.metas = make([]SrcMeta, 0, n)
	}

	for i, m := range e.metas {
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
	}

	frame.emitCtx.pbEmissionOk = [2]bool{}
	frame.emitCtx.pyPBOk = false
	frame.emitCtx.objcopyOk = false
	frame.emitCtx.protoResVal = ProtoSrcsResult{}

	return &frame.emitCtx
}

func (e *EmitContext) emitNode(node Node) NodeRef {
	e.resolveNodeCodegenDeps(&node)

	return e.ctx.emit.emitNode(node)
}

func (e *EmitContext) emitReservedNode(node Node, ref NodeRef) {
	e.resolveNodeCodegenDeps(&node)
	e.ctx.emit.emitReservedNode(node, ref)
}

func (e *EmitContext) resolveNodeCodegenDeps(node *Node) {
	sourceOnlyChunks := e.ctx.srcOnly

	if sourceOnlyChunks == nil {
		sourceOnlyChunks = newIntSet(8)
		e.ctx.srcOnly = sourceOnlyChunks
	}

	refs := nodeRefScratches.get()

	defer func() { nodeRefScratches.put(refs) }()

	for _, chunk := range node.Inputs {
		if len(chunk) == 0 {
			continue
		}

		key := mix64(uint64(uintptr(unsafe.Pointer(unsafe.SliceData(chunk)))))

		if sourceOnly, _ := sourceOnlyChunks.get(key); sourceOnly {
			continue
		}

		hasBuild := false

		for _, input := range chunk {
			if !input.isBuild() {
				continue
			}

			hasBuild = true

			if info := e.codegen.useBuild(input); info != nil {
				refs = append(refs, info.ProducerRef)
			}
		}

		if !hasBuild {
			sourceOnlyChunks.put(key, true)
		}
	}

	if len(refs) == 0 {
		return
	}

	var result []NodeRef

	dedupers.with(func(deduper *DeDuper) {
		out := e.ctx.na.noderefs.alloc(len(node.DepRefs) + len(refs))
		k := 0

		for _, ref := range node.DepRefs {
			deduper.add(ref.strID())
			out[k] = ref
			k++
		}

		for _, ref := range refs {
			if !deduper.add(ref.strID()) {
				continue
			}

			out[k] = ref
			k++
		}

		e.ctx.na.noderefs.commit(k)
		result = out[:k]
	})

	node.DepRefs = result
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
