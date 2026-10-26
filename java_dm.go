package main

import (
	"strings"
)

var emptyJavaPeersClosure = &javaPeersClosure{}

const javaPathCountMask = 1<<31 - 1

const (
	jprUnversioned javaPeerResolution = iota
	jprDefault
	jprDirect
	jprManaged
	jprTransitive
)

type javaPeerResolution uint8

type javaResolvedPeer struct {
	m          *JavaModule
	resolution javaPeerResolution
	impl       bool
}

type javaClosureStat struct {
	excluded  bool
	pathCount uint32
}

type javaPeersClosure struct {
	stats   map[*JavaModule]*javaClosureStat
	topSort []*JavaModule
}

func newJavaPeersClosure() *javaPeersClosure {
	return &javaPeersClosure{stats: map[*JavaModule]*javaClosureStat{}}
}

func (c *javaPeersClosure) merge(node *JavaModule, closure *javaPeersClosure, pathsCount uint32) {
	for _, id := range closure.topSort {
		st := closure.stats[id]
		count := (st.pathCount * pathsCount) & javaPathCountMask

		if existing, ok := c.stats[id]; ok {
			existing.pathCount = (existing.pathCount + count) & javaPathCountMask
			existing.excluded = existing.excluded && st.excluded
		} else {
			c.stats[id] = &javaClosureStat{excluded: st.excluded, pathCount: count}
			c.topSort = append(c.topSort, id)
		}
	}

	if existing, ok := c.stats[node]; ok {
		existing.pathCount = (existing.pathCount + pathsCount) & javaPathCountMask
		existing.excluded = false
	} else {
		c.stats[node] = &javaClosureStat{pathCount: pathsCount & javaPathCountMask}
		c.topSort = append(c.topSort, node)
	}
}

func (c *javaPeersClosure) exclude(pred func(*JavaModule) bool, nodeClosure func(*JavaModule) *javaPeersClosure, skipPropagation func(*JavaModule) bool) *javaPeersClosure {
	res := &javaPeersClosure{stats: make(map[*JavaModule]*javaClosureStat, len(c.topSort)), topSort: make([]*JavaModule, 0, len(c.topSort))}
	excludedPaths := map[*JavaModule]uint32{}

	for i := len(c.topSort) - 1; i >= 0; i-- {
		id := c.topSort[i]
		st := *c.stats[id]

		res.stats[id] = &st
		res.topSort = append(res.topSort, id)

		if !pred(id) {
			continue
		}

		stp := res.stats[id]

		stp.excluded = true

		prev, seen := excludedPaths[id]
		newExcludes := stp.pathCount

		if seen {
			newExcludes = stp.pathCount - prev
		}

		excludedPaths[id] = stp.pathCount

		if newExcludes == 0 {
			continue
		}

		if skipPropagation != nil && skipPropagation(id) {
			continue
		}

		for peer, peerStat := range nodeClosure(id).stats {
			excludedPaths[peer] += newExcludes * peerStat.pathCount
		}
	}

	for i, j := 0, len(res.topSort)-1; i < j; i, j = i+1, j-1 {
		res.topSort[i], res.topSort[j] = res.topSort[j], res.topSort[i]
	}

	for id, excludes := range excludedPaths {
		st := res.stats[id]

		if st.pathCount == excludes {
			st.excluded = true
		}
	}

	return res
}

func (c *javaPeersClosure) contains(id *JavaModule) bool {
	st, ok := c.stats[id]

	return ok && !st.excluded
}

type javaDMRules struct {
	lib2Ver  map[string]string
	excludes []string
}

func javaPathIsPrefixOf(prefix, path string) bool {
	prefix = strings.TrimSuffix(prefix, "/")

	return path == prefix || strings.HasPrefix(path, prefix) && path[len(prefix)] == '/'
}

func javaPathParent(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}

	return ""
}

func javaPathBasename(p string) string {
	return p[strings.LastIndexByte(p, '/')+1:]
}

func (r *javaDMRules) isExcluded(path string) bool {
	for _, e := range r.excludes {
		if javaPathIsPrefixOf(e, path) {
			return true
		}
	}

	return false
}

func (r *javaDMRules) ruleForPeer(peerDir string, isDirect bool) string {
	if rule, ok := r.lib2Ver[peerDir]; ok && (isDirect || !r.isExcluded(rule)) {
		return rule
	}

	parent := javaPathParent(peerDir)

	if rule, ok := r.lib2Ver[parent]; ok {
		if isDirect {
			return peerDir
		}

		if !r.isExcluded(rule) {
			return rule
		}
	}

	return ""
}

