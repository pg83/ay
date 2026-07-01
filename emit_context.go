package main

type EmitContext struct {
	ctx      *GenCtx
	instance ModuleInstance
	d        *ModuleData
	scanner  *IncludeScanner
	codegen  *CodegenRegistry
	srcs     []STR
	srcMeta  map[STR]SrcMeta
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

func (e *EmitContext) drainSrcs() {
	for len(e.srcs) > 0 {
		src := e.srcs[0]
		e.srcs = e.srcs[1:]

		e.emitOneSource(src)
	}
}
