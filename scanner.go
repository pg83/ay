package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
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
	target STR
}

type IncludeScanner struct {
	// sysincl owns the sysincl rule set + its lookup indexes (see sysincl_ctx.go).
	sysincl *sysinclCtx

	parsers *includeParserManager

	// subgraphClosures holds each cached transitive closure as a slice. The
	// slices are not owned arrays: they are address-stable sub-slices into
	// closureArena (a bump allocator), so storing them costs no copy. closureRef
	// is just an index into this slice.
	subgraphClosures [][]VFS
	closureArena     *bumpAllocator[VFS]
	// scanCache holds both the cached transitive closure (under DFS) and the
	// cached immediate resolved children per includer, keyed by includer
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
	//
	// All three caches live in one DenseMap3 keyed by the includer's STR
	// (v.strID()): column 1 the resolved immediate children, column 2 the
	// transitive-closure ref, column 3 source-file existence. One idx array
	// (sized to vfsBound — the expensive part) is shared by all three columns
	// instead of one per cache. strID is unique per VFS (the $(S)/$(B) prefix is
	// part of the interned string) and lossless, halving idx versus the 2x-wider
	// VFS space. The columns are filled at different times (children during dfs
	// pass 1, closure on pop, existence on first probe), so each relies on its
	// own per-column presence rather than the map's shared key-present bit.
	scanCache DenseMap3[STR, []VFS, closureRef, bool]

	// searchTierFlat caches resolveContextSearchTier results in one scanner-wide
	// table keyed by morton(ctxNum, target STR) — the two dense ids bit-interleaved
	// (Z-order) rather than shift-packed, so the key's low bits mix BOTH ids and an
	// identity-hashed IntValueMap spreads (ctx, target) pairs instead of clustering
	// them by target. ctxNum is a dense per-distinct-config id (ctxNumByHash). The
	// value (a searchTierResult) lives in IntValueMap's side slice, so table entries
	// stay small. searchTierSeen is a 1-bit-per-target-STR presence gate (set once
	// the target has any cached entry, in any config): a hit there means the table
	// is worth probing, a miss short-circuits straight to the resolve.
	searchTierFlat *IntValueMap[searchTierResult]
	searchTierSeen BitSet
	ctxNumByHash   map[uint64]uint32

	resolveIndexByConfig map[uint64]*cfgResolveIndex

	// sourceUnderCache memoizes the includer-local quoted-include resolve
	// (resolveSourceUnder(incDir, target)) — the hottest existence probe (~505k/run,
	// 92% of resolveSourceUnder), since incDir (the includer's own dir) is rarely an
	// addincl, so the addincl index can't cover it. The result is a pure function of
	// (incDir, target) and the FS, so it's context-free and run-wide. Keyed by
	// morton(incDir VFS, target STR) — bit-interleaved so the low bits mix both ids
	// and an identity-hashed IntValueMap spreads them; value (the resolved source
	// rel, or "" for "does not resolve here") lives in the side slice.
	sourceUnderCache *IntValueMap[string]

	// tjc points at the run-wide Tarjan/closure working state owned by genCtx and
	// shared by the target and host scanners (see tarjanCtx).
	tjc *tarjanCtx

	// dfsActive marks the roots whose dfs is currently in flight. It is set-once
	// (never reset): within one scanner a root is cached the moment its dfs
	// finishes, so closureOf re-enters dfs(root) only along an include cycle —
	// which dfs hands to strongconnect. Per-scanner, not shared, so the host
	// scanner does not see target's roots as spurious cycles. A bit set (1 bit/id)
	// rather than an epoch idSet, since membership is permanent and binary.
	dfsActive BitSet

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
	statsCallCount         uint64

	codegen *CodegenRegistry
}

type scanCtx struct {
	scanner      *IncludeScanner
	cfg          ScanContext
	ctxNum       uint32
	resolveIndex *cfgResolveIndex
}

type idSet struct {
	gen   []uint32
	epoch uint32
}

// closureRef is an index into IncludeScanner.subgraphClosures.
type closureRef uint32

// cachedChildren returns the resolved immediate children of v (column 1). A
// resolved-but-empty child set reads back present with a nil/empty slice, since
// presence is the column slot, not nil-ness — so no sentinel slice is needed.
func (s *IncludeScanner) cachedChildren(v VFS) ([]VFS, bool) {
	return s.scanCache.Get1(STR(v.strID()))
}

func (s *IncludeScanner) putChildren(v VFS, children []VFS) {
	s.scanCache.Put1(STR(v.strID()), children)
}

func (s *IncludeScanner) cachedClosure(v VFS) (closureRef, bool) {
	return s.scanCache.Get2(STR(v.strID()))
}

func (s *IncludeScanner) putClosure(v VFS, ref closureRef) {
	s.scanCache.Put2(STR(v.strID()), ref)
}

