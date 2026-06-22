package main

type GeneratedFileInfo struct {
	ProducerKvP ProcKind

	OutputPath VFS

	// SourcePath is the canonical pre-generation file when the producer is a
	// pass-through (CP / COPY_FILE). Reported as the closure input edge instead of
	// OutputPath, so include walks rewrite to it. Zero means no remapping.
	SourcePath VFS

	// IsText marks a COPY_FILE(TEXT) registration: the source expands verbatim, so
	// it is a real compiler input even across module boundaries. Such a dst
	// registers its source as a closure leaf so it rides with the dst's window.
	IsText bool

	// ProducerRef is the NodeRef of the node that produces OutputPath, reserved
	// before registering so a consumer always reads a valid ref.
	ProducerRef NodeRef

	// GeneratorRefs are the codegen TOOLS that produce this file. The scanner
	// resolves each tool's INDUCED_DEPS into this file's child set generically, so
	// the emitter need not hand-weave the tools' runtime headers.
	GeneratorRefs []NodeRef

	// SourceInputs are the producer node's $(S)-rooted leaf inputs, propagated to
	// consumers compiling this generated file. Zero len means nothing to propagate.
	SourceInputs []VFS

	// CythonInducedPyx is the producing cython node's resolved "pyx" closure,
	// recorded on a generated _H / _API_H header. A CYTHON source that cdef-externs
	// this header uses it as its own cython source deps. Read explicitly when
	// building a consuming CY node's inputs, NOT spliced into the closure window,
	// so it reaches the transpile node but not the generated .c's C++ compile.
	CythonInducedPyx []VFS

	// ProducerSourceClosure is the producer node's FULL transitive $(S) input
	// closure, not just its direct leaves. The flat-input model folds it onto a
	// bytecode node compiling this generated source. Unlike SourceInputs (the
	// direct-leaf subset), read only by emitPySrcs. Zero len means none.
	ProducerSourceClosure []VFS

	// ProtoImportRels are the declared direct proto imports of a build-generated
	// `.proto` output (its producer's OUTPUT_INCLUDES, rel form). The generated
	// proto's source does not exist at configure time, so the consuming CPP_PROTO
	// node reads this list and registers each import's `.pb.h` as a direct include
	// of the generated `.pb.h`. Zero len means not a generated proto / no imports.
	ProtoImportRels []string

	// CythonMainOut is the producing cython node's MAIN generated output (the .c /
	// .cpp), recorded on each header output (an OutTogether sibling). A generated
	// cython compile reaching the header lists the main as an input. Zero means none.
	CythonMainOut VFS

	// ProducerMainOut is the producing node's MAIN (first declared) output. A
	// multi-output node links the others to it via OutTogether and the build
	// command lives on it, so a consumer of any additional output depends on it and
	// carries it as a flat input. Recorded so a resource objcopy embedding only
	// additional outputs still carries the main-output input. Zero when single-output.
	ProducerMainOut VFS

	// ClosureLeaves are extra VFS that ride in this output's closure window as bare,
	// non-expanded members — a "generated-from" source input edge, not a C++
	// include. The scanner splices these into the window so they ride to every
	// consumer without their own #includes being re-resolved per module.
	ClosureLeaves []VFS
}

type CodegenRegistry struct {
	// byStr maps an interned path STR (full $(B)/… or its rel form) to the producer
	// info. A DenseMap keyed by STR drops the hashing on the hot Lookup. strID
	// losslessly encodes the root, so a $(B) dst and a $(S) source never collide.
	byStr DenseMap[STR, *GeneratedFileInfo]

	// splitPrefixSeen marks split-key PREFIX dirs that occur as a bySplit key
	// prefix. LookupSplit checks it first so the bySplit hash lookup short-circuits
	// when the prefix has no split entry — the common case, where most addincl
	// prefixes hold no codegen outputs.
	splitPrefixSeen BitSet

	// leafEver marks every VFS ever registered as a ClosureLeaf. The scanner's
	// window-subsumption skip consults it: a leaf rides as a bare member, so its
	// presence does not imply its window is present and it must never short-circuit
	// a splice. Set at registration, so always visible in time.
	leafEver BitSet

	// bySplit maps a (Source-dir-VFS prefix, suffix STR) pair to its producer info,
	// keyed by splitMix64(prefix, suffix). Gated by splitPrefixSeen.
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
	r.splitPrefixSeen.add(uint32(prefix)) // gate LookupSplit's probe
}

func (r *CodegenRegistry) lookup(path VFS) *GeneratedFileInfo {
	info, _ := r.byStr.get(STR(path.strID()))

	return info
}

// lookupSTR probes byStr by an already-interned id (full rooted path or bare-rel
// form), so callers holding the id need no string round-trip.
func (r *CodegenRegistry) lookupSTR(id STR) *GeneratedFileInfo {
	info, _ := r.byStr.get(id)

	return info
}

func (r *CodegenRegistry) lookupSplit(prefix VFS, suffix STR) *GeneratedFileInfo {
	// Gate the hash-map probe on the dense prefix flag.
	if !r.splitPrefixSeen.has(uint32(prefix)) {
		return nil
	}

	if info := r.bySplit.get(splitKey(prefix, suffix)); info != nil {
		return *info
	}

	return nil
}

