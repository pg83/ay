package main

type GeneratedFileInfo struct {
	ProducerKvP string

	OutputPath VFS

	// SourcePath is the canonical pre-generation file when the producer is a
	// pass-through (currently: CP / COPY_FILE). Upstream reports this — not
	// OutputPath — as the input edge in transitive closures, so include walks
	// rewrite OutputPath to SourcePath on the way out. Zero value means "no
	// remapping; OutputPath is the canonical input edge".
	SourcePath VFS

	// IsText marks a COPY_FILE(TEXT) registration. TEXT copies expand the
	// source content into the destination verbatim, so the .txt source is a
	// real compiler input even when the COPY lives in a different module than
	// the CC node that includes the generated header. withContextSourceExtras
	// uses this flag to extend source-tracking across module boundaries.
	IsText bool

	ProducerRef    NodeRef
	HasProducerRef bool

	// SourceInputs are the producer node's $(S)-rooted leaf inputs, propagated
	// to consumers that compile this generated file. Upstream's flat input model
	// lists the full transitive source closure on every node, so a node
	// compiling a build-generated source (e.g. a PB protoc node fed a
	// RUN_ANTLR-generated .proto) carries the generator's grammar / template /
	// tool / script sources too. Zero len means "nothing to propagate".
	SourceInputs []VFS

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

func (r *CodegenRegistry) SetSourceInputs(path VFS, src []VFS) {
	info := r.byStr[STR(path.strID())]

	if info == nil {
		ThrowFmt("CodegenRegistry: SetSourceInputs on unregistered path %q", path.String())
	}

	info.SourceInputs = src
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
	registerBoundGeneratedParsedOutputWithSource(ctx, instance, kind, output, 0, parsed, ref)
}

// registerBoundGeneratedParsedOutputWithSource is the CP variant that records
// the COPY source alongside the dst so the closure walker can rewrite the
// emitted input edge to the source. Pass sourcePath = 0 for non-CP producers
// to fall back to OutputPath as the canonical edge.
func registerBoundGeneratedParsedOutputWithSource(ctx *genCtx, instance ModuleInstance, kind string, output VFS, sourcePath VFS, parsed []includeDirective, ref NodeRef) {
	reg := codegenRegForInstance(ctx, instance)

	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP:    kind,
			OutputPath:     output,
			SourcePath:     sourcePath,
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
