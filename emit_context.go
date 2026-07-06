package main

type PeerContext struct {
	SelfAddInclGlobal []VFS
	PeerAddInclGlobal []VFS
	ResourceGlobals   []ResourceDecl
	ProtoInclude      []VFS
}

type EmitContext struct {
	ctx        *GenCtx
	instance   ModuleInstance
	d          *ModuleData
	peers      *PeerContext
	scanner    *IncludeScanner
	codegen    *CodegenRegistry
	srcs       []SrcMeta
	pass2      []func()
	refs       []NodeRef
	outs       []VFS
	metas      []SrcMeta
	objcopyRes *ObjcopyEmitResult
	protoRes   *ProtoSrcsResult
	goRes      *GoSrcsResult
	pySrcsReg  []PySrc
	resources  []ResourceEntry
}

type collectedObjs struct {
	refs  []NodeRef
	outs  []VFS
	metas []SrcMeta
}

func (e *EmitContext) partitionCollected() (local, global collectedObjs) {
	n := len(e.metas)

	local.refs = make([]NodeRef, 0, n)
	local.outs = make([]VFS, 0, n)
	local.metas = make([]SrcMeta, 0, n)

	for i, m := range e.metas {
		dst := &local

		if m.Global {
			dst = &global
		}

		dst.refs = append(dst.refs, e.refs[i])
		dst.outs = append(dst.outs, e.outs[i])
		dst.metas = append(dst.metas, m)
	}

	return local, global
}

func newEmitContext(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peers *PeerContext) *EmitContext {
	scanner := ctx.scannerFor(instance)
	k := len(d.resources)

	return &EmitContext{ctx: ctx, instance: instance, d: d, peers: peers, scanner: scanner, codegen: scanner.codegen, resources: d.resources[:k:k]}
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
	e.srcs = append(e.srcs, meta)
}

func (e *EmitContext) deferPass2(cb func()) {
	e.pass2 = append(e.pass2, cb)
}

func (e *EmitContext) emit() {
	d := e.d

	if n := len(d.srcs) + len(d.simdSrcs) + len(d.srcExtraFlat); n > 0 {
		e.refs = make([]NodeRef, 0, n)
		e.outs = make([]VFS, 0, n)
		e.metas = make([]SrcMeta, 0, n)
	}

	fsMemberRefs, fsMemberPaths := e.emitFromSandboxes()

	e.emitBundles()

	cythonPlans := e.planCythonCpp()

	e.emitDeclaredProducers(cythonPlans)

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
		flags := internArgs(simd.CFlags)

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

		if pyRes := e.flushPyProtoSrcs(); pyRes != nil {
			e.protoRes = pyRes
		}
	} else if pyModuleTypeUsesPython3(d.moduleStmt.Name) {
		e.emitPyProtoBytecode()

		if pyRes := e.flushPyProtoSrcs(); pyRes != nil {
			e.protoRes = pyRes
		}
	}

	if !isProgramModuleType(d.moduleStmt.Name) || d.unit.Tag != 0 || len(e.resources) > 0 {
		e.objcopyRes = e.emitResourceObjcopy()
	}

	for i, ref := range fsMemberRefs {
		e.collectObj(ref, fsMemberPaths[i], SrcMeta{Prio: stmtPrioDefault})
	}

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
