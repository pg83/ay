package main

import "slices"

func (e *EmitContext) genGeneratedLibrary() *ModuleEmitResult {
	var globals []ResourceDecl

	for _, peer := range e.d.peerdirs {
		res := genModule(e.ctx, ModuleInstance{Path: source(peer.string()), Kind: KindLib, Platform: e.instance.Platform})

		globals = append(globals, res.ResourceGlobalClosure...)
	}

	globals = e.scriptModuleResources(globals, "build/platform/python/ymake_python3")
	e.emitFromSandboxes()
	e.emitDeclaredProducers(nil)

	if e.instance.Demand != demandNone {
		for _, out := range e.outs {
			e.codegen.use(out)
		}
	}

	result := &ModuleEmitResult{styleChecks: slices.Clone(e.d.styleChecks), ModuleStmtName: tokGenLibrary, ResourceGlobalClosure: globals}

	e.ctx.memoPut(e.instance, result)

	return result
}
