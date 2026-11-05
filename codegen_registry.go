package main

import (
	"slices"
	"strings"

	"github.com/zeebo/xxh3"
)

const splitBucketBits = 18

type GeneratedFileInfo struct {
	attributes      *GeneratedFileAttributes
	rare            *GeneratedFileRare
	id              uint32
	GeneratorRefs   []NodeRef
	OutputPath      VFS
	SourcePath      VFS
	PrimaryInput    VFS
	OwnerModule     VFS
	OwnerSerial     uint32
	ProducerRef     NodeRef
	ProducerMainOut VFS
	ModuleType      TOK
	InputsOnly      bool
	ResourceObjcopy bool
}

type GeneratedFileRare struct {
	BuildInputs         []VFS
	OnUse               *PendingEmit
	ClosureLeaves       []VFS
	InputLoopSources    []VFS
	LateOutputs         []VFS
	ModuleOutputs       []VFS
	IncludedBuildInputs []VFS
	IncludedInputs      *IncludedInputPlan
}

func (info *GeneratedFileInfo) rareFields() *GeneratedFileRare {
	if info.rare == nil {
		info.rare = &GeneratedFileRare{}
	}

	return info.rare
}

func (info *GeneratedFileInfo) buildInputs() []VFS {
	if info.rare != nil {
		return info.rare.BuildInputs
	}

	return nil
}

func (info *GeneratedFileInfo) onUse() *PendingEmit {
	if info.rare != nil {
		return info.rare.OnUse
	}

	return nil
}

func (info *GeneratedFileInfo) closureLeaves() []VFS {
	if info.rare != nil {
		return info.rare.ClosureLeaves
	}

	return nil
}

func (info *GeneratedFileInfo) inputLoopSources() []VFS {
	if info.rare != nil {
		return info.rare.InputLoopSources
	}

	return nil
}

func (info *GeneratedFileInfo) lateOutputs() []VFS {
	if info.rare != nil {
		return info.rare.LateOutputs
	}

	return nil
}

func (info *GeneratedFileInfo) moduleOutputs() []VFS {
	if info.rare != nil {
		return info.rare.ModuleOutputs
	}

	return nil
}

func (info *GeneratedFileInfo) includedBuildInputs() []VFS {
	if info.rare != nil {
		return info.rare.IncludedBuildInputs
	}

	return nil
}

func (info *GeneratedFileInfo) includedInputs() *IncludedInputPlan {
	if info.rare != nil {
		return info.rare.IncludedInputs
	}

	return nil
}

type GeneratedFileSpec struct {
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
	FbsFlatc           *FbsFlatcRecipe
	FbsNative          *FbsNativeRecipe
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
	settle func(*ModuleIncludedDeps)
}

func (d *ModuleIncludedDeps) settled() *ModuleIncludedDeps {
	if settle := d.settle; settle != nil {
		d.settle = nil
		settle(d)
	}

	return d
}

