package main

type GeneratedFileInfo struct {
	ProducerKvP ProcKind

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

	// splitPrefixSeen marks split-key PREFIX dirs (the Source-rooted VFS of
	// rel[:i] — the canonical dir identity dirKey produces, not an output path)
	// that occur as a bySplit key prefix. LookupSplit checks it first: a
	// 1-bit-per-VFS probe short-circuits the uint64 bySplit hash-map lookup
	// whenever the prefix has no split entry — the common case on the hot resolve
	// path, where most addincl prefixes hold no codegen outputs. A bitset rather
	// than a bool DenseMap column: the value is always true, only presence matters.
	splitPrefixSeen BitSet

	// leafEver marks every VFS that has EVER been registered as a ClosureLeaf
	// (Register / AddClosureLeaf). The scanner's window-subsumption skip
	// (scanCtx.windowSubsumed) consults it: a leaf rides in closure windows as a
	// bare, non-expanded member, so its presence in a block does not imply its
	// window is present too — such a VFS must never short-circuit a splice. The
	// bit is set at registration, strictly before ClosureLeaves can hand the
	// leaf to any window build, so it is always visible by the time a leaf-path
	// element can appear in a dedup set. Conservative: a VFS that is both a
	// leaf somewhere and a regular include elsewhere merely loses the skip.
	leafEver BitSet

	// bySplit maps a (Source-dir-VFS prefix, suffix STR) pair to its producer
	// info, keyed by splitMix64(prefix, suffix) — two ids hashed into a uniform key,
	// letting an identity-hashed IntMap spread the pairs. Gated by splitPrefixSeen so
	// the probe runs only for prefixes known to have an entry.
	bySplit *IntMap[*GeneratedFileInfo]
}

func splitKey(prefix VFS, suffix STR) uint64 {
	return splitMix64(uint32(prefix), uint32(suffix))
}

func newCodegenRegistry() *CodegenRegistry {
	return &CodegenRegistry{bySplit: newIntMap[*GeneratedFileInfo](1 << 14)}
}

func (r *CodegenRegistry) register(info *GeneratedFileInfo) {
	full := STR(info.OutputPath.strID())

	if existing, ok := r.byStr.get(full); ok {
		throwFmt("CodegenRegistry: duplicate producer for %q (existing kind=%q, new kind=%q)",
			info.OutputPath.string(), existing.ProducerKvP, info.ProducerKvP)
	}

	rel := info.OutputPath.rel()
	r.byStr.put(full, info)
	r.byStr.put(internStr(rel), info)

	for _, leaf := range info.ClosureLeaves {
		r.leafEver.add(uint32(leaf))
	}

	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' {
			r.putSplit(source(rel[:i]), internStr(rel[i+1:]), info)
		}
	}
}

func (r *CodegenRegistry) putSplit(prefix VFS, suffix STR, info *GeneratedFileInfo) {
	r.bySplit.put(splitKey(prefix, suffix), info)
	r.splitPrefixSeen.add(uint32(prefix)) // mark the prefix so LookupSplit can gate the probe
}

func (r *CodegenRegistry) lookup(path VFS) *GeneratedFileInfo {
	info, _ := r.byStr.get(STR(path.strID()))

	return info
}

// lookupSTR probes byStr by an already-interned id — either a full rooted path
// (== VFS strID) or the bare-rel form register also keys. Callers always hold
// the id (an include-target token), so no string round-trip exists here.
func (r *CodegenRegistry) lookupSTR(id STR) *GeneratedFileInfo {
	info, _ := r.byStr.get(id)

	return info
}

func (r *CodegenRegistry) lookupSplit(prefix VFS, suffix STR) *GeneratedFileInfo {
	// Gate the uint64 hash-map probe on the dense prefix flag: most addincl
	// prefixes hold no codegen split entry, so the array probe short-circuits.
	if !r.splitPrefixSeen.has(uint32(prefix)) {
		return nil
	}

	if info := r.bySplit.get(splitKey(prefix, suffix)); info != nil {
		return *info
	}

	return nil
}

