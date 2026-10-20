package main

import (
	"slices"
	"strings"
)

type GeneratedFileInfo struct {
	Nots            *NotsPrepared
	OutputPath      VFS
	SourcePath      VFS
	PrimaryInput    VFS
	OwnerModule     VFS
	OwnerSerial     uint32
	ProducerRef     NodeRef
	OnUse           *PendingEmit
	GeneratorRefs   []NodeRef
	SourceInputs    []VFS
	ProducerMainOut VFS
	ClosureLeaves   []VFS
	ParsedIncludes  ParsedIncludeSet
}

type pendingEmitter interface {
	emitPending()
}

type PendingEmit struct {
	fn      func()
	emitter pendingEmitter
	frame   *ModuleFrame
}

func (p *PendingEmit) fire() {
	if p == nil || p.fn == nil && p.emitter == nil {
		return
	}

	fn, emitter, frame := p.fn, p.emitter, p.frame

	p.fn = nil
	p.emitter = nil
	p.frame = nil

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
	byRel           DenseMap[STR, *GeneratedFileInfo]
	bySplit         *IntMap[splitLookup]
	leafEver        BitSet
	splitGeneration uint32
	na              *NodeArenas
	addInputs       bool
}

type splitLookup struct {
	info       *GeneratedFileInfo
	generation uint32
}

func newCodegenRegistry(na *NodeArenas, addInputs bool) *CodegenRegistry {
	return &CodegenRegistry{na: na, addInputs: addInputs}
}

func (r *CodegenRegistry) use(path VFS) *GeneratedFileInfo {
	info := r.lookup(path)

	if info != nil && info.OnUse != nil {
		fireGenerated(info)
	}

	return info
}

func (r *CodegenRegistry) useSplit(prefix VFS, suffix ANY) *GeneratedFileInfo {
	info := r.lookupSplit(prefix, suffix)

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
	if !info.OutputPath.isBuild() {
		throwFmt("CodegenRegistry: register of a source path %q", info.OutputPath.string())
	}

	if existing, ok := r.byRel.get(info.OutputPath.rel()); ok {
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

	if r.addInputs {
		return stored
	}

	for _, leaf := range stored.ClosureLeaves {
		r.leafEver.add(uint32(leaf))
	}

	return stored
}

func (r *CodegenRegistry) lookup(path VFS) *GeneratedFileInfo {
	if !path.isBuild() {
		return nil
	}

	info, _ := r.byRel.get(path.rel())

	return info
}

func (r *CodegenRegistry) lookupSTR(id STR) *GeneratedFileInfo {
	info, _ := r.byRel.get(id)

	return info
}

func (r *CodegenRegistry) lookupSplit(prefix VFS, suffix ANY) *GeneratedFileInfo {
	suffixID := suffix.str()

	if suffixID == 0 {
		return nil
	}

	key := splitMix64(uint32(prefix.rel()), uint32(suffixID))

	if r.bySplit != nil {
		if cached := r.bySplit.get(key); cached != nil {
			if cached.info != nil || cached.generation == r.splitGeneration {
				return cached.info
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

	if r.bySplit == nil {
		r.bySplit = newIntMap[splitLookup](1 << 15)
	}

	r.bySplit.put(key, splitLookup{info: info, generation: r.splitGeneration})

	return info
}

func (r *CodegenRegistry) mustInfo(path VFS) *GeneratedFileInfo {
	info := r.lookup(path)

	assert(info != nil, "CodegenRegistry: unregistered path")

	return info
}

func (r *CodegenRegistry) addClosureLeafNoSubsume(node, leaf VFS) {
	info := r.mustInfo(node)

	info.ClosureLeaves = arenaAppend(r.na.vfs, info.ClosureLeaves, leaf)
}

func (r *CodegenRegistry) isLeafEver(v VFS) bool {
	return r.leafEver.has(uint32(v))
}

func (r *CodegenRegistry) closureLeaves(node VFS) []VFS {
	if r.addInputs {
		return nil
	}

	if info := r.lookup(node); info != nil {
		return info.ClosureLeaves
	}

	return nil
}

func (r *CodegenRegistry) addSourceInputs(na *NodeArenas, path VFS, extra []VFS) {
	info := r.mustInfo(path)

	info.SourceInputs = na.dedupClosure(info.SourceInputs, [][]VFS{extra})
}

func (r *CodegenRegistry) buildParsedFor(out VFS) ParsedIncludeSet {
	if info := r.lookup(out); info != nil {
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
