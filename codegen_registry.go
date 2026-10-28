package main

import (
	"slices"
	"strings"
)

type GeneratedFileInfo struct {
	GztProto           *GztProtoProducer
	ModuleIncludedDeps *ModuleIncludedDeps
	ModuleInputs       *ModuleInputInfo
	CollectionPeers    *CollectionPeerInfo
	Nots               *NotsPrepared
	NotsInputs         []VFS
	Sandbox            *SandboxRecipe
	BuildInputs        []VFS
	OnUse              *PendingEmit
	Scoped             *ScopedCommand
	GeneratorRefs      []NodeRef
	ClosureLeaves      []VFS
	InputLoopSources   []VFS
	LateOutputs        []VFS
	ModuleOutputs      []VFS
	ParsedIncludes     ParsedIncludeSet

	IncludedBuildInputs []VFS
	IncludedInputs      *IncludedInputPlan
	OutputPath          VFS
	SourcePath          VFS
	PrimaryInput        VFS
	OwnerModule         VFS
	OwnerSerial         uint32
	ProducerRef         NodeRef
	ProducerMainOut     VFS
	ModuleType          TOK
	InputsOnly          bool
	ResourceObjcopy     bool
}

type CollectionPeerInfo struct {
	Modules   []ModuleInstance
	PassPeers bool
}

type IncludedInputPlan struct {
	Roots []VFS
}

type ModuleIncludedDeps struct {
	Inputs []VFS
	Refs   []NodeRef
}

type ModuleInputInfo struct {
	IncludeRoots         []VFS
	Includes             []VFS
	Globals              []VFS
	GlobalInputs         []VFS
	CythonGlobalIncludes []VFS
	PassPeers            bool
}

type pendingEmitter interface {
	emitPending()
}

type PendingEmit struct {
	fn          func()
	emitter     pendingEmitter
	frame       *ModuleFrame
	publication *StreamingEmitter
}

func (p *PendingEmit) fire() {
	if p == nil || p.fn == nil && p.emitter == nil {
		return
	}

	fn, emitter, frame, publication := p.fn, p.emitter, p.frame, p.publication

	p.fn = nil
	p.emitter = nil
	p.frame = nil
	p.publication = nil

	if publication != nil {
		publication.deferPublication++

		defer func() { publication.deferPublication-- }()
	}

	if frame != nil {
		defer func() { frame.pending-- }()
	}

	if emitter != nil {
		emitter.emitPending()
	} else {
		fn()
	}
}

type CodegenRegistry struct {
	inducedIncludes map[VFS][]TypedInclude
	files           ScannedFileCache
	moduleBuildDeps DenseMap[NodeRef, []NodeRef]
	byRel           DenseMap[STR, *GeneratedFileInfo]
	bySplit         *IntMap[splitLookup]
	textTemplates   DenseMap[VFS, []VFS]
	splitGeneration uint32
	na              *NodeArenas
}

type splitLookup struct {
	slot       uint32
	generation uint32
}

func newCodegenRegistry(na *NodeArenas) *CodegenRegistry {
	return &CodegenRegistry{na: na}
}

func (r *CodegenRegistry) use(path VFS) *GeneratedFileInfo {
	info := r.lookup(path)

	if info != nil && info.OnUse != nil {
		fireGenerated(info)
	}

	return info
}

//go:noinline
func fireGenerated(info *GeneratedFileInfo) {
	pending := info.OnUse

	info.OnUse = nil
	pending.fire()
}

func (r *CodegenRegistry) register(info GeneratedFileInfo) *GeneratedFileInfo {
	if existing, ok := r.byRel.get(info.OutputPath.rel()); ok {
		if existing.InputsOnly {
			info.ClosureLeaves = r.na.dedupClosure(info.ClosureLeaves, [][]VFS{existing.ClosureLeaves})
			*existing = info
			r.splitGeneration++

			return existing
		}

		if existing.OwnerModule != info.OwnerModule || existing.OwnerSerial == info.OwnerSerial {
			throwFmt("CodegenRegistry: duplicate producer for %q (existing ref=%d, new ref=%d)",
				info.OutputPath.string(), existing.ProducerRef, info.ProducerRef)
		}

		discarded := r.na.geninfos.one()

		*discarded = info

		return discarded
	}

	stored := r.na.geninfos.one()

	*stored = info

	r.byRel.put(stored.OutputPath.rel(), stored)
	r.splitGeneration++

	return stored
}