func (r *javaDMRules) addDependencyManagement(libWithVer string) {
	if _, ok := r.lib2Ver[libWithVer]; ok {
		return
	}

	r.lib2Ver[javaPathParent(libWithVer)] = libWithVer
}

type javaResolutionInfo struct {
	minDepth   int
	resolution javaResolvedPeer
}

type javaResolutionRecord struct {
	choice  javaResolvedPeer
	version string
}

type javaConflictResolver struct {
	ctx         *GenCtx
	parent      *JavaModule
	rules       *javaDMRules
	accumulated map[*JavaModule]javaResolutionInfo
	resolutions map[string]*javaResolutionRecord
}

func (r *javaConflictResolver) accept(conflict *JavaModule, resolution javaResolvedPeer, depth int) *JavaModule {
	info, ok := r.accumulated[conflict]

	if !ok {
		info = javaResolutionInfo{minDepth: depth, resolution: resolution}
		r.accumulated[conflict] = info
	}

	if resolution.resolution == jprManaged {
		if _, ok := r.accumulated[info.resolution.m]; !ok {
			r.accumulated[info.resolution.m] = info
		}
	}

	return info.resolution.m
}

func (r *javaConflictResolver) resolve(cur javaResolvedPeer, depth int) *JavaModule {
	if cur.resolution == jprUnversioned {
		return r.accept(cur.m, cur, depth)
	}

	peerDir := cur.m.dir
	libName := javaPathParent(peerDir)
	libVer := javaPathBasename(peerDir)

	if rec, ok := r.resolutions[libName]; ok {
		if rec.choice.resolution == jprManaged {
			if _, ok := r.accumulated[cur.m]; !ok {
				r.accumulated[cur.m] = javaResolutionInfo{minDepth: depth, resolution: rec.choice}
			}
		}

		return nil
	}

	rec := &javaResolutionRecord{choice: cur, version: libVer}

	r.resolutions[libName] = rec

	resolve, dmVer := r.explicitResolution(peerDir, depth == 1)

	if resolve == nil {
		if cur.resolution == jprManaged {
			rec.choice.resolution = jprTransitive
		}

		return r.accept(cur.m, rec.choice, depth)
	}

	rec.choice = javaResolvedPeer{m: resolve, resolution: jprManaged}
	rec.version = dmVer

	return r.accept(cur.m, rec.choice, depth)
}

func (r *javaConflictResolver) explicitResolution(peerDir string, isDirect bool) (*JavaModule, string) {
	rule := r.rules.ruleForPeer(peerDir, isDirect)

	if rule == "" {
		return nil, ""
	}

	m := r.ctx.javaContrib(r.parent, rule)

	if m == nil {
		return nil, ""
	}

	return m, javaPathBasename(rule)
}

func (ctx *GenCtx) javaManageLocalPeers(jm *JavaModule, rawPeers []*JavaModule, rules *javaDMRules) []javaResolvedPeer {
	out := make([]javaResolvedPeer, 0, len(rawPeers))

	for _, peer := range rawPeers {
		impl := jm.implPeers[peer.dir]

		if peer.proxyFor != nil {
			rule := rules.ruleForPeer(peer.dir, true)

			if rule == "" {
				ctx.onWarn(Warn{Kind: WarnModuleFailed, Message: jm.dir + ": Dependency with default version: " + peer.proxyFor.dir})

				continue
			}

			target := ctx.javaContrib(jm, rule)

			if target == nil {
				ctx.onWarn(Warn{Kind: WarnModuleFailed, Message: jm.dir + ": DEPENDENCY_MANAGEMENT replaces peerdir to " + peer.dir + " with dependency to missing directory or directory without module " + rule})

				continue
			}

			out = append(out, javaResolvedPeer{m: target, resolution: jprManaged, impl: impl})

			continue
		}

		resolution := jprUnversioned

		if peer.contribWithVer {
			resolution = jprDirect
		}

		out = append(out, javaResolvedPeer{m: peer, resolution: resolution, impl: impl})

		if len(rawPeers) == 1 && peer.proxyFor == nil && javaPathParent(peer.dir) == jm.dir && peer.contribWithVer {
			jm.proxyFor = peer
		}
	}

	return out
}