// AddClosureLeaf appends leaf to node's GeneratedFileInfo.ClosureLeaves. node
// must already be registered (the producer info exists). Cold path (codegen
// registration); the scanner reads the result on the hot path via ClosureLeaves.
func (r *CodegenRegistry) addClosureLeaf(node, leaf VFS) {
	info, ok := r.byStr.get(STR(node.strID()))

	if !ok {
		throwFmt("CodegenRegistry: AddClosureLeaf on unregistered path %q", node.string())
	}

	info.ClosureLeaves = append(info.ClosureLeaves, leaf)
	r.leafEver.add(uint32(leaf))
}

// IsLeafEver reports whether v was ever registered as a ClosureLeaf — see the
// leafEver field comment.
func (r *CodegenRegistry) isLeafEver(v VFS) bool {
	return r.leafEver.has(uint32(v))
}

// ClosureLeaves returns the non-expanded closure-window members of node (nil
// when node is not a registered output or has none).
func (r *CodegenRegistry) closureLeaves(node VFS) []VFS {
	if info, ok := r.byStr.get(STR(node.strID())); ok {
		return info.ClosureLeaves
	}

	return nil
}

func (r *CodegenRegistry) setProducerRef(path VFS, ref NodeRef) {
	info, ok := r.byStr.get(STR(path.strID()))

	if !ok {
		throwFmt("CodegenRegistry: SetProducerRef on unregistered path %q", path.string())
	}

	if info.HasProducerRef && info.ProducerRef != ref {
		throwFmt("CodegenRegistry: conflicting ProducerRef for %q (existing=%v, new=%v)",
			path.string(), info.ProducerRef, ref)
	}

	info.ProducerRef = ref
	info.HasProducerRef = true
}

func (r *CodegenRegistry) setSourceInputs(path VFS, src []VFS) {
	if len(src) == 0 {
		return
	}

	info, ok := r.byStr.get(STR(path.strID()))

	if !ok {
		throwFmt("CodegenRegistry: SetSourceInputs on unregistered path %q", path.string())
	}

	info.SourceInputs = src
}

func registerGeneratedParsedOutput(ctx *GenCtx, instance ModuleInstance, kind ProcKind, output VFS, parsed []IncludeDirective, generatorRefs []NodeRef) {
	codegenRegForInstance(ctx, instance).register(&GeneratedFileInfo{
		ProducerKvP:   kind,
		OutputPath:    output,
		GeneratorRefs: generatorRefs,
	})

	ctx.scannerFor(instance).parsers.registerBuildParsedIncludes(output, parsed)
}

func bindGeneratedOutput(ctx *GenCtx, instance ModuleInstance, output VFS, ref NodeRef) {
	codegenRegForInstance(ctx, instance).setProducerRef(output, ref)
}

func registerBoundGeneratedParsedOutput(ctx *GenCtx, instance ModuleInstance, kind ProcKind, output VFS, parsed []IncludeDirective, ref NodeRef, generatorRefs []NodeRef) {
	registerBoundGeneratedParsedOutputWithSource(ctx, instance, kind, output, 0, parsed, ref, generatorRefs)
}

// registerBoundGeneratedParsedOutputWithSource is the CP variant that records
// the COPY source alongside the dst so the closure walker can rewrite the
// emitted input edge to the source. Pass sourcePath = 0 for non-CP producers
// to fall back to OutputPath as the canonical edge. generatorRefs are the codegen
// tools whose INDUCED_DEPS the scanner mixes into this output's closure (nil when
// the producer has no induced-deps tool).
func registerBoundGeneratedParsedOutputWithSource(ctx *GenCtx, instance ModuleInstance, kind ProcKind, output VFS, sourcePath VFS, parsed []IncludeDirective, ref NodeRef, generatorRefs []NodeRef) {
	codegenRegForInstance(ctx, instance).register(&GeneratedFileInfo{
		ProducerKvP:    kind,
		OutputPath:     output,
		SourcePath:     sourcePath,
		ProducerRef:    ref,
		HasProducerRef: true,
		GeneratorRefs:  generatorRefs,
	})

	ctx.scannerFor(instance).parsers.registerBuildParsedIncludes(output, parsed)
}

func generatedOutputClosure(ctx *GenCtx, instance ModuleInstance, output VFS, in ModuleCCInputs) []VFS {
	return walkClosureTail(ctx.scannerFor(instance), output, in.ScanCfg)
}

func codegenRegForInstance(ctx *GenCtx, instance ModuleInstance) *CodegenRegistry {
	return ctx.scannerFor(instance).codegen
}
