package main

func emitPRDownstreamCC(ctx *genCtx, instance ModuleInstance, out string, prRef NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	return emitCodegenDownstreamCC(ctx, instance, out, []NodeRef{prRef}, in)
}

func emitCodegenDownstreamCC(ctx *genCtx, instance ModuleInstance, cppRel string, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	cppPath := Build(instance.Path + "/" + cppRel)

	closure := walkClosure(ctx, instance, cppPath, in)
	// A RUN_ANTLR .cpp reaches the generated .proto transitively through its
	// .pb.h (the proto-split protoc step induces the .proto onto .pb.*). The
	// $(B) .proto is a codegen intermediate, not a real input of this compile —
	// drop it (and the spurious dep on its RUN_ANTLR producer that
	// resolveCodegenDepRefs would otherwise add); the generator's $(S) sources
	// the walk gathered through it stay. See dropTransitiveGeneratedProto.
	includeInputs := dropTransitiveGeneratedProto(closure)

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
