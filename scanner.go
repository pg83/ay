package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"unsafe"
)

var (
	scannerStatsEnabled = os.Getenv("SCANNER_STATS") != ""
	perfStatsEnabled    = os.Getenv("YATOOL_PERF_STATS") != ""
)

type includeKind int

const (
	includeSystem includeKind = iota
	includeQuoted
)

type includeDirective struct {
	kind   includeKind
	next   bool
	target STR
}

type IncludeScanner struct {
	sysincl SysInclSet

	parsers *includeParserManager

	anySrcView PerSourceView

	sourceClassCache map[string]uint32

	sourceClassViews map[uint32]PerSourceView

	sourceClassBuckets map[uint64][]uint32
	nextSourceClass    uint32
	sourceKeyedCount   int

	sysinclKeyBits []bool
	sysinclKeyCI   map[string]bool

	includerClassCache   map[string]uint32
	includerClassRecords map[uint32][]*SysIncl
	includerClassBuckets map[uint64][]uint32
	nextIncluderClass    uint32

	sysinclSourceCache map[sysinclSourceKey]sysinclCacheEntry

	sysinclIncluderCache map[sysinclIncluderKey]sysinclCacheEntry

	subgraphChunks [][]uint32
	subgraphLen    uint32
	// subgraphCache (cached transitive closure under DFS) and childrenCache
	// (cached immediate resolved children) are both keyed by includer absID
	// ONLY — no scan-context component. This is the same invariant upstream
	// ymake exploits: each File node is parsed-and-resolved exactly once across
	// the whole add-iter (see TUpdEntryStats::OnceProcessedAsFile in
	// yatool/devtools/ymake/add_iter.h:377, gate in
	// add_iter.cpp:671 and the set in add_iter.cpp:548,681). Upstream's
	// TParsersCache (include_processors/parsers_cache.h) likewise keys parse
	// results by (parserId, fileId) with no module context, and its per-module
	// TResolveCaches (resolver/resolve_cache.h) only dedupes resolves WITHIN a
	// single module's visit — not across modules, because cross-module reentry
	// is blocked by OnceProcessedAsFile.
	//
	// Do NOT add a scanCtx/hashScanContext component to these keys "for
	// safety" — the load-bearing assumption is that the first scanner to reach
	// a file is its semantic owner, the resolution is stable thereafter, and
	// the closure stays valid for every subsequent context. Adding a context
	// key collapses subgraph caching and regresses wall-time by an order of
	// magnitude. If you suspect divergence is caused here, fix it upstream of
	// the cache: parsedIncludes, sysincl rules, or searchTier construction.
	subgraphCache map[uint32]closureRef
	childrenCache map[uint32][]uint32

	searchTierByConfig map[uint64]map[STR]searchTierResult

	resolveIndexByConfig map[uint64]*cfgResolveIndex

	tj        tarjanScratch
	tjStack   []uint32
	tjNext    int32
	tjClosure idSet
	tjBuf     []uint32

	visitedIDPool sync.Pool
	orderIDPool   sync.Pool

	seenPool sync.Pool

	onWarn func(Warn)

	// generatedFirstClaim records the first scan-context module path that
	// resolved an include directive to a CodegenRegistry output. This mirrors
	// upstream ymake's Node2Module rule (devtools/ymake/json_visitor.cpp:638
	// — Node2Module gets set on first DFS leave by FindModule on the visitor
	// stack), specifically applied to generated headers whose producer's
	// `module_dir` would otherwise be the RUN_PROGRAM-owner module. Used by
	// the finalize pass in attribute_generated.go to override producer-node
	// target_properties.
	generatedFirstClaim map[VFS]string

	walkClosureCalls       uint64
	dfsCalls               uint64
	plainDfsCalls          uint64
	subgraphHits           uint64
	subgraphMisses         uint64
	subgraphTainted        uint64
	searchTierHits         uint64
	searchTierMisses       uint64
	resolveSearchPathCalls uint64
	sysinclSourceHits      uint64
	sysinclSourceMisses    uint64
	sysinclIncluderHits    uint64
	sysinclIncluderMisses  uint64
	statsCallCount         uint64

	codegen *CodegenRegistry

	fallbackLocators []pathLocator
}

type scanCtx struct {
	scanner         *IncludeScanner
	cfg             ScanContext
	searchTierCache map[STR]searchTierResult
	resolveIndex    *cfgResolveIndex
}

type idSet struct {
	gen   []uint32
	epoch uint32
}

const closureChunkLen = 1 << 18

type closureRef struct {
	off uint32
	n   uint32
}

func (s *idSet) reset(size uint32) {
	if uint32(len(s.gen)) < size {
		grown := uint32(len(s.gen)) * 2
		if grown < size {
			grown = size
		}

		s.gen = make([]uint32, grown)
		s.epoch = 1

		return
	}

	s.epoch++
	if s.epoch == 0 {
		for i := range s.gen {
			s.gen[i] = 0
		}

		s.epoch = 1
	}
}

func (s *idSet) has(id uint32) bool {
	return id < uint32(len(s.gen)) && s.gen[id] == s.epoch
}

func (s *idSet) add(id uint32) {
	if id >= uint32(len(s.gen)) {
		grown := uint32(len(s.gen)) * 2
		if grown <= id {
			grown = id + 1
		}

		g := make([]uint32, grown)
		copy(g, s.gen)
		s.gen = g
	}

	s.gen[id] = s.epoch
}

type tarjanScratch struct {
	stamp   []uint32
	index   []int32
	low     []int32
	onStack []bool
	epoch   uint32
}