func (r *CodegenRegistry) forwardTemplateInputs(output VFS, templates, textTemplates []VFS) {
	info, ok := r.byRel.get(output.rel())

	if !ok {
		info = r.na.geninfos.one()
		*info = GeneratedFileInfo{OutputPath: output, InputsOnly: true}
		r.byRel.put(output.rel(), info)
	}

	info.ClosureLeaves = r.na.dedupClosure(info.ClosureLeaves, [][]VFS{templates})
	r.forwardTextTemplates(output, textTemplates)
}

func (r *CodegenRegistry) forwardModuleIncludedDeps(output VFS, deps *ModuleIncludedDeps) {
	if deps == nil {
		return
	}

	info, ok := r.byRel.get(output.rel())

	if !ok {
		info = r.na.geninfos.one()
		*info = GeneratedFileInfo{OutputPath: output, InputsOnly: true}
		r.byRel.put(output.rel(), info)
	}

	info.ModuleIncludedDeps = deps
}

func (r *CodegenRegistry) moduleIncludedDeps(input VFS) *ModuleIncludedDeps {
	if !input.isBuild() {
		return nil
	}

	if info, ok := r.byRel.get(input.rel()); ok {
		return info.ModuleIncludedDeps
	}

	return nil
}

func (r *CodegenRegistry) forwardTextTemplates(output VFS, inputs []VFS) {
	if len(inputs) == 0 {
		return
	}

	previous, _ := r.textTemplates.get(output)

	r.textTemplates.put(output, r.na.dedupClosure(previous, [][]VFS{inputs}))
}

func (r *CodegenRegistry) shareInputLoop(members []VFS) {
	if len(members) < 2 {
		return
	}

	sources := vfsScratches.get()
	roots := vfsScratches.get()

	for _, member := range members {
		if info := r.lookup(member); info != nil {
			if info.SourcePath != 0 && info.SourcePath.isSource() {
				sources = append(sources, info.SourcePath)
			}

			sources = append(sources, info.InputLoopSources...)

			if info.IncludedInputs != nil {
				roots = append(roots, info.IncludedInputs.Roots...)
			}
		}
	}

	if len(sources)+len(roots) > 0 {
		shared := r.na.dedupClosure(sources)

		var included *IncludedInputPlan

		if len(roots) > 0 {
			included = &IncludedInputPlan{Roots: r.na.dedupClosure(roots)}
		}

		for _, member := range members {
			if info := r.lookup(member); info != nil {
				info.InputLoopSources = shared
				info.IncludedInputs = included
			}
		}
	}

	vfsScratches.put(sources)
	vfsScratches.put(roots)
}

func (r *CodegenRegistry) inheritParsedIncludes(output VFS, producer NodeRef, inherited ParsedIncludeSet) {
	info := r.lookup(output)

	if info == nil || info.ProducerRef != producer {
		return
	}

	for _, pair := range [...][2]ParsedIncludeBucket{
		{parsedIncludesAll, parsedIncludesLocal},
		{parsedIncludesAll, parsedIncludesAll},
		{parsedIncludesHeader, parsedIncludesHeader},
		{parsedIncludesCpp, parsedIncludesCpp},
	} {
		added := inherited[pair[0]]

		if len(added) == 0 {
			continue
		}

		previous := info.ParsedIncludes[pair[1]]
		merged := r.na.dirs.alloc(len(previous) + len(added))
		k := copy(merged, previous)

		k += copy(merged[k:], added)
		r.na.dirs.commit(k)
		info.ParsedIncludes[pair[1]] = merged[:k:k]
	}
}

func (r *CodegenRegistry) setMainOut(ref NodeRef, outputs []VFS) {
	for _, out := range outputs {
		if info := r.lookup(out); info != nil && info.ProducerRef == ref {
			info.ProducerMainOut = outputs[0]
		}
	}
}

func (r *CodegenRegistry) lookup(path VFS) *GeneratedFileInfo {
	if !path.isBuild() {
		return nil
	}

	info, _ := r.byRel.get(path.rel())

	if info != nil && info.InputsOnly {
		return nil
	}

	return info
}

func (r *CodegenRegistry) moduleType(path VFS) TOK {
	info, _ := r.byRel.get(path.rel())

	return info.ModuleType
}

func (r *CodegenRegistry) moduleInputs(path VFS) *ModuleInputInfo {
	info, _ := r.byRel.get(path.rel())

	if info == nil {
		return nil
	}

	return info.ModuleInputs
}

