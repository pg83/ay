package main

type GeneratedFileAttributes struct {
	gztProto           DenseMap[uint32, *GztProtoProducer]
	moduleIncludedDeps DenseMap[uint32, *ModuleIncludedDeps]
	moduleInputs       DenseMap[uint32, *ModuleInputInfo]
	collectionPeers    DenseMap[uint32, *CollectionPeerInfo]
	nots               DenseMap[uint32, *NotsPrepared]
	notsInputs         DenseMap[uint32, []VFS]
	sandbox            DenseMap[uint32, *SandboxRecipe]
	scoped             DenseMap[uint32, *ScopedCommand]
	parsedIncludes     DenseMap[uint32, ParsedIncludeSet]
	fbsFlatc           DenseMap[uint32, *FbsFlatcRecipe]
	fbsNative          DenseMap[uint32, *FbsNativeRecipe]
}

func (info *GeneratedFileInfo) gztProto() *GztProtoProducer {
	value, _ := info.attributes.gztProto.get(info.id)

	return value
}

func (info *GeneratedFileInfo) moduleIncludedDeps() *ModuleIncludedDeps {
	value, _ := info.attributes.moduleIncludedDeps.get(info.id)

	return value
}

func (info *GeneratedFileInfo) moduleInputs() *ModuleInputInfo {
	value, _ := info.attributes.moduleInputs.get(info.id)

	return value
}

func (info *GeneratedFileInfo) collectionPeers() *CollectionPeerInfo {
	value, _ := info.attributes.collectionPeers.get(info.id)

	return value
}

func (info *GeneratedFileInfo) nots() *NotsPrepared {
	value, _ := info.attributes.nots.get(info.id)

	return value
}

func (info *GeneratedFileInfo) notsInputs() []VFS {
	value, _ := info.attributes.notsInputs.get(info.id)

	return value
}

func (info *GeneratedFileInfo) sandbox() *SandboxRecipe {
	value, _ := info.attributes.sandbox.get(info.id)

	return value
}

func (info *GeneratedFileInfo) scoped() *ScopedCommand {
	value, _ := info.attributes.scoped.get(info.id)

	return value
}

func (info *GeneratedFileInfo) parsedIncludes() ParsedIncludeSet {
	value, _ := info.attributes.parsedIncludes.get(info.id)

	return value
}

func (info *GeneratedFileInfo) fbsFlatc() *FbsFlatcRecipe {
	value, _ := info.attributes.fbsFlatc.get(info.id)

	return value
}

func (info *GeneratedFileInfo) fbsNative() *FbsNativeRecipe {
	value, _ := info.attributes.fbsNative.get(info.id)

	return value
}

func (r *CodegenRegistry) store(dst *GeneratedFileInfo, spec GeneratedFileSpec) {
	r.infoCount++
	*dst = GeneratedFileInfo{attributes: &r.attributes, id: r.infoCount,
		BuildInputs:         spec.BuildInputs,
		OnUse:               spec.OnUse,
		GeneratorRefs:       spec.GeneratorRefs,
		ClosureLeaves:       spec.ClosureLeaves,
		InputLoopSources:    spec.InputLoopSources,
		LateOutputs:         spec.LateOutputs,
		ModuleOutputs:       spec.ModuleOutputs,
		IncludedBuildInputs: spec.IncludedBuildInputs,
		IncludedInputs:      spec.IncludedInputs,
		OutputPath:          spec.OutputPath,
		SourcePath:          spec.SourcePath,
		PrimaryInput:        spec.PrimaryInput,
		OwnerModule:         spec.OwnerModule,
		OwnerSerial:         spec.OwnerSerial,
		ProducerRef:         spec.ProducerRef,
		ProducerMainOut:     spec.ProducerMainOut,
		ModuleType:          spec.ModuleType,
		InputsOnly:          spec.InputsOnly,
		ResourceObjcopy:     spec.ResourceObjcopy,
	}

	if spec.GztProto != nil {
		r.attributes.gztProto.put(dst.id, spec.GztProto)
	}

	if spec.ModuleIncludedDeps != nil {
		r.attributes.moduleIncludedDeps.put(dst.id, spec.ModuleIncludedDeps)
	}

	if spec.ModuleInputs != nil {
		r.attributes.moduleInputs.put(dst.id, spec.ModuleInputs)
	}

	if spec.CollectionPeers != nil {
		r.attributes.collectionPeers.put(dst.id, spec.CollectionPeers)
	}

	if spec.Nots != nil {
		r.attributes.nots.put(dst.id, spec.Nots)
	}

	if spec.NotsInputs != nil {
		r.attributes.notsInputs.put(dst.id, spec.NotsInputs)
	}

	if spec.Sandbox != nil {
		r.attributes.sandbox.put(dst.id, spec.Sandbox)
	}

	if spec.Scoped != nil {
		r.attributes.scoped.put(dst.id, spec.Scoped)
	}

	if spec.FbsFlatc != nil {
		r.attributes.fbsFlatc.put(dst.id, spec.FbsFlatc)
	}

	if spec.FbsNative != nil {
		r.attributes.fbsNative.put(dst.id, spec.FbsNative)
	}

	for _, bucket := range spec.ParsedIncludes {
		if bucket != nil {
			r.attributes.parsedIncludes.put(dst.id, spec.ParsedIncludes)

			break
		}
	}
}

func (info *GeneratedFileInfo) specification() GeneratedFileSpec {
	return GeneratedFileSpec{
		GztProto:            info.gztProto(),
		ModuleIncludedDeps:  info.moduleIncludedDeps(),
		ModuleInputs:        info.moduleInputs(),
		CollectionPeers:     info.collectionPeers(),
		Nots:                info.nots(),
		NotsInputs:          info.notsInputs(),
		Sandbox:             info.sandbox(),
		BuildInputs:         info.BuildInputs,
		OnUse:               info.OnUse,
		Scoped:              info.scoped(),
		FbsFlatc:            info.fbsFlatc(),
		FbsNative:           info.fbsNative(),
		GeneratorRefs:       info.GeneratorRefs,
		ClosureLeaves:       info.ClosureLeaves,
		InputLoopSources:    info.InputLoopSources,
		LateOutputs:         info.LateOutputs,
		ModuleOutputs:       info.ModuleOutputs,
		ParsedIncludes:      info.parsedIncludes(),
		IncludedBuildInputs: info.IncludedBuildInputs,
		IncludedInputs:      info.IncludedInputs,
		OutputPath:          info.OutputPath,
		SourcePath:          info.SourcePath,
		PrimaryInput:        info.PrimaryInput,
		OwnerModule:         info.OwnerModule,
		OwnerSerial:         info.OwnerSerial,
		ProducerRef:         info.ProducerRef,
		ProducerMainOut:     info.ProducerMainOut,
		ModuleType:          info.ModuleType,
		InputsOnly:          info.InputsOnly,
		ResourceObjcopy:     info.ResourceObjcopy,
	}
}
