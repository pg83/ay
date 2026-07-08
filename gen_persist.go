package main

func persistParsedIncludeSet(ctx *GenCtx, set ParsedIncludeSet) ParsedIncludeSet {
	out := set

	for i, bucket := range set {
		out[i] = ctx.dirSlices.internCopy(bucket)
	}

	return out
}

func persistResult(ctx *GenCtx, r *ModuleEmitResult) {
	if r.persisted {
		return
	}

	r.persisted = true
	r.WholeArchiveRefs = ctx.refSlices.internCopy(r.WholeArchiveRefs)
	r.WholeArchivePaths = ctx.vfsSlices.internCopy(r.WholeArchivePaths)
	r.WholeArchiveCmdPaths = ctx.vfsSlices.internCopy(r.WholeArchiveCmdPaths)
	r.AddInclGlobal = ctx.vfsSlices.internCopy(r.AddInclGlobal)
	r.OwnAddInclGlobal = ctx.vfsSlices.internCopy(r.OwnAddInclGlobal)
	r.ProtoInclude = ctx.vfsSlices.internCopy(r.ProtoInclude)
	r.AddInclOneLevel = ctx.vfsSlices.internCopy(r.AddInclOneLevel)
	r.AddInclUserGlobal = ctx.vfsSlices.internCopy(r.AddInclUserGlobal)
	r.CFlagsGlobal = ctx.argSlices.internCopy(r.CFlagsGlobal)
	r.CXXFlagsGlobal = ctx.argSlices.internCopy(r.CXXFlagsGlobal)
	r.COnlyFlagsGlobal = ctx.argSlices.internCopy(r.COnlyFlagsGlobal)
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
	r.LDPluginRefs = ctx.refSlices.internCopy(r.LDPluginRefs)
	r.LDPluginPaths = ctx.vfsSlices.internCopy(r.LDPluginPaths)
	r.PeerDynamicClosureRefs = ctx.refSlices.internCopy(r.PeerDynamicClosureRefs)
	r.PeerDynamicClosurePaths = ctx.vfsSlices.internCopy(r.PeerDynamicClosurePaths)
	r.PeerSbomClosureRefs = ctx.refSlices.internCopy(r.PeerSbomClosureRefs)
	r.PeerSbomClosurePaths = ctx.vfsSlices.internCopy(r.PeerSbomClosurePaths)
	r.InducedDeps = persistParsedIncludeSet(ctx, r.InducedDeps)
	r.DescClosure = ctx.descSlices.internCopy(r.DescClosure)
	r.ResourceGlobalClosure = ctx.declSlices.internCopy(r.ResourceGlobalClosure)
	r.GoSrcClosure = ctx.vfsSlices.internCopy(r.GoSrcClosure)
}
