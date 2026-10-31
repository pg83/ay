package main

func (ctx *GenCtx) moduleResultRefs(instance ModuleInstance, result *ModuleEmitResult) (refs []NodeRef) {
	reg := ctx.scannerFor(instance).codegen
	output, ref := result.ARPath, result.ARRef

	if result.java != nil {
		output, ref = &result.java.target, result.java.outRef
	} else if result.LDPath != nil {
		output, ref = result.LDPath, result.LDRef
	}

	if output != nil && ref != 0 {
		reg.use(*output)
		refs = append(refs, ref)
	}

	if result.GlobalRef != nil {
		refs = append(refs, *result.GlobalRef)
	}

	for _, ref := range result.GlobalObjectRefs {
		if ref != 0 {
			refs = append(refs, ref)
		}
	}

	if result.java != nil || result.testDependencyModule.complete || result.testDependencyModule.bundle {
		return refs
	}

	if inputs := reg.moduleInputs(moduleOutputPath(result)); inputs != nil {
		for _, global := range inputs.Globals {
			if info := reg.lookup(global); info != nil && info.OwnerModule == instance.Path {
				reg.use(global)
				refs = append(refs, info.ProducerRef)
			}
		}
	}

	return dedupRefs(refs)
}
