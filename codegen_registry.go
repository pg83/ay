package main

import (
	"slices"
	"strings"
)

type GeneratedFileInfo struct {
	InputsOnly      bool
	ModuleType      TOK
	Nots            *NotsPrepared
	OutputPath      VFS
	SourcePath      VFS
	PrimaryInput    VFS
	OwnerModule     VFS
	OwnerSerial     uint32
	ProducerRef     NodeRef
	OnUse           *PendingEmit
	GeneratorRefs   []NodeRef
	ProducerMainOut VFS
	ClosureLeaves   []VFS
	LateOutputs     []VFS
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
	splitGeneration uint32
	na              *NodeArenas
}

type splitLookup struct {
	info       *GeneratedFileInfo
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

func (r *CodegenRegistry) forwardTemplateInputs(output VFS, templates []VFS) {
	info, ok := r.byRel.get(output.rel())

	if !ok {
		info = r.na.geninfos.one()
		*info = GeneratedFileInfo{OutputPath: output, InputsOnly: true}
		r.byRel.put(output.rel(), info)
	}

	info.ClosureLeaves = r.na.dedupClosure(info.ClosureLeaves, [][]VFS{templates})
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
	if !path.isBuild() {
		return 0
	}

	info, _ := r.byRel.get(path.rel())

	if info == nil {
		return 0
	}

	return info.ModuleType
}

func (r *CodegenRegistry) lookupSTR(id STR) *GeneratedFileInfo {
	info, _ := r.byRel.get(id)

	if info != nil && info.InputsOnly {
		return nil
	}

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

		if info != nil && info.InputsOnly {
			info = nil
		}
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