func (t *tarjanScratch) reset(size uint32) {
	if uint32(len(t.stamp)) < size {
		grown := uint32(len(t.stamp)) * 2
		if grown < size {
			grown = size
		}

		t.stamp = make([]uint32, grown)
		t.index = make([]int32, grown)
		t.low = make([]int32, grown)
		t.onStack = make([]bool, grown)
		t.epoch = 1

		return
	}

	t.epoch++
	if t.epoch == 0 {
		for i := range t.stamp {
			t.stamp[i] = 0
		}

		t.epoch = 1
	}
}

func (t *tarjanScratch) visited(id uint32) bool {
	return id < uint32(len(t.stamp)) && t.stamp[id] == t.epoch
}

func (t *tarjanScratch) discover(id uint32, idx int32) {
	if id >= uint32(len(t.stamp)) {
		grown := uint32(len(t.stamp)) * 2
		if grown <= id {
			grown = id + 1
		}

		stamp := make([]uint32, grown)
		copy(stamp, t.stamp)
		t.stamp = stamp
		index := make([]int32, grown)
		copy(index, t.index)
		t.index = index
		low := make([]int32, grown)
		copy(low, t.low)
		t.low = low
		onStack := make([]bool, grown)
		copy(onStack, t.onStack)
		t.onStack = onStack
	}

	t.stamp[id] = t.epoch
	t.index[id] = idx
	t.low[id] = idx
	t.onStack[id] = true
}

func (t *tarjanScratch) onStackHas(id uint32) bool {
	return t.visited(id) && t.onStack[id]
}

type searchTierResult struct {
	paths []VFS
	found bool
}

type sysinclSourceKey struct {
	sourceClass uint32
	target      STR
}

type sysinclIncluderKey struct {
	class  uint32
	target STR
}

type sysinclCacheEntry struct {
	paths          []VFS
	hasMultiTarget bool
	claimed        bool
}

type scannerPerfStats struct {
	walkClosureCalls       uint64
	dfsCalls               uint64
	plainDfsCalls          uint64
	subgraphHits           uint64
	subgraphMisses         uint64
	subgraphTainted        uint64
	searchTierHits         uint64
	searchTierMisses       uint64
	resolveSearchPathCalls uint64
	sysinclSourceHits      uint64
	sysinclSourceMisses    uint64
	sysinclIncluderHits    uint64
	sysinclIncluderMisses  uint64
}

func NewIncludeScanner(sourceRoot string, sysincl SysInclSet) *IncludeScanner {
	return newIncludeScannerWith(newIncludeParserManager(sourceRoot), sysincl, func(Warn) {})
}

func newIncludeScannerWith(parsers *includeParserManager, sysincl SysInclSet, onWarn func(Warn)) *IncludeScanner {

	s := &IncludeScanner{
		sysincl:              sysincl,
		parsers:              parsers,
		generatedFirstClaim:  make(map[VFS]string, 2048),
		sourceClassCache:     make(map[string]uint32, 1024),
		sourceClassViews:     make(map[uint32]PerSourceView, 1024),
		sourceClassBuckets:   make(map[uint64][]uint32, 1024),
		includerClassCache:   make(map[string]uint32, 1024),
		includerClassRecords: make(map[uint32][]*SysIncl, 256),
		includerClassBuckets: make(map[uint64][]uint32, 256),
		sysinclSourceCache:   make(map[sysinclSourceKey]sysinclCacheEntry, 131072),
		sysinclIncluderCache: make(map[sysinclIncluderKey]sysinclCacheEntry, 8192),
		onWarn:               onWarn,
		subgraphChunks:       make([][]uint32, 0, 256),
		subgraphCache:        make(map[uint32]closureRef, 65536),
		childrenCache:        make(map[uint32][]uint32, 65536),
		searchTierByConfig:   make(map[uint64]map[STR]searchTierResult, 1024),
		resolveIndexByConfig: make(map[uint64]*cfgResolveIndex, 1024),
	}
	for i := range sysincl {
		if sysincl[i].KeyBySource {
			s.sourceKeyedCount++
		}
	}

	var csKeyIDs []STR
	for i := range sysincl {
		rec := &sysincl[i]
		for k := range rec.Mappings {
			if rec.CaseInsensitive {
				if s.sysinclKeyCI == nil {
					s.sysinclKeyCI = make(map[string]bool, len(rec.Mappings))
				}
				s.sysinclKeyCI[k] = true
			} else {
				csKeyIDs = append(csKeyIDs, internString(k))
			}
		}
	}
	s.sysinclKeyBits = make([]bool, internBound())
	for _, id := range csKeyIDs {
		s.sysinclKeyBits[id] = true
	}

	s.anySrcView = s.sysincl.PreparePerSource("")

	s.visitedIDPool.New = func() any {
		return &idSet{}
	}

	s.orderIDPool.New = func() any {
		o := make([]uint32, 0, 64)

		return &o
	}

	s.seenPool.New = func() any {
		m := make(map[string]struct{}, 8)

		return &m
	}

	return s
}

type ScanContext struct {
	SourceRel       string
	OwnAddIncl      []VFS
	PeerAddInclSet  []VFS
	BaseSearchPaths []VFS
	// OwnerModuleDir identifies the consumer module whose CC compile (or
	// equivalent) triggered this scan. Used to populate
	// IncludeScanner.generatedFirstClaim on the first resolve of any
	// CodegenRegistry output — see that field's comment for the rationale.
	OwnerModuleDir string
}

