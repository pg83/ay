package main

// emitPRDownstreamCC emits the CC compiling a PR-generated source.
// IsGenerated=true; IncludeInputs from WalkBuildRootClosure over the
// registered output (populated by emitRunProgram with EmitsIncludes=
// nil — opaque tool output, empty closure today).
//
// PR-emitted source: $(B)/<instance.Path>/<out>; composeCCPaths
// yields $(B)/<instance.Path>/<out>.o (flat for slash-free <out>).
func emitPRDownstreamCC(ctx *genCtx, instance ModuleInstance, out string, prRef NodeRef, in ModuleCCInputs) (NodeRef, VFS, []VFS) {
	// Thread prRef as the downstream CC's leading dep. walkClosure
	// skips the root so a registry probe alone can't surface PR; REF
	// places PR as CC's leading dep (dep_types.h_dumper.cpp.o → PR).
	return emitCodegenDownstreamCC(ctx, instance, out, nil, []NodeRef{prRef}, in)
}

// emitCodegenDownstreamCC emits the downstream CC for a codegen
// producer's `.cpp/.cc/.cxx/.c` output. Owning module compiles it as
// an implicit AR member.
//
// Producer MUST register EmitsIncludes in the codegen registry first
// (walkClosure reads it). depPrefix entries are prepended ahead of the
// scanner closure so cross-codegen deps appear before the consumer's
// own .cpp in Inputs[] (EN: cross-EN `_serialized.{cpp,h}`; PR: none).
func emitCodegenDownstreamCC(ctx *genCtx, instance ModuleInstance, cppRel string, depPrefix []VFS, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS, []VFS) {
	cppPath := Build(instance.Path + "/" + cppRel)

	closure := walkClosure(ctx, instance, cppPath, in)

	// Prepend depPrefix into IncludeInputs so CC's Inputs[] carries
	// the cross-codegen dep paths ahead of the consumer's own .cpp
	// (sg2.json export_json_debug.h_serialized.cpp.o inputs[0..1] =
	// the cross-EN dep .cpp + .h). Dedup against the scanner closure
	// — cross-EN .h already arrives via the registry's `_serialized.h`
	// EmitsIncludes; only the .cpp reliably needs the explicit prepend.
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

	// Append codegen-producer refs reached via the .cpp's transitive
	// include closure (PB/EV peers pulled in by .pb.h / .ev.pb.h).
	// Filter out anything already in depRefs to avoid duplication.
	extra := resolveCodegenDepRefs(ctx, instance, includeInputs, depRefs...)
	if len(extra) > 0 {
		ccIn.ExtraDepRefs = append(append([]NodeRef(nil), depRefs...), extra...)
	}

	ref, outPath := EmitCC(instance, cppRel, cppPath, ccIn, ctx.host, ctx.emit)

	// AR member-inputs: SOURCE_ROOT-rooted closure entries only.
	// REF (libdevtools-ymake.a) carries the EN-downstream CC's include-
	// closure .h entries but never the BUILD_ROOT `_serialized.{cpp,h}`
	// outputs themselves — those are wired implicitly via the .o
	// archive members.
	ccInputs := make([]VFS, 0, len(closure))
	for _, p := range closure {
		if p.IsBuild() {
			continue
		}
		ccInputs = append(ccInputs, p)
	}

	return ref, outPath, ccInputs
}
