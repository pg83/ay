package main

func aggregateSbomComponents(deduper *DeDuper, e *EmitContext, name TOK, linkTarget bool, resolved []ResolvedPeer, refs []NodeRef, paths []VFS) ([]NodeRef, []VFS, int) {
	sbomOrder := resolved
	keepLld := linkTarget || isGoModuleType(name)

	deduper.reset()

	ownInsertIdx := -1

	for _, rp := range sbomOrder {
		pr := rp.result

		for i, p := range pr.PeerSbomClosurePaths {
			if p == lldToolchainSbomVFS && !keepLld {
				continue
			}

			if deduper.addStable(p.strID()) {
				refs = append(refs, pr.PeerSbomClosureRefs[i])
				paths = append(paths, p)
			}
		}

		if rp.path == strBuildPlatformLld && name == tokPy3Program {
			ownInsertIdx = len(paths)
		}

		if pr.SbomComponentRef != nil && (*pr.SbomComponentPath != lldToolchainSbomVFS || keepLld) && deduper.addStable(pr.SbomComponentPath.strID()) {
			refs = append(refs, *pr.SbomComponentRef)
			paths = append(paths, *pr.SbomComponentPath)
		}
	}

	return refs, paths, ownInsertIdx
}

func insertOwnSbomComponent(refs []NodeRef, paths []VFS, ownRef NodeRef, ownPath VFS, insertIdx int) ([]NodeRef, []VFS) {
	if insertIdx < 0 || insertIdx > len(paths) {
		return concat(refs, []NodeRef{ownRef}), concat(paths, []VFS{ownPath})
	}

	outRefs := concat(refs[:insertIdx], []NodeRef{ownRef}, refs[insertIdx:])
	outPaths := concat(paths[:insertIdx], []VFS{ownPath}, paths[insertIdx:])

	return outRefs, outPaths
}
