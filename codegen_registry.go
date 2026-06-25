package main

type GeneratedFileInfo struct {
	ProducerKvP           ProcKind
	OutputPath            VFS
	SourcePath            VFS
	IsText                bool
	ProducerRef           NodeRef
	GeneratorRefs         []NodeRef
	SourceInputs          []VFS
	CythonInducedPyx      []VFS
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

func (r *CodegenRegistry) addClosureLeaf(node, leaf VFS) {
	info, ok := r.byStr.get(STR(node.strID()))

	if !ok {
		throwFmt("CodegenRegistry: AddClosureLeaf on unregistered path %q", node.string())
	}

	info.ClosureLeaves = append(info.ClosureLeaves, leaf)
	r.leafEver.add(uint32(leaf))
}

func (r *CodegenRegistry) isLeafEver(v VFS) bool {
	return r.leafEver.has(uint32(v))
}

func (r *CodegenRegistry) closureLeaves(node VFS) []VFS {
	if info, ok := r.byStr.get(STR(node.strID())); ok {
		return info.ClosureLeaves
	}

	return nil
}

func (r *CodegenRegistry) setCythonPyxInduced(node VFS, pyx []VFS, mainOut VFS) {
	info, ok := r.byStr.get(STR(node.strID()))

	if !ok {
		throwFmt("CodegenRegistry: setCythonPyxInduced on unregistered path %q", node.string())
	}

	info.CythonInducedPyx = pyx
	info.ProducerMainOut = mainOut
}

func (r *CodegenRegistry) cythonPyxInduced(node VFS) []VFS {
	if info, ok := r.byStr.get(STR(node.strID())); ok {
		return info.CythonInducedPyx
	}

	return nil
}

func (r *CodegenRegistry) cythonPyxInducedInfo(node VFS) ([]VFS, VFS) {
	if info, ok := r.byStr.get(STR(node.strID())); ok {
		return info.CythonInducedPyx, info.ProducerMainOut
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

func (r *CodegenRegistry) setProducerMainOut(path VFS, mainOut VFS) {
	info, ok := r.byStr.get(STR(path.strID()))

	if !ok {
		throwFmt("CodegenRegistry: setProducerMainOut on unregistered path %q", path.string())
	}

	info.ProducerMainOut = mainOut
}

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

func registerBoundGeneratedParsedOutput(ctx *GenCtx, instance ModuleInstance, kind ProcKind, output VFS, parsed []IncludeDirective, ref NodeRef, generatorRefs []NodeRef) {
	registerBoundGeneratedParsedOutputWithSource(ctx, instance, kind, output, 0, parsed, ref, generatorRefs)
}

func registerBoundGeneratedParsedOutputWithSource(ctx *GenCtx, instance ModuleInstance, kind ProcKind, output VFS, sourcePath VFS, parsed []IncludeDirective, ref NodeRef, generatorRefs []NodeRef) {
	codegenRegForInstance(ctx, instance).register(&GeneratedFileInfo{
		ProducerKvP:    kind,
		OutputPath:     output,
		SourcePath:     sourcePath,
		ProducerRef:    ref,
		GeneratorRefs:  generatorRefs,
		ParsedIncludes: parsed,
	})
}

func (r *CodegenRegistry) setBuildParsed(out VFS, parsed []IncludeDirective) {
	if !out.isBuild() {
		throwFmt("setBuildParsed: source-rooted output %q", out.string())
	}

	info, ok := r.byStr.get(STR(out.strID()))

	if !ok {
		throwFmt("setBuildParsed: no generated info for %q", out.string())
	}

	info.ParsedIncludes = parsed
}

func (r *CodegenRegistry) setCompileSpec(out VFS, spec *CompileSpec) {
	info, ok := r.byStr.get(STR(out.strID()))

	if !ok {
		throwFmt("setCompileSpec: no generated info for %q", out.string())
	}

	info.Compile = spec
}

func (r *CodegenRegistry) buildParsedFor(out VFS) []IncludeDirective {
	if info, ok := r.byStr.get(STR(out.strID())); ok {
		return info.ParsedIncludes
	}

	return nil
}

func generatedOutputClosure(ctx *GenCtx, instance ModuleInstance, output VFS, in ModuleCCInputs) []VFS {
	return walkClosureTail(ctx.scannerFor(instance), output, in.ScanCfg)
}

func codegenRegForInstance(ctx *GenCtx, instance ModuleInstance) *CodegenRegistry {
	return ctx.scannerFor(instance).codegen
}
