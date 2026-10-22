package main

func persistParsedIncludeSet(ctx *GenCtx, set ParsedIncludeSet) ParsedIncludeSet {
	out := set

	for i, bucket := range set {
		out[i] = ctx.dirSlices.internCopy(bucket)
	}

	return out
}

func persistResult(ctx *GenCtx, instance ModuleInstance, r *ModuleEmitResult) {
	ctx.publishModuleOutputs(instance, r)
	r.GlobalSources = ctx.vfsSlices.internCopy(r.GlobalSources)

	if r.persisted {
		return
	}

	r.persisted = true
	r.JavaClosure = ctx.vfsSlices.internCopy(r.JavaClosure)
	r.DocsClosure = ctx.vfsSlices.internCopy(r.DocsClosure)
	r.WholeArchiveRefs = ctx.refSlices.internCopy(r.WholeArchiveRefs)
	r.WholeArchivePaths = ctx.vfsSlices.internCopy(r.WholeArchivePaths)
	r.WholeArchiveCmdPaths = ctx.vfsSlices.internCopy(r.WholeArchiveCmdPaths)
	r.AddInclGlobal = ctx.vfsSlices.internCopy(r.AddInclGlobal)
	r.OwnAddInclGlobal = ctx.vfsSlices.internCopy(r.OwnAddInclGlobal)
	r.ProtoInclude = ctx.vfsSlices.internCopy(r.ProtoInclude)
	r.AddInclOneLevel = ctx.vfsSlices.internCopy(r.AddInclOneLevel)
	r.AddInclUserGlobal = ctx.vfsSlices.internCopy(r.AddInclUserGlobal)
	r.PchFlagsGlobal = ctx.argSlices.internCopy(r.PchFlagsGlobal)
	r.LinkExcludeLibraries = ctx.argSlices.internCopy(r.LinkExcludeLibraries)
	r.ObjAddLibsGlobal = ctx.argSlices.internCopy(r.ObjAddLibsGlobal)
	r.LDFlagsGlobal = ctx.argSlices.internCopy(r.LDFlagsGlobal)
	r.RPathFlagsGlobal = ctx.argSlices.internCopy(r.RPathFlagsGlobal)
	r.PeerArchiveClosureRefs = ctx.refSlices.internCopy(r.PeerArchiveClosureRefs)
	r.PeerArchiveClosurePaths = ctx.vfsSlices.internCopy(r.PeerArchiveClosurePaths)
	r.PeerGlobalClosureRefs = ctx.refSlices.internCopy(r.PeerGlobalClosureRefs)
	r.PeerGlobalClosurePaths = ctx.vfsSlices.internCopy(r.PeerGlobalClosurePaths)
	r.PeerWholeArchiveClosureRefs = ctx.refSlices.internCopy(r.PeerWholeArchiveClosureRefs)
	r.PeerWholeArchiveClosurePaths = ctx.vfsSlices.internCopy(r.PeerWholeArchiveClosurePaths)
	r.PeerWholeArchiveCmdClosurePaths = ctx.vfsSlices.internCopy(r.PeerWholeArchiveCmdClosurePaths)
	r.PeerDynamicClosureRefs = ctx.refSlices.internCopy(r.PeerDynamicClosureRefs)
	r.PeerDynamicClosurePaths = ctx.vfsSlices.internCopy(r.PeerDynamicClosurePaths)
	r.PeerSbomClosureRefs = ctx.refSlices.internCopy(r.PeerSbomClosureRefs)
	r.PeerSbomClosurePaths = ctx.vfsSlices.internCopy(r.PeerSbomClosurePaths)
	r.InducedDeps = persistParsedIncludeSet(ctx, r.InducedDeps)
	r.DescClosure = ctx.descSlices.internCopy(r.DescClosure)
	r.ResourceGlobalClosure = ctx.declSlices.internCopy(r.ResourceGlobalClosure)
}
