package main

type GeneratedFileInfo struct {
	ProducerKvP           ProcKind
	OutputPath            VFS
	SourcePath            VFS
	IsText                bool
	ProducerRef           NodeRef
	GeneratorRefs         []NodeRef
	SourceInputs          []VFS
	ProducerSourceClosure []VFS
	ProtoImportRels       []string
	ProducerMainOut       VFS
	ClosureLeaves         []VFS
	ParsedIncludes        []IncludeDirective
	Compile               *CompileSpec
}

type CompileSpec struct {
	CFlags           []ARG
	FlatOutput       bool
	Variant          *string
	ObjectSuffixStem *string
	Py3Suffix        bool
	ForceCxx         bool
}

type CodegenRegistry struct {
	byStr           DenseMap[STR, *GeneratedFileInfo]
	splitPrefixSeen BitSet
	leafEver        BitSet
	bySplit         *IntMap[*GeneratedFileInfo]
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
	r.splitPrefixSeen.add(uint32(prefix))
}

func (r *CodegenRegistry) lookup(path VFS) *GeneratedFileInfo {
	info, _ := r.byStr.get(STR(path.strID()))

	return info
}

func (r *CodegenRegistry) lookupSTR(id STR) *GeneratedFileInfo {
	info, _ := r.byStr.get(id)

	return info
}

func (r *CodegenRegistry) lookupSplit(prefix VFS, suffix STR) *GeneratedFileInfo {
	if !r.splitPrefixSeen.has(uint32(prefix)) {
		return nil
	}

	if info := r.bySplit.get(splitKey(prefix, suffix)); info != nil {
		return *info
	}

	return nil
}

func (r *CodegenRegistry) mustInfo(path VFS, op string) *GeneratedFileInfo {
	if info, ok := r.byStr.get(STR(path.strID())); ok {
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

func (r *CodegenRegistry) cythonMainOut(node VFS) VFS {
	if info := r.lookup(node); info != nil && info.ProducerKvP == pkCY {
		return info.ProducerMainOut
	}

	return 0
}

func (r *CodegenRegistry) setProducerSourceClosure(path VFS, closure []VFS) {
	if len(closure) == 0 {
		return
	}

	r.mustInfo(path, "setProducerSourceClosure").ProducerSourceClosure = closure
}

func (r *CodegenRegistry) buildParsedFor(out VFS) []IncludeDirective {
	if info := r.lookup(out); info != nil {
		return info.ParsedIncludes
	}

	return nil
}

func (ctx *GenCtx) codegenFor(instance ModuleInstance) *CodegenRegistry {
	return ctx.scannerFor(instance).codegen
}
