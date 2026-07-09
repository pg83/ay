package main

type PendingEmit struct {
	owner uint64
	prep  func()
	fn    func()
}

func (p *PendingEmit) run() {
	if p == nil || p.fn == nil {
		return
	}

	fn := p.fn

	p.fn = nil

	if p.prep != nil {
		prep := p.prep

		p.prep = nil

		prep()
	}

	fn()
}

func runPendingPrep(info *GeneratedFileInfo) {
	p := info.pending

	if p == nil || p.prep == nil {
		return
	}

	prep := p.prep

	p.prep = nil

	prep()
}

func runPending(info *GeneratedFileInfo) {
	p := info.pending

	if p == nil {
		return
	}

	runPendingPrep(info)

	if p.fn == nil {
		return
	}

	fn := p.fn

	p.fn = nil

	fn()
}

func runPendingFor(info *GeneratedFileInfo, consumerKey uint64) {
	p := info.pending

	if p == nil || p.owner == consumerKey {
		return
	}

	runPending(info)
}

type GeneratedFileInfo struct {
	OutputPath      VFS
	SourcePath      VFS
	IsText          bool
	ProducerRef     NodeRef
	GeneratorRefs   []NodeRef
	SourceInputs    []VFS
	ProducerMainOut VFS
	ClosureLeaves   []VFS
	ParsedIncludes  ParsedIncludeSet
	Compile         *CompileSpec
	pending         *PendingEmit
}

type CompileSpec struct {
	CFlags           []ANY
	FlatOutput       bool
	Variant          *string
	ObjectSuffixStem *string
	Py3Suffix        bool
	ForceCxx         bool
	EnvAddIncl       []VFS
	EnvCFlags        []ANY
	blocksMemo       *CcModuleArgBlocks
}

type CodegenRegistry struct {
	byRel           DenseMap[STR, *GeneratedFileInfo]
	splitPrefixSeen BitSet
	leafEver        BitSet
	bySplit         *IntMap[*GeneratedFileInfo]
	na              *NodeArenas
}

func splitKey(prefix VFS, suffix ANY) uint64 {
	return splitMix64(uint32(prefix), uint32(suffix))
}

func newCodegenRegistry(na *NodeArenas) *CodegenRegistry {
	return &CodegenRegistry{bySplit: newIntMap[*GeneratedFileInfo](1 << 14), na: na}
}

func (r *CodegenRegistry) use(path VFS) *GeneratedFileInfo {
	info := r.lookup(path)

	if info != nil {
		runPending(info)
	}

	return info
}

func (r *CodegenRegistry) register(info GeneratedFileInfo) *GeneratedFileInfo {
	if !info.OutputPath.isBuild() {
		throwFmt("CodegenRegistry: register of a source path %q", info.OutputPath.string())
	}

	if existing, ok := r.byRel.get(info.OutputPath.rel()); ok {
		throwFmt("CodegenRegistry: duplicate producer for %q (existing ref=%d, new ref=%d)",
			info.OutputPath.string(), existing.ProducerRef, info.ProducerRef)
	}

	rel := info.OutputPath.relString()
	stored := r.na.geninfos.one()

	*stored = info

	r.byRel.put(stored.OutputPath.rel(), stored)

	for _, leaf := range stored.ClosureLeaves {
		r.leafEver.add(uint32(leaf))
	}

	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' {
			r.putSplit(source(rel[:i]), internStr(rel[i+1:]), stored)
		}
	}

	return stored
}

func (r *CodegenRegistry) putSplit(prefix VFS, suffix STR, info *GeneratedFileInfo) {
	r.bySplit.put(splitKey(prefix, suffix.any()), info)
	r.splitPrefixSeen.add(uint32(prefix))
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
	if !r.splitPrefixSeen.has(uint32(prefix)) {
		return nil
	}

	if info := r.bySplit.get(splitKey(prefix, suffix)); info != nil {
		return *info
	}

	return nil
}

func (r *CodegenRegistry) mustInfo(path VFS, op string) *GeneratedFileInfo {
	if info := r.lookup(path); info != nil {
		return info
	}

	throwFmt("CodegenRegistry: %s on unregistered path %q", op, path.string())

	return nil
}

func (r *CodegenRegistry) addClosureLeafNoSubsume(node, leaf VFS) {
	info := r.mustInfo(node, "addClosureLeafNoSubsume")

	info.ClosureLeaves = arenaAppend(r.na.vfs, info.ClosureLeaves, leaf)
}

func (r *CodegenRegistry) isLeafEver(v VFS) bool {
	return r.leafEver.has(uint32(v))
}

func (r *CodegenRegistry) closureLeaves(node VFS) []VFS {
	if info := r.lookup(node); info != nil {
		return info.ClosureLeaves
	}

	return nil
}

func (r *CodegenRegistry) addSourceInputs(na *NodeArenas, path VFS, extra []VFS) {
	if len(extra) == 0 {
		return
	}

	info := r.mustInfo(path, "addSourceInputs")

	info.SourceInputs = dedupClosure(na, info.SourceInputs, [][]VFS{extra})
}

func (r *CodegenRegistry) buildParsedFor(out VFS) ParsedIncludeSet {
	if info := r.lookup(out); info != nil {
		return info.ParsedIncludes
	}

	return ParsedIncludeSet{}
}

func (ctx *GenCtx) codegenFor(instance ModuleInstance) *CodegenRegistry {
	return ctx.scannerFor(instance).codegen
}
