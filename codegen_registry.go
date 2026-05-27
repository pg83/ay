package main

type GeneratedFileInfo struct {
	ProducerKvP string

	OutputPath VFS

	ProducerRef    NodeRef
	HasProducerRef bool

	DeferredCF *deferredCF
}

type deferredCF struct {
	instance      ModuleInstance
	srcVFS        VFS
	outVFS        VFS
	cfgVars       []string
	includeInputs []VFS
}

type CodegenRegistry struct {
	byStr map[STR]*GeneratedFileInfo

	bySplit map[STR]map[STR]*GeneratedFileInfo
}

func NewCodegenRegistry() *CodegenRegistry {
	return &CodegenRegistry{
		byStr:   make(map[STR]*GeneratedFileInfo, 256),
		bySplit: make(map[STR]map[STR]*GeneratedFileInfo, 256),
	}
}

func (r *CodegenRegistry) Register(info *GeneratedFileInfo) {
	full := STR(info.OutputPath.strID())
	if existing := r.byStr[full]; existing != nil {
		ThrowFmt("CodegenRegistry: duplicate producer for %q (existing kind=%q, new kind=%q)",
			info.OutputPath.String(), existing.ProducerKvP, info.ProducerKvP)
	}

	rel := info.OutputPath.Rel()
	r.byStr[full] = info
	r.byStr[internString(rel)] = info

	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' {
			r.putSplit(internString(rel[:i]), internString(rel[i+1:]), info)
		}
	}
}

func (r *CodegenRegistry) putSplit(prefix, suffix STR, info *GeneratedFileInfo) {
	inner := r.bySplit[prefix]
	if inner == nil {
		inner = make(map[STR]*GeneratedFileInfo, 2)
		r.bySplit[prefix] = inner
	}

	inner[suffix] = info
}

func (r *CodegenRegistry) Lookup(path VFS) *GeneratedFileInfo {
	return r.byStr[STR(path.strID())]
}

func (r *CodegenRegistry) LookupRel(rel string) *GeneratedFileInfo {
	id := interned(rel)
	if id == nil {
		return nil
	}

	return r.byStr[*id]
}

func (r *CodegenRegistry) LookupSplit(prefix, suffix STR) *GeneratedFileInfo {
	return r.bySplit[prefix][suffix]
}

func (r *CodegenRegistry) SetProducerRef(path VFS, ref NodeRef) {
	info := r.byStr[STR(path.strID())]
	if info == nil {
		ThrowFmt("CodegenRegistry: SetProducerRef on unregistered path %q", path.String())
	}

	if info.HasProducerRef && info.ProducerRef != ref {
		ThrowFmt("CodegenRegistry: conflicting ProducerRef for %q (existing=%v, new=%v)",
			path.String(), info.ProducerRef, ref)
	}

	info.ProducerRef = ref
	info.HasProducerRef = true
}

func registerGeneratedParsedOutput(ctx *genCtx, instance ModuleInstance, kind string, output VFS, parsed []includeDirective) {
	reg := codegenRegForInstance(ctx, instance)
	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP: kind,
			OutputPath:  output,
		})
	}

	scanner := ctx.scannerFor(instance)
	if scanner != nil {
		scanner.parsers.RegisterBuildParsedIncludes(output.Rel(), parsed)
	}
}

func registerDeferredCF(ctx *genCtx, instance ModuleInstance, output VFS, parsed []includeDirective, def *deferredCF) {
	reg := codegenRegForInstance(ctx, instance)
	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP: "CF",
			OutputPath:  output,
			DeferredCF:  def,
		})
	}

	scanner := ctx.scannerFor(instance)
	if scanner != nil {
		scanner.parsers.RegisterBuildParsedIncludes(output.Rel(), parsed)
	}
}

func bindGeneratedOutput(ctx *genCtx, instance ModuleInstance, output VFS, ref NodeRef) {
	reg := codegenRegForInstance(ctx, instance)
	if reg == nil {
		return
	}

	reg.SetProducerRef(output, ref)
}

func registerBoundGeneratedParsedOutput(ctx *genCtx, instance ModuleInstance, kind string, output VFS, parsed []includeDirective, ref NodeRef) {
	reg := codegenRegForInstance(ctx, instance)
	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP:    kind,
			OutputPath:     output,
			ProducerRef:    ref,
			HasProducerRef: true,
		})
	}

	scanner := ctx.scannerFor(instance)
	if scanner != nil {
		scanner.parsers.RegisterBuildParsedIncludes(output.Rel(), parsed)
	}
}

func generatedOutputClosure(ctx *genCtx, instance ModuleInstance, output VFS, in ModuleCCInputs) []VFS {
	return walkClosure(ctx, instance, output, in)
}

func codegenRegForInstance(ctx *genCtx, instance ModuleInstance) *CodegenRegistry {
	sc := ctx.scannerFor(instance)
	if sc == nil {
		return nil
	}
	return sc.codegen
}
