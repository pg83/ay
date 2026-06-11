package main

func emitPRDownstreamCC(ctx *GenCtx, instance ModuleInstance, out string, prRef NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	return emitCodegenDownstreamCC(ctx, instance, out, []NodeRef{prRef}, in)
}

func emitCodegenDownstreamCC(ctx *GenCtx, instance ModuleInstance, cppRel string, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	cppPath := Build(instance.Path.rel() + "/" + cppRel)

	includeInputs := walkClosure(ctx, instance, cppPath, in)

	ccIn := in
	ccIn.IncludeInputs = includeInputs
	ccIn.ExtraDepRefs = depRefs

	extra := resolveCodegenDepRefs(ctx, instance, includeInputs, depRefs...)

	if len(extra) > 0 {
		ccIn.ExtraDepRefs = append(append([]NodeRef(nil), depRefs...), extra...)
	}

	ref, outPath, _ := EmitCC(instance, cppRel, cppPath, ccIn, ctx.host, ctx.emit)

	return ref, outPath
}