type ModuleInputInfo struct {
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

func (p *PendingEmit) pendingEmitter() pendingEmitter {
	if p == nil {
		return nil
	}

	return p.emitter
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
	attributes      GeneratedFileAttributes
	infoCount       uint32
	inducedIncludes map[VFS][]TypedInclude
	files           ScannedFileCache
	moduleBuildDeps DenseMap[NodeRef, []NodeRef]
	byRel           DenseMap[STR, *GeneratedFileInfo]
	byOwner         DenseMap[VFS, []uint32]
	bySplit         *IntMap[splitLookup]
	textTemplates   DenseMap[VFS, []VFS]
	splitGeneration uint32
	splitGens       []uint32
	na              *NodeArenas

	moduleOutputDeps func(*GeneratedFileInfo) []IncludeDirective
}

type splitLookup struct {
	slot       uint32
	generation uint32
}

func newCodegenRegistry(na *NodeArenas) *CodegenRegistry {
	return &CodegenRegistry{na: na, splitGens: make([]uint32, 1<<splitBucketBits)}
}

func splitBucketOf(rel string) uint32 {
	return uint32(xxh3.HashString(rel[strings.LastIndexByte(rel, '/')+1:])) & (1<<splitBucketBits - 1)
}

func (r *CodegenRegistry) putRel(info *GeneratedFileInfo) {
	r.byRel.put(info.OutputPath.rel(), info)
	r.splitChanged(info.OutputPath)
}

func (r *CodegenRegistry) splitChanged(output VFS) {
	r.splitGeneration++
	r.splitGens[splitBucketOf(output.relString())]++
}

func (r *CodegenRegistry) use(path VFS) *GeneratedFileInfo {
	info := r.lookup(path)

	if info != nil && info.onUse() != nil {
		fireGenerated(info)
	}

	return info
}

//go:noinline
func fireGenerated(info *GeneratedFileInfo) {
	pending := info.rare.OnUse

	info.rare.OnUse = nil
	pending.fire()
}

func (r *CodegenRegistry) register(info GeneratedFileSpec) *GeneratedFileInfo {
	if existing, ok := r.byRel.get(info.OutputPath.rel()); ok {
		if existing.InputsOnly {
			info.ClosureLeaves = r.na.dedupClosure(info.ClosureLeaves, [][]VFS{existing.closureLeaves()})
			r.store(existing, info)
			r.noteOwner(existing)
			r.splitChanged(existing.OutputPath)

			return existing
		}

		if existing.OwnerModule != info.OwnerModule || existing.OwnerSerial == info.OwnerSerial {
			throwFmt("CodegenRegistry: duplicate producer for %q (existing ref=%d, new ref=%d)",
				info.OutputPath.string(), existing.ProducerRef, info.ProducerRef)
		}

		discarded := r.na.geninfos.one()

		r.store(discarded, info)

		return discarded
	}

	stored := r.na.geninfos.one()

	r.store(stored, info)

	r.putRel(stored)
	r.noteOwner(stored)

	return stored
}

func (r *CodegenRegistry) noteOwner(info *GeneratedFileInfo) {
	if info.OwnerModule == 0 {
		return
	}

	slot := r.byRel.idx.get(uint32(info.OutputPath.rel()))
	slots, _ := r.byOwner.get(info.OwnerModule)

	if n := len(slots); n > 0 && slots[n-1] == slot {
		return
	}

	r.byOwner.put(info.OwnerModule, append(slots, slot))
}

func (r *CodegenRegistry) forwardTemplateInputs(output VFS, templates, textTemplates []VFS) {
	info, ok := r.byRel.get(output.rel())

	if !ok {
		info = r.na.geninfos.one()
		r.store(info, GeneratedFileSpec{OutputPath: output, InputsOnly: true})
		r.putRel(info)
	}

	info.rareFields().ClosureLeaves = r.na.dedupClosure(info.closureLeaves(), [][]VFS{templates})
	r.forwardTextTemplates(output, textTemplates)
}

func (r *CodegenRegistry) forwardModuleIncludedDeps(output VFS, deps *ModuleIncludedDeps) {
	if deps == nil {
		return
	}

	info, ok := r.byRel.get(output.rel())

	if !ok {
		info = r.na.geninfos.one()
		r.store(info, GeneratedFileSpec{OutputPath: output, InputsOnly: true})
		r.putRel(info)
	}

	info.attributes.moduleIncludedDeps.put(info.id, deps)
}

func (r *CodegenRegistry) moduleIncludedDeps(input VFS) *ModuleIncludedDeps {
	if !input.isBuild() {
		return nil
	}

	if info, ok := r.byRel.get(input.rel()); ok {
		if deps := info.moduleIncludedDeps(); deps != nil {
			return deps.settled()
		}
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

			sources = append(sources, info.inputLoopSources()...)

			if included := info.includedInputs(); included != nil {
				roots = append(roots, included.Roots...)
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
				rare := info.rareFields()

				rare.InputLoopSources = shared
				rare.IncludedInputs = included
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

	parsed := info.parsedIncludes()

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

		previous := parsed[pair[1]]
		merged := r.na.dirs.alloc(len(previous) + len(added))
		k := copy(merged, previous)

		k += copy(merged[k:], added)
		r.na.dirs.commit(k)
		parsed[pair[1]] = merged[:k:k]
	}

	info.attributes.parsedIncludes.put(info.id, parsed)
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

	return info.moduleInputs()
}

func (r *CodegenRegistry) setModuleInputs(output VFS, moduleType TOK, includes, globals, globalInputs, cythonIncludes []VFS, passPeers bool) {
	info, ok := r.byRel.get(output.rel())

	if !ok {
		info = r.na.geninfos.one()
		r.store(info, GeneratedFileSpec{OutputPath: output, InputsOnly: true, ModuleType: moduleType})
		r.putRel(info)
	}

	inputs := info.moduleInputs()

	if inputs == nil {
		inputs = r.na.moduleInputs.one()
		info.attributes.moduleInputs.put(info.id, inputs)
	}

	*inputs = ModuleInputInfo{Includes: includes, Globals: globals, GlobalInputs: globalInputs, CythonGlobalIncludes: cythonIncludes, PassPeers: passPeers}
}

func (r *CodegenRegistry) lookupSTR(id STR) *GeneratedFileInfo {
	info, _ := r.byRel.get(id)

	if info != nil && info.InputsOnly {
		return nil
	}

	return info
}

func (r *CodegenRegistry) lookupSplit(prefix VFS, suffixID STR) *GeneratedFileInfo {
	info := r.lookupSplitAny(prefix, suffixID, splitBucketOf(suffixID.string()))

	if info != nil && info.InputsOnly {
		return nil
	}

	return info
}

func (info *GeneratedFileInfo) includable() bool {
	return !info.InputsOnly || info.ModuleType != 0
}

func (r *CodegenRegistry) lookupIncludeSTR(id STR) *GeneratedFileInfo {
	info, _ := r.byRel.get(id)

	if info != nil && !info.includable() {
		return nil
	}

	return info
}

func (r *CodegenRegistry) lookupIncludeSplit(prefix VFS, suffixID STR, bucket uint32) *GeneratedFileInfo {
	info := r.lookupSplitAny(prefix, suffixID, bucket)

	if info != nil && !info.includable() {
		return nil
	}

	return info
}

func (r *CodegenRegistry) moduleOutputIncludes(path VFS) ([]IncludeDirective, bool) {
	info, _ := r.byRel.get(path.rel())

	if info == nil || !info.InputsOnly || info.ModuleType == 0 || r.moduleOutputDeps == nil {
		return nil, false
	}

	set := info.parsedIncludes()

	if set[parsedIncludesLocal] == nil {
		set[parsedIncludesLocal] = r.moduleOutputDeps(info)
		info.attributes.parsedIncludes.put(info.id, set)
	}

	return set[parsedIncludesLocal], true
}

func (r *CodegenRegistry) lookupSplitAny(prefix VFS, suffixID STR, bucket uint32) *GeneratedFileInfo {
	generation := r.splitGeneration

	if suffixID.cleanPath() {
		if generation = r.splitGens[bucket]; generation == 0 {
			return nil
		}
	}

	key := splitMix64(uint32(prefix.rel()), uint32(suffixID))

	if r.bySplit != nil {
		if cached := r.bySplit.get(key); cached != nil {
			if cached.slot != 0 {
				return *unsafeAt(r.byRel.vals.s, uint64(cached.slot))
			}

			if cached.generation == generation {
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
	}

	if (info == nil || info.InputsOnly) && suffixID != strEmpty && !suffixID.cleanPath() {
		joined := suffixID.string()

		if prefixRel != "" {
			joined = prefixRel + "/" + joined
		}

		if normalized := interned(cleanRel(joined)); normalized != 0 {
			if alt, _ := r.byRel.get(normalized); alt != nil && (info == nil || !alt.InputsOnly) {
				info = alt
			}
		}
	}

	if r.bySplit == nil {
		r.bySplit = newIntMap[splitLookup](1 << 15)
	}

	var slot uint32

	if info != nil {
		slot = r.byRel.idx.get(uint32(info.OutputPath.rel()))
	}

	r.bySplit.put(key, splitLookup{slot: slot, generation: generation})

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
		return info.parsedIncludes()
	}

	return ParsedIncludeSet{}
}

func (r *CodegenRegistry) moduleSourceInputs(owner VFS, suffix string) []VFS {
	var inputs []VFS

	slots, _ := r.byOwner.get(owner)

	if !slices.IsSorted(slots) {
		slices.Sort(slots)
		slots = slices.Compact(slots)
		r.byOwner.put(owner, slots)
	}

	for _, slot := range slots {
		info := r.byRel.vals.s[slot]

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

	for _, entry := range info.parsedIncludes().bucket(parsedIncludesLocal) {
		if v := entry.target.vfs(); v.isBuild() && v.relString() == rel {
			return true
		}
	}

	return false
}