// sourceFileExists memoizes IsFile(srcRootVFS, abs.Rel()) by the file VFS
// (column 3), so the repeated existence checks of cached sysincl mappings probe
// the FS — and intern the parent dir — only once per file. The column's own
// presence is the "already probed" bit; an absent column means not yet checked.
func (s *IncludeScanner) sourceFileExists(abs VFS) bool {
	key := STR(abs.strID())

	if exists, probed := s.scanCache.Get3(key); probed {
		return exists
	}

	v := s.parsers.fs.IsFile(srcRootVFS, abs.Rel())
	s.scanCache.Put3(key, v)

	return v
}

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

type scannerPerfStats struct {
	walkClosureCalls       uint64
	subgraphHits           uint64
	subgraphMisses         uint64
	subgraphTainted        uint64
	searchTierHits         uint64
	searchTierMisses       uint64
	resolveSearchPathCalls uint64
}

func newIncludeScannerWith(parsers *includeParserManager, sysincl SysInclSet, onWarn func(Warn), tjc *tarjanCtx) *IncludeScanner {
	s := &IncludeScanner{
		sysincl:             newSysinclCtx(sysincl),
		parsers:             parsers,
		generatedFirstClaim: make(map[VFS]string, 2048),
		onWarn:              onWarn,
		// Index 0 reserved so a fresh closureRef is always >= 1 (closureOf's
		// straighten path and closureWindow treat ref as a 1-based index).
		subgraphClosures:     make([][]VFS, 1, 256),
		closureArena:         newBumpAllocator[VFS](closureArenaInitial),
		searchTierFlat:       NewIntValueMap[searchTierResult](4096),
		ctxNumByHash:         make(map[uint64]uint32, 1024),
		resolveIndexByConfig: make(map[uint64]*cfgResolveIndex, 1024),
		sourceUnderCache:     NewIntValueMap[string](1 << 16),
		tjc:                  tjc,
	}

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

	ctxNum, ok := s.ctxNumByHash[ctxHash]

	if !ok {
		ctxNum = uint32(len(s.ctxNumByHash)) // dense id: next == count of distinct configs
		s.ctxNumByHash[ctxHash] = ctxNum
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
		scanner:      s,
		cfg:          cfg,
		ctxNum:       ctxNum,
		resolveIndex: ri,
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
// IncludeScanner.scanCache for the upstream-mirroring
// invariant that makes this correct: each file is parse-and-resolved exactly
// once per run.
func (sc *scanCtx) forEachResolvedChildID(abs VFS, fn func(VFS)) {
	s := sc.scanner

	if cached, ok := s.cachedChildren(abs); ok {
		for _, id := range cached {
			fn(id)
		}

		return
	}

	var children []VFS
	sc.forEachResolvedChild(abs, func(rabs VFS) {
		children = append(children, rabs)
	})
	s.putChildren(abs, children)

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

	if s.dfsActive.has(uint32(abs)) {
		s.subgraphHits += s.tjc.runSCC(sc, abs)

		return
	}

	s.dfsActive.add(uint32(abs))

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
	// cache — no temporary [][]VFS, and forEachResolvedChildID hits scanCache
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

		cref, _ := s.cachedClosure(ch)

		for _, id := range s.closureWindow(cref) {
			if s.tjc.closure.has(id) {
				continue
			}

			s.tjc.closure.add(id)
			block[k] = id
			k++
		}
	})

	// Splice non-expanded closure leaves (COPY_FILE(TEXT) $(B) dst → its $(S)
	// source + copy tooling) for every $(B) member: bare window members that ride
	// transitively to every consumer (which splices this cached window) but are
	// never traversed as children — re-resolving their own #includes per consuming
	// module leaked sibling staging copies.
	for i := 0; i < k; i++ {
		if block[i].IsBuild() {
			k += copy(block[k:], s.codegen.ClosureLeaves(block[i]))
		}
	}

	s.closureArena.commit(k)
	ref := closureRef(len(s.subgraphClosures))
	s.subgraphClosures = append(s.subgraphClosures, block[:k:k])
	s.putClosure(abs, ref)
}

func (sc *scanCtx) closureOf(abs VFS) []VFS {
	s := sc.scanner

	ref, ok := s.cachedClosure(abs)

	if ok {
		s.subgraphHits++
	} else {
		sc.dfs(abs)

		ref, _ = s.cachedClosure(abs)
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
		s.putClosure(abs, ref)

		return straight
	}

	return w
}

func (s *IncludeScanner) closureWindow(ref closureRef) []VFS {
	return s.subgraphClosures[ref]
}

// scanCtx implements closureSink (tarjan_ctx.go) so tarjanCtx.strongconnect can
// build SCC closures without depending on scanner internals.

func (sc *scanCtx) forEachChild(v VFS, fn func(VFS)) {
	sc.forEachResolvedChildID(v, fn)
}

func (sc *scanCtx) cachedWindow(v VFS) ([]VFS, bool) {
	ref, ok := sc.scanner.cachedClosure(v)

	if !ok {
		return nil, false
	}

	return sc.scanner.closureWindow(ref), true
}

