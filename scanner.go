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

type IncludeKind int

const (
	includeSystem IncludeKind = iota
	includeQuoted
)

type IncludeDirective struct {
	kind   IncludeKind
	target STR
}

type IncludeScanner struct {
	// sysincl owns the sysincl rule set + its lookup indexes (see sysincl_ctx.go).
	sysincl *SysinclCtx

	parsers *IncludeParserManager

	// subgraphClosures holds each cached transitive closure as a slice. The
	// slices are not owned arrays: they are address-stable sub-slices into
	// closureArena (a bump allocator), so storing them costs no copy. closureRef
	// is just an index into this slice.
	subgraphClosures [][]VFS
	closureArena     *BumpAllocator[VFS]
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
	scanCache DenseMap3[STR, []VFS, ClosureRef, bool]

	// searchTierFlat caches resolveContextSearchTier results in one scanner-wide
	// table keyed by splitMix64(ctxNum, target STR) — the two dense ids hashed into a
	// uniform 64-bit key, so an identity-hashed IntValueMap spreads (ctx, target)
	// pairs instead of clustering them. ctxNum is a dense per-distinct-config id
	// (ctxNumByHash). The
	// value (a searchTierResult) lives in IntValueMap's side slice, so table entries
	// stay small. searchTierSeen is a 1-bit-per-target-STR presence gate (set once
	// the target has any cached entry, in any config): a hit there means the table
	// is worth probing, a miss short-circuits straight to the resolve.
	searchTierFlat *IntValueMap[SearchTierResult]
	searchTierSeen BitSet

	// configByHash memoizes both per-distinct-config products — the dense
	// ctxNum and the resolve index — under ONE probe of the shared ctxHash
	// (they were two maps probed back-to-back with the same key).
	configByHash map[uint64]ScanConfigEntry

	// sourceUnderCache memoizes the includer-local quoted-include resolve
	// (resolveSourceUnder(incDir, target)) — the hottest existence probe (~505k/run,
	// 92% of resolveSourceUnder), since incDir (the includer's own dir) is rarely an
	// addincl, so the addincl index can't cover it. The result is a pure function of
	// (incDir, target) and the FS, so it's context-free and run-wide. Keyed by
	// splitMix64(incDir VFS, target STR) — the two ids hashed into a uniform 64-bit
	// key so an identity-hashed IntValueMap spreads them. The value is the resolved
	// $(S) VFS already interned (0 = "does not resolve here"): storing the rel
	// string made every HIT re-intern it via Source(rel) — a full xxh3 + table
	// probe per hit, several hundred k per run.
	sourceUnderCache *IntValueMap[VFS]

	// tjc points at the run-wide Tarjan/closure working state owned by genCtx and
	// shared by the target and host scanners (see tarjanCtx).
	tjc *TarjanCtx

	// dfsActive marks the roots whose dfs is currently in flight. It is set-once
	// (never reset): within one scanner a root is cached the moment its dfs
	// finishes, so closureOf re-enters dfs(root) only along an include cycle —
	// which dfs hands to strongconnect. Per-scanner, not shared, so the host
	// scanner does not see target's roots as spurious cycles. A bit set (1 bit/id)
	// rather than an epoch IdSet, since membership is permanent and binary.
	dfsActive BitSet

	visitedIDPool sync.Pool

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
	subgraphSubsumed       uint64
	searchTierHits         uint64
	searchTierMisses       uint64
	resolveSearchPathCalls uint64
	statsCallCount         uint64

	codegen *CodegenRegistry

	// moduleByRef points at genCtx.moduleByRef: a generated file's producing tools
	// (GeneratedFileInfo.GeneratorRefs) are looked up here to mix their declared
	// INDUCED_DEPS into the file's resolved children. nil in standalone scanners.
	moduleByRef *DenseMap[NodeRef, *ModuleEmitResult]
}

type ScanCtx struct {
	scanner      *IncludeScanner
	cfg          ScanContext
	ctxNum       uint32
	resolveIndex *CfgResolveIndex

	// parser is the scan context's parser for files with UNREGISTERED
	// extensions (swig's .i, …), resolved ONCE from the walk's root file —
	// registered extensions always use their own parser. nil = C-like default.
	parser IncludeDirectiveParser
}

