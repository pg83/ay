package main

func (e *EmitContext) emitCodegenDownstreamAS(asmRel string, depRefs []NodeRef) (NodeRef, VFS) {
	ctx, instance, _ := e.ctx, e.instance, e.d
	asmPath := copyFileOutputVFS(instance.Path.rel(), asmRel)
	in := e.ccInputsFor(asmPath)
	asIn := in

	asIn.IncludeInputs = walkClosure(e.scanner, asmPath, in.ScanCfg)
	asIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, asIn.IncludeInputs, depRefs...)

	if instance.Platform.ISA == ISAX8664 && extIsAsm(asmRel) {
		yasmLD, _ := ctx.tool(argContribToolsYasm)

		return emitASYasm(instance, asmRel, asmPath, asIn, yasmLD, ctx.emit)
	}

	return emitAS(instance, asmRel, asmPath, asIn, ctx.host, ctx.emit)
}