// emitClosure reserves an arena block, lets fill write the deduped closure into
// it (returning the count), then commits that prefix — an address-stable
// sub-slice of the arena — into subgraphClosures and caches it for every member.
func (sc *scanCtx) emitClosure(members []VFS, fill func(block []VFS) int) {
	s := sc.scanner

	block := s.closureArena.alloc(closureAllocHint)
	k := fill(block)

	// Splice non-expanded closure leaves for every $(B) member (same as dfs
	// pass-2; see there).
	for i := 0; i < k; i++ {
		if block[i].IsBuild() {
			k += copy(block[k:], s.codegen.ClosureLeaves(block[i]))
		}
	}

	s.closureArena.commit(k)

	ref := closureRef(len(s.subgraphClosures))
	s.subgraphClosures = append(s.subgraphClosures, block[:k:k])

	s.subgraphMisses += uint64(len(members))

	if len(members) > 1 {
		s.subgraphTainted++
	}

	for _, u := range members {
		s.putClosure(u, ref)
	}
}

func (sc *scanCtx) resolve(includerAbs, incDir VFS, d includeDirective) (out []VFS) {
	s := sc.scanner

	var sysinclClaimed bool

	defer func() {
		if len(out) > 0 || sysinclClaimed {
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

	searchOut := sc.resolveSearchPath(includerAbs, incDir, d)

	includerRel := includerAbs.Rel()
	var mappings []VFS
	var hasMultiTarget bool
	mappings, hasMultiTarget, sysinclClaimed = s.sysincl.lookup(includerRel, d.target)

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

			if !s.sourceFileExists(abs) {
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

		if !s.sourceFileExists(abs) {
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

		if p.Root() == VFSRootBuild {
			idx.buildEntries = append(idx.buildEntries, cfgBuildAddincl{
				prefix:   p,
				prefixID: internString(p.Rel()),
				rank:     r,
			})
		}

		r++
	}

	for _, p := range cfg.OwnAddIncl {
		add(p)
	}

	for _, p := range cfg.PeerAddInclSet {
		add(p)
	}

	return idx
}

func (sc *scanCtx) cacheSearchTier(targetID STR, out searchTierResult) searchTierResult {
	s := sc.scanner
	s.searchTierFlat.Put(morton(sc.ctxNum, uint32(targetID)), out)
	s.searchTierSeen.add(uint32(targetID))

	return out
}

func (sc *scanCtx) resolveContextSearchTier(targetID STR, target string) searchTierResult {
	s := sc.scanner

	// Gate the composite-key hash probe on the 1-bit per-target presence flag: a
	// target never cached in any config skips straight to the resolve.
	if s.searchTierSeen.has(uint32(targetID)) {
		if cached := s.searchTierFlat.Get(morton(sc.ctxNum, uint32(targetID))); cached != nil {
			s.searchTierHits++
			return *cached
		}
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

			cands, _ := s.parsers.addinclIndex.Get(targetID)

			for _, a := range cands {
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
				return sc.cacheSearchTier(targetID, out)
			}

			for _, p := range sc.cfg.BaseSearchPaths {
				if addInclPath(p) {
					return sc.cacheSearchTier(targetID, out)
				}
			}

			return sc.cacheSearchTier(targetID, out)
		}
	}

	for _, p := range sc.cfg.OwnAddIncl {
		if addInclPath(p) {
			return sc.cacheSearchTier(targetID, out)
		}
	}

	for _, p := range sc.cfg.PeerAddInclSet {
		if addInclPath(p) {
			return sc.cacheSearchTier(targetID, out)
		}
	}

	for _, p := range sc.cfg.BaseSearchPaths {
		if addInclPath(p) {
			return sc.cacheSearchTier(targetID, out)
		}
	}

	return sc.cacheSearchTier(targetID, out)
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

		if !s.parsers.fs.IsFile(srcRootVFS, rel) {
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
		}
	}

	if d.kind == includeQuoted {
		matched := false

		// Memoize the includer-local resolve by morton(incDir, target) — both 32-bit
		// ids bit-interleaved into one key. "" means "does not resolve under incDir".
		suKey := morton(uint32(incDir), uint32(d.target))
		rel := ""

		if p := s.sourceUnderCache.Get(suKey); p != nil {
			rel = *p
		} else {
			if r, ok := s.resolveSourceUnder(incDir, d.target.String()); ok {
				rel = r
			}

			s.sourceUnderCache.Put(suKey, rel)
		}

		if rel != "" {
			out = append(out, Source(rel))
			searchPathFound = true
			matched = true
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

func (s *IncludeScanner) resolveSourceUnder(prefix VFS, target string) (string, bool) {
	// IsFile(prefix, target) is exactly the gating this used to hand-roll
	// (firstComponent + Listdir(prefix), then the deep check) — but keyed off the
	// already-interned prefix VFS, so it skips the Listdir(srcRoot) + re-gating the
	// prefix's own components that IsFile(srcRootVFS, joinRel(...)) did from the root.
	if !s.parsers.fs.IsFile(prefix, target) {
		return "", false
	}

	return normalisePath(joinRel(prefix.Rel(), target)), true
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
