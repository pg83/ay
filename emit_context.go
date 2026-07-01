package main

type EmitContext struct {
	ctx      *GenCtx
	instance ModuleInstance
	d        *ModuleData
}

func newEmitContext(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *EmitContext {
	return &EmitContext{ctx: ctx, instance: instance, d: d}
}

func (e *EmitContext) at(instance ModuleInstance) *EmitContext {
	return &EmitContext{ctx: e.ctx, instance: instance, d: e.d}
}