// AddClosureLeaf appends leaf to node's ClosureLeaves; node must already be
// registered.
func (r *CodegenRegistry) addClosureLeaf(node, leaf VFS) {
	info, ok := r.byStr.get(STR(node.strID()))

	if !ok {
		throwFmt("CodegenRegistry: AddClosureLeaf on unregistered path %q", node.string())
	}

	info.ClosureLeaves = append(info.ClosureLeaves, leaf)
	r.leafEver.add(uint32(leaf))
}

// IsLeafEver reports whether v was ever registered as a ClosureLeaf.
func (r *CodegenRegistry) isLeafEver(v VFS) bool {
	return r.leafEver.has(uint32(v))
}

// ClosureLeaves returns the non-expanded closure-window members of node.
func (r *CodegenRegistry) closureLeaves(node VFS) []VFS {
	if info, ok := r.byStr.get(STR(node.strID())); ok {
		return info.ClosureLeaves
	}

	return nil
}

// setCythonPyxInduced records a generated header's cython-induced "pyx" closure
// and the producing node's main output. node must already be registered.
func (r *CodegenRegistry) setCythonPyxInduced(node VFS, pyx []VFS, mainOut VFS) {
	info, ok := r.byStr.get(STR(node.strID()))

	if !ok {
		throwFmt("CodegenRegistry: setCythonPyxInduced on unregistered path %q", node.string())
	}

	info.CythonInducedPyx = pyx
	info.CythonMainOut = mainOut
}

// cythonPyxInduced returns node's recorded cython-induced "pyx" closure.
func (r *CodegenRegistry) cythonPyxInduced(node VFS) []VFS {
	if info, ok := r.byStr.get(STR(node.strID())); ok {
		return info.CythonInducedPyx
	}

	return nil
}

// cythonPyxInducedInfo returns node's recorded cython-induced "pyx" closure and
// the producing node's main output.
func (r *CodegenRegistry) cythonPyxInducedInfo(node VFS) ([]VFS, VFS) {
	if info, ok := r.byStr.get(STR(node.strID())); ok {
		return info.CythonInducedPyx, info.CythonMainOut
	}

	return nil, 0
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

// setProducerMainOut records the producing node's main output on an
// already-registered output.
func (r *CodegenRegistry) setProducerMainOut(path VFS, mainOut VFS) {
	info, ok := r.byStr.get(STR(path.strID()))

	if !ok {
		throwFmt("CodegenRegistry: setProducerMainOut on unregistered path %q", path.string())
	}

	info.ProducerMainOut = mainOut
}

// setProducerSourceClosure records the producer's full transitive $(S) input
// closure on an already-registered output. The slice is shared, not copied.
func (r *CodegenRegistry) setProducerSourceClosure(path VFS, closure []VFS) {
	if len(closure) == 0 {
		return
	}

	info, ok := r.byStr.get(STR(path.strID()))

	if !ok {
		throwFmt("CodegenRegistry: setProducerSourceClosure on unregistered path %q", path.string())
	}

	info.ProducerSourceClosure = closure
}

// setProtoImportRels records a build-generated `.proto` output's declared direct
// proto imports, read by the CPP_PROTO emission to seed the generated `.pb.h`.
func (r *CodegenRegistry) setProtoImportRels(path VFS, rels []string) {
	if len(rels) == 0 {
		return
	}

	info, ok := r.byStr.get(STR(path.strID()))

	if !ok {
		throwFmt("CodegenRegistry: setProtoImportRels on unregistered path %q", path.string())
	}

	info.ProtoImportRels = rels
}

// registerBoundGeneratedParsedOutput registers a generated output against its
// producer ref and records its parsed includes. generatorRefs are the codegen
// tools whose INDUCED_DEPS the scanner mixes into the closure (nil when none).
func registerBoundGeneratedParsedOutput(ctx *GenCtx, instance ModuleInstance, kind ProcKind, output VFS, parsed []IncludeDirective, ref NodeRef, generatorRefs []NodeRef) {
	registerBoundGeneratedParsedOutputWithSource(ctx, instance, kind, output, 0, parsed, ref, generatorRefs)
}

// registerBoundGeneratedParsedOutputWithSource is the CP variant recording the
// COPY source alongside the dst so the closure walker can rewrite the input edge
// to it. Pass sourcePath = 0 for non-CP producers.
func registerBoundGeneratedParsedOutputWithSource(ctx *GenCtx, instance ModuleInstance, kind ProcKind, output VFS, sourcePath VFS, parsed []IncludeDirective, ref NodeRef, generatorRefs []NodeRef) {
	codegenRegForInstance(ctx, instance).register(&GeneratedFileInfo{
		ProducerKvP:   kind,
		OutputPath:    output,
		SourcePath:    sourcePath,
		ProducerRef:   ref,
		GeneratorRefs: generatorRefs,
	})

	ctx.scannerFor(instance).parsers.registerBuildParsedIncludes(output, parsed)
}

func generatedOutputClosure(ctx *GenCtx, instance ModuleInstance, output VFS, in ModuleCCInputs) []VFS {
	return walkClosureTail(ctx.scannerFor(instance), output, in.ScanCfg)
}

func codegenRegForInstance(ctx *GenCtx, instance ModuleInstance) *CodegenRegistry {
	return ctx.scannerFor(instance).codegen
}