func (s *IncludeScanner) NewScanCtx(cfg ScanContext) *scanCtx {
	ctxHash := hashScanContext(&cfg)
	searchTier := s.searchTierByConfig[ctxHash]
	if searchTier == nil {
		searchTier = make(map[STR]searchTierResult, 256)
		s.searchTierByConfig[ctxHash] = searchTier
	}

	ri := s.resolveIndexByConfig[ctxHash]
	if ri == nil {
		ri = buildCfgResolveIndex(&cfg)
		s.resolveIndexByConfig[ctxHash] = ri

		if ri.indexable {
			for _, p := range cfg.OwnAddIncl {
				s.parsers.indexAddincl(p)
			}
			for _, p := range cfg.PeerAddInclSet {
				s.parsers.indexAddincl(p)
			}
		}
	}

	return &scanCtx{
		scanner:         s,
		cfg:             cfg,
		searchTierCache: searchTier,
		resolveIndex:    ri,
	}
}

func hashScanContext(ctx *ScanContext) uint64 {
	const (
		offset uint64 = 1469598103934665603
		prime  uint64 = 1099511628211
	)

	h := offset

	mix := func(s string) {
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= prime
		}

		h ^= 0xff
		h *= prime
	}

	mixSlice := func(ss []VFS) {
		for _, v := range ss {
			h ^= uint64(v.Root())
			h *= prime
			mix(v.Rel())
		}

		h ^= 0xfe
		h *= prime
	}

	mixSlice(ctx.OwnAddIncl)
	mixSlice(ctx.PeerAddInclSet)
	mixSlice(ctx.BaseSearchPaths)

	return h
}

func (s *IncludeScanner) WalkClosure(cfg ScanContext) []VFS {
	return s.NewScanCtx(cfg).WalkSource(cfg.SourceRel)
}

func (sc *scanCtx) WalkSource(sourceRel string) []VFS {
	return sc.WalkClosure(Source(sourceRel))[1:]
}

func (sc *scanCtx) WalkClosure(vfsPath VFS) []VFS {
	s := sc.scanner
	s.walkClosureCalls++

	if vfsPath.IsSource() {
		sc.cfg.SourceRel = vfsPath.Rel()
	}

	visited := s.visitedIDPool.Get().(*idSet)
	visited.reset(vfsBound())
	orderP := s.orderIDPool.Get().(*[]uint32)

	order := (*orderP)[:0]
	rootID := uint32(vfsPath)

	sc.dfsID(rootID, visited, &order)

	out := make([]VFS, 0, len(order))
	out = append(out, VFS(rootID))

	for _, absID := range order {

		if absID == rootID {
			continue
		}

		out = append(out, VFS(absID))
	}

	*orderP = order[:0]

	s.visitedIDPool.Put(visited)
	s.orderIDPool.Put(orderP)

	if scannerStatsEnabled {
		s.statsCallCount++

		if s.statsCallCount%500 == 0 {
			fmt.Fprintf(os.Stderr, "scanner-stats[%d]: subgraph hits=%d misses=%d tainted=%d cache=%d\n", s.statsCallCount, s.subgraphHits, s.subgraphMisses, s.subgraphTainted, len(s.subgraphCache))
		}
	}

	return out
}

func (s *IncludeScanner) IncludeDirectiveTargets(vfsPath VFS) []string {
	entries := s.parsers.parsedIncludes(vfsPath)
	if len(entries) == 0 {
		return nil
	}

	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.target.String())
	}
	return out
}

func (s *IncludeScanner) emittedRel(abs string) string {
	return abs
}

func recordSliceSignature(active []*SysIncl) uint64 {
	const (
		offset uint64 = 1469598103934665603
		prime  uint64 = 1099511628211
	)

	h := offset

	for _, rec := range active {
		addr := uintptr(unsafe.Pointer(rec))

		for i := 0; i < 8; i++ {
			h ^= uint64(byte(addr >> (i * 8)))
			h *= prime
		}
	}

	h ^= 0xfd
	h *= prime

	return h
}

func (s *IncludeScanner) sourceClass(sourceRel string) (uint32, PerSourceView) {
	if id, ok := s.sourceClassCache[sourceRel]; ok {
		return id, s.sourceClassViews[id]
	}

	view := s.prepareSourceView(sourceRel)
	sig := recordSliceSignature(view.activeSourceKeyed)

	for _, id := range s.sourceClassBuckets[sig] {
		cached := s.sourceClassViews[id]
		if sameRecordSlice(cached.activeSourceKeyed, view.activeSourceKeyed) {
			s.sourceClassCache[sourceRel] = id

			return id, cached
		}
	}

	s.nextSourceClass++
	id := s.nextSourceClass
	s.sourceClassCache[sourceRel] = id
	s.sourceClassViews[id] = view
	s.sourceClassBuckets[sig] = append(s.sourceClassBuckets[sig], id)

	return id, view
}

func (s *IncludeScanner) includerClass(includerPath string) (uint32, []*SysIncl) {
	if id, ok := s.includerClassCache[includerPath]; ok {
		return id, s.includerClassRecords[id]
	}

	active := s.anySrcView.computeActiveIncluderRecords(includerPath)
	sig := recordSliceSignature(active)

	for _, id := range s.includerClassBuckets[sig] {
		if sameRecordSlice(s.includerClassRecords[id], active) {
			s.includerClassCache[includerPath] = id

			return id, s.includerClassRecords[id]
		}
	}

	s.nextIncluderClass++
	id := s.nextIncluderClass
	s.includerClassCache[includerPath] = id
	s.includerClassRecords[id] = active
	s.includerClassBuckets[sig] = append(s.includerClassBuckets[sig], id)

	return id, active
}

