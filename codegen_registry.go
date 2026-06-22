package main

type GeneratedFileInfo struct {
	ProducerKvP ProcKind

	OutputPath VFS

	// SourcePath is the canonical pre-generation file when the producer is a
	// pass-through (CP / COPY_FILE). Upstream reports this, not OutputPath, as
	// the input edge in transitive closures, so include walks rewrite OutputPath
	// to SourcePath on the way out. Zero means no remapping.
	SourcePath VFS

	// IsText marks a COPY_FILE(TEXT) registration. TEXT copies expand the source
	// verbatim, so the source is a real compiler input even when the COPY lives
	// in a different module than the CC node that includes the generated header.
	// Such a dst registers its source as a closure leaf so it rides across module
	// boundaries with the dst's window.
	IsText bool

	// ProducerRef is the NodeRef of the node that produces OutputPath. The
	// producer reserves its ref before registering, so a consumer resolving this
	// output to a dep edge always reads a valid ref.
	ProducerRef NodeRef

	// GeneratorRefs are the NodeRefs of the codegen TOOLS that produce this file,
	// as returned by ctx.tool(). The include scanner resolves each tool's
	// INDUCED_DEPS into this file's child set generically, so the producing
	// emitter need not hand-weave the tools' runtime headers into the registered
	// parsed includes.
	GeneratorRefs []NodeRef

	// SourceInputs are the producer node's $(S)-rooted leaf inputs, propagated
	// to consumers that compile this generated file. Upstream's flat input model
	// lists the full transitive source closure on every node, so a node compiling
	// a build-generated source carries the generator's grammar / template / tool /
	// script sources too. Zero len means nothing to propagate.
	SourceInputs []VFS

	// CythonInducedPyx is the producing cython node's resolved "pyx"-language
	// cimport/include/pxd closure, recorded on a generated _H / _API_H header
	// output. A CYTHON source that cdef-externs this header Uses the set as its
	// own cython source dependencies. Read explicitly when building a consuming
	// CY node's inputs — NOT spliced into the closure window — so it reaches the
	// cython transpile node but not the generated .c's C++ compile.
	CythonInducedPyx []VFS

	// ProducerSourceClosure is the producer node's full transitive $(S) input
	// closure (every source leaf reachable through the producer's command inputs),
	// not just its direct $(S) leaves. A bytecode node compiling this generated
	// source carries `${input:Src}` only; the flat-input model then folds the
	// producer's whole transitive SOURCE closure onto the bytecode node (the $(B)
	// intermediates stay behind the producer node edge). Unlike SourceInputs (the
	// direct-leaf subset, which must NOT over-carry the closure), this is the full
	// source set, read only by emitPySrcs for a generated PY_SRCS source. Zero len
	// means none.
	ProducerSourceClosure []VFS

	// ProtoImportRels are the declared direct proto imports of a build-generated
	// `.proto` output: its producer's OUTPUT_INCLUDES `.proto` entries (rel form).
	// The generated proto's source does not exist at configure time, so its `.pb.h`
	// cannot register direct imports from a parse; the consuming CPP_PROTO node
	// reads this list and registers each import's `.pb.h` as a direct include of
	// the generated `.pb.h`. The scanner's per-`.pb.h` transitive walk then
	// reconstructs the full import closure (incl. canonical descriptor remap) on
	// the `.pb.cc` compile. Zero len means not a generated proto / no imports.
	ProtoImportRels []string

	// CythonMainOut is the producing cython node's MAIN generated output (the .c /
	// .cpp), recorded on each of its _H / _API_H header outputs. The header is an
	// OutTogether sibling of this main; a generated cython compile whose include
	// closure reaches the header lists the main as an input. Read together with
	// CythonInducedPyx by cythonCompileInducedInputs. Zero means no main recorded.
	CythonMainOut VFS

	// ProducerMainOut is the producing node's MAIN output — the first declared
	// output. A node with several outputs links the additional ones to this main
	// via OutTogether; the build command lives on the main-output node, so a
	// consumer of any additional output depends on the main-output node and the
	// main output appears in the consumer's flat inputs even though the command
	// never names it. Recorded so a resource objcopy that embeds only additional
	// outputs still carries the spurious main-output input. Zero when the producer
	// is single-output or records no main.
	ProducerMainOut VFS

	// ClosureLeaves are extra VFS that ride in this output's include-closure
	// window as bare, non-expanded members — a "generated-from"/source input edge,
	// not a C++ include. COPY_FILE(TEXT) registers its $(S) source; a PB header
	// registers the $(S) .proto it was generated from. The scanner splices these
	// into the output's window at build time so they ride transitively to every
	// consumer that includes the output, without their own #includes being
	// re-resolved per consuming module.
	ClosureLeaves []VFS
}

