package main

import "path/filepath"

func applySbomComponentOrder(e *EmitContext, name TOK, linkTarget bool, resolved []ResolvedPeer, allocatorExplicitPeers []string) []ResolvedPeer {
	sbomOrder := resolved

	cloneOrder := func() {
		if len(resolved) > 0 && len(sbomOrder) == len(resolved) && &sbomOrder[0] == &resolved[0] {
			sbomOrder = append(e.sbomOrder[:0], resolved...)
			e.sbomOrder = sbomOrder[:0]
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

func aggregateSbomComponents(e *EmitContext, name TOK, linkTarget bool, resolved []ResolvedPeer, allocatorExplicitPeers []string, refs []NodeRef, paths []VFS) ([]NodeRef, []VFS, int) {
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

			if deduper.add(p.strID()) {
				refs = append(refs, pr.PeerSbomClosureRefs[i])
				paths = append(paths, p)
			}
		}

		if rp.path == "build/platform/lld" && name == tokPy3Program {
			ownInsertIdx = len(paths)
		}

		if pr.SbomComponentRef != nil && (*pr.SbomComponentPath != lldToolchainSbomVFS || keepLld) && deduper.add(pr.SbomComponentPath.strID()) {
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

func applyDeferredPeerOrder(name TOK, allPeers []string, peerKinds []int, allocatorExplicitPeers []string) ([]string, []int) {
	switch name {
	case tokPy2Program, tokPy3ProgramBin:
		headP := make([]string, 0, len(allPeers))
		headK := make([]int, 0, len(peerKinds))
		tailP := make([]string, 0, 2)
		tailK := make([]int, 0, 2)

		for i, p := range allPeers {
			if filepath.Clean(p) == "contrib/libs/python" || filepath.Clean(p) == "library/python/runtime_py3" {
				tailP = append(tailP, p)
				tailK = append(tailK, peerKinds[i])

				continue
			}

			headP = append(headP, p)
			headK = append(headK, peerKinds[i])
		}

		return append(headP, tailP...), append(headK, tailK...)
	case tokPy3Program:
		allocatorExplicitSet := make(map[string]struct{}, len(allocatorExplicitPeers))

		for _, p := range allocatorExplicitPeers {
			allocatorExplicitSet[filepath.Clean(p)] = struct{}{}
		}

		headP := make([]string, 0, len(allPeers))
		headK := make([]int, 0, len(peerKinds))
		progP := make([]string, 0, 8)
		progK := make([]int, 0, 8)
		pyP := make([]string, 0, 4)
		pyK := make([]int, 0, 4)

		for i, p := range allPeers {
			clean := filepath.Clean(p)

			if clean == "contrib/tools/python3/Modules/_sqlite" ||
				clean == "library/python/runtime_py3/main" ||
				clean == "library/python/import_tracing/constructor" ||
				clean == "library/python/testing/import_test" {
				pyP = append(pyP, p)
				pyK = append(pyK, peerKinds[i])

				continue
			}

			if _, alloc := allocatorExplicitSet[clean]; alloc || peerKinds[i] == peerKindProgramDefault {
				progP = append(progP, p)
				progK = append(progK, peerKinds[i])

				continue
			}

			headP = append(headP, p)
			headK = append(headK, peerKinds[i])
		}

		return concat(headP, progP, pyP), concat(headK, progK, pyK)
	}

	return allPeers, peerKinds
}
