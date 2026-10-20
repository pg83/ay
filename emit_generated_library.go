package main

func (e *EmitContext) genGeneratedLibrary() *ModuleEmitResult {
	var globals []ResourceDecl

	for _, peer := range e.d.peerdirs {
		res := genModule(e.ctx, ModuleInstance{Path: source(peer.string()), Kind: KindLib, Language: LangCPP, Platform: e.instance.Platform})

		globals = append(globals, res.ResourceGlobalClosure...)
	}

	globals = e.scriptModuleResources(globals, "build/platform/python/ymake_python3")
	e.emitFromSandboxes()
	e.emitDeclaredProducers(nil)

	result := &ModuleEmitResult{TestSuites: e.styleSuites(globals), ModuleStmtName: tokGenLibrary, ResourceGlobalClosure: globals}

	for _, kind := range []ModuleKind{KindLib, KindBin} {
		for _, language := range []Language{LangNone, LangCPP} {
			for _, demand := range []ModuleDemand{demandNone, demandSelf, demandLinked} {
				alias := e.instance

				alias.Kind, alias.Language, alias.Demand = kind, language, demand
				e.ctx.memoPut(alias, result)
			}
		}
	}

	return result
}
