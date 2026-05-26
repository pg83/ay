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
// duplicate Register throws.

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

	// DeferredCF, when non-nil, describes a CONFIGURE_FILE-generated header
	// (.h.in declared in SRCS) whose owning module is the first CONSUMER to
	// #include it, not the declaring module. The CF node is emitted lazily by
	// the probe in resolveCodegenDepRefsExt with module_dir = the consuming
	// module, mirroring ymake realizing a generated header in the module that
	// includes it. nil for eagerly-emitted producers.
	DeferredCF *deferredCF
}

// deferredCF captures everything EmitCF needs to realize a deferred .h.in
// header in a consuming module. instance is the DECLARING module (provides the
// $(S)/$(B) paths); the module_dir is supplied by the consumer at emit time.
type deferredCF struct {
	instance      ModuleInstance
	srcVFS        VFS
	outVFS        VFS
	cfgVars       []string
	includeInputs []VFS
}

// CodegenRegistry maps every $(B)-prefixed generated file path to its
// producer metadata. Populated incrementally during the emit walk (codegen
// emitters fire before CC emitters per PEERDIR-DFS order). The scanner
// consults it as a third existence tier.
type CodegenRegistry struct {
	// byStr is the full-path index, keyed by interned-string id (STR). Each
	// output goes in under two ids: OutputPath.strID() — the intern id of the
	// full "$(B)/<rel>" string, root-aware, what Lookup probes — and
	// internString(OutputPath.Rel()) — the bare rel id, what LookupRel probes.
	// The two never collide ("$(B)/<rel>" vs "<rel>"). Both must be true intern
	// ids (not raw VFS values, which pack a root bit and live in a 2x id space).
	byStr map[STR]*GeneratedFileInfo

	// bySplit answers the scanner's search-tier question "is
	// <addinclDir>/<target> a generated output?" as the plain two-level read
	// bySplit[addinclDir][target] — no candidate-path concatenation. Each output
	// is indexed under every NON-EMPTY-prefix split of its rel: a/b/c registers
	// bySplit["a"]["b/c"] and bySplit["a/b"]["c"]. The empty-prefix (full-path)
	// case is not stored here — that is LookupRel against byStr. Keyed by STR:
	// Register interns each split fragment (a small, bounded load on the table),
	// the maps stay compact uint32→uint32→ptr, and the caller resolves its
	// operands through `interned` once — a never-interned fragment can't be a
	// registered split, so it skips the lookup without a string compare.
	bySplit map[STR]map[STR]*GeneratedFileInfo
}

// NewCodegenRegistry allocates an empty CodegenRegistry. Pre-sized for the
// observed codegen output count in the devtools/ymake/bin closure.
func NewCodegenRegistry() *CodegenRegistry {
	return &CodegenRegistry{
		byStr:   make(map[STR]*GeneratedFileInfo, 256),
		bySplit: make(map[STR]map[STR]*GeneratedFileInfo, 256),
	}
}

// Register records info under info.OutputPath.
//
// Precondition: info.OutputPath is non-empty and Build-rooted.
// Throws if the same OutputPath is registered a second time — this mirrors
// upstream's DupSrc diagnostic (macro_processor.cpp:957) and enforces the
// build-system invariant that no two nodes produce the same output file.
func (r *CodegenRegistry) Register(info *GeneratedFileInfo) {
	full := STR(info.OutputPath.strID())
	if existing := r.byStr[full]; existing != nil {
		ThrowFmt("CodegenRegistry: duplicate producer for %q (existing kind=%q, new kind=%q)",
			info.OutputPath.String(), existing.ProducerKvP, info.ProducerKvP)
	}

	rel := info.OutputPath.Rel()
	r.byStr[full] = info
	r.byStr[internString(rel)] = info

	// Index every non-empty-prefix split into bySplit.
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' {
			r.putSplit(internString(rel[:i]), internString(rel[i+1:]), info)
		}
	}
}

func (r *CodegenRegistry) putSplit(prefix, suffix STR, info *GeneratedFileInfo) {
	inner := r.bySplit[prefix]
	if inner == nil {
		inner = make(map[STR]*GeneratedFileInfo, 2)
		r.bySplit[prefix] = inner
	}

	inner[suffix] = info
}

// Lookup returns the producer for the full (root-aware) VFS path, or nil.
func (r *CodegenRegistry) Lookup(path VFS) *GeneratedFileInfo {
	return r.byStr[STR(path.strID())]
}

// LookupRel returns the producer for a full Build-relative path, or nil.
func (r *CodegenRegistry) LookupRel(rel string) *GeneratedFileInfo {
	id := interned(rel)
	if id == nil {
		return nil
	}

	return r.byStr[*id]
}

// LookupSplit returns the producer for <prefix>/<suffix> — the two interned ids
// of a non-empty addincl-dir and an include target — or nil. A plain two-level
// map read; the caller resolves prefix/suffix to STR (and short-circuits a
// never-interned target) before calling.
func (r *CodegenRegistry) LookupSplit(prefix, suffix STR) *GeneratedFileInfo {
	return r.bySplit[prefix][suffix]
}

// SetProducerRef backfills the ProducerRef for an already-registered path.
// Throws if path is not registered. Idempotent on identical refs; conflicting
// refs throw. Most emitters Register before Emit returns so the scanner's
// existence-tier sees the output; this helper fills the NodeRef in after Emit
// so resolveCodegenDepRefs can lift it into consumer CC `deps[]`.
func (r *CodegenRegistry) SetProducerRef(path VFS, ref NodeRef) {
	info := r.byStr[STR(path.strID())]
	if info == nil {
		ThrowFmt("CodegenRegistry: SetProducerRef on unregistered path %q", path.String())
	}

	if info.HasProducerRef && info.ProducerRef != ref {
		ThrowFmt("CodegenRegistry: conflicting ProducerRef for %q (existing=%v, new=%v)",
			path.String(), info.ProducerRef, ref)
	}

	info.ProducerRef = ref
	info.HasProducerRef = true
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
		scanner.parsers.RegisterBuildParsedIncludes(output.Rel(), parsed)
	}
}

// registerDeferredCF registers a .h.in generated header whose CF node is
// emitted lazily by the first consumer that #includes it (see deferredCF).
// The scanner's build-parsed-includes are registered eagerly so consumer
// walkClosure resolves the generated header into includeInputs even before the
// node exists; resolveCodegenDepRefsExt then emits the node and threads it into
// the consumer's deps.
func registerDeferredCF(ctx *genCtx, instance ModuleInstance, output VFS, parsed []includeDirective, def *deferredCF) {
	reg := codegenRegForInstance(ctx, instance)
	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP: "CF",
			OutputPath:  output,
			DeferredCF:  def,
		})
	}

	scanner := ctx.scannerFor(instance)
	if scanner != nil {
		scanner.parsers.RegisterBuildParsedIncludes(output.Rel(), parsed)
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
			ProducerKvP:    kind,
			OutputPath:     output,
			ProducerRef:    ref,
			HasProducerRef: true,
		})
	}

	scanner := ctx.scannerFor(instance)
	if scanner != nil {
		scanner.parsers.RegisterBuildParsedIncludes(output.Rel(), parsed)
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