func (s *IncludeScanner) prepareSourceView(sourceRel string) PerSourceView {
	view := PerSourceView{
		activeSourceKeyed: make([]*SysIncl, 0, s.sourceKeyedCount),
	}

	for i := range s.sysincl {
		rec := &s.sysincl[i]

		if !rec.KeyBySource {
			continue
		}

		if rec.Filter == nil || rec.Filter.match(sourceRel) {
			view.activeSourceKeyed = append(view.activeSourceKeyed, rec)
		}
	}

	return view
}

func sameRecordSlice(a, b []*SysIncl) bool {
	if len(a) != len(b) {
		return false
	}

	for i, rec := range a {
		if rec != b[i] {
			return false
		}
	}

	return true
}

func (sc *scanCtx) dfsID(absID uint32, visited *idSet, order *[]uint32) {
	sc.scanner.dfsCalls++

	if visited.has(absID) {
		return
	}

	absPath := VFS(absID)

	if isSourceLike(absPath) {
		sc.plainDfsID(absID, visited, order)

		return
	}

	for _, id := range sc.closureOf(absID) {
		if visited.has(id) {
			continue
		}

		visited.add(id)
		*order = append(*order, id)
	}
}

func (sc *scanCtx) plainDfsID(absID uint32, visited *idSet, order *[]uint32) {
	sc.scanner.plainDfsCalls++

	if visited.has(absID) {
		return
	}

	visited.add(absID)
	*order = append(*order, absID)

	sc.forEachResolvedChildID(absID, func(childID uint32) {
		sc.dfsID(childID, visited, order)
	})
}

func (sc *scanCtx) forEachResolvedChild(vfsPath VFS, fn func(rabs VFS)) {
	s := sc.scanner

	for _, entry := range s.parsers.parsedIncludes(vfsPath) {
		resolved := sc.resolve(vfsPath, entry)
		for _, rabs := range resolved {
			fn(rabs)
		}
	}
}

// forEachResolvedChildID returns the resolved immediate children of absID,
// caching by absID alone (no scan-context key). See the comment above
// IncludeScanner.subgraphCache / childrenCache for the upstream-mirroring
// invariant that makes this correct: each file is parse-and-resolved exactly
// once per run.
func (sc *scanCtx) forEachResolvedChildID(absID uint32, fn func(uint32)) {
	s := sc.scanner
	if cached, ok := s.childrenCache[absID]; ok {
		for _, id := range cached {
			fn(id)
		}

		return
	}

	vfsPath := VFS(absID)
	var children []uint32
	sc.forEachResolvedChild(vfsPath, func(rabs VFS) {
		children = append(children, uint32(rabs))
	})
	s.childrenCache[absID] = children

	for _, id := range children {
		fn(id)
	}
}

func (s *IncludeScanner) SubgraphCacheStats() (hits, misses, tainted uint64) {
	return s.subgraphHits, s.subgraphMisses, s.subgraphTainted
}

func (s *IncludeScanner) perfStats() scannerPerfStats {
	return scannerPerfStats{
		walkClosureCalls:       s.walkClosureCalls,
		dfsCalls:               s.dfsCalls,
		plainDfsCalls:          s.plainDfsCalls,
		subgraphHits:           s.subgraphHits,
		subgraphMisses:         s.subgraphMisses,
		subgraphTainted:        s.subgraphTainted,
		searchTierHits:         s.searchTierHits,
		searchTierMisses:       s.searchTierMisses,
		resolveSearchPathCalls: s.resolveSearchPathCalls,
		sysinclSourceHits:      s.sysinclSourceHits,
		sysinclSourceMisses:    s.sysinclSourceMisses,
		sysinclIncluderHits:    s.sysinclIncluderHits,
		sysinclIncluderMisses:  s.sysinclIncluderMisses,
	}
}

func (sc *scanCtx) closureOf(absID uint32) []uint32 {
	s := sc.scanner
	if ref, ok := s.subgraphCache[absID]; ok {
		s.subgraphHits++

		return s.closureWindow(ref)
	}

	s.tj.reset(vfsBound())
	s.tjStack = s.tjStack[:0]
	s.tjNext = 0

	sc.strongconnect(absID)

	ref := s.subgraphCache[absID]

	return s.closureWindow(ref)
}

func (s *IncludeScanner) closureWindow(ref closureRef) []uint32 {
	o := ref.off % closureChunkLen

	return s.subgraphChunks[ref.off/closureChunkLen][o : o+ref.n]
}

func (s *IncludeScanner) appendClosure(buf []uint32) closureRef {
	n := uint32(len(buf))
	o := s.subgraphLen % closureChunkLen
	if o+n > closureChunkLen {
		s.subgraphLen += closureChunkLen - o
		o = 0
	}

	ci := s.subgraphLen / closureChunkLen
	for uint32(len(s.subgraphChunks)) <= ci {
		s.subgraphChunks = append(s.subgraphChunks, make([]uint32, closureChunkLen))
	}
	copy(s.subgraphChunks[ci][o:], buf)

	ref := closureRef{off: s.subgraphLen, n: n}
	s.subgraphLen += n

	return ref
}

