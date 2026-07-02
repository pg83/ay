package main

import "path/filepath"

func applySbomComponentOrder(name TOK, linkTarget bool, resolved []resolvedPeer, allocatorExplicitPeers []string) []resolvedPeer {
	sbomOrder := resolved
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
		reordered := make([]resolvedPeer, 0, len(resolved))
		cxx := resolved[cxxIdx]

		for i, rp := range resolved {
			if i == cxxIdx {
				continue
			}

			reordered = append(reordered, rp)

			if i == libcxxIdx {
				reordered = append(reordered, cxx)
			}
		}

		sbomOrder = reordered
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
			relocated := make([]resolvedPeer, 0, len(sbomOrder))
			lld := sbomOrder[lldIdx]

			for i, rp := range sbomOrder {
				if i == lldIdx {
					continue
				}

				if i == allocIdx {
					relocated = append(relocated, lld)
				}

				relocated = append(relocated, rp)
			}

			sbomOrder = relocated
		}
	}

	return sbomOrder
}

func aggregateSbomComponents(name TOK, linkTarget bool, resolved []resolvedPeer, allocatorExplicitPeers []string, refs []NodeRef, paths []VFS) ([]NodeRef, []VFS, int) {
	sbomOrder := applySbomComponentOrder(name, linkTarget, resolved, allocatorExplicitPeers)

	deduper.reset()

	ownInsertIdx := -1

	for _, rp := range sbomOrder {
		pr := rp.result

		for i, p := range pr.PeerSbomClosurePaths {
			if p == lldToolchainSbomVFS {
				continue
			}

			if deduper.add(p) {
				refs = append(refs, pr.PeerSbomClosureRefs[i])
				paths = append(paths, p)
			}
		}

		if rp.path == "build/platform/lld" && name == tokPy3Program {
			ownInsertIdx = len(paths)
		}

		if pr.SbomComponentRef != nil && (*pr.SbomComponentPath != lldToolchainSbomVFS || linkTarget) && deduper.add(*pr.SbomComponentPath) {
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

	outRefs := make([]NodeRef, 0, len(refs)+1)

	outRefs = append(outRefs, refs[:insertIdx]...)
	outRefs = append(outRefs, ownRef)
	outRefs = append(outRefs, refs[insertIdx:]...)

	outPaths := make([]VFS, 0, len(paths)+1)

	outPaths = append(outPaths, paths[:insertIdx]...)
	outPaths = append(outPaths, ownPath)
	outPaths = append(outPaths, paths[insertIdx:]...)

	return outRefs, outPaths
}
