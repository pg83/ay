package main

type EmitContext struct {
	ctx      *GenCtx
	instance ModuleInstance
	d        *ModuleData
	scanner  *IncludeScanner
	codegen  *CodegenRegistry
}

func newEmitContext(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *EmitContext {
	scanner := ctx.scannerFor(instance)

	return &EmitContext{ctx: ctx, instance: instance, d: d, scanner: scanner, codegen: scanner.codegen}
}

func (e *EmitContext) at(instance ModuleInstance) *EmitContext {
	return newEmitContext(e.ctx, instance, e.d)
}
