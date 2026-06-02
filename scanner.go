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
	// includerActiveScratch is reused by includerClass to compute the active
	// record set for a fresh includer path; copied out only when a new class is
	// created (most paths hit an existing class and discard it).
	includerActiveScratch []*SysIncl

	sysinclSourceCache map[sysinclSourceKey]sysinclCacheEntry

	sysinclIncluderCache map[sysinclIncluderKey]sysinclCacheEntry

	// subgraphClosures holds each cached transitive closure as a slice. The
	// slices are not owned arrays: they are address-stable sub-slices into
	// closureArena (a bump allocator), so storing them costs no copy. closureRef
	// is just an index into this slice.
	subgraphClosures [][]VFS
	closureArena     *bumpAllocator[VFS]
	// subgraphCache (cached transitive closure under DFS) and childrenCache
	// (cached immediate resolved children) are both keyed by includer VFS
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
	subgraphCache map[VFS]closureRef
	childrenCache map[VFS][]VFS

	searchTierByConfig map[uint64]map[STR]searchTierResult

	resolveIndexByConfig map[uint64]*cfgResolveIndex

	// tjc points at the run-wide Tarjan/closure working state owned by genCtx and
	// shared by the target and host scanners (see tarjanCtx).
	tjc *tarjanCtx

	// dfsActive marks the roots whose dfs is currently in flight. It is set-once
	// (never reset): within one scanner a root is cached the moment its dfs
	// finishes, so closureOf re-enters dfs(root) only along an include cycle —
	// which dfs hands to strongconnect. Per-scanner, not shared, so the host
	// scanner does not see target's roots as spurious cycles. A bit set (1 bit/id)
	// rather than an epoch idSet, since membership is permanent and binary.
	dfsActive idBitSet

	visitedIDPool sync.Pool

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

// closureRef is an index into IncludeScanner.subgraphClosures.
type closureRef uint32

const (
	// closureAllocHint is the per-closure reservation passed to the closure
	// arena. A single transitive closure never exceeds this, so the arena always
	// hands back a region large enough to build the closure into without
	// overflow. Derived from the measured sg5 maximum closure size (3935) with a
	// ~2x margin.
	closureAllocHint = 1 << 13 // 8192

	// closureArenaInitial is the first chunk size; the arena grows chunks by
	// 1.5x from there without bound.
	closureArenaInitial = closureAllocHint
)

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

// idSet is keyed by VFS value (the dense array is indexed by uint32(v)).
func (s *idSet) has(v VFS) bool {
	id := uint32(v)

	return id < uint32(len(s.gen)) && s.gen[id] == s.epoch
}

func (s *idSet) add(v VFS) {
	id := uint32(v)

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
	return newIncludeScannerWith(newIncludeParserManager(sourceRoot), sysincl, func(Warn) {}, &tarjanCtx{})
}