func (sc *scanCtx) strongconnect(v uint32) {
	s := sc.scanner

	s.tjNext++
	s.tj.discover(v, s.tjNext)
	s.tjStack = append(s.tjStack, v)

	sc.forEachResolvedChildID(v, func(w uint32) {
		if _, cached := s.subgraphCache[w]; cached {
			s.subgraphHits++

			return
		}

		if !s.tj.visited(w) {
			sc.strongconnect(w)
			if s.tj.low[w] < s.tj.low[v] {
				s.tj.low[v] = s.tj.low[w]
			}
		} else if s.tj.onStack[w] {
			if s.tj.index[w] < s.tj.low[v] {
				s.tj.low[v] = s.tj.index[w]
			}
		}
	})

	if s.tj.low[v] != s.tj.index[v] {
		return
	}

	sccStart := len(s.tjStack) - 1
	for s.tjStack[sccStart] != v {
		sccStart--
	}
	members := s.tjStack[sccStart:]

	s.tjClosure.reset(vfsBound())
	buf := s.tjBuf[:0]

	for _, u := range members {
		if !s.tjClosure.has(u) {
			s.tjClosure.add(u)
			buf = append(buf, u)
		}
	}

	for _, u := range members {
		sc.forEachResolvedChildID(u, func(w uint32) {
			if s.tj.onStackHas(w) {
				return
			}

			for _, id := range s.closureWindow(s.subgraphCache[w]) {
				if !s.tjClosure.has(id) {
					s.tjClosure.add(id)
					buf = append(buf, id)
				}
			}
		})
	}

	ref := s.appendClosure(buf)
	s.tjBuf = buf[:0]

	s.subgraphMisses += uint64(len(members))
	if len(members) > 1 {
		s.subgraphTainted++
	}

	for _, u := range members {
		s.subgraphCache[u] = ref
		s.tj.onStack[u] = false
	}

	s.tjStack = s.tjStack[:sccStart]
}

func (sc *scanCtx) resolve(includerAbs VFS, d includeDirective) (out []VFS) {
	s := sc.scanner

	var sysinclClaimed bool
	defer func() {
		if len(out) > 0 || d.next || sysinclClaimed {
			return
		}
		open, close := `<`, `>`
		if d.kind == includeQuoted {
			open, close = `"`, `"`
		}
		s.onWarn(Warn{
			Kind: WarnMissingInclude,
			Message: fmt.Sprintf("%s: unresolved include %s%s%s — not found in source, build, search path, or sysincl",
				includerAbs.String(), open, d.target.String(), close),
		})
	}()

	if d.next {
		return nil
	}

	searchOut := sc.resolveSearchPath(includerAbs, d)

	includerRel := includerAbs.Rel()
	var mappings []VFS
	var hasMultiTarget bool
	mappings, hasMultiTarget, sysinclClaimed = s.sysinclLookup(includerRel, includerRel, d.target)

	if d.kind == includeQuoted && len(searchOut) > 0 {

		bypass := !hasMultiTarget
		if !bypass && searchOut[0].IsSource() {
			incDir := pathDir(includerRel)

			var sameDirRel string

			if incDir != "" {
				sameDirRel = normalisePath(incDir + "/" + d.target.String())
			} else {
				sameDirRel = d.target.String()
			}

			bypass = searchOut[0].Rel() == sameDirRel
		}

		if bypass {
			return searchOut
		}
	}

	if len(mappings) == 0 {
		return searchOut
	}

	if len(searchOut) == 0 {
	fastLoop:
		for _, abs := range mappings {
			for _, q := range out {
				if q == abs {
					continue fastLoop
				}
			}

			if !s.fileExistsByRel(abs.Rel()) {
				continue
			}

			if out == nil {
				out = make([]VFS, 0, len(mappings))
			}

			out = append(out, abs)
		}

		return out
	}

mapLoop:
	for _, abs := range mappings {
		if out != nil {
			for _, q := range out {
				if q == abs {
					continue mapLoop
				}
			}
		} else {
			for _, q := range searchOut {
				if q == abs {
					continue mapLoop
				}
			}
		}

		if !s.fileExistsByRel(abs.Rel()) {
			continue
		}

		if out == nil {
			out = make([]VFS, len(searchOut), len(searchOut)+len(mappings))
			copy(out, searchOut)
		}

		out = append(out, abs)
	}

	if out == nil {
		return searchOut
	}

	return out
}

func (s *IncludeScanner) sysinclLookup(sourceRel, includerRel string, target STR) (paths []VFS, hasMultiTarget, claimed bool) {

	if !s.sysinclMightClaim(target) {
		return nil, false, false
	}

	srcMappings, srcMT, srcClaimed := s.sysinclSourceLookup(sourceRel, target)
	incMappings, incMT, incClaimed := s.sysinclIncluderLookup(includerRel, target)
	claimed = srcClaimed || incClaimed

	switch {
	case len(srcMappings) == 0:
		paths = incMappings
	case len(incMappings) == 0:
		paths = srcMappings
	default:
		out := make([]VFS, 0, len(srcMappings)+len(incMappings))
		out = append(out, srcMappings...)

	incLoop:
		for _, p := range incMappings {
			for _, q := range out {
				if p == q {
					continue incLoop
				}
			}

			out = append(out, p)
		}

		paths = out
	}

	hasMultiTarget = srcMT || incMT || len(paths) >= 2

	return paths, hasMultiTarget, claimed
}

func (s *IncludeScanner) sysinclMightClaim(target STR) bool {
	if int(target) < len(s.sysinclKeyBits) && s.sysinclKeyBits[target] {
		return true
	}

	if len(s.sysinclKeyCI) != 0 {
		return s.sysinclKeyCI[strings.ToLower(target.String())]
	}

	return false
}

func (s *IncludeScanner) sysinclSourceLookup(sourceRel string, target STR) ([]VFS, bool, bool) {
	classID, view := s.sourceClass(sourceRel)
	key := sysinclSourceKey{
		sourceClass: classID,
		target:      target,
	}

	if cached, ok := s.sysinclSourceCache[key]; ok {
		s.sysinclSourceHits++
		return cached.paths, cached.hasMultiTarget, cached.claimed
	}

	s.sysinclSourceMisses++

	rels, claimed, hasMultiTarget := view.LookupSourceKeyed(target.String())

	entry := sysinclCacheEntry{
		paths:          s.absifyRels(rels),
		hasMultiTarget: hasMultiTarget,
		claimed:        claimed,
	}
	s.sysinclSourceCache[key] = entry

	return entry.paths, entry.hasMultiTarget, entry.claimed
}

