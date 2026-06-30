package main

func emitCodegenDownstreamAS(ctx *GenCtx, instance ModuleInstance, d *ModuleData, asmRel string, depRefs []NodeRef) (NodeRef, VFS) {
	asmPath := copyFileOutputVFS(instance.Path.rel(), asmRel)
	in := d.cc.ccInputsFor(ctx, instance, d, asmPath)
	asIn := in

	asIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), asmPath, in.ScanCfg)
	asIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, asIn.IncludeInputs, depRefs...)

	if instance.Platform.ISA == ISAX8664 && extIsAsm(asmRel) {
		yasmLD, _ := ctx.tool(argContribToolsYasm)

		return emitASYasm(instance, asmRel, asmPath, asIn, yasmLD, ctx.emit)
	}

	return emitAS(instance, asmRel, asmPath, asIn, ctx.host, ctx.emit)
}
