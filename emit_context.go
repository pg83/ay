package main

type EmitContext struct {
	ctx      *GenCtx
	instance ModuleInstance
	d        *ModuleData
	scanner  *IncludeScanner
	codegen  *CodegenRegistry
	srcs     []STR
	srcMeta  map[STR]SrcMeta
	pass2    []func()
	refs     []NodeRef
	outs     []VFS
	declMeta map[VFS]SrcMeta
}

func newEmitContext(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *EmitContext {
	scanner := ctx.scannerFor(instance)

	return &EmitContext{ctx: ctx, instance: instance, d: d, scanner: scanner, codegen: scanner.codegen, srcMeta: map[STR]SrcMeta{}, declMeta: map[VFS]SrcMeta{}}
}

func (e *EmitContext) collectObj(ref NodeRef, out VFS, meta SrcMeta) {
	e.refs = append(e.refs, ref)
	e.outs = append(e.outs, out)
	e.declMeta[out] = meta
}

func (e *EmitContext) metaForSrc(src STR) SrcMeta {
	if m, ok := e.srcMeta[src]; ok {
		return m
	}

	return e.d.srcMetaOf(src)
}

func (e *EmitContext) at(instance ModuleInstance) *EmitContext {
	return newEmitContext(e.ctx, instance, e.d)
}

func (e *EmitContext) enqueueSrc(src STR, meta SrcMeta) {
	e.srcs = append(e.srcs, src)
	e.srcMeta[src] = meta
}

func (e *EmitContext) deferPass2(cb func()) {
	e.pass2 = append(e.pass2, cb)
}

func (e *EmitContext) emit(selfPeerAddInclGlobal []VFS) []VFS {
	d := e.d

	for _, src := range d.srcs {
		if isCodegenProducingSrcID(src) {
			e.emitOneSource(src)
		}
	}

	cythonPlans := e.planCythonCpp()
	cpMemberSrcs := e.emitCopyFiles()

	e.emitMiscNodes()
	e.emitRunProgramsForAR()
	e.emitDecimalMD5ForAR()
	e.emitSplitCodegensForAR()
	e.emitBaseCodegensForAR()
	e.emitRunPythonForAR()
	e.emitArchiveAsmForAR()
	e.emitEnumSrcs(selfPeerAddInclGlobal)
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

	return cpMemberSrcs
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