// closureRef is an index into IncludeScanner.subgraphClosures.
type ClosureRef uint32

// cachedChildren returns the resolved immediate children of v (column 1). A
// resolved-but-empty child set reads back present with a nil/empty slice, since
// presence is the column slot, not nil-ness — so no sentinel slice is needed.
func (s *IncludeScanner) cachedChildren(v VFS) ([]VFS, bool) {
	return s.scanCache.get1(STR(v.strID()))
}

func (s *IncludeScanner) putChildren(v VFS, children []VFS) {
	s.scanCache.put1(STR(v.strID()), children)
}

func (s *IncludeScanner) cachedClosure(v VFS) (ClosureRef, bool) {
	return s.scanCache.get2(STR(v.strID()))
}

func (s *IncludeScanner) putClosure(v VFS, ref ClosureRef) {
	s.scanCache.put2(STR(v.strID()), ref)
}

// sourceFileExists memoizes IsFile(srcRootVFS, abs.Rel()) by the file VFS
// (column 3), so the repeated existence checks of cached sysincl mappings probe
// the FS — and intern the parent dir — only once per file. The column's own
// presence is the "already probed" bit; an absent column means not yet checked.
func (s *IncludeScanner) sourceFileExists(abs VFS) bool {
	key := STR(abs.strID())

	if exists, probed := s.scanCache.get3(key); probed {
		return exists
	}

	v := s.parsers.fs.isFile(srcRootVFS, abs.rel())
	s.scanCache.put3(key, v)

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

type SearchTierResult struct {
	paths []VFS
	found bool
}

type ScannerPerfStats struct {
	walkClosureCalls       uint64
	subgraphHits           uint64
	subgraphMisses         uint64
	subgraphTainted        uint64
	subgraphSubsumed       uint64
	searchTierHits         uint64
	searchTierMisses       uint64
	resolveSearchPathCalls uint64
}

func newIncludeScannerWith(parsers *IncludeParserManager, sysincl SysInclSet, onWarn func(Warn), tjc *TarjanCtx) *IncludeScanner {
	s := &IncludeScanner{
		sysincl:             newSysinclCtx(sysincl),
		parsers:             parsers,
		generatedFirstClaim: make(map[VFS]string, 2048),
		onWarn:              onWarn,
		// Index 0 reserved so a fresh closureRef is always >= 1 (closureOf's
		// straighten path and closureWindow treat ref as a 1-based index).
		subgraphClosures: make([][]VFS, 1, 256),
		closureArena:     newBumpAllocator[VFS](closureArenaInitial),
		searchTierFlat:   newIntValueMap[SearchTierResult](4096),
		configByHash:     make(map[uint64]ScanConfigEntry, 1024),
		sourceUnderCache: newIntValueMap[VFS](1 << 16),
		tjc:              tjc,
	}

	s.visitedIDPool.New = func() any {
		return &IdSet{}
	}

	return s
}

type ScanContext struct {
	// RootParser is the parser for unregistered-extension files reached from
	// this walk, resolved once from the walk's root file (nil = C default).
	// Not part of hashScanContext: it affects parsing, not path resolution,
	// and parse results cache by (file, parser).
	RootParser IncludeDirectiveParser

	OwnAddIncl      []VFS
	PeerAddInclSet  []VFS
	BaseSearchPaths []VFS
	// OwnerModuleDir identifies the consumer module whose CC compile (or
	// equivalent) triggered this scan. Used to populate
	// IncludeScanner.generatedFirstClaim on the first resolve of any
	// CodegenRegistry output — see that field's comment for the rationale.
	OwnerModuleDir string
}

type ScanConfigEntry struct {
	ctxNum uint32
	ri     *CfgResolveIndex
}

func (s *IncludeScanner) newScanCtx(cfg ScanContext) *ScanCtx {
	ctxHash := hashScanContext(&cfg)

	entry, ok := s.configByHash[ctxHash]

	if !ok {
		// Dense id: next == count of distinct configs.
		entry = ScanConfigEntry{ctxNum: uint32(len(s.configByHash)), ri: buildCfgResolveIndex(&cfg)}
		s.configByHash[ctxHash] = entry

		if entry.ri.indexable {
			for _, p := range cfg.OwnAddIncl {
				s.parsers.indexAddincl(p)
			}

			for _, p := range cfg.PeerAddInclSet {
				s.parsers.indexAddincl(p)
			}
		}
	}

	ctxNum, ri := entry.ctxNum, entry.ri

	return &ScanCtx{
		scanner:      s,
		cfg:          cfg,
		ctxNum:       ctxNum,
		resolveIndex: ri,
		parser:       cfg.RootParser,
	}
}

// hashScanContext fingerprints the resolve-relevant context fields for the
// in-memory config maps (ctxNumByHash, resolveIndexByConfig) — nothing
// persistent, so in-run stability is all it needs. Each element contributes its
// interned string's xxh3 lo (internTable.los — the same per-STR hash the uid
// layer mixes), chained through mix64, instead of re-walking the path bytes:
// the lo is bijective-in-practice with the string (root prefix included), so
// the discrimination matches the old per-byte FNV at a fraction of the cost.
// Length-prefixing each slice keeps the three-slice boundaries unambiguous.
func hashScanContext(ctx *ScanContext) uint64 {
	var h uint64

	mixSlice := func(ss []VFS) {
		h = mix64(h ^ uint64(len(ss)))

		for _, v := range ss {
			h = mix64(h ^ internTable.los[v.strID()])
		}
	}

	mixSlice(ctx.OwnAddIncl)
	mixSlice(ctx.PeerAddInclSet)
	mixSlice(ctx.BaseSearchPaths)

	return h
}

func (sc *ScanCtx) forEachResolvedChild(vfsPath VFS, fn func(rabs VFS)) {
	s := sc.scanner
	incDir := dirKey(pathDir(vfsPath.rel()))

	for _, entry := range s.parsers.parsedIncludes(vfsPath, sc.parser) {
		resolved := sc.resolve(vfsPath, incDir, entry)

		for _, rabs := range resolved {
			fn(rabs)
		}
	}

	sc.resolveInducedDeps(vfsPath, incDir, fn)
}

// resolveInducedDeps mixes the INDUCED_DEPS of a generated file's producing tools
// into its resolved children, so a tool's runtime headers (and their closure) come
// from the tool's declared INDUCED_DEPS rather than a hardcoded list woven into the
// registered parsed includes. Only build outputs with a codegen registry entry and
// recorded GeneratorRefs contribute.
func (sc *ScanCtx) resolveInducedDeps(vfsPath VFS, incDir VFS, fn func(rabs VFS)) {
	s := sc.scanner

	if !vfsPath.isBuild() || s.codegen == nil || s.moduleByRef == nil {
		return
	}

	info := s.codegen.lookup(vfsPath)

	if info == nil {
		return
	}

	// A header output reads the Header induced bucket; a translation unit reads Cpp.
	// (h+cpp …) deps live in both buckets, so a single read per output suffices.
	bucket := parsedIncludesCpp

	if isHeaderSource(vfsPath.rel()) {
		bucket = parsedIncludesHeader
	}

	for _, gref := range info.GeneratorRefs {
		tool, ok := s.moduleByRef.get(gref)

		if !ok {
			continue
		}

		for _, entry := range tool.InducedDeps.bucket(bucket) {
			for _, rabs := range sc.resolve(vfsPath, incDir, entry) {
				fn(rabs)
			}
		}
	}
}

// forEachResolvedChildID returns the resolved immediate children of absID,
// caching by absID alone (no scan-context key). See the comment above
// IncludeScanner.scanCache for the upstream-mirroring
// invariant that makes this correct: each file is parse-and-resolved exactly
// once per run.
func (sc *ScanCtx) forEachResolvedChildID(abs VFS, fn func(VFS)) {
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

func (s *IncludeScanner) subgraphCacheStats() (hits, misses, tainted uint64) {
	return s.subgraphHits, s.subgraphMisses, s.subgraphTainted
}

func (s *IncludeScanner) perfStats() ScannerPerfStats {
	return ScannerPerfStats{
		walkClosureCalls:       s.walkClosureCalls,
		subgraphHits:           s.subgraphHits,
		subgraphMisses:         s.subgraphMisses,
		subgraphTainted:        s.subgraphTainted,
		subgraphSubsumed:       s.subgraphSubsumed,
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
func (sc *ScanCtx) dfs(abs VFS) {
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

		if sc.windowSubsumed(ch) {
			return
		}

		cref, _ := s.cachedClosure(ch)
		k = s.tjc.closure.spliceNew(s.closureWindow(cref), block, k)
	})

	// Splice non-expanded closure leaves (COPY_FILE(TEXT) $(B) dst → its $(S)
	// source + copy tooling) for every $(B) member: bare window members that ride
	// transitively to every consumer (which splices this cached window) but are
	// never traversed as children — re-resolving their own #includes per consuming
	// module leaked sibling staging copies.
	for i := 0; i < k; i++ {
		if block[i].isBuild() {
			k += copy(block[k:], s.codegen.closureLeaves(block[i]))
		}
	}

	s.closureArena.commit(k)
	ref := ClosureRef(len(s.subgraphClosures))
	s.subgraphClosures = append(s.subgraphClosures, block[:k:k])
	s.putClosure(abs, ref)
}

func (sc *ScanCtx) closureOf(abs VFS) []VFS {
	s := sc.scanner

	ref, ok := s.cachedClosure(abs)

	if ok {
		s.subgraphHits++
	} else {
		sc.dfs(abs)

		ref, _ = s.cachedClosure(abs)
	}

	w := s.subgraphClosures[ref]

	return w
}

func (s *IncludeScanner) closureWindow(ref ClosureRef) []VFS {
	return s.subgraphClosures[ref]
}

// windowSubsumed reports whether ch's whole cached window is already inside the
// closure block under construction, letting the splice loops (dfs pass 2,
// strongconnect) skip it after one membership probe instead of re-checking every
// window element. Windows are transitively closed, so ch arriving via an earlier
// window splice means that window contained closure(ch) entirely. The leafEver
// guard keeps this sound: a ClosureLeaf rides in windows as a bare, non-expanded
// member — its presence does NOT imply its own window is present — so any VFS
// ever registered as a leaf never short-circuits. A nil codegen registry has no
// leaves at all, so membership alone suffices there.
func (sc *ScanCtx) windowSubsumed(ch VFS) bool {
	s := sc.scanner

	if !s.tjc.closure.has(ch) {
		return false
	}

	if s.codegen != nil && s.codegen.isLeafEver(ch) {
		return false
	}

	s.subgraphSubsumed++

	return true
}

// scanCtx implements closureSink (tarjan_ctx.go) so tarjanCtx.strongconnect can
// build SCC closures without depending on scanner internals.

func (sc *ScanCtx) forEachChild(v VFS, fn func(VFS)) {
	sc.forEachResolvedChildID(v, fn)
}

func (sc *ScanCtx) cachedWindow(v VFS) ([]VFS, bool) {
	ref, ok := sc.scanner.cachedClosure(v)

	if !ok {
		return nil, false
	}

	return sc.scanner.closureWindow(ref), true
}

// emitClosure reserves an arena block, lets fill write the deduped closure into
// it (returning the count), then commits that prefix — an address-stable
// sub-slice of the arena — into subgraphClosures and caches it for every member.
func (sc *ScanCtx) emitClosure(members []VFS, fill func(block []VFS) int) {
	s := sc.scanner

	block := s.closureArena.alloc(closureAllocHint)
	k := fill(block)

	// Splice non-expanded closure leaves for every $(B) member (same as dfs
	// pass-2; see there).
	for i := 0; i < k; i++ {
		if block[i].isBuild() {
			k += copy(block[k:], s.codegen.closureLeaves(block[i]))
		}
	}

	s.closureArena.commit(k)

	ref := ClosureRef(len(s.subgraphClosures))
	s.subgraphClosures = append(s.subgraphClosures, block[:k:k])

	s.subgraphMisses += uint64(len(members))

	if len(members) > 1 {
		s.subgraphTainted++
	}

	for _, u := range members {
		s.putClosure(u, ref)
	}
}

func (sc *ScanCtx) resolve(includerAbs, incDir VFS, d IncludeDirective) (out []VFS) {
	// A rooted target ($(S)/... or $(B)/...) is already bound to its root —
	// INDUCED_DEPS spells deps via the reserved ${ARCADIA_ROOT}-family refs.
	// Upstream classifies such paths directly (ResolveAsKnownWithoutCheck →
	// NPath::ToYPath), with no include search, sysincl, or FS check; mirror
	// that. The STR already backs the full path, so the binding is a shift.
	if v := d.target.vfs(); v != 0 {
		return []VFS{v}
	}

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
				includerAbs.string(), open, d.target.string(), close),
		})
	}()

	searchOut := sc.resolveSearchPath(includerAbs, incDir, d)

	includerRel := includerAbs.rel()
	var mappings []VFS
	var hasMultiTarget bool
	mappings, hasMultiTarget, sysinclClaimed = s.sysincl.lookup(includerRel, d.target)

	if d.kind == includeQuoted && len(searchOut) > 0 {
		bypass := !hasMultiTarget

		if !bypass && searchOut[0].isSource() {
			incDir := pathDir(includerRel)

			var sameDirRel string

			if incDir != "" {
				sameDirRel = incDir + "/" + d.target.string()
			} else {
				sameDirRel = d.target.string()
			}

			bypass = searchOut[0].rel() == sameDirRel
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

type CfgResolveIndex struct {
	indexable    bool
	rank         *IntValueMap[int32]
	buildEntries []CfgBuildAddincl
}

// cfgBuildAddincl is a Build-rooted addincl prefix paired with its rank in the
// unified declaration order across OwnAddIncl ⨁ PeerAddInclSet. prefixID is
// pre-interned so codegen.LookupSplit needs no per-resolve allocation.
type CfgBuildAddincl struct {
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
func buildCfgResolveIndex(cfg *ScanContext) *CfgResolveIndex {
	idx := &CfgResolveIndex{}

	for _, p := range cfg.OwnAddIncl {
		if p.root() == VFSRootSource && p.rel() == "" {
			return idx
		}
	}

	for _, p := range cfg.PeerAddInclSet {
		if p.root() == VFSRootSource && p.rel() == "" {
			return idx
		}
	}

	idx.indexable = true
	idx.rank = newIntValueMap[int32](2 * (len(cfg.OwnAddIncl) + len(cfg.PeerAddInclSet)))

	// Membership rides the global epoch deduper (a bitset probe, not a map
	// read); the leaf contract holds — nothing below allocates the deduper.
	deduper.reset()

	r := int32(0)
	add := func(p VFS) {
		if !deduper.add(p) {
			return
		}

		idx.rank.put(uint64(p), r)

		if p.root() == VFSRootBuild {
			idx.buildEntries = append(idx.buildEntries, CfgBuildAddincl{
				prefix:   p,
				prefixID: internStr(p.rel()),
				rank:     int(r),
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

func (sc *ScanCtx) cacheSearchTier(targetID STR, out SearchTierResult) SearchTierResult {
	s := sc.scanner
	s.searchTierFlat.put(splitMix64(sc.ctxNum, uint32(targetID)), out)
	s.searchTierSeen.add(uint32(targetID))

	return out
}

func (sc *ScanCtx) resolveContextSearchTier(targetID STR, target string) SearchTierResult {
	s := sc.scanner

	// Gate the composite-key hash probe on the 1-bit per-target presence flag: a
	// target never cached in any config skips straight to the resolve.
	if s.searchTierSeen.has(uint32(targetID)) {
		if cached := s.searchTierFlat.get(splitMix64(sc.ctxNum, uint32(targetID))); cached != nil {
			s.searchTierHits++

			return *cached
		}
	}

	s.searchTierMisses++

	var out SearchTierResult

	normTarget := normalisePath(target)

	addSource := func(prefix VFS) bool {
		rel, ok := s.resolveSourceUnder(prefix, target)

		if !ok {
			return false
		}

		out.paths = []VFS{source(rel)}
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
			info = s.codegen.lookupRel(normTarget)
		} else if pid := interned(prefixRel); pid != nil {
			info = s.codegen.lookupSplit(*pid, *buildSuffix)
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
		switch prefix.root() {
		case VFSRootBuild:
			return addBuild(prefix.rel())
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

			cands, _ := s.parsers.addinclIndex.get(targetID)

			for _, a := range cands {
				if rp := idx.rank.get(uint64(a)); rp != nil && int(*rp) < bestRank {
					bestRank = int(*rp)
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

					info := s.codegen.lookupSplit(b.prefixID, *buildSuffix)

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
					out.paths = []VFS{source(joinRel(bestAddincl.rel(), target))}
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

func (sc *ScanCtx) resolveSearchPath(includerAbs, incDir VFS, d IncludeDirective) []VFS {
	s := sc.scanner
	s.resolveSearchPathCalls++

	// out doubles as the dedup set: every accepted candidate lands in it, each
	// branch below adds at most one entry, so membership is a linear scan over
	// <= 3 elements — no pooled map, no per-call "B:"+rel key allocs, no clear.
	var out []VFS

	outHas := func(v VFS) bool {
		for _, x := range out {
			if x == v {
				return true
			}
		}

		return false
	}

	addPath := func(rel string) bool {
		rel = normalisePath(rel)

		if !s.parsers.fs.isFile(srcRootVFS, rel) {
			return false
		}

		v := source(rel)

		if outHas(v) {
			return false
		}

		out = append(out, v)

		return true
	}

	addBuildPath := func(rel string) bool {
		if s.codegen == nil {
			return false
		}

		info := s.codegen.lookupRel(rel)

		if info == nil {
			return false
		}

		if outHas(info.OutputPath) {
			return false
		}

		out = append(out, info.OutputPath)

		return true
	}

	searchPathFound := false

	if candidate, ok := cythonPy2SiblingOverride(includerAbs, d); ok && addPath(candidate) {
		searchPathFound = true
	}

	if includerAbs.isBuild() && strings.Contains(d.target.string(), "/") {
		rel := d.target.string()

		if addBuildPath(rel) {
			searchPathFound = true

			if sc.cfg.OwnerModuleDir != "" && s.codegen != nil {
				if info := s.codegen.lookupRel(rel); info != nil {
					if _, ok := s.generatedFirstClaim[info.OutputPath]; !ok {
						s.generatedFirstClaim[info.OutputPath] = sc.cfg.OwnerModuleDir
					}
				}
			}
		}
	}

	if d.kind == includeQuoted {
		matched := false

		// Memoize the includer-local resolve by splitMix64(incDir, target) — both 32-bit
		// ids hashed into one uniform key. 0 means "does not resolve under incDir".
		suKey := splitMix64(uint32(incDir), uint32(d.target))
		var sv VFS

		if p := s.sourceUnderCache.get(suKey); p != nil {
			sv = *p
		} else {
			if r, ok := s.resolveSourceUnder(incDir, d.target.string()); ok {
				sv = source(r)
			}

			s.sourceUnderCache.put(suKey, sv)
		}

		if sv != 0 {
			out = append(out, sv)
			searchPathFound = true
			matched = true
		}

		if !matched {
			if info := s.codegenUnder(incDir.rel(), d.target.string()); info != nil {
				if !outHas(info.OutputPath) {
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
		tier := sc.resolveContextSearchTier(d.target, d.target.string())

		if tier.found {
			out = append(out, tier.paths...)
			searchPathFound = true
		}
	}

	return out
}

func cythonPy2SiblingOverride(includerAbs VFS, d IncludeDirective) (string, bool) {
	if !includerAbs.isSource() || d.kind != includeQuoted {
		return "", false
	}

	if hasPrefix(includerAbs.rel(), "contrib/tools/cython_py2/Cython/Includes/") {
		if hasPrefix(d.target.string(), "libc/") || hasPrefix(d.target.string(), "libcpp/") {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.string(), true
		}

		return "", false
	}

	switch includerAbs.rel() {
	case "util/generic/string.pxd":
		if d.target.string() == "libcpp/string.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.string(), true
		}
	case "util/generic/hash.pxd", "util/generic/hash_set.pxd":
		if d.target.string() == "libcpp/pair.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.string(), true
		}
	case "util/system/types.pxd":
		if d.target.string() == "libc/stdint.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.string(), true
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
	if !s.parsers.fs.isFile(prefix, target) {
		return "", false
	}

	return normalisePath(joinRel(prefix.rel(), target)), true
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

		return s.codegen.lookupSplit(*pid, *sid)
	}

	return s.codegen.lookupRel(normalisePath(joinRel(prefixDir, target)))
}

func canRelFilter(first, target string) bool {
	return first != "" && first != "." && first != ".." && !strings.Contains(target, "/..")
}
