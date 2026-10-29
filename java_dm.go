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

type javaPeerSlot struct {
	visited uint32
	member  uint32
	acc     uint32
	seen    uint32
	target  uint32
}

type javaLibSlot struct {
	epoch uint32
	m     uint32
	kind  javaPeerResolution
}

type javaResolveLevel struct {
	epoch uint32
	peers Vec[javaPeerSlot]
	libs  Vec[javaLibSlot]
	queue []javaResolvedPeer
	stack [][]javaResolvedPeer
	order []*JavaModule
}

type javaClosureScratch struct {
	byID      []*JavaModule
	slot      Vec[uint64]
	slotEpoch uint32
	buf       []uint64
	excl      Vec[uint64]
	exclEpoch uint32
	exBuf     []uint64
	libIDs    map[string]uint32
	levels    []*javaResolveLevel
	depth     int
}

func (s *javaClosureScratch) register(jm *JavaModule) {
	jm.id = uint32(len(s.byID))
	s.byID = append(s.byID, jm)

	lib := javaPathParent(jm.dir)
	id, ok := s.libIDs[lib]

	if !ok {
		if s.libIDs == nil {
			s.libIDs = map[string]uint32{}
		}

		id = uint32(len(s.libIDs))
		s.libIDs[lib] = id
	}

	jm.lib = id
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

func (s *javaClosureScratch) pushLevel() *javaResolveLevel {
	if s.depth == len(s.levels) {
		s.levels = append(s.levels, &javaResolveLevel{})
	}

	lv := s.levels[s.depth]

	s.depth++
	lv.epoch++

	return lv
}

func (s *javaClosureScratch) popLevel() {
	s.depth--
}

func (lv *javaResolveLevel) peer(id uint32) *javaPeerSlot {
	if int(id) >= lv.peers.len() {
		lv.peers.ensureLen(int(id) + 1)
	}

	return &lv.peers.s[id]
}

func (lv *javaResolveLevel) lib(id uint32) *javaLibSlot {
	if int(id) >= lv.libs.len() {
		lv.libs.ensureLen(int(id) + 1)
	}

	return &lv.libs.s[id]
}

func (lv *javaResolveLevel) addMember(id uint32) {
	lv.peer(id).member = lv.epoch
}

func (lv *javaResolveLevel) addMembers(c javaClosure) {
	for _, e := range c {
		if !javaClosureIsExcluded(e) {
			lv.addMember(javaClosureID(e))
		}
	}
}

func (lv *javaResolveLevel) visit(id uint32) bool {
	p := lv.peer(id)

	if p.visited == lv.epoch {
		return false
	}

	p.visited = lv.epoch

	return true
}

func (lv *javaResolveLevel) resolveTo(conflict, target uint32) {
	if p := lv.peer(conflict); p.acc != lv.epoch {
		p.acc = lv.epoch
		p.target = target
	}
}

type javaDMRules struct {
	lib2Ver  map[string]string
	dm       []ANY
	excludes []ANY
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

type javaConflictResolver struct {
	ctx    *GenCtx
	parent *JavaModule
	rules  *javaDMRules
	lv     *javaResolveLevel
}

func (r *javaConflictResolver) accept(conflict, target *JavaModule, managed bool) *JavaModule {
	lv := r.lv

	lv.resolveTo(conflict.id, target.id)

	t := lv.peer(conflict.id).target

	if managed {
		lv.resolveTo(t, t)
	}

	return r.ctx.jc.byID[t]
}

func (r *javaConflictResolver) resolve(cur javaResolvedPeer, depth int) *JavaModule {
	if cur.resolution == jprUnversioned {
		return r.accept(cur.m, cur.m, false)
	}

	lv := r.lv

	if l := lv.lib(cur.m.lib); l.epoch == lv.epoch {
		if l.kind == jprManaged {
			lv.resolveTo(cur.m.id, l.m)
		}

		return nil
	}

	*lv.lib(cur.m.lib) = javaLibSlot{epoch: lv.epoch, m: cur.m.id, kind: cur.resolution}

	resolved := r.explicitResolution(cur.m.dir, depth == 1)

	if resolved == nil {
		if cur.resolution == jprManaged {
			lv.lib(cur.m.lib).kind = jprTransitive
		}

		return r.accept(cur.m, cur.m, false)
	}

	*lv.lib(cur.m.lib) = javaLibSlot{epoch: lv.epoch, m: resolved.id, kind: jprManaged}

	return r.accept(cur.m, resolved, true)
}

func (r *javaConflictResolver) explicitResolution(peerDir string, isDirect bool) *JavaModule {
	rule := r.rules.ruleForPeer(peerDir, isDirect)

	if rule == "" {
		return nil
	}

	return r.ctx.javaContrib(r.parent, rule)
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

func (ctx *GenCtx) javaResolveConflicts(jm *JavaModule, lv *javaResolveLevel) {
	if len(jm.direct) == 0 {
		return
	}

	resolver := javaConflictResolver{ctx: ctx, parent: jm, rules: jm.rules, lv: lv}

	lv.addMembers(jm.closure)

	queue := lv.queue[:0]

	admit := func(peer javaResolvedPeer) bool {
		if !lv.visit(peer.m.id) || lv.peer(peer.m.id).member != lv.epoch {
			return false
		}

		queue = append(queue, peer)

		return true
	}

	enqueue := func(peers []javaResolvedPeer) {
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
			lv.visit(resolved.id)

			if resolved.closureReady {
				lv.addMembers(ctx.jc.exclude(resolved.closure, excludePred, false))
				lv.addMember(resolved.id)
			}
		}

		enqueue(resolved.direct)
	}

	lv.queue = queue[:0]
}

func (lv *javaResolveLevel) preorder(byID []*JavaModule, direct []javaResolvedPeer, skipTransitiveImpl bool) []*JavaModule {
	res := lv.order[:0]
	stack := append(lv.stack[:0], direct)

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

		p := lv.peer(cur.m.id)

		if p.acc != lv.epoch {
			continue
		}

		t := p.target

		if tp := lv.peer(t); tp.seen == lv.epoch {
			continue
		} else {
			tp.seen = lv.epoch
		}

		target := byID[t]

		res = append(res, target)

		if len(target.direct) > 0 {
			stack = append(stack, target.direct)
		}
	}

	lv.stack = stack[:0]
	lv.order = res

	return res
}

func (ctx *GenCtx) javaManagedPeersClosure(jm *JavaModule) []JavaClosureEntry {
	if jm.managedDone {
		return jm.managed
	}

	jm.managedDone = true

	lv := ctx.jc.pushLevel()

	defer ctx.jc.popLevel()

	ctx.javaResolveConflicts(jm, lv)

	sorted := lv.preorder(ctx.jc.byID, jm.direct, jm.unit.compileView)
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
