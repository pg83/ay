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
	pass2        []func()
	refs         []NodeRef
	outs         []VFS
	metas        []SrcMeta
	objcopyRes   *ObjcopyEmitResult
	protoRes     *ProtoSrcsResult
	goRes        *GoSrcsResult
	goInclJoined []ANY
	goInclSplit  []ANY
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
		pass2:       scrub(prev.pass2),
		refs:        prev.refs[:0],
		outs:        prev.outs[:0],
		metas:       prev.metas[:0],
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
	if cap(e.srcs) == 0 {
		e.srcs = make([]SrcMeta, 0, len(e.d.srcs)+8)
	}

	e.srcs = append(e.srcs, meta)
}

func (e *EmitContext) deferPass2(cb func()) {
	e.pass2 = append(e.pass2, cb)
}

func (e *EmitContext) producersOnly() bool {
	return e.instance.Demand == demandNone
}

func (e *EmitContext) emit() {
	d := e.d
	fsMemberRefs, fsMemberPaths := e.emitFromSandboxes()

	e.emitBundles()

	cythonPlans := e.planCythonCpp()

	e.emitDeclaredProducers(cythonPlans)

	if e.producersOnly() {
		for _, src := range d.srcs {
			if !isCodegenProducingSrcID(src) {
				e.emitOneSource(d.srcMetaOf(src))
			}
		}

		e.drainSrcs()

		return
	}

	for _, fe := range d.srcExtraFlat {
		srcVFS := e.moduleSourceVFS(fe.Src)
		ref, out := e.emitCCFlat(srcVFS, nil, fe.Flags)

		e.collectObj(ref, out, SrcMeta{Prio: stmtPrioDefault, Seq: fe.Seq})
	}

	for _, src := range d.srcs {
		if !isCodegenProducingSrcID(src) {
			e.emitOneSource(d.srcMetaOf(src))
		}
	}

	e.drainSrcs()

	for _, simd := range d.simdSrcs {
		srcVFS := e.moduleSourceVFS(simd.Src)
		flags := internAnys(simd.CFlags)

		if extras := d.perSrcCFlagsFor(simd.Src); extras != nil {
			flags = append(flags, *extras...)
		}

		variant := simd.Variant
		ref, out := e.emitCCFlat(srcVFS, &variant, flags)

		e.collectObj(ref, out, SrcMeta{Prio: stmtPrioDefault, Seq: simd.Seq})
	}

	for _, src := range d.globalSrcs {
		m := d.srcMetaOf(src)

		m.Global = true

		e.emitOneSource(m)
	}

	e.registerCollectPySrcs()

	regCCPy3Suffix := d.moduleStmt.Name == tokPy23NativeLibrary || d.moduleStmt.Name == tokPy23Library

	if regRes := e.emitPyRegister(regCCPy3Suffix); regRes != nil {
		for i, ref := range regRes.Refs {
			e.collectObj(ref, regRes.Outputs[i], SrcMeta{Prio: stmtPrioDefault, Generated: true, Global: true})
		}
	}

	if !isProgramModuleType(d.moduleStmt.Name) {
		e.emitPyProtoBytecode()
		e.emitPyBytecode()

		genPyAuxRefs, genPyAuxOuts := e.emitGeneratedPyAuxChunks()

		for i, ref := range genPyAuxRefs {
			e.collectObj(ref, genPyAuxOuts[i], SrcMeta{Prio: stmtPrioDefault, Global: true})
		}

		if d.moduleStmt.Name == tokProtoLibrary {
			if pyRes := e.flushPyProtoSrcs(); pyRes != nil {
				e.protoRes = pyRes
			}
		}
	} else if pyModuleTypeUsesPython3(d.moduleStmt.Name) {
		e.emitPyProtoBytecode()
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

func (e *EmitContext) drainSrcs() {
	for {
		for len(e.srcs) > 0 {
			meta := e.srcs[0]

			e.srcs = e.srcs[1:]

			e.emitOneSource(meta)
		}

		if len(e.pass2) == 0 {
			return
		}

		cbs := e.pass2

		e.pass2 = nil

		for _, cb := range cbs {
			cb()
		}
	}
}
