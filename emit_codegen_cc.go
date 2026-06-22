package main

import "strings"

func emitPRDownstreamCC(ctx *GenCtx, instance ModuleInstance, out string, prRef NodeRef, in ModuleCCInputs) (NodeRef, VFS) {
	return emitCodegenDownstreamCC(ctx, instance, out, []NodeRef{prRef}, in)
}

// emitCodegenDownstreamAS compiles a generated $(B) assembler source (an
// auto-output .asm/.s/.S re-fed as a module source) by the same per-platform
// assembler rule emitLibraryAsmSource uses for a declared .asm SRC (x86_64 .asm
// → yasm; else the clang assembler), threading the producer node as a real
// build dependency so the object's archive edge pulls the producer's tool
// closure into the consuming program's target closure.
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

// emitCodegenDownstreamCCFromVFS compiles a generated $(B) source whose output VFS
// is already resolved by the caller — used when that path lies OUTSIDE the module
// dir, so the module-dir-prepending copyFileOutputVFS of emitCodegenDownstreamCC
// would misplace it. cppRel is the module-relative spelling (for the cxx-source
// suffix test); composeCCPaths derives the object path from cppPath directly.
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
