package main

func emitCodegenDownstreamAS(ctx *GenCtx, instance ModuleInstance, asmRel string, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	asmPath := copyFileOutputVFS(instance.Path.rel(), asmRel)
	asIn := in

	asIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), asmPath, in.ScanCfg)
	asIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, asIn.IncludeInputs, depRefs...)

	if instance.Platform.ISA == ISAX8664 && extIsAsm(asmRel) {
		yasmLD, _ := ctx.tool(argContribToolsYasm)

		return emitASYasm(instance, asmRel, asmPath, asIn, yasmLD, ctx.emit)
	}

	return emitAS(instance, asmRel, asmPath, asIn, ctx.host, ctx.emit)
}