func (s *IncludeScanner) sysinclIncluderLookup(includerRel string, target STR) ([]VFS, bool, bool) {

	classID, active := s.includerClass(includerRel)
	key := sysinclIncluderKey{
		class:  classID,
		target: target,
	}

	if cached, ok := s.sysinclIncluderCache[key]; ok {
		s.sysinclIncluderHits++
		return cached.paths, cached.hasMultiTarget, cached.claimed
	}

	s.sysinclIncluderMisses++

	rels, claimed, hasMultiTarget := unionIncluderMappings(active, target.String())

	entry := sysinclCacheEntry{
		paths:          s.absifyRels(rels),
		hasMultiTarget: hasMultiTarget,
		claimed:        claimed,
	}
	s.sysinclIncluderCache[key] = entry

	return entry.paths, entry.hasMultiTarget, entry.claimed
}

func (s *IncludeScanner) absifyRels(rels []string) []VFS {
	if len(rels) == 0 {
		return nil
	}

	out := make([]VFS, 0, len(rels))

	for _, rel := range rels {
		out = append(out, Source(normalisePath(rel)))
	}

	return out
}

type cfgResolveIndex struct {
	indexable    bool
	rank         map[VFS]int
	buildEntries []cfgBuildAddincl
}

// cfgBuildAddincl is a Build-rooted addincl prefix paired with its rank in the
// unified declaration order across OwnAddIncl ⨁ PeerAddInclSet. prefixID is
// pre-interned so codegen.LookupSplit needs no per-resolve allocation.
type cfgBuildAddincl struct {
	prefix   VFS
	prefixID STR
	rank     int
}

const resolveNoRank = int(^uint(0) >> 1)

// buildCfgResolveIndex assigns a single declaration-order rank to every addincl
// entry (Source and Build both), so the fast path can pick the first-wins
// match the way upstream's ResolveName(MakeResolvePlan(MakeIterPair(incDirs)))
// does (devtools/ymake/module_resolver.cpp:371). Source entries keep their
// existing inverted-index lookup; Build entries are collected separately for a
// cheap codegen.LookupSplit pass over a typically tiny set (0–2 per module).
func buildCfgResolveIndex(cfg *ScanContext) *cfgResolveIndex {
	idx := &cfgResolveIndex{}

	for _, p := range cfg.OwnAddIncl {
		if p.Root() == VFSRootSource && p.Rel() == "" {
			return idx
		}
	}
	for _, p := range cfg.PeerAddInclSet {
		if p.Root() == VFSRootSource && p.Rel() == "" {
			return idx
		}
	}

	idx.indexable = true
	idx.rank = make(map[VFS]int, len(cfg.OwnAddIncl)+len(cfg.PeerAddInclSet))

	r := 0
	add := func(p VFS) {
		if _, ok := idx.rank[p]; ok {
			return
		}
		idx.rank[p] = r
		r++
		if p.Root() == VFSRootBuild {
			idx.buildEntries = append(idx.buildEntries, cfgBuildAddincl{
				prefix:   p,
				prefixID: internString(p.Rel()),
				rank:     idx.rank[p],
			})
		}
	}

	for _, p := range cfg.OwnAddIncl {
		add(p)
	}
	for _, p := range cfg.PeerAddInclSet {
		add(p)
	}

	return idx
}

func (sc *scanCtx) resolveContextSearchTier(targetID STR, target string) searchTierResult {
	s := sc.scanner

	if cached, ok := sc.searchTierCache[targetID]; ok {
		s.searchTierHits++
		return cached
	}

	s.searchTierMisses++

	var out searchTierResult

	normTarget := normalisePath(target)

	addSource := func(prefixRel string) bool {
		rel, ok := s.resolveSourceUnder(prefixRel, target)
		if !ok {
			return false
		}

		out.paths = []VFS{Source(rel)}
		out.found = true

		return true
	}

	var buildSuffix *STR
	if s.codegen != nil {
		buildSuffix = interned(normTarget)
	}

	addBuild := func(prefixRel string) bool {
		if buildSuffix == nil {
			return false
		}

		var info *GeneratedFileInfo
		if prefixRel == "" {

			info = s.codegen.LookupRel(normTarget)
		} else if pid := interned(prefixRel); pid != nil {
			info = s.codegen.LookupSplit(*pid, *buildSuffix)
		}

		if info == nil {
			return false
		}

		out.paths = []VFS{info.OutputPath}
		out.found = true

		if sc.cfg.OwnerModuleDir != "" {
			if _, ok := s.generatedFirstClaim[info.OutputPath]; !ok {
				s.generatedFirstClaim[info.OutputPath] = sc.cfg.OwnerModuleDir
			}
		}

		return true
	}

	addInclPath := func(prefix VFS) bool {
		switch prefix.Root() {
		case VFSRootBuild:
			return addBuild(prefix.Rel())
		case VFSRootSource:
			return addSource(prefix.Rel())
		}

		panic("resolveContextSearchTier: zero-valued search path")
	}

	first, _ := firstComponent(target)
	if canRelFilter(first, target) && !strings.Contains(target, "/./") && !strings.Contains(target, "//") {
		idx := sc.resolveIndex
		if idx.indexable {

			// Source side: precomputed FS-existence inverted index keyed by
			// target → addincl prefixes containing it. Pick the smallest rank.
			bestRank := resolveNoRank
			var bestAddincl VFS
			for _, a := range s.parsers.addinclIndex[targetID] {
				if r, ok := idx.rank[a]; ok && r < bestRank {
					bestRank = r
					bestAddincl = a
				}
			}
			bestIsSource := bestRank != resolveNoRank

			// Build side: walk the (typically tiny) set of Build-rooted addincl
			// entries in declaration order via the registry's 2-level split
			// lookup. Take the smallest rank among hits — if it beats the Source
			// best, the Build entry wins, mirroring upstream's first-match
			// semantics over IncDirs (module_resolver.cpp:371).
			var bestBuild *GeneratedFileInfo
			if buildSuffix != nil {
				for i := range idx.buildEntries {
					b := &idx.buildEntries[i]
					if b.rank >= bestRank {
						continue
					}

					info := s.codegen.LookupSplit(b.prefixID, *buildSuffix)
					if info == nil {
						continue
					}

					bestRank = b.rank
					bestBuild = info
					bestIsSource = false
				}
			}

			if bestRank != resolveNoRank {
				if bestIsSource {
					out.paths = []VFS{Source(joinRel(bestAddincl.Rel(), target))}
				} else {
					out.paths = []VFS{bestBuild.OutputPath}
					if sc.cfg.OwnerModuleDir != "" {
						if _, ok := s.generatedFirstClaim[bestBuild.OutputPath]; !ok {
							s.generatedFirstClaim[bestBuild.OutputPath] = sc.cfg.OwnerModuleDir
						}
					}
				}

				out.found = true
				sc.searchTierCache[targetID] = out
				return out
			}

			for _, p := range sc.cfg.BaseSearchPaths {
				if addInclPath(p) {
					sc.searchTierCache[targetID] = out
					return out
				}
			}

			sc.searchTierCache[targetID] = out
			return out
		}
	}

	for _, p := range sc.cfg.OwnAddIncl {
		if addInclPath(p) {
			sc.searchTierCache[targetID] = out
			return out
		}
	}

	for _, p := range sc.cfg.PeerAddInclSet {
		if addInclPath(p) {
			sc.searchTierCache[targetID] = out
			return out
		}
	}

	for _, p := range sc.cfg.BaseSearchPaths {
		if addInclPath(p) {
			sc.searchTierCache[targetID] = out
			return out
		}
	}

	sc.searchTierCache[targetID] = out

	return out
}

