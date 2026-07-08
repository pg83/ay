package main

func cloneRefs(in []NodeRef) []NodeRef {
	if len(in) == 0 {
		return in
	}

	return append([]NodeRef(nil), in...)
}

func cloneAnys(in []ANY) []ANY {
	if len(in) == 0 {
		return in
	}

	return append([]ANY(nil), in...)
}

func cloneParsedIncludeSet(set ParsedIncludeSet) ParsedIncludeSet {
	out := set

	for i, bucket := range set {
		if len(bucket) > 0 {
			out[i] = append([]IncludeDirective(nil), bucket...)
		}
	}

	return out
}

func persistVFSs(in []VFS) []VFS {
	if len(in) == 0 {
		return in
	}

	return cloneVFSs(in)
}

func persistResult(r *ModuleEmitResult) {
	if r.persisted {
		return
	}

	r.persisted = true
	r.WholeArchiveRefs = cloneRefs(r.WholeArchiveRefs)
	r.WholeArchivePaths = persistVFSs(r.WholeArchivePaths)
	r.WholeArchiveCmdPaths = persistVFSs(r.WholeArchiveCmdPaths)
	r.AddInclGlobal = persistVFSs(r.AddInclGlobal)
	r.OwnAddInclGlobal = persistVFSs(r.OwnAddInclGlobal)
	r.ProtoInclude = persistVFSs(r.ProtoInclude)
	r.AddInclOneLevel = persistVFSs(r.AddInclOneLevel)
	r.AddInclUserGlobal = persistVFSs(r.AddInclUserGlobal)
	r.CFlagsGlobal = cloneAnys(r.CFlagsGlobal)
	r.CXXFlagsGlobal = cloneAnys(r.CXXFlagsGlobal)
	r.COnlyFlagsGlobal = cloneAnys(r.COnlyFlagsGlobal)
	r.ObjAddLibsGlobal = cloneAnys(r.ObjAddLibsGlobal)
	r.LDFlagsGlobal = cloneAnys(r.LDFlagsGlobal)
	r.RPathFlagsGlobal = cloneAnys(r.RPathFlagsGlobal)
	r.PeerArchiveClosureRefs = cloneRefs(r.PeerArchiveClosureRefs)
	r.PeerArchiveClosurePaths = persistVFSs(r.PeerArchiveClosurePaths)
	r.PeerGlobalClosureRefs = cloneRefs(r.PeerGlobalClosureRefs)
	r.PeerGlobalClosurePaths = persistVFSs(r.PeerGlobalClosurePaths)
	r.PeerWholeArchiveClosureRefs = cloneRefs(r.PeerWholeArchiveClosureRefs)
	r.PeerWholeArchiveClosurePaths = persistVFSs(r.PeerWholeArchiveClosurePaths)
	r.PeerWholeArchiveCmdClosurePaths = persistVFSs(r.PeerWholeArchiveCmdClosurePaths)
	r.LDPluginRefs = cloneRefs(r.LDPluginRefs)
	r.LDPluginPaths = persistVFSs(r.LDPluginPaths)
	r.PeerDynamicClosureRefs = cloneRefs(r.PeerDynamicClosureRefs)
	r.PeerDynamicClosurePaths = persistVFSs(r.PeerDynamicClosurePaths)
	r.PeerSbomClosureRefs = cloneRefs(r.PeerSbomClosureRefs)
	r.PeerSbomClosurePaths = persistVFSs(r.PeerSbomClosurePaths)
	r.InducedDeps = cloneParsedIncludeSet(r.InducedDeps)
	r.DescClosure = append([]DescProtoPeer(nil), r.DescClosure...)
	r.ResourceGlobalClosure = append([]ResourceDecl(nil), r.ResourceGlobalClosure...)
	r.GoSrcClosure = persistVFSs(r.GoSrcClosure)
}
