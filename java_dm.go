package main

import (
	"slices"
	"strings"
)

const (
	javaPathCountMask   = 1<<31 - 1
	javaClosureExcluded = 1 << 31
)

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

type javaClosure []uint64

func javaClosureElem(id, pathCount uint32, excluded bool) uint64 {
	e := uint64(id)<<32 | uint64(pathCount&javaPathCountMask)

	if excluded {
		e |= javaClosureExcluded
	}

	return e
}

func javaClosureID(e uint64) uint32 {
	return uint32(e >> 32)
}

func javaClosurePaths(e uint64) uint32 {
	return uint32(e) & javaPathCountMask
}

func javaClosureIsExcluded(e uint64) bool {
	return e&javaClosureExcluded != 0
}

type javaClosureMarks struct {
	gen   Vec[uint64]
	epoch uint32
}

type javaClosureScratch struct {
	byID      []*JavaModule
	slot      Vec[uint64]
	slotEpoch uint32
	buf       []uint64
	excl      Vec[uint64]
	exclEpoch uint32
	exBuf     []uint64
	marks     []*javaClosureMarks
	depth     int
}

func (s *javaClosureScratch) register(jm *JavaModule) {
	jm.id = uint32(len(s.byID))
	s.byID = append(s.byID, jm)
}

func (s *javaClosureScratch) begin() {
	s.slotEpoch++
	s.buf = s.buf[:0]
	s.slot.ensureLen(len(s.byID))
}

func (s *javaClosureScratch) mergeElem(id, pathCount uint32, excluded bool) {
	cell := &s.slot.s[id]

	if uint32(*cell>>32) == s.slotEpoch {
		p := &s.buf[uint32(*cell)]

		*p = javaClosureElem(id, javaClosurePaths(*p)+pathCount, javaClosureIsExcluded(*p) && excluded)

		return
	}

	*cell = uint64(s.slotEpoch)<<32 | uint64(len(s.buf))
	s.buf = append(s.buf, javaClosureElem(id, pathCount, excluded))
}

func (s *javaClosureScratch) merge(node *JavaModule, closure javaClosure) {
	for _, e := range closure {
		s.mergeElem(javaClosureID(e), javaClosurePaths(e), javaClosureIsExcluded(e))
	}

	s.mergeElem(node.id, 1, false)
}

func (s *javaClosureScratch) commit() javaClosure {
	return slices.Clone(s.buf)
}

func (s *javaClosureScratch) exclude(c javaClosure, pred func(*JavaModule) bool, skipTransparent bool) javaClosure {
	if pred == nil {
		return c
	}

	byID := s.byID

	if !slices.ContainsFunc(c, func(e uint64) bool { return pred(byID[javaClosureID(e)]) }) {
		return c
	}

	s.exclEpoch++
	s.excl.ensureLen(len(byID))

	epoch := s.exclEpoch
	excl := s.excl.s
	out := append(s.exBuf[:0], c...)

	for i := len(out) - 1; i >= 0; i-- {
		id := javaClosureID(out[i])
		m := byID[id]

		if !pred(m) {
			continue
		}

		out[i] |= javaClosureExcluded

		paths := javaClosurePaths(out[i])
		cell := &excl[id]
		prev := uint32(0)

		if uint32(*cell>>32) == epoch {
			prev = uint32(*cell)
		}

		*cell = uint64(epoch)<<32 | uint64(paths)

		if paths == prev || skipTransparent && m.unit.transparent {
			continue
		}

		newExcludes := paths - prev

		for _, pe := range javaClosureOf(m) {
			pc := &excl[javaClosureID(pe)]
			acc := uint32(0)

			if uint32(*pc>>32) == epoch {
				acc = uint32(*pc)
			}

			*pc = uint64(epoch)<<32 | uint64(acc+newExcludes*javaClosurePaths(pe))
		}
	}

	for i, e := range out {
		cell := excl[javaClosureID(e)]

		if uint32(cell>>32) == epoch && javaClosurePaths(e) == uint32(cell) {
			out[i] |= javaClosureExcluded
		}
	}

	s.exBuf = out

	return out
}

func (s *javaClosureScratch) pushMarks() *javaClosureMarks {
	if s.depth == len(s.marks) {
		s.marks = append(s.marks, &javaClosureMarks{})
	}

	m := s.marks[s.depth]

	s.depth++
	m.epoch++

	return m
}

func (s *javaClosureScratch) popMarks() {
	s.depth--
}