func javaClosureExcludePred(rules *javaDMRules) func(*JavaModule) bool {
	return func(m *JavaModule) bool {
		return rules.isExcluded(m.fileName())
	}
}

func javaClosureOf(m *JavaModule) *javaPeersClosure {
	if m.closure == nil {
		return emptyJavaPeersClosure
	}

	return m.closure
}

func javaSkipPropagation(m *JavaModule) bool {
	return m.unit.transparent
}

func (jm *JavaModule) buildExpandedTransparentClosure(transparent *JavaModule, rules *javaDMRules) *javaPeersClosure {
	expanded := newJavaPeersClosure()
	excludePred := javaClosureExcludePred(rules)
	directChildren := map[*JavaModule]bool{}

	for _, d := range transparent.direct {
		directChildren[d.m] = true
	}

	for _, d := range transparent.direct {
		if !d.m.passPeers {
			expanded.merge(d.m, emptyJavaPeersClosure, 1)

			continue
		}

		pred := func(m *JavaModule) bool {
			return !directChildren[m] && excludePred(m)
		}

		expanded.merge(d.m, d.m.closure.exclude(pred, javaClosureOf, javaSkipPropagation), 1)
	}

	return expanded
}

func (jm *JavaModule) mergeClosure(rules *javaDMRules) *javaPeersClosure {
	closure := newJavaPeersClosure()
	excludePred := javaClosureExcludePred(rules)

	for _, d := range jm.direct {
		if d.m.unit.transparent {
			closure.merge(d.m, jm.buildExpandedTransparentClosure(d.m, rules), 1)

			continue
		}

		closure.merge(d.m, javaClosureOf(d.m).exclude(excludePred, javaClosureOf, javaSkipPropagation), 1)
	}

	return closure
}

func (ctx *GenCtx) javaResolveConflicts(jm *JavaModule) map[*JavaModule]javaResolutionInfo {
	if len(jm.direct) == 0 {
		return nil
	}

	resolver := &javaConflictResolver{
		ctx:         ctx,
		parent:      jm,
		rules:       jm.rules,
		accumulated: map[*JavaModule]javaResolutionInfo{},
		resolutions: map[string]*javaResolutionRecord{},
	}

	explicitResolves := newJavaPeersClosure()
	visited := map[*JavaModule]bool{}

	var queue []javaResolvedPeer

	enqueue := func(peers []javaResolvedPeer) {
		admit := func(peer javaResolvedPeer) bool {
			if visited[peer.m] {
				return false
			}

			visited[peer.m] = true

			if !jm.closure.contains(peer.m) && !explicitResolves.contains(peer.m) {
				return false
			}

			queue = append(queue, peer)

			return true
		}

		for _, peer := range peers {
			if !admit(peer) || !peer.m.unit.transparent {
				continue
			}

			for _, child := range peer.m.direct {
				admit(child)
			}
		}
	}

	depth := 0
	currDepthEnd := 0
	excludePred := javaClosureExcludePred(jm.rules)

	enqueue(jm.direct)

	for head := 0; head < len(queue); head++ {
		cur := queue[head]

		if head == currDepthEnd {
			depth++
			currDepthEnd += len(queue) - head
		}

		resolved := resolver.resolve(cur, depth)

		switch {
		case resolved == cur.m:
		case resolved == nil:
			continue
		default:
			visited[resolved] = true

			if resolved.closure != nil {
				explicitResolves.merge(resolved, resolved.closure.exclude(excludePred, javaClosureOf, nil), 1)
			}
		}

		enqueue(resolved.direct)
	}

	return resolver.accumulated
}

func javaPreorderSort(direct []javaResolvedPeer, resolution map[*JavaModule]javaResolutionInfo, skipTransitiveImpl bool) []*JavaModule {
	var res []*JavaModule

	seen := map[*JavaModule]bool{}
	stack := [][]javaResolvedPeer{direct}

	for len(stack) > 0 {
		top := stack[len(stack)-1]

		if len(top) == 0 {
			stack = stack[:len(stack)-1]

			continue
		}

		cur := top[0]

		stack[len(stack)-1] = top[1:]

		if skipTransitiveImpl && len(stack) > 1 && cur.impl {
			continue
		}

		info, ok := resolution[cur.m]

		if !ok {
			continue
		}

		target := info.resolution.m

		if seen[target] {
			continue
		}

		seen[target] = true
		res = append(res, target)

		if len(target.direct) > 0 {
			stack = append(stack, target.direct)
		}
	}

	return res
}

