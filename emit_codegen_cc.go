package main

func emitPRDownstreamCC(ctx *genCtx, instance ModuleInstance, out string, prRef NodeRef, in ModuleCCInputs) (NodeRef, VFS) {

	return emitCodegenDownstreamCC(ctx, instance, out, nil, []NodeRef{prRef}, in)
}

func emitCodegenDownstreamCC(ctx *genCtx, instance ModuleInstance, cppRel string, depPrefix []VFS, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	cppPath := Build(instance.Path + "/" + cppRel)

	closure := walkClosure(ctx, instance, cppPath, in)

	includeInputs := make([]VFS, 0, len(depPrefix)+len(closure))
	seen := make(map[VFS]struct{}, len(depPrefix)+len(closure))
	for _, p := range depPrefix {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		includeInputs = append(includeInputs, p)
	}
	for _, p := range closure {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		includeInputs = append(includeInputs, p)
	}

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
