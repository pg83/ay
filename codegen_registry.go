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
	// source as a closure leaf (see closureLeaves) so it rides across module
	// boundaries with the dst's window.
	IsText bool

	ProducerRef    NodeRef
	HasProducerRef bool

	// SourceInputs are the producer node's $(S)-rooted leaf inputs, propagated
	// to consumers that compile this generated file. Upstream's flat input model
	// lists the full transitive source closure on every node, so a node
	// compiling a build-generated source (e.g. a PB protoc node fed a
	// RUN_ANTLR-generated .proto) carries the generator's grammar / template /
	// tool / script sources too. Zero len means "nothing to propagate".
	SourceInputs []VFS

	DeferredCF *deferredCF
}

type deferredCF struct {
	instance      ModuleInstance
	srcVFS        VFS
	outVFS        VFS
	cfgVars       []string
	includeInputs []VFS
}

type CodegenRegistry struct {
	// byStr maps an interned path STR (full $(B)/… or its rel form) to its
	// producer info. The hot Lookup (once per scanned include) was the top map
	// in the CPU profile; a DenseMap keyed by STR drops the hashing/probing.
	byStr DenseMap[STR, *GeneratedFileInfo]

	// bySplit maps a (prefix, suffix) STR pair to its producer info, packed into a
	// single uint64 key (prefix << 32 | suffix) so a split lookup is one fast64 map
	// probe instead of a two-level DenseMap-then-inner-map dance.
	bySplit map[uint64]*GeneratedFileInfo

	// closureLeaves maps a generated $(B) output to extra VFS that must ride in
	// its include-closure window as bare, non-expanded members. A COPY_FILE(TEXT)
	// dst registers its $(S) source (and the fs_tools.py copy tooling) here: the
	// dst's content is the source verbatim, so the source is a real compiler input
	// of every unit that includes the dst — but its own #include directives must
	// NOT be re-resolved per consuming module (that leaked sibling staging copies,
	// see copyFileParsedIncludes). The scanner splices these leaves into the dst's
	// window at build time (dfs pass-2 / emitClosure), so they ride transitively
	// to every consumer without being traversed as children. This replaces the
	// per-CC-source closure re-walk that withContextSourceExtras used to do.
	closureLeaves DenseMap[VFS, []VFS]
}

func splitKey(prefix, suffix STR) uint64 { return uint64(prefix)<<32 | uint64(suffix) }

func NewCodegenRegistry() *CodegenRegistry {
	return &CodegenRegistry{bySplit: make(map[uint64]*GeneratedFileInfo, 1<<14)}
}

func (r *CodegenRegistry) Register(info *GeneratedFileInfo) {
	full := STR(info.OutputPath.strID())

	if existing, ok := r.byStr.Get(full); ok {
		ThrowFmt("CodegenRegistry: duplicate producer for %q (existing kind=%q, new kind=%q)",
			info.OutputPath.String(), existing.ProducerKvP, info.ProducerKvP)
	}

	rel := info.OutputPath.Rel()
	r.byStr.Put(full, info)
	r.byStr.Put(internString(rel), info)

	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' {
			r.putSplit(internString(rel[:i]), internString(rel[i+1:]), info)
		}
	}
}

func (r *CodegenRegistry) putSplit(prefix, suffix STR, info *GeneratedFileInfo) {
	r.bySplit[splitKey(prefix, suffix)] = info
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
	return r.bySplit[splitKey(prefix, suffix)]
}

// AddClosureLeaf records leaf as a non-expanded member of node's closure window.
// Cold path (codegen registration); the scanner reads the result on the hot path
// via ClosureLeaves.
func (r *CodegenRegistry) AddClosureLeaf(node, leaf VFS) {
	leaves, _ := r.closureLeaves.Get(node)
	r.closureLeaves.Put(node, append(leaves, leaf))
}

// ClosureLeaves returns the non-expanded closure-window members registered for
// node (nil when none). Keyed by VFS — not strID — so a $(B) dst and a $(S)
// source sharing the same path STR do not collide.
func (r *CodegenRegistry) ClosureLeaves(node VFS) []VFS {
	leaves, _ := r.closureLeaves.Get(node)
	return leaves
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
	info, ok := r.byStr.Get(STR(path.strID()))

	if !ok {
		ThrowFmt("CodegenRegistry: SetSourceInputs on unregistered path %q", path.String())
	}

	info.SourceInputs = src
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
	registerBoundGeneratedParsedOutputWithSource(ctx, instance, kind, output, 0, parsed, ref)
}

// registerBoundGeneratedParsedOutputWithSource is the CP variant that records
// the COPY source alongside the dst so the closure walker can rewrite the
// emitted input edge to the source. Pass sourcePath = 0 for non-CP producers
// to fall back to OutputPath as the canonical edge.
func registerBoundGeneratedParsedOutputWithSource(ctx *genCtx, instance ModuleInstance, kind string, output VFS, sourcePath VFS, parsed []includeDirective, ref NodeRef) {
	reg := codegenRegForInstance(ctx, instance)

	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP:    kind,
			OutputPath:     output,
			SourcePath:     sourcePath,
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
