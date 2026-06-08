package main

type GeneratedFileInfo struct {
	ProducerKvP string

	OutputPath VFS

	// SourcePath is the canonical pre-generation file when the producer is a
	// pass-through (currently: CP / COPY_FILE). Upstream reports this — not
	// OutputPath — as the input edge in transitive closures, so include walks
	// rewrite OutputPath to SourcePath on the way out. Zero value means "no
	// remapping; OutputPath is the canonical input edge".
	SourcePath VFS

	// IsText marks a COPY_FILE(TEXT) registration. TEXT copies expand the
	// source content into the destination verbatim, so the .txt source is a
	// real compiler input even when the COPY lives in a different module than
	// the CC node that includes the generated header. Such a dst registers its
	// source as a closure leaf (see AddClosureLeaf) so it rides across module
	// boundaries with the dst's window.
	IsText bool

	ProducerRef    NodeRef
	HasProducerRef bool

	// GeneratorRefs are the NodeRefs of the codegen TOOLS that produce this file
	// (e.g. event2cpp/protoc/cpp_styleguide for a .ev.pb.h), as returned by
	// ctx.tool(). The include scanner resolves each tool's INDUCED_DEPS
	// (genCtx.toolInduced[ref]) into this file's child set generically, so the
	// producing emitter need not hand-weave the tools' runtime headers into the
	// registered parsed includes.
	GeneratorRefs []NodeRef

	// SourceInputs are the producer node's $(S)-rooted leaf inputs, propagated
	// to consumers that compile this generated file. Upstream's flat input model
	// lists the full transitive source closure on every node, so a node
	// compiling a build-generated source (e.g. a PB protoc node fed a
	// RUN_ANTLR-generated .proto) carries the generator's grammar / template /
	// tool / script sources too. Zero len means "nothing to propagate".
	SourceInputs []VFS

	// ClosureLeaves are extra VFS that must ride in this output's include-closure
	// window as bare, non-expanded members — a "generated-from"/source input edge,
	// not a C++ include. COPY_FILE(TEXT) registers its $(S) source (+ fs_tools.py
	// tooling); a PB header registers the $(S) .proto it was generated from. The
	// scanner splices these into the output's window at build time (dfs pass-2 /
	// emitClosure) so they ride transitively to every consumer that includes the
	// output, without their own #includes being re-resolved per consuming module.
	ClosureLeaves []VFS

	DeferredCF *deferredCF
}

type deferredCF struct {
	instance      ModuleInstance
	srcVFS        VFS
	outVFS        VFS
	cfgVars       []string
	includeInputs []VFS
	tc            moduleToolchain
}

type CodegenRegistry struct {
	// byStr maps an interned path STR (full $(B)/… or its rel form) to the producer
	// info. The hot Lookup (once per scanned include) was the top map in the CPU
	// profile; a DenseMap keyed by STR drops the hashing/probing. strID losslessly
	// encodes the root (the interned string carries the $(S)/ or $(B)/ prefix), so a
	// $(B) dst and a $(S) source never collide despite the shared STR key space.
	// Closure leaves are NOT stored here — they are a field on GeneratedFileInfo
	// (per-output data), read via reg.Lookup.
	byStr DenseMap[STR, *GeneratedFileInfo]

	// splitPrefixSeen marks split-key PREFIX STRs (the first component rel[:i], not
	// an output path) that occur as a bySplit key prefix. LookupSplit checks it
	// first: a 1-bit-per-STR probe short-circuits the uint64 bySplit hash-map lookup
	// whenever the prefix has no split entry — the common case on the hot resolve
	// path, where most addincl prefixes hold no codegen outputs. A bitset rather
	// than a bool DenseMap column: the value is always true, only presence matters.
	splitPrefixSeen BitSet

	// bySplit maps a (prefix, suffix) STR pair to its producer info, keyed by
	// splitMix64(prefix, suffix) — the two ids hashed into a uniform 64-bit key,
	// letting an identity-hashed IntMap spread the pairs. Gated by splitPrefixSeen so
	// the probe runs only for prefixes known to have an entry.
	bySplit *IntMap[*GeneratedFileInfo]
}

func splitKey(prefix, suffix STR) uint64 {
	return splitMix64(uint32(prefix), uint32(suffix))
}

func NewCodegenRegistry() *CodegenRegistry {
	return &CodegenRegistry{bySplit: NewIntMap[*GeneratedFileInfo](1 << 14)}
}