func (m *javaClosureMarks) cell(id uint32) *uint64 {
	m.gen.ensureLen(int(id) + 1)

	return &m.gen.s[id]
}

func (m *javaClosureMarks) visit(id uint32) bool {
	c := m.cell(id)

	if uint32(*c>>32) == m.epoch {
		return false
	}

	*c = uint64(m.epoch)<<32 | uint64(uint32(*c))

	return true
}

func (m *javaClosureMarks) addMember(id uint32) {
	c := m.cell(id)

	*c = *c&^(1<<32-1) | uint64(m.epoch)
}

func (m *javaClosureMarks) member(id uint32) bool {
	return int(id) < m.gen.len() && uint32(m.gen.s[id]) == m.epoch
}

func (m *javaClosureMarks) addMembers(c javaClosure) {
	for _, e := range c {
		if !javaClosureIsExcluded(e) {
			m.addMember(javaClosureID(e))
		}
	}
}

type javaDMRules struct {
	lib2Ver  map[string]string
	dm       []string
	excludes []string
	excluded map[string]struct{}
	loose    []string
	memo     TwoBitSet
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

func (r *javaDMRules) addExclude(e string) {
	prefix := strings.TrimSuffix(e, "/")

	if strings.HasSuffix(prefix, "/") {
		r.loose = append(r.loose, e)

		return
	}

	r.excluded[prefix] = struct{}{}
}

func (r *javaDMRules) isExcluded(path string) bool {
	if len(r.excluded) > 0 {
		for p := path; ; {
			if _, ok := r.excluded[p]; ok {
				return true
			}

			i := strings.LastIndexByte(p, '/')

			if i < 0 {
				break
			}

			p = p[:i]
		}
	}

	for _, e := range r.loose {
		if javaPathIsPrefixOf(e, path) {
			return true
		}
	}

	return false
}

func (r *javaDMRules) excludesModule(m *JavaModule) bool {
	if v := r.memo.get(m.id); v != 0 {
		return v == 2
	}

	excluded := r.isExcluded(m.fileName())

	if excluded {
		r.memo.set(m.id, 2)
	} else {
		r.memo.set(m.id, 1)
	}

	return excluded
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
	if len(rules.excluded) == 0 && len(rules.loose) == 0 {
		return nil
	}

	return rules.excludesModule
}

func javaClosureOf(m *JavaModule) javaClosure {
	return m.closure
}

func (ctx *GenCtx) javaExpandTransparent(transparent *JavaModule, rules *javaDMRules) javaClosure {
	s := &ctx.jc
	excludePred := javaClosureExcludePred(rules)

	var pred func(*JavaModule) bool

	if excludePred != nil {
		pred = func(m *JavaModule) bool {
			return excludePred(m) && !slices.ContainsFunc(transparent.direct, func(d javaResolvedPeer) bool { return d.m == m })
		}
	}

	s.begin()

	for _, d := range transparent.direct {
		if !d.m.passPeers {
			s.merge(d.m, nil)

			continue
		}

		s.merge(d.m, s.exclude(d.m.closure, pred, true))
	}

	return s.commit()
}

func (ctx *GenCtx) javaMergeClosure(jm *JavaModule) javaClosure {
	s := &ctx.jc

	var expanded []javaClosure

	for i, d := range jm.direct {
		if !d.m.unit.transparent {
			continue
		}

		if expanded == nil {
			expanded = make([]javaClosure, len(jm.direct))
		}

		expanded[i] = ctx.javaExpandTransparent(d.m, jm.rules)
	}

	excludePred := javaClosureExcludePred(jm.rules)

	s.begin()

	for i, d := range jm.direct {
		if d.m.unit.transparent {
			s.merge(d.m, expanded[i])

			continue
		}

		s.merge(d.m, s.exclude(javaClosureOf(d.m), excludePred, true))
	}

	return s.commit()
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

	marks := ctx.jc.pushMarks()

	defer ctx.jc.popMarks()

	marks.addMembers(jm.closure)

	var queue []javaResolvedPeer

	enqueue := func(peers []javaResolvedPeer) {
		admit := func(peer javaResolvedPeer) bool {
			if !marks.visit(peer.m.id) || !marks.member(peer.m.id) {
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
			marks.visit(resolved.id)

			if resolved.closureReady {
				marks.addMembers(ctx.jc.exclude(resolved.closure, excludePred, false))
				marks.addMember(resolved.id)
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