type CodegenRegistry struct {
	// byStr maps an interned path STR (full $(B)/… or its rel form) to the producer
	// info. The hot Lookup (once per scanned include) was the top map in the CPU
	// profile; a DenseMap keyed by STR drops the hashing/probing. strID losslessly
	// encodes the root (the interned string carries the $(S)/ or $(B)/ prefix), so a
	// $(B) dst and a $(S) source never collide in the shared STR key space.
	// Closure leaves live on GeneratedFileInfo, not here.
	byStr DenseMap[STR, *GeneratedFileInfo]

	// splitPrefixSeen marks split-key PREFIX dirs (the Source-rooted VFS of
	// rel[:i]) that occur as a bySplit key prefix. LookupSplit checks it first: a
	// 1-bit-per-VFS probe short-circuits the uint64 bySplit hash-map lookup
	// whenever the prefix has no split entry — the common case on the hot resolve
	// path, where most addincl prefixes hold no codegen outputs. A bitset, not a
	// bool DenseMap column: only presence matters.
	splitPrefixSeen BitSet

	// leafEver marks every VFS ever registered as a ClosureLeaf. The scanner's
	// window-subsumption skip consults it: a leaf rides in closure windows as a
	// bare, non-expanded member, so its presence in a block does not imply its
	// window is present too — such a VFS must never short-circuit a splice. The
	// bit is set at registration, before ClosureLeaves can hand the leaf to any
	// window build, so it is always visible in time. Conservative: a VFS that is
	// both a leaf somewhere and a regular include elsewhere merely loses the skip.
	leafEver BitSet

	// bySplit maps a (Source-dir-VFS prefix, suffix STR) pair to its producer
	// info, keyed by splitMix64(prefix, suffix) so an identity-hashed IntMap
	// spreads the pairs. Gated by splitPrefixSeen so the probe runs only for
	// prefixes known to have an entry.
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

// lookupSTR probes byStr by an already-interned id — a full rooted path or the
// bare-rel form register also keys. Callers hold the id, so no string round-trip.
func (r *CodegenRegistry) lookupSTR(id STR) *GeneratedFileInfo {
	info, _ := r.byStr.get(id)

	return info
}

func (r *CodegenRegistry) lookupSplit(prefix VFS, suffix STR) *GeneratedFileInfo {
	// Gate the hash-map probe on the dense prefix flag: most addincl prefixes
	// hold no split entry, so the array probe short-circuits.
	if !r.splitPrefixSeen.has(uint32(prefix)) {
		return nil
	}

	if info := r.bySplit.get(splitKey(prefix, suffix)); info != nil {
		return *info
	}

	return nil
}

// AddClosureLeaf appends leaf to node's ClosureLeaves; node must already be
// registered. Cold path; the scanner reads the result on the hot path.
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

// setCythonPyxInduced records a generated header's cython-induced "pyx" closure
// and the producing node's main generated output. node must already be registered.
func (r *CodegenRegistry) setCythonPyxInduced(node VFS, pyx []VFS, mainOut VFS) {
	info, ok := r.byStr.get(STR(node.strID()))

	if !ok {
		throwFmt("CodegenRegistry: setCythonPyxInduced on unregistered path %q", node.string())
	}

	info.CythonInducedPyx = pyx
	info.CythonMainOut = mainOut
}

// cythonPyxInduced returns node's recorded cython-induced "pyx" closure (nil if
// node is not a registered generated header or carries none).
func (r *CodegenRegistry) cythonPyxInduced(node VFS) []VFS {
	if info, ok := r.byStr.get(STR(node.strID())); ok {
		return info.CythonInducedPyx
	}

	return nil
}

// cythonPyxInducedInfo returns node's recorded cython-induced "pyx" closure and
// the producing node's main generated output (nil/0 if node is not a registered
// generated header or carries none).
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
// already-registered output. Called once per output of a multi-output producer
// with that producer's first declared output.
func (r *CodegenRegistry) setProducerMainOut(path VFS, mainOut VFS) {
	info, ok := r.byStr.get(STR(path.strID()))

	if !ok {
		throwFmt("CodegenRegistry: setProducerMainOut on unregistered path %q", path.string())
	}

	info.ProducerMainOut = mainOut
}

// setProducerSourceClosure records the producer's full transitive $(S) input
// closure on an already-registered output. The slice is shared, not copied —
// the producer already holds it as its node inputs.
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
// proto imports. Read by the consuming CPP_PROTO emission to seed the generated
// `.pb.h`'s direct includes.
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
// producer ref and records the output's parsed includes for the scanner.
// generatorRefs are the codegen tools whose INDUCED_DEPS the scanner mixes into
// this output's closure (nil when the producer has no induced-deps tool).
func registerBoundGeneratedParsedOutput(ctx *GenCtx, instance ModuleInstance, kind ProcKind, output VFS, parsed []IncludeDirective, ref NodeRef, generatorRefs []NodeRef) {
	registerBoundGeneratedParsedOutputWithSource(ctx, instance, kind, output, 0, parsed, ref, generatorRefs)
}

// registerBoundGeneratedParsedOutputWithSource is the CP variant that records
// the COPY source alongside the dst so the closure walker can rewrite the
// emitted input edge to the source. Pass sourcePath = 0 for non-CP producers
// to fall back to OutputPath.
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