func resolveCythonPy2Override(includerAbs VFS, d includeDirective) (string, bool) {
	if !includerAbs.IsSource() || d.kind != includeQuoted {
		return "", false
	}

	switch includerAbs.Rel() {
	case "util/generic/string.pxd":
		if d.target.String() == "libcpp/string.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/libcpp/string.pxd", true
		}
	case "util/generic/hash.pxd":
		if d.target.String() == "libcpp/pair.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/libcpp/pair.pxd", true
		}
	case "util/system/types.pxd":
		if d.target.String() == "libc/stdint.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/libc/stdint.pxd", true
		}
	}

	if strings.HasPrefix(includerAbs.Rel(), "contrib/tools/cython_py2/Cython/Includes/") {
		switch d.target.String() {
		case "libc/string.pxd", "libcpp/string.pxd", "libcpp/pair.pxd", "libcpp/utility.pxd":
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.String(), true
		}
	}

	return "", false
}

func (sc *scanCtx) resolveSearchPath(includerAbs VFS, d includeDirective) []VFS {
	s := sc.scanner
	s.resolveSearchPathCalls++

	var out []VFS

	seenP := s.seenPool.Get().(*map[string]struct{})
	seen := *seenP

	addPath := func(rel string) bool {

		rel = normalisePath(rel)

		if _, dup := seen[rel]; dup {
			return false
		}

		if !s.fileExistsByRel(rel) {
			return false
		}

		seen[rel] = struct{}{}

		out = append(out, Source(rel))

		return true
	}

	addBuildPath := func(rel string) bool {
		rel = normalisePath(rel)

		if s.codegen == nil {
			return false
		}

		info := s.codegen.LookupRel(rel)
		if info == nil {
			return false
		}

		dedupKey := "B:" + rel
		if _, dup := seen[dedupKey]; dup {
			return false
		}

		seen[dedupKey] = struct{}{}
		out = append(out, info.OutputPath)

		return true
	}

	searchPathFound := false

	var buildRootFallbackRel string

	if candidate, ok := cythonPy2SiblingOverride(includerAbs, d); ok && addPath(candidate) {
		searchPathFound = true
	}

	if includerAbs.IsBuild() && strings.Contains(d.target.String(), "/") {
		rel := normalisePath(d.target.String())

		if addBuildPath(rel) {
			searchPathFound = true
			if sc.cfg.OwnerModuleDir != "" && s.codegen != nil {
				if info := s.codegen.LookupRel(rel); info != nil {
					if _, ok := s.generatedFirstClaim[info.OutputPath]; !ok {
						s.generatedFirstClaim[info.OutputPath] = sc.cfg.OwnerModuleDir
					}
				}
			}
		} else {
			buildRootFallbackRel = rel
		}
	}

	if candidate, ok := resolveCythonPy2Override(includerAbs, d); ok && addPath(candidate) {
		searchPathFound = true
	}

	if d.kind == includeQuoted {

		incDir := pathDir(includerAbs.Rel())

		matched := false
		if rel, ok := s.resolveSourceUnder(incDir, d.target.String()); ok {
			if _, dup := seen[rel]; !dup {
				seen[rel] = struct{}{}
				out = append(out, Source(rel))
				searchPathFound = true
				matched = true
			}
		}

		if !matched {
			if info := s.codegenUnder(incDir, d.target.String()); info != nil {
				dedupKey := "B:" + info.OutputPath.Rel()
				if _, dup := seen[dedupKey]; !dup {
					seen[dedupKey] = struct{}{}
					out = append(out, info.OutputPath)
					searchPathFound = true
				}
				// Mirror resolveContextSearchTier's addBuild: when a
				// quoted include resolves to a generated path via the
				// includer-dir codegenUnder branch (e.g. X86CallingConv.cpp
				// → X86GenCallingConv.inc), record the first consumer
				// module so the attribute_generated.go finalize pass can
				// re-attribute the .inc node's target_properties.module_dir.
				if sc.cfg.OwnerModuleDir != "" {
					if _, ok := s.generatedFirstClaim[info.OutputPath]; !ok {
						s.generatedFirstClaim[info.OutputPath] = sc.cfg.OwnerModuleDir
					}
				}
			}
		}
	}

	if !searchPathFound {
		tier := sc.resolveContextSearchTier(d.target, d.target.String())
		if tier.found {
			out = append(out, tier.paths...)
			searchPathFound = true
		}
	}

	if !searchPathFound && len(s.fallbackLocators) > 0 {

		abs := Build(d.target.String())

		for _, loc := range s.fallbackLocators {
			if !loc.Exists(abs) {
				continue
			}

			dedupKey := "B:" + d.target.String()
			if _, dup := seen[dedupKey]; !dup {
				seen[dedupKey] = struct{}{}
				out = append(out, abs)
			}

			break
		}
	}

	if !searchPathFound && buildRootFallbackRel != "" {
		if _, dup := seen[buildRootFallbackRel]; !dup {
			seen[buildRootFallbackRel] = struct{}{}
			out = append(out, Source(buildRootFallbackRel))
		}
		searchPathFound = true
	}

	clear(seen)
	s.seenPool.Put(seenP)

	return out
}

