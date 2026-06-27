package main

import "strings"

func emitPRDownstreamCC(ctx *GenCtx, instance ModuleInstance, out string, prRef NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	return emitCodegenDownstreamCC(ctx, instance, out, []NodeRef{prRef}, in)
}

func emitCodegenDownstreamAS(ctx *GenCtx, instance ModuleInstance, asmRel string, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	asmPath := copyFileOutputVFS(instance.Path.rel(), asmRel)
	asIn := in

	asIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), asmPath, in.ScanCfg)
	asIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, asIn.IncludeInputs, depRefs...)

	if instance.Platform.ISA == ISAX8664 && strings.HasSuffix(asmRel, ".asm") {
		yasmLD, _ := ctx.tool(argContribToolsYasm)

		return emitASYasm(instance, asmRel, asmPath, asIn, yasmLD, ctx.emit)
	}

	return emitAS(instance, asmRel, asmPath, asIn, ctx.host, ctx.emit)
}

func emitCodegenDownstreamCC(ctx *GenCtx, instance ModuleInstance, cppRel string, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	return emitCodegenDownstreamCCFromVFS(ctx, instance, cppRel, copyFileOutputVFS(instance.Path.rel(), cppRel), depRefs, in)
}

func emitCodegenDownstreamCCFromVFS(ctx *GenCtx, instance ModuleInstance, cppRel string, cppPath VFS, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	includeInputs := walkClosure(ctx.scannerFor(instance), cppPath, in.ScanCfg)
	ccIn := in

	ccIn.IncludeInputs = includeInputs
	ccIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, includeInputs, depRefs...)

	ref, outPath, _ := emitCC(instance, internStr(cppRel), cppPath, ccIn, ctx.host, ctx.emit)

	return ref, outPath
}
