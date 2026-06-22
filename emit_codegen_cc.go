package main

import "strings"

func emitPRDownstreamCC(ctx *GenCtx, instance ModuleInstance, out string, prRef NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	return emitCodegenDownstreamCC(ctx, instance, out, []NodeRef{prRef}, in)
}

// emitCodegenDownstreamAS compiles a generated $(B) assembler source by the same
// per-platform assembler rule (x86_64 .asm → yasm; else the clang assembler),
// threading the producer node as a build dependency so the producer's tool closure
// reaches the consuming program's target closure.
func emitCodegenDownstreamAS(ctx *GenCtx, instance ModuleInstance, asmRel string, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	asmPath := copyFileOutputVFS(instance.Path.rel(), asmRel)

	asIn := in
	asIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), asmPath, in.ScanCfg)
	asIn.ExtraDepRefs = depRefs

	if extra := resolveCodegenDepRefs(ctx, instance, asIn.IncludeInputs, depRefs...); len(extra) > 0 {
		asIn.ExtraDepRefs = append(append([]NodeRef(nil), depRefs...), extra...)
	}

	if instance.Platform.ISA == ISAX8664 && strings.HasSuffix(asmRel, ".asm") {
		yasmLD, _ := ctx.tool(argContribToolsYasm)

		return emitASYasm(instance, asmRel, asmPath, asIn, yasmLD, ctx.emit)
	}

	return emitAS(instance, asmRel, asmPath, asIn, ctx.host, ctx.emit)
}

func emitCodegenDownstreamCC(ctx *GenCtx, instance ModuleInstance, cppRel string, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	return emitCodegenDownstreamCCFromVFS(ctx, instance, cppRel, copyFileOutputVFS(instance.Path.rel(), cppRel), depRefs, in)
}

// emitCodegenDownstreamCCFromVFS compiles a generated $(B) source whose output VFS is
// already resolved by the caller — used when the path lies outside the module dir,
// where the module-dir-prepending copyFileOutputVFS would misplace it.
func emitCodegenDownstreamCCFromVFS(ctx *GenCtx, instance ModuleInstance, cppRel string, cppPath VFS, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	includeInputs := walkClosure(ctx.scannerFor(instance), cppPath, in.ScanCfg)

	ccIn := in
	ccIn.IncludeInputs = includeInputs
	ccIn.ExtraDepRefs = depRefs

	extra := resolveCodegenDepRefs(ctx, instance, includeInputs, depRefs...)

	if len(extra) > 0 {
		ccIn.ExtraDepRefs = append(append([]NodeRef(nil), depRefs...), extra...)
	}

	ref, outPath, _ := emitCC(instance, cppRel, cppPath, ccIn, ctx.host, ctx.emit)

	return ref, outPath
}
