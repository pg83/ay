package main

type PeerContext struct {
	SelfAddInclGlobal []VFS
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
	srcs       []STR
	srcMeta    map[STR]SrcMeta
	pass2      []func()
	refs       []NodeRef
	outs       []VFS
	globalRefs []NodeRef
	globalOuts []VFS
	objcopyRes *ObjcopyEmitResult
	protoRes   *ProtoSrcsResult
	pySrcsReg  []PySrc
	declMeta   map[VFS]SrcMeta
}

func newEmitContext(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peers *PeerContext) *EmitContext {
	scanner := ctx.scannerFor(instance)

	return &EmitContext{ctx: ctx, instance: instance, d: d, peers: peers, scanner: scanner, codegen: scanner.codegen, srcMeta: map[STR]SrcMeta{}, declMeta: map[VFS]SrcMeta{}}
}

func (e *EmitContext) collectObj(ref NodeRef, out VFS, meta SrcMeta) {
	if meta.Global {
		e.globalRefs = append(e.globalRefs, ref)
		e.globalOuts = append(e.globalOuts, out)

		return
	}

	e.refs = append(e.refs, ref)
	e.outs = append(e.outs, out)
	e.declMeta[out] = meta
}

func (e *EmitContext) markGlobalSrc(src STR) {
	m := e.metaForSrc(src)

	m.Global = true
	e.srcMeta[src] = m
}

func (e *EmitContext) metaForSrc(src STR) SrcMeta {
	if m, ok := e.srcMeta[src]; ok {
		return m
	}

	return e.d.srcMetaOf(src)
}

func (e *EmitContext) at(instance ModuleInstance) *EmitContext {
	return newEmitContext(e.ctx, instance, e.d, e.peers)
}

func (e *EmitContext) enqueueSrc(src STR, meta SrcMeta) {
	e.srcs = append(e.srcs, src)
	e.srcMeta[src] = meta
}

func (e *EmitContext) deferPass2(cb func()) {
	e.pass2 = append(e.pass2, cb)
}

func (e *EmitContext) emit() {
	d := e.d
	fsMemberRefs, fsMemberPaths := e.emitFromSandboxes()

	e.emitBundles()

	cythonPlans := e.planCythonCpp()

	e.emitCopyFiles()

	e.emitMiscNodes()
	e.emitRunProgramsForAR()
	e.emitDecimalMD5ForAR()
	e.emitSplitCodegensForAR()
	e.emitBaseCodegensForAR()
	e.emitRunPythonForAR()
	e.emitArchiveAsmForAR()

	for _, src := range d.srcs {
		if isCodegenProducingSrcID(src) {
			e.emitOneSource(src)
		}
	}

	e.emitEnumSrcs(e.peers.SelfAddInclGlobal)
	e.emitLuaJit21()
	e.emitArchives()
	e.emitCheckConfigH()
	e.emitCythonCppPlanned(cythonPlans)
	e.emitSwigC()
	e.emitJoinSrcs()

	for _, fe := range d.srcExtraFlat {
		srcVFS := e.moduleSourceVFS(fe.Src)
		ref, out := e.emitCCFlat(srcVFS, nil, fe.Flags)

		e.collectObj(ref, out, SrcMeta{Prio: stmtPrioDefault, Seq: fe.Seq})
	}

	for _, src := range d.srcs {
		if !isCodegenProducingSrcID(src) {
			e.emitOneSource(src)
		}
	}

	e.emitYmapsSprotoHeaders(e.peers.SelfAddInclGlobal, e.ymapsSprotoProducedBases())

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
		e.markGlobalSrc(src)
		e.emitOneSource(src)
	}

	e.registerCollectPySrcs()

	regCCPy3Suffix := d.moduleStmt.Name == tokPy23NativeLibrary || d.moduleStmt.Name == tokPy23Library

	if regRes := e.emitPyRegister(regCCPy3Suffix); regRes != nil {
		for i, ref := range regRes.Refs {
			e.globalRefs = append(e.globalRefs, ref)
			e.globalOuts = append(e.globalOuts, regRes.Outputs[i])
			e.declMeta[regRes.Outputs[i]] = SrcMeta{Prio: stmtPrioDefault, Generated: true}
		}
	}

	if !isProgramModuleType(d.moduleStmt.Name) {
		e.emitPyBytecode()

		genPyAuxRefs, genPyAuxOuts := e.emitGeneratedPyAuxChunks()

		e.globalRefs = append(e.globalRefs, genPyAuxRefs...)
		e.globalOuts = append(e.globalOuts, genPyAuxOuts...)

		if pyRes := e.flushPyProtoSrcs(); pyRes != nil {
			e.protoRes = pyRes
		}

		e.emitLLVMBC(e.peers.ResourceGlobals)
	}

	if !isProgramModuleType(d.moduleStmt.Name) || d.unit.Tag != 0 || len(d.resources) > 0 {
		e.objcopyRes = e.emitResourceObjcopy()
	}

	for i, ref := range fsMemberRefs {
		e.collectObj(ref, fsMemberPaths[i], SrcMeta{Prio: stmtPrioDefault})
	}
}

func (e *EmitContext) drainSrcs() {
	for len(e.srcs) > 0 {
		src := e.srcs[0]

		e.srcs = e.srcs[1:]

		e.emitOneSource(src)
	}

	for _, cb := range e.pass2 {
		cb()
	}

	e.pass2 = e.pass2[:0]
}