func newIncludeScannerWith(parsers *includeParserManager, sysincl SysInclSet, onWarn func(Warn), tjc *tarjanCtx) *IncludeScanner {
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
		subgraphClosures:     make([][]VFS, 0, 256),
		closureArena:         newBumpAllocator[VFS](closureArenaInitial),
		subgraphCache:        make(map[VFS]closureRef, 65536),
		childrenCache:        make(map[VFS][]VFS, 65536),
		searchTierByConfig:   make(map[uint64]map[STR]searchTierResult, 1024),
		resolveIndexByConfig: make(map[uint64]*cfgResolveIndex, 1024),
		tjc:                  tjc,
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

	s.includerActiveScratch = s.anySrcView.computeActiveIncluderRecordsInto(s.includerActiveScratch, includerPath)
	active := s.includerActiveScratch
	sig := recordSliceSignature(active)

	for _, id := range s.includerClassBuckets[sig] {
		if sameRecordSlice(s.includerClassRecords[id], active) {
			s.includerClassCache[includerPath] = id

			return id, s.includerClassRecords[id]
		}
	}

	s.nextIncluderClass++
	id := s.nextIncluderClass
	owned := append([]*SysIncl(nil), active...) // new class: copy out of the scratch
	s.includerClassCache[includerPath] = id
	s.includerClassRecords[id] = owned
	s.includerClassBuckets[sig] = append(s.includerClassBuckets[sig], id)

	return id, owned
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

func (sc *scanCtx) forEachResolvedChild(vfsPath VFS, fn func(rabs VFS)) {
	s := sc.scanner
	incDir := dirKey(pathDir(vfsPath.Rel()))

	for _, entry := range s.parsers.parsedIncludes(vfsPath) {
		resolved := sc.resolve(vfsPath, incDir, entry)

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
func (sc *scanCtx) forEachResolvedChildID(abs VFS, fn func(VFS)) {
	s := sc.scanner

	if cached, ok := s.childrenCache[abs]; ok {
		for _, id := range cached {
			fn(id)
		}

		return
	}

	var children []VFS
	sc.forEachResolvedChild(abs, func(rabs VFS) {
		children = append(children, rabs)
	})
	s.childrenCache[abs] = children

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

// dfs builds the transitive include closure of any root — closureOf routes every
// uncached file here. Most files are acyclic (a node ∪ its children's windows),
// so a flat dfs without Tarjan's SCC bookkeeping suffices: abs leads its own
// closure (element 0), which the [1:] consumers strip, and the children's flat
// windows are spliced in — the same cached closures strongconnect builds.
//
// When the subgraph below abs contains an include cycle, a flat window cannot
// represent the SCC. dfsActive detects it: a cycle re-enters dfs(abs) before abs
// is cached, so the guard hands abs to strongconnect, which collapses the SCC
// (reusing every acyclic subtree dfs already cached). This covers header<->header
// cycles, the arch-conditional .S sibling includes, and self-includes alike.
//
// Two passes are required by the single-pending arena. Pass 1 builds and caches
// every direct child's closure (closureOf there may recurse into strongconnect/
// dfs and allocate from closureArena). Pass 2 reserves our block and splices each
// child's cached window in place — no temporary [][]VFS — and allocates nothing
// else, so holding the uncommitted block is safe. tjClosure is the dedup set,
// shared with strongconnect; dfs's pass-2 use never overlaps a nested call.
func (sc *scanCtx) dfs(abs VFS) {
	s := sc.scanner

	if s.dfsActive.has(abs) {
		s.tjc.scratch.reset(vfsBound())
		s.tjc.stack = s.tjc.stack[:0]
		s.tjc.next = 0

		sc.strongconnect(abs)

		return
	}

	s.dfsActive.add(abs)

	// Pass 1: build and cache each child's closure. closureOf may recurse into
	// dfs/strongconnect and allocate from closureArena, so it must finish before
	// pass 2 reserves our block. Skip the self-edge (a source that #includes
	// itself): abs is not cached yet, so closureOf(abs) would re-enter dfs(abs);
	// abs leads the window below and its self-contribution is a dedup fixpoint.
	sc.forEachResolvedChildID(abs, func(ch VFS) {
		if ch == abs {
			return
		}

		sc.closureOf(ch)
	})

	// Pass 2: every child is cached now, so splice its window straight from the
	// cache — no temporary [][]VFS, and forEachResolvedChildID hits childrenCache
	// so nothing here allocates from closureArena while the block is open. tjClosure
	// is the dedup set (shared with strongconnect): dfs's pass-2 use never overlaps
	// a nested dfs/strongconnect (pass 1 has fully returned), so no pool is needed.
	s.tjc.closure.reset(vfsBound())

	block := s.closureArena.alloc(closureAllocHint)
	k := 0

	s.tjc.closure.add(abs)
	block[k] = abs
	k++

	sc.forEachResolvedChildID(abs, func(ch VFS) {
		if ch == abs {
			return
		}

		for _, id := range s.closureWindow(s.subgraphCache[ch]) {
			if s.tjc.closure.has(id) {
				continue
			}

			s.tjc.closure.add(id)
			block[k] = id
			k++
		}
	})

	s.closureArena.commit(k)
	ref := closureRef(len(s.subgraphClosures))
	s.subgraphClosures = append(s.subgraphClosures, block[:k:k])
	s.subgraphCache[abs] = ref
}

func (sc *scanCtx) closureOf(abs VFS) []VFS {
	s := sc.scanner

	ref, ok := s.subgraphCache[abs]

	if ok {
		s.subgraphHits++
	} else {
		sc.dfs(abs)

		ref = s.subgraphCache[abs]
	}

	w := s.subgraphClosures[ref]

	// Lead the window with the queried node. On a miss abs is the SCC root, so
	// strongconnect already wrote it at w[0]. The branch fires only for a HIT on
	// a non-root member of a multi-node SCC (a real include cycle): such a member
	// shares the SCC root's window, which leads with the root, not abs. Rather
	// than swap in place — which would corrupt the SCC-shared window every other
	// member reads — copy it, lead the copy with abs, and re-key the cache to the
	// straightened copy. The cache thus straightens each member on first query;
	// subsequent queries for abs hit w[0]==abs and skip the branch. Measured at
	// ~1.2% of queried files on sg5, so the copy is off the hot path.
	if len(w) > 1 && w[0] != abs {
		straight := make([]VFS, len(w))
		copy(straight, w)

		for i := 1; i < len(straight); i++ {
			if straight[i] == abs {
				straight[0], straight[i] = straight[i], straight[0]

				break
			}
		}

		ref = closureRef(len(s.subgraphClosures))
		s.subgraphClosures = append(s.subgraphClosures, straight)
		s.subgraphCache[abs] = ref

		return straight
	}

	return w
}

func (s *IncludeScanner) closureWindow(ref closureRef) []VFS {
	return s.subgraphClosures[ref]
}

func (sc *scanCtx) strongconnect(v VFS) {
	s := sc.scanner

	s.tjc.next++
	s.tjc.scratch.discover(v, s.tjc.next)
	s.tjc.stack = append(s.tjc.stack, v)

	sc.forEachResolvedChildID(v, func(w VFS) {
		if _, cached := s.subgraphCache[w]; cached {
			s.subgraphHits++

			return
		}

		if !s.tjc.scratch.visited(w) {
			sc.strongconnect(w)

			if s.tjc.scratch.lowOf(w) < s.tjc.scratch.lowOf(v) {
				s.tjc.scratch.setLow(v, s.tjc.scratch.lowOf(w))
			}
		} else if s.tjc.scratch.onStackOf(w) {
			if s.tjc.scratch.indexOf(w) < s.tjc.scratch.lowOf(v) {
				s.tjc.scratch.setLow(v, s.tjc.scratch.indexOf(w))
			}
		}
	})

	if s.tjc.scratch.lowOf(v) != s.tjc.scratch.indexOf(v) {
		return
	}

	sccStart := len(s.tjc.stack) - 1

	for s.tjc.stack[sccStart] != v {
		sccStart--
	}

	members := s.tjc.stack[sccStart:]

	s.tjc.closure.reset(vfsBound())

	// Build the closure directly into an arena block (no scratch, no copy).
	// alloc hands back a region of at least closureAllocHint; we write into it
	// by index, then commit the count actually written and store that prefix —
	// itself an address-stable sub-slice of the arena — into subgraphClosures.
	block := s.closureArena.alloc(closureAllocHint)
	k := 0

	for _, u := range members {
		if !s.tjc.closure.has(u) {
			s.tjc.closure.add(u)
			block[k] = u
			k++
		}
	}

	for _, u := range members {
		sc.forEachResolvedChildID(u, func(ch VFS) {
			if s.tjc.scratch.onStackHas(ch) {
				return
			}

			for _, id := range s.closureWindow(s.subgraphCache[ch]) {
				if !s.tjc.closure.has(id) {
					s.tjc.closure.add(id)
					block[k] = id
					k++
				}
			}
		})
	}

	s.closureArena.commit(k)
	ref := closureRef(len(s.subgraphClosures))
	s.subgraphClosures = append(s.subgraphClosures, block[:k:k])

	s.subgraphMisses += uint64(len(members))

	if len(members) > 1 {
		s.subgraphTainted++
	}

	for _, u := range members {
		s.subgraphCache[u] = ref
		s.tjc.scratch.setOnStack(u, false)
	}

	s.tjc.stack = s.tjc.stack[:sccStart]
}

func (sc *scanCtx) resolve(includerAbs, incDir VFS, d includeDirective) (out []VFS) {
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

	searchOut := sc.resolveSearchPath(includerAbs, incDir, d)

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

	addSource := func(prefix VFS) bool {
		rel, ok := s.resolveSourceUnder(prefix, target)

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
			return addSource(prefix)
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

func (sc *scanCtx) resolveSearchPath(includerAbs, incDir VFS, d includeDirective) []VFS {
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
			if info := s.codegenUnder(incDir.Rel(), d.target.String()); info != nil {
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

func (s *IncludeScanner) resolveSourceUnder(prefix VFS, target string) (string, bool) {
	prefixDir := prefix.Rel()

	if first, more := firstComponent(target); canRelFilter(first, target) {
		isDir, ok := s.parsers.fs.Listdir(prefix)[first]

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
