package main

import (
	"sort"
	"strings"
)

// emitEnumSrcs emits one EN node per GENERATE_ENUM_SERIALIZATION(*)
// in d.enumSrcs.
//
// Algorithm:
//  1. Walk tools/enum_parser/enum_parser as host tool → LD NodeRef
//     (ParseError → canonical binary path fallback).
//  2. For each stmt, scan the header's transitive include closure
//     (same scanner as CC nodes).
//  3. Cross-EN deps: any previously emitted EN output appearing in
//     the closure contributes its NodeRef and path.
//  4. Call EmitEN, record outputs in ctx.enOutputs.
//
// EN nodes are always emitted on the TARGET platform; enum_parser is
// host x86_64 but the EN outputs are target-axis.
//
// When `consumerInputs` is non-nil, also emit one downstream CC per
// `_serialized.cpp` output (the EN-emitted .cpp is an implicit module
// source archived alongside declared SRCS). consumerInputs must carry
// the consuming module's full CC compile bag. nil → EN nodes only.
type enumSrcsResult struct {
	CCRefs           []NodeRef
	CCOutputs        []VFS
	MemberInputsList [][]VFS
}

func emitEnumSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerAddInclGlobal []VFS, consumerInputs *ModuleCCInputs) *enumSrcsResult {
	if len(d.enumSrcs) == 0 {
		return nil
	}

	const enumParserPath = "tools/enum_parser/enum_parser"

	// Walk enum_parser as a HOST tool (x86_64).
	enumParserLD, enumParserBin := ctx.tool(enumParserPath)

	// Synthesize a ModuleCCInputs for the include scanner using the
	// module's own ADDINCL + peer-global ADDINCL set so headers from
	// transitive peer libraries (abseil, protobuf) resolve correctly.
	// Mirrors the ModuleCCInputs built for CC nodes in the same module.
	scanIn := ModuleCCInputs{
		AddIncl:           mergeDedupVFS(d.addIncl, nil),
		PeerAddInclGlobal: peerAddInclGlobal,
		SourceRoot:        ctx.sourceRoot,
	}

	res := &enumSrcsResult{}

	for _, stmt := range d.enumSrcs {
		headerRel := stmt.Header
		withHeader := stmt.Variant == "with_header"

		// Scan the header's transitive include closure with the target
		// scanner (EN nodes always compile on the target axis).
		closure := walkClosure(ctx, instance, resolveSourceVFS(ctx, instance, headerRel, scanIn.SrcDir), scanIn)

		// Cross-EN deps: when a previously emitted EN produced a
		// _serialized.h (--header variant) and the current header's
		// closure contains a file `#include`-ing that _serialized.h,
		// the current EN deps on the prior one. The scanner cannot
		// resolve $(B)/_serialized.h (absent at scan time); the signal
		// is a literal `#include` in any closure file matching a known
		// EN output.
		var depENRefs []NodeRef
		var depENOutputs []VFS

		if len(ctx.enOutputs) > 0 {
			// Build a map from bare rel-path suffix → buildRootPath for
			// all known _serialized.h EN outputs. Key is the path a
			// source header would write in an #include angle-bracket
			// form, e.g. "devtools/ymake/diag/stats_enums.h_serialized.h".
			serializedHByRel := make(map[string]VFS, len(ctx.enOutputs))
			for buildRootPath := range ctx.enOutputs {
				if !buildRootPath.IsBuild() || !strings.HasSuffix(buildRootPath.Rel, "_serialized.h") {
					continue
				}
				serializedHByRel[buildRootPath.Rel] = buildRootPath
			}

			depSeen := map[NodeRef]struct{}{}

			if len(serializedHByRel) > 0 {
				// Consult the scanner's parsed-directive cache rather
				// than re-opening each closure entry. The scanner
				// already parsed each header while building `closure`;
				// IncludeDirectiveTargets returns cached target strings.
				enScanner := ctx.scannerTarget
				for _, srcAbsPath := range closure {
					targets := enScanner.IncludeDirectiveTargets(srcAbsPath)
					for _, includePath := range targets {
						if !strings.HasSuffix(includePath, "_serialized.h") {
							continue
						}
						buildRootPath, ok := serializedHByRel[includePath]
						if !ok {
							continue
						}
						ref := ctx.enOutputs[buildRootPath]
						if _, dup := depSeen[ref]; dup {
							continue
						}
						depSeen[ref] = struct{}{}
						depENRefs = append(depENRefs, ref)
						depENOutputs = append(depENOutputs, buildRootPath)
						// Also include the corresponding _serialized.cpp path.
						cppPath := Build(strings.TrimSuffix(buildRootPath.Rel, "_serialized.h") + "_serialized.cpp")
						if cppRef, ok2 := ctx.enOutputs[cppPath]; ok2 && cppRef == ref {
							depENOutputs = append(depENOutputs, cppPath)
						}
					}
				}
			}
		}

		// Register EN outputs in the target scanner's CodegenRegistry
		// with populated EmitsIncludes (EN emits on target axis).
		// Per enum_parser/main.cpp::WriteHeader:
		//   _serialized.h  → util/generic/serialized_enum.h + input header.
		//   _serialized.cpp → enum_serialization_runtime headers + util.
		//
		// Registered BEFORE EmitEN so the EN node can walk its
		// _serialized.cpp via the registry to augment its `inputs`
		// closure (surfaces dispatch_methods.h / ordered_pairs.h /
		// enum_runtime.h in the EN node's inputs).
		serializedCPPPath := Build(instance.Path + "/" + headerRel + "_serialized.cpp")
		var serializedHPath VFS
		if withHeader {
			serializedHPath = Build(instance.Path + "/" + headerRel + "_serialized.h")
		}
		if ctx.scannerTarget.codegen != nil {
			headerSrc := Source(instance.Path + "/" + headerRel)
			cppParsed := []includeDirective{
				{kind: includeQuoted, target: headerSrc.Rel},
				{kind: includeQuoted, target: "tools/enum_parser/enum_parser/stdlib_deps.h"},
				{kind: includeQuoted, target: "tools/enum_parser/enum_serialization_runtime/dispatch_methods.h"},
				{kind: includeQuoted, target: "tools/enum_parser/enum_serialization_runtime/enum_runtime.h"},
				{kind: includeQuoted, target: "tools/enum_parser/enum_serialization_runtime/ordered_pairs.h"},
				{kind: includeQuoted, target: "util/generic/map.h"},
				{kind: includeQuoted, target: "util/generic/serialized_enum.h"},
				{kind: includeQuoted, target: "util/generic/singleton.h"},
				{kind: includeQuoted, target: "util/generic/string.h"},
				{kind: includeQuoted, target: "util/generic/typetraits.h"},
				{kind: includeQuoted, target: "util/generic/vector.h"},
				{kind: includeQuoted, target: "util/stream/output.h"},
				{kind: includeQuoted, target: "util/string/cast.h"},
			}
			sort.Slice(cppParsed, func(i, j int) bool { return cppParsed[i].target < cppParsed[j].target })
			registerGeneratedParsedOutput(ctx, instance, "EN", serializedCPPPath, cppParsed)
			if withHeader {
				// Include the sibling _serialized.cpp so CC consumers
				// that #include the _serialized.h transitively pull the
				// .cpp into their inputs and (via its EmitsIncludes) the
				// enum_serialization_runtime header set. REF bundles
				// the EN producer's .h and .cpp outputs together in
				// every downstream CC's inputs.
				hParsed := []includeDirective{
					{kind: includeQuoted, target: headerSrc.Rel},
					{kind: includeQuoted, target: serializedCPPPath.Rel},
					{kind: includeQuoted, target: "util/generic/serialized_enum.h"},
				}
				sort.Slice(hParsed, func(i, j int) bool { return hParsed[i].target < hParsed[j].target })
				registerGeneratedParsedOutput(ctx, instance, "EN", serializedHPath, hParsed)
			}
		}

		// Walk each cross-EN dep's _serialized.cpp to fold its transitive
		// closure into THIS EN node's `inputs`. The cross-EN dep's .cpp
		// carries the enum_runtime.h closure (dispatch_methods.h,
		// ordered_pairs.h). Leaf EN nodes (no cross-EN deps) keep REF's
		// tight 2-input shape.
		//
		// Exclude headerSrc and depENOutputs (EmitEN appends them
		// separately) and filter the source-header closure against
		// depENOutputs to avoid multiset duplicates.
		enClosureExcl := map[VFS]struct{}{
			Source(instance.Path + "/" + headerRel): {},
		}
		for _, p := range depENOutputs {
			enClosureExcl[p] = struct{}{}
		}
		filteredClosure := make([]VFS, 0, len(closure))
		for _, p := range closure {
			if _, drop := enClosureExcl[p]; drop {
				continue
			}
			filteredClosure = append(filteredClosure, p)
		}
		var crossCppClosure []VFS
		for _, depOut := range depENOutputs {
			if !strings.HasSuffix(depOut.Rel, "_serialized.cpp") {
				continue
			}
			sub := walkClosure(ctx, instance, depOut, scanIn)
			for _, p := range sub {
				if _, drop := enClosureExcl[p]; drop {
					continue
				}
				crossCppClosure = append(crossCppClosure, p)
			}
		}
		// Walk OUR OWN _serialized.cpp output through the codegen
		// registry to fold its transitive include closure into THIS EN
		// node's `inputs`. REF's EN node inputs equal the consuming CC
		// node's inputs for the plain variant; WITH_HEADER variants
		// keep tight source-header-only inputs (consumers absorb the
		// full closure on their side).
		var ownOutputClosure []VFS
		if !withHeader && ctx.scannerTarget.codegen != nil {
			sub := walkClosure(ctx, instance, serializedCPPPath, scanIn)
			for _, p := range sub {
				if _, drop := enClosureExcl[p]; drop {
					continue
				}
				ownOutputClosure = append(ownOutputClosure, p)
			}
		}
		enClosure := mergeDedupVFS(filteredClosure, crossCppClosure)
		enClosure = mergeDedupVFS(enClosure, ownOutputClosure)
		sort.Slice(enClosure, func(i, j int) bool { return enClosure[i].String() < enClosure[j].String() })

		// When this EN's transitive closure pulls in a PB/EV producer's
		// $(B) output (e.g. EN whose header eventually #includes
		// msg.ev.pb.h), the EN node deps on that producer. Filter out
		// refs already in depENRefs.
		augmentedDepENRefs := depENRefs
		if extra := resolveCodegenDepRefs(ctx, instance, enClosure, depENRefs...); len(extra) > 0 {
			augmentedDepENRefs = append(append([]NodeRef(nil), depENRefs...), extra...)
		}

		enRef, enOutPaths := EmitEN(
			instance,
			Source(instance.Path+"/"+headerRel),
			withHeader,
			enumParserLD,
			enumParserBin,
			augmentedDepENRefs,
			depENOutputs,
			enClosure,
			ctx.emit,
		)

		// Record outputs so later EN nodes can dep on them.
		for _, p := range enOutPaths {
			ctx.enOutputs[p] = enRef
		}

		// Emit the downstream CC compiling the EN-produced
		// `_serialized.cpp` as an implicit module source. The CC
		// inherits consumerInputs (full compile bag). depPrefix is the
		// cross-EN dep set placed ahead of the consumer's own
		// `_serialized.cpp` in the CC's inputs[].
		if consumerInputs != nil {
			cppRel := headerRel + "_serialized.cpp"
			// DepRefs: own EN + cross-EN dep refs.
			allDepRefs := make([]NodeRef, 0, 1+len(depENRefs))
			allDepRefs = append(allDepRefs, enRef)
			allDepRefs = append(allDepRefs, depENRefs...)
			ccRef, ccOut, ccIns := emitCodegenDownstreamCC(ctx, instance, cppRel, depENOutputs, allDepRefs, *consumerInputs)
			res.CCRefs = append(res.CCRefs, ccRef)
			res.CCOutputs = append(res.CCOutputs, ccOut)
			res.MemberInputsList = append(res.MemberInputsList, ccIns)
		}
	}

	return res
}