func cythonPy2SiblingOverride(includerAbs VFS, d includeDirective) (string, bool) {
	if !includerAbs.IsSource() || d.kind != includeQuoted {
		return "", false
	}

	if hasPrefix(includerAbs.Rel(), "contrib/tools/cython_py2/Cython/Includes/") {
		if hasPrefix(d.target.String(), "libc/") || hasPrefix(d.target.String(), "libcpp/") {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.String(), true
		}

		return "", false
	}

	switch includerAbs.Rel() {
	case "util/generic/string.pxd":
		if d.target.String() == "libcpp/string.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.String(), true
		}
	case "util/generic/hash.pxd", "util/generic/hash_set.pxd":
		if d.target.String() == "libcpp/pair.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.String(), true
		}
	case "util/system/types.pxd":
		if d.target.String() == "libc/stdint.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.String(), true
		}
	}

	return "", false
}

func isSourceLike(absPath VFS) bool {

	rel := absPath.Rel()
	idx := strings.LastIndexByte(rel, '.')

	if idx < 0 {
		return false
	}

	ext := rel[idx:]

	switch ext {
	case ".cpp", ".cc", ".cxx", ".c", ".C", ".S", ".s", ".m", ".mm":
		return true
	}

	return false
}

func pathDir(p string) string {
	idx := strings.LastIndexByte(p, '/')

	if idx < 0 {
		return ""
	}

	return p[:idx]
}

func normalisePath(p string) string {
	if !strings.Contains(p, "..") && !strings.Contains(p, "./") && !strings.Contains(p, "//") {
		return p
	}

	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))

	for _, seg := range parts {
		switch seg {
		case "", ".":

			continue
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, seg)
		}
	}

	return strings.Join(out, "/")
}

type pathLocator interface {
	Exists(vfsPath VFS) bool
}

type fsLocator struct {
	scanner *IncludeScanner
}

func (f fsLocator) Exists(vfsPath VFS) bool {
	if !vfsPath.IsSource() {
		return false
	}

	return f.scanner.fileExistsByRel(vfsPath.Rel())
}

type codegenLocator struct {
	reg *CodegenRegistry
}

func (c codegenLocator) Exists(vfsPath VFS) bool {
	if c.reg == nil {
		return false
	}

	if !vfsPath.IsBuild() {
		return false
	}

	return c.reg.Lookup(vfsPath) != nil
}

func (s *IncludeScanner) fileExistsByRel(rel string) bool {
	return s.parsers.fileExistsByRel(rel)
}

func (s *IncludeScanner) listdir(rel string) map[string]bool {
	return s.parsers.fs.Listdir(rel)
}

func (s *IncludeScanner) resolveSourceUnder(prefixDir, target string) (string, bool) {
	if first, more := firstComponent(target); canRelFilter(first, target) {
		isDir, ok := s.listdir(prefixDir)[first]
		if !ok {
			return "", false
		}

		if !more {
			if isDir {
				return "", false
			}

			return joinRel(prefixDir, target), true
		}

		if !isDir {
			return "", false
		}
	}

	rel := normalisePath(joinRel(prefixDir, target))
	if !s.fileExistsByRel(rel) {
		return "", false
	}

	return rel, true
}

func (s *IncludeScanner) codegenUnder(prefixDir, target string) *GeneratedFileInfo {
	if s.codegen == nil {
		return nil
	}

	if first, _ := firstComponent(target); prefixDir != "" && canRelFilter(first, target) &&
		!strings.Contains(target, "/./") && !strings.Contains(target, "//") {
		pid := interned(prefixDir)
		if pid == nil {
			return nil
		}

		sid := interned(target)
		if sid == nil {
			return nil
		}

		return s.codegen.LookupSplit(*pid, *sid)
	}

	return s.codegen.LookupRel(normalisePath(joinRel(prefixDir, target)))
}

func canRelFilter(first, target string) bool {
	return first != "" && first != "." && first != ".." && !strings.Contains(target, "/..")
}
