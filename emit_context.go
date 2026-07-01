package main

type EmitContext struct {
	ctx      *GenCtx
	instance ModuleInstance
	d        *ModuleData
	scanner  *IncludeScanner
	codegen  *CodegenRegistry
	srcs     []STR
	srcMeta  map[STR]SrcMeta
}

func newEmitContext(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *EmitContext {
	scanner := ctx.scannerFor(instance)

	return &EmitContext{ctx: ctx, instance: instance, d: d, scanner: scanner, codegen: scanner.codegen, srcMeta: map[STR]SrcMeta{}}
}

func (e *EmitContext) at(instance ModuleInstance) *EmitContext {
	return newEmitContext(e.ctx, instance, e.d)
}

func (e *EmitContext) enqueueSrc(src STR, meta SrcMeta) {
	e.srcs = append(e.srcs, src)
	e.srcMeta[src] = meta
}

func (e *EmitContext) drainSrcs() (refs []NodeRef, outs []VFS, declMeta map[VFS]SrcMeta) {
	declMeta = map[VFS]SrcMeta{}

	for len(e.srcs) > 0 {
		src := e.srcs[0]
		e.srcs = e.srcs[1:]

		emit := e.emitOneSource(src)

		if emit == nil {
			continue
		}

		meta := e.srcMeta[src]

		refs = append(refs, emit.Ref)
		outs = append(outs, emit.OutPath)
		declMeta[emit.OutPath] = meta

		for _, ex := range emit.Extra {
			refs = append(refs, ex.Ref)
			outs = append(outs, ex.OutPath)
			declMeta[ex.OutPath] = meta
		}
	}

	return refs, outs, declMeta
}
