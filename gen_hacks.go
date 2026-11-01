package main

import "path/filepath"

func applySbomComponentOrder(e *EmitContext, name TOK, linkTarget bool, resolved []ResolvedPeer, allocatorExplicitPeers []string) []ResolvedPeer {
	sbomOrder := resolved

	cloneOrder := func() {
		if len(resolved) > 0 && len(sbomOrder) == len(resolved) && &sbomOrder[0] == &resolved[0] {
			sbomOrder = append(e.sbomOrder[:0], resolved...)
			e.sbomOrder = retainMaxLen(e.sbomOrder, sbomOrder)
		}
	}

	cxxIdx, libcxxIdx := -1, -1

	for i, rp := range resolved {
		switch rp.path {
		case "contrib/libs/cxxsupp":
			cxxIdx = i
		case "contrib/libs/cxxsupp/libcxx":
			libcxxIdx = i
		}
	}

	if !isSpecializedLibraryType(name) && cxxIdx > libcxxIdx && libcxxIdx >= 0 {
		cloneOrder()

		cxx := sbomOrder[cxxIdx]

		copy(sbomOrder[libcxxIdx+2:cxxIdx+1], sbomOrder[libcxxIdx+1:cxxIdx])
		sbomOrder[libcxxIdx+1] = cxx
	}

	if linkTarget && len(allocatorExplicitPeers) > 0 {
		allocSet := make(map[string]struct{}, len(allocatorExplicitPeers))

		for _, p := range allocatorExplicitPeers {
			allocSet[filepath.Clean(p)] = struct{}{}
		}

		lldIdx, allocIdx := -1, -1

		for i, rp := range sbomOrder {
			if rp.path == "build/platform/lld" {
				lldIdx = i
			}

			if _, ok := allocSet[rp.path]; ok && allocIdx < 0 {
				allocIdx = i
			}
		}

		if lldIdx >= 0 && allocIdx >= 0 && lldIdx < allocIdx {
			cloneOrder()

			lld := sbomOrder[lldIdx]

			copy(sbomOrder[lldIdx:allocIdx-1], sbomOrder[lldIdx+1:allocIdx])
			sbomOrder[allocIdx-1] = lld
		}
	}

	return sbomOrder
}

func aggregateSbomComponents(deduper *DeDuper, e *EmitContext, name TOK, linkTarget bool, resolved []ResolvedPeer, allocatorExplicitPeers []string, refs []NodeRef, paths []VFS) ([]NodeRef, []VFS, int) {
	sbomOrder := applySbomComponentOrder(e, name, linkTarget, resolved, allocatorExplicitPeers)
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

		if rp.path == "build/platform/lld" && name == tokPy3Program {
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