func (ctx *GenCtx) javaManagedPeersClosure(jm *JavaModule) []JavaClosureEntry {
	if jm.managedDone {
		return jm.managed
	}

	jm.managedDone = true

	sorted := javaPreorderSort(jm.direct, ctx.javaResolveConflicts(jm), jm.unit.compileView)
	excludedTags := jm.unit.dmTagsExclude

	if jm.data.uberjar {
		excludedTags = javaTagsIJar
	}

	out := make([]JavaClosureEntry, 0, len(sorted))

	for _, m := range sorted {
		if javaTagIn(m.unit.tag, excludedTags) {
			continue
		}

		out = append(out, JavaClosureEntry{path: m.target, java: m})
	}

	if jm.consumesUnmanageable() {
		seen := map[*ModuleEmitResult]bool{}

		appendUnmanageable := func(peers []javaForeignPeer) {
			for _, p := range peers {
				if seen[p.res] {
					continue
				}

				seen[p.res] = true

				if path := javaForeignFileName(p); path != 0 {
					out = append(out, JavaClosureEntry{path: path, foreign: p.res})
				}
			}
		}

		appendUnmanageable(jm.unmanageable)

		for _, e := range out {
			if e.java != nil {
				appendUnmanageable(e.java.unmanageableClosure())
			}
		}
	}

	jm.managed = out

	return out
}

func (ctx *GenCtx) javaIncludedDeps(jm *JavaModule) *ModuleIncludedDeps {
	if jm.included != nil {
		return jm.included
	}

	jm.included = &ModuleIncludedDeps{}

	if !jm.passPeers {
		return jm.included
	}

	var paths []VFS
	var own []NodeRef
	var chunks [][]VFS
	var refs [][]NodeRef

	for _, p := range jm.unmanageable {
		res := p.res

		if res.LDPath != nil {
			paths = append(paths, *res.LDPath)
			own = append(own, res.LDRef)

			if res.IncludedDeps != nil {
				chunks = append(chunks, res.IncludedDeps.Inputs)
				refs = append(refs, res.IncludedDeps.Refs)
			}

			continue
		}

		if res.ARPath != nil {
			paths = append(paths, *res.ARPath)
			own = append(own, res.ARRef)
		}

		if res.GlobalPath != nil && res.GlobalRef != nil {
			paths = append(paths, *res.GlobalPath)
			own = append(own, *res.GlobalRef)
		}

		if res.SbomComponentRef != nil {
			paths = append(paths, *res.SbomComponentPath)
			own = append(own, *res.SbomComponentRef)
		}

		chunks = append(chunks, res.PeerArchiveClosurePaths, res.PeerGlobalClosurePaths, res.PeerDynamicClosurePaths, res.PeerSbomClosurePaths)
		refs = append(refs, res.PeerArchiveClosureRefs, res.PeerGlobalClosureRefs, res.PeerDynamicClosureRefs, res.PeerSbomClosureRefs)
	}

	edges := make(map[*JavaModule]bool, len(jm.managedPeers))

	for _, p := range jm.managedPeers {
		edges[p] = true
	}

	for _, e := range ctx.javaManagedPeersClosure(jm) {
		p := e.java

		if p == nil || !edges[p] && jm.rules.lib2Ver[javaPathParent(p.dir)] != p.dir {
			continue
		}

		if p.outRef != 0 {
			paths = append(paths, p.target)
			own = append(own, p.outRef)
		}

		peer := ctx.javaIncludedDeps(p)

		chunks = append(chunks, peer.Inputs)
		refs = append(refs, peer.Refs)
	}

	jm.included = &ModuleIncludedDeps{Inputs: ctx.na.dedupClosure(paths, chunks), Refs: ctx.na.uniqueRefs(append(refs, own)...)}

	return jm.included
}

func (jm *JavaModule) consumesUnmanageable() bool {
	return jm.unit.consumeNonManageable || jm.data.addDLLs
}

func (jm *JavaModule) unmanageableClosure() []javaForeignPeer {
	if !jm.unit.transparent {
		return jm.unmanageable
	}

	out := jm.unmanageable

	for _, d := range jm.direct {
		out = append(out, d.m.unmanageableClosure()...)
	}

	return out
}
