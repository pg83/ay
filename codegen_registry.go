package main

// codegen_registry.go — per-scanner registry of codegen-emitted file metadata.
//
// Generated files do not exist at gen time, so the include scanner consults
// this buildRootPath → producerInfo map as an existence tier alongside the
// source tree.
//
// One registry per IncludeScanner so same-filename outputs across target/host
// axes do not collide.
//
// Uniqueness invariant mirrors upstream's DupSrc (macro_processor.cpp:957):
// duplicate Register throws. All() returns entries sorted by OutputPath.

import "sort"

// GeneratedFileInfo describes one codegen-emitted file. Populated during the
// emit walk by each codegen emitter.
//
// Some emitters Register before the producer NodeRef is known, then
// SetProducerRef backfills it post-Emit. HasProducerRef discriminates
// "registered, no ref yet" from "registered, ref valid" — NodeRef{} (id=0)
// collides with the first emitted node so the flag is mandatory.
type GeneratedFileInfo struct {
	// ProducerKvP is the node-kind key matching the KV["p"] value emitted by
	// the producer node (e.g. "EN", "PB", "AR", "CP").
	ProducerKvP string

	// OutputPath is the $(B)-rooted path of this generated file (e.g.
	// VFS{Build, "devtools/ymake/diag/stats_enums.h_serialized.cpp"}).
	OutputPath VFS

	// ProducerRef is the NodeRef of the emitted producer node. Valid only when
	// HasProducerRef is true. resolveCodegenDepRefs uses this to thread the
	// producer ref into consumer CC `deps[]` for both #include-driven (header
	// closure) and input-driven (inputs[] $(B) paths) lookups.
	ProducerRef    NodeRef
	HasProducerRef bool
}

// CodegenRegistry maps every $(B)-prefixed generated file path to its
// producer metadata. Populated incrementally during the emit walk (codegen
// emitters fire before CC emitters per PEERDIR-DFS order). The scanner
// consults it as a third existence tier.
type CodegenRegistry struct {
	byOutput VFSMap[*GeneratedFileInfo]
}

// NewCodegenRegistry allocates an empty CodegenRegistry. Pre-sized for the
// observed codegen output count in the devtools/ymake/bin closure.
func NewCodegenRegistry() *CodegenRegistry {
	return &CodegenRegistry{
		byOutput: NewVFSMap[*GeneratedFileInfo](256),
	}
}

// Register records info under info.OutputPath.
//
// Precondition: info.OutputPath is non-empty and Build-rooted.
// Throws if the same OutputPath is registered a second time — this mirrors
// upstream's DupSrc diagnostic (macro_processor.cpp:957) and enforces the
// build-system invariant that no two nodes produce the same output file.
func (r *CodegenRegistry) Register(info *GeneratedFileInfo) {
	if existing, dup := r.byOutput.Get(info.OutputPath); dup {
		ThrowFmt("CodegenRegistry: duplicate producer for %q (existing kind=%q, new kind=%q)",
			info.OutputPath.String(), existing.ProducerKvP, info.ProducerKvP)
	}

	r.byOutput.Set(info.OutputPath, info)
}

// Lookup returns the GeneratedFileInfo for path, or (nil, false) if path is
// not registered. O(1) map lookup.
func (r *CodegenRegistry) Lookup(path VFS) (*GeneratedFileInfo, bool) {
	return r.byOutput.Get(path)
}

// SetProducerRef backfills the ProducerRef for an already-registered path.
// Throws if path is not registered. Idempotent on identical refs; conflicting
// refs throw. Most emitters Register before Emit returns so the scanner's
// existence-tier sees the output; this helper fills the NodeRef in after Emit
// so resolveCodegenDepRefs can lift it into consumer CC `deps[]`.
func (r *CodegenRegistry) SetProducerRef(path VFS, ref NodeRef) {
	info, ok := r.byOutput.Get(path)
	if !ok {
		ThrowFmt("CodegenRegistry: SetProducerRef on unregistered path %q", path.String())
	}

	if info.HasProducerRef && info.ProducerRef != ref {
		ThrowFmt("CodegenRegistry: conflicting ProducerRef for %q (existing=%v, new=%v)",
			path.String(), info.ProducerRef, ref)
	}

	info.ProducerRef = ref
	info.HasProducerRef = true
}

// All returns all registered entries sorted by OutputPath. Deterministic across
// runs because OutputPath is derived from source file paths (which are fixed).
// Allocates a new slice on each call; callers that need a stable snapshot
// should retain the result.
func (r *CodegenRegistry) All() []*GeneratedFileInfo {
	out := make([]*GeneratedFileInfo, 0, r.byOutput.Len())

	for _, bucket := range r.byOutput {
		for _, info := range bucket {
			out = append(out, info)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return lessVFS(out[i].OutputPath, out[j].OutputPath)
	})

	return out
}

// Len returns the number of registered entries.
func (r *CodegenRegistry) Len() int {
	return r.byOutput.Len()
}

func registerGeneratedParsedOutput(ctx *genCtx, instance ModuleInstance, kind string, output VFS, parsed []includeDirective) {
	reg := codegenRegForInstance(ctx, instance)
	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP: kind,
			OutputPath:  output,
		})
	}

	scanner := ctx.scannerFor(instance)
	if scanner != nil {
		scanner.parsers.RegisterBuildParsedIncludes(output.Rel, parsed)
	}
}

func bindGeneratedOutput(ctx *genCtx, instance ModuleInstance, output VFS, ref NodeRef) {
	reg := codegenRegForInstance(ctx, instance)
	if reg == nil {
		return
	}

	reg.SetProducerRef(output, ref)
}

func registerBoundGeneratedParsedOutput(ctx *genCtx, instance ModuleInstance, kind string, output VFS, parsed []includeDirective, ref NodeRef) {
	reg := codegenRegForInstance(ctx, instance)
	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP: kind,
			OutputPath:  output,
			ProducerRef: ref,
			HasProducerRef: true,
		})
	}

	scanner := ctx.scannerFor(instance)
	if scanner != nil {
		scanner.parsers.RegisterBuildParsedIncludes(output.Rel, parsed)
	}
}

func generatedOutputClosure(ctx *genCtx, instance ModuleInstance, output VFS, in ModuleCCInputs) []VFS {
	return walkClosure(ctx, instance, output, in)
}

func codegenRegForInstance(ctx *genCtx, instance ModuleInstance) *CodegenRegistry {
	sc := ctx.scannerFor(instance)
	if sc == nil {
		return nil
	}
	return sc.codegen
}