func (r *CodegenRegistry) Register(info *GeneratedFileInfo) {
	full := STR(info.OutputPath.strID())

	if existing, ok := r.byStr.Get(full); ok {
		ThrowFmt("CodegenRegistry: duplicate producer for %q (existing kind=%q, new kind=%q)",
			info.OutputPath.String(), existing.ProducerKvP, info.ProducerKvP)
	}

	rel := info.OutputPath.Rel()
	r.byStr.Put(full, info)
	r.byStr.Put(internStr(rel), info)

	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' {
			r.putSplit(internStr(rel[:i]), internStr(rel[i+1:]), info)
		}
	}
}

func (r *CodegenRegistry) putSplit(prefix, suffix STR, info *GeneratedFileInfo) {
	r.bySplit.Put(splitKey(prefix, suffix), info)
	r.splitPrefixSeen.add(uint32(prefix)) // mark the prefix so LookupSplit can gate the probe
}

func (r *CodegenRegistry) Lookup(path VFS) *GeneratedFileInfo {
	info, _ := r.byStr.Get(STR(path.strID()))

	return info
}

func (r *CodegenRegistry) LookupRel(rel string) *GeneratedFileInfo {
	id := interned(rel)

	if id == nil {
		return nil
	}

	info, _ := r.byStr.Get(*id)

	return info
}

func (r *CodegenRegistry) LookupSplit(prefix, suffix STR) *GeneratedFileInfo {
	// Gate the uint64 hash-map probe on the dense prefix flag: most addincl
	// prefixes hold no codegen split entry, so the array probe short-circuits.
	if !r.splitPrefixSeen.has(uint32(prefix)) {
		return nil
	}

	if info := r.bySplit.Get(splitKey(prefix, suffix)); info != nil {
		return *info
	}

	return nil
}

// AddClosureLeaf appends leaf to node's GeneratedFileInfo.ClosureLeaves. node
// must already be registered (the producer info exists). Cold path (codegen
// registration); the scanner reads the result on the hot path via ClosureLeaves.
func (r *CodegenRegistry) AddClosureLeaf(node, leaf VFS) {
	info, ok := r.byStr.Get(STR(node.strID()))

	if !ok {
		ThrowFmt("CodegenRegistry: AddClosureLeaf on unregistered path %q", node.String())
	}

	info.ClosureLeaves = append(info.ClosureLeaves, leaf)
}

// ClosureLeaves returns the non-expanded closure-window members of node (nil
// when node is not a registered output or has none).
func (r *CodegenRegistry) ClosureLeaves(node VFS) []VFS {
	if info, ok := r.byStr.Get(STR(node.strID())); ok {
		return info.ClosureLeaves
	}

	return nil
}

func (r *CodegenRegistry) SetProducerRef(path VFS, ref NodeRef) {
	info, ok := r.byStr.Get(STR(path.strID()))

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

func (r *CodegenRegistry) SetSourceInputs(path VFS, src []VFS) {
	if len(src) == 0 {
		return
	}

	info, ok := r.byStr.Get(STR(path.strID()))

	if !ok {
		ThrowFmt("CodegenRegistry: SetSourceInputs on unregistered path %q", path.String())
	}

	info.SourceInputs = src
}

func registerGeneratedParsedOutput(ctx *genCtx, instance ModuleInstance, kind string, output VFS, parsed []includeDirective, generatorRefs []NodeRef) {
	reg := codegenRegForInstance(ctx, instance)

	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP:   kind,
			OutputPath:    output,
			GeneratorRefs: generatorRefs,
		})
	}

	scanner := ctx.scannerFor(instance)

	if scanner != nil {
		scanner.parsers.RegisterBuildParsedIncludes(output.Rel(), parsed)
	}
}

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

func registerBoundGeneratedParsedOutput(ctx *genCtx, instance ModuleInstance, kind string, output VFS, parsed []includeDirective, ref NodeRef, generatorRefs []NodeRef) {
	registerBoundGeneratedParsedOutputWithSource(ctx, instance, kind, output, 0, parsed, ref, generatorRefs)
}

// registerBoundGeneratedParsedOutputWithSource is the CP variant that records
// the COPY source alongside the dst so the closure walker can rewrite the
// emitted input edge to the source. Pass sourcePath = 0 for non-CP producers
// to fall back to OutputPath as the canonical edge. generatorRefs are the codegen
// tools whose INDUCED_DEPS the scanner mixes into this output's closure (nil when
// the producer has no induced-deps tool).
func registerBoundGeneratedParsedOutputWithSource(ctx *genCtx, instance ModuleInstance, kind string, output VFS, sourcePath VFS, parsed []includeDirective, ref NodeRef, generatorRefs []NodeRef) {
	reg := codegenRegForInstance(ctx, instance)

	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP:    kind,
			OutputPath:     output,
			SourcePath:     sourcePath,
			ProducerRef:    ref,
			HasProducerRef: true,
			GeneratorRefs:  generatorRefs,
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