func (r *CodegenRegistry) setModuleInputs(output VFS, moduleType TOK, includes, roots, globals, globalInputs, cythonIncludes []VFS, passPeers bool) {
	info, ok := r.byRel.get(output.rel())

	if !ok {
		info = r.na.geninfos.one()
		*info = GeneratedFileInfo{OutputPath: output, InputsOnly: true, ModuleType: moduleType}
		r.byRel.put(output.rel(), info)
	}

	if info.ModuleInputs == nil {
		info.ModuleInputs = r.na.moduleInputs.one()
	}

	*info.ModuleInputs = ModuleInputInfo{Includes: includes, IncludeRoots: roots, Globals: globals, GlobalInputs: globalInputs, CythonGlobalIncludes: cythonIncludes, PassPeers: passPeers}
}

func (r *CodegenRegistry) lookupSTR(id STR) *GeneratedFileInfo {
	info, _ := r.byRel.get(id)

	if info != nil && info.InputsOnly {
		return nil
	}

	return info
}

func (r *CodegenRegistry) lookupSplit(prefix VFS, suffixID STR) *GeneratedFileInfo {
	key := splitMix64(uint32(prefix.rel()), uint32(suffixID))

	if r.bySplit != nil {
		if cached := r.bySplit.get(key); cached != nil {
			if cached.slot != 0 {
				return *unsafeAt(r.byRel.vals.s, uint64(cached.slot))
			}

			if cached.generation == r.splitGeneration {
				return nil
			}
		}
	}

	prefixRel := prefix.relString()
	rel := suffixID

	if prefixRel != "" {
		rel = internedV(prefixRel, "/", suffixID.string())
	}

	var info *GeneratedFileInfo

	if rel != 0 {
		info, _ = r.byRel.get(rel)

		if info != nil && info.InputsOnly {
			info = nil
		}
	}

	if info == nil && suffixID.string() != "" && !pathIsClean(suffixID.string()) {
		joined := suffixID.string()

		if prefixRel != "" {
			joined = prefixRel + "/" + joined
		}

		if normalized := interned(cleanRel(joined)); normalized != 0 {
			info = r.lookupSTR(normalized)
		}
	}

	if r.bySplit == nil {
		r.bySplit = newIntMap[splitLookup](1 << 15)
	}

	var slot uint32

	if info != nil {
		slot = r.byRel.idx.s[info.OutputPath.rel()]
	}

	r.bySplit.put(key, splitLookup{slot: slot, generation: r.splitGeneration})

	return info
}

func (r *CodegenRegistry) mustInfo(path VFS) *GeneratedFileInfo {
	info := r.lookup(path)

	assert(info != nil, "CodegenRegistry: unregistered path")

	return info
}

func (r *CodegenRegistry) generatorRefsFor(closure Closure) []NodeRef {
	refs := nodeRefScratches.get()

	closure.each(func(path VFS) {
		if info := r.lookup(path); info != nil {
			refs = append(refs, info.GeneratorRefs...)
		}
	})

	var result []NodeRef

	if len(refs) > 0 {
		result = r.na.uniqueRefs(refs)
	}

	nodeRefScratches.put(refs)

	return result
}

func (r *CodegenRegistry) buildParsedFor(out VFS) ParsedIncludeSet {
	if info := r.use(out); info != nil {
		return info.ParsedIncludes
	}

	return ParsedIncludeSet{}
}

func (r *CodegenRegistry) moduleSourceInputs(owner VFS, suffix string) []VFS {
	var inputs []VFS

	for _, info := range r.byRel.vals.s {
		if info == nil || info.OwnerModule != owner || info.PrimaryInput == 0 {
			continue
		}

		if strings.HasSuffix(info.PrimaryInput.relString(), suffix) && !slices.Contains(inputs, info.PrimaryInput) {
			inputs = append(inputs, info.PrimaryInput)
		}
	}

	return inputs
}

func (info *GeneratedFileInfo) providesSourceInclude(source VFS, d IncludeDirective) bool {
	return info != nil && info.SourcePath == source && info.providesOutputInclude(d)
}

func (info *GeneratedFileInfo) providesOutputInclude(d IncludeDirective) bool {
	if d.kind != includeQuoted || d.target.vfs() != 0 {
		return false
	}

	rel := normalisePath(pathDir(info.OutputPath.relString()) + "/" + d.target.string())

	for _, entry := range info.ParsedIncludes.bucket(parsedIncludesLocal) {
		if v := entry.target.vfs(); v.isBuild() && v.relString() == rel {
			return true
		}
	}

	return false
}
