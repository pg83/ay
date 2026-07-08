package main

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
	arena           *BumpAllocator[GeneratedFileInfo]
	dirs            *BumpAllocator[IncludeDirective]
	refs            *BumpAllocator[NodeRef]
	leaves          *BumpAllocator[VFS]
}

func splitKey(prefix VFS, suffix ANY) uint64 {
	return splitMix64(uint32(prefix), uint32(suffix))
}

func newCodegenRegistry() *CodegenRegistry {
	return &CodegenRegistry{
		bySplit: newIntMap[*GeneratedFileInfo](1 << 14),
		arena:   newBumpAllocator[GeneratedFileInfo](1 << 10),
		dirs:    newBumpAllocator[IncludeDirective](1 << 10),
		refs:    newBumpAllocator[NodeRef](1 << 10),
		leaves:  newBumpAllocator[VFS](1 << 8),
	}
}

func (r *CodegenRegistry) register(info *GeneratedFileInfo) {
	if !info.OutputPath.isBuild() {
		throwFmt("CodegenRegistry: register of a source path %q", info.OutputPath.string())
	}

	if existing, ok := r.byRel.get(info.OutputPath.rel()); ok {
		throwFmt("CodegenRegistry: duplicate producer for %q (existing ref=%d, new ref=%d)",
			info.OutputPath.string(), existing.ProducerRef, info.ProducerRef)
	}

	rel := info.OutputPath.relString()
	stored := r.arena.one()

	*stored = *info
	info = stored

	if len(info.GeneratorRefs) > 0 {
		info.GeneratorRefs = r.refs.list(info.GeneratorRefs...)
	}

	if len(info.ClosureLeaves) > 0 {
		info.ClosureLeaves = r.leaves.list(info.ClosureLeaves...)
	}

	for b := range info.ParsedIncludes {
		if len(info.ParsedIncludes[b]) > 0 {
			info.ParsedIncludes[b] = r.dirs.list(info.ParsedIncludes[b]...)
		}
	}

	r.byRel.put(info.OutputPath.rel(), info)

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

	info.ClosureLeaves = append(info.ClosureLeaves, leaf)
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
