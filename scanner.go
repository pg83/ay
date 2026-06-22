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
	// includeCythonOptional is a speculative cython cimport candidate, dropped on a miss.
	includeCythonOptional
	// includeCythonModule is the `from X cimport …` module candidate `X.pxd`. On a
	// hit it suppresses the following includeCythonName probes.
	includeCythonModule
	// includeCythonName is a `from X cimport name` submodule candidate's primary
	// probe. Skipped when a preceding includeCythonModule resolved.
	includeCythonName
	// includeCythonFallback is probed only when its primary (the preceding probe)
	// was attempted and did NOT resolve.
	includeCythonFallback
)

type IncludeDirective struct {
	kind   IncludeKind
	target STR
}

// GenOwner is the directory AND tag of the first module to claim a generated file
// under first-leave-wins. Tag is 0 unless the claiming module is a tagged submodule.
type GenOwner struct {
	Dir string
	Tag STR
}

// quotedLike reports whether the directive resolves through the quoted-include
// search. The cython cimport probes share that resolution.
func (d IncludeDirective) quotedLike() bool {
	return d.kind == includeQuoted || d.cythonProbe()
}

// cythonProbe reports whether the directive is a best-effort cython cimport
// candidate, silently dropped on a miss.
func (d IncludeDirective) cythonProbe() bool {
	return d.kind == includeCythonOptional || d.kind == includeCythonModule || d.kind == includeCythonName || d.kind == includeCythonFallback
}

type IncludeScanner struct {
	// sysincl owns the sysincl rule set + its lookup indexes (see sysincl_ctx.go).
	sysincl *SysinclCtx

	parsers *IncludeParserManager

	// subgraphClosures holds each cached transitive closure as an address-stable
	// sub-slice into closureArena, so storing costs no copy. closureRef indexes it.
	subgraphClosures [][]VFS
	closureArena     *BumpAllocator[VFS]
	// scanCache holds the cached transitive closure and resolved children per
	// includer, keyed by includer STR ONLY — no scan-context component. Relies on
	// each file being parsed-and-resolved exactly once per run.
	//
	// Do NOT add a scanCtx component "for safety": the load-bearing assumption is
	// that the first scanner to reach a file is its semantic owner. A context key
	// collapses subgraph caching and regresses wall-time by an order of magnitude.
	//
	// Three columns in one DenseMap3: resolved children, closure ref, source-file
	// existence. Keyed by strID (unique per VFS, halving idx vs VFS space). The
	// columns fill at different times, so each relies on its own per-column presence.
	scanCache DenseMap3[STR, []VFS, ClosureRef, bool]

	// searchTierFlat caches resolveContextSearchTier results, keyed by
	// splitMix64(ctxNum, target STR). searchTierSeen is a per-target presence gate;
	// a miss there short-circuits straight to the resolve.
	searchTierFlat *IntValueMap[SearchTierResult]
	searchTierSeen BitSet

	// sourceUnderCache memoizes the includer-local quoted-include resolve — the
	// hottest existence probe (incDir is rarely an addincl). Context-free and
	// run-wide, keyed by splitMix64(incDir, target). The value is the resolved $(S)
	// VFS already interned (0 = "does not resolve here").
	sourceUnderCache *IntValueMap[VFS]

	// childArena holds the cached resolved-children blocks. Filled like the closure
	// arena: reserve, collect, commit. Collection never nests, so one pending block
	// suffices.
	childArena *BumpAllocator[VFS]

	// spOut / resolveOut back resolveSearchPath's and resolve's per-call result
	// slices, consumed by the caller before the next resolve.
	spOut      []VFS
	resolveOut []VFS

	// tjc points at the run-wide Tarjan/closure working state owned by genCtx,
	// shared by the target and host scanners.
	tjc *TarjanCtx

	// dfsActive marks the roots whose dfs is in flight. Set-once: closureOf
	// re-enters dfs(root) only along an include cycle, which dfs hands to
	// strongconnect. Per-scanner, so host roots and target roots stay separate.
	dfsActive BitSet

	visitedIDPool sync.Pool

	onWarn func(Warn)

	// generatedFirstClaim records the first module that resolved an include to a
	// CodegenRegistry output (first-DFS-leave-wins); the attribute_generated.go
	// finalize pass uses it to override producer-node target_properties. A
	// self-consuming RUN_PROGRAM records its OWN module dir at registration
	// (markGeneratedProducerOwned) before any consumer can resolve the output.
	generatedFirstClaim map[VFS]GenOwner

	// generatedNodeClaim records, keyed by a generated file's PRODUCER node ref, the
	// first module naming one of that producer's outputs in OUTPUT_INCLUDES — a
	// node-level attribution taking precedence over the per-output consensus.
	generatedNodeClaim map[NodeRef]string

	// generatedENIncluderDirs records, per EN output, the directories of files that
	// #include it — for directory-ownership of a serialized header reached through a
	// submodule's directory-owned header. Intrinsic to the includer, so context-free.
	generatedENIncluderDirs map[VFS][]string

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
	// are looked up here to mix their INDUCED_DEPS into its children. nil in
	// standalone scanners.
	moduleByRef *DenseMap[NodeRef, *ModuleEmitResult]
}

type ScanCtx struct {
	scanner      *IncludeScanner
	cfg          ScanContext
	ctxNum       uint32
	resolveIndex *CfgResolveIndex

	// parser handles files with UNREGISTERED extensions, resolved ONCE from the
	// walk's root. nil = C-like default.
	parser IncludeDirectiveParser
}

// closureRef is an index into IncludeScanner.subgraphClosures.
type ClosureRef uint32

// cachedChildren returns the resolved immediate children of v. Presence is the
// column slot, not nil-ness, so a resolved-but-empty set reads back present.
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

// sourceFileExists memoizes IsFile by the file VFS, so cached sysincl mappings
// probe the FS only once per file. The column's presence is the "already probed" bit.
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
	// closureAllocHint is the per-closure arena reservation: the measured maximum
	// closure size with a ~2x margin, so a single closure never exceeds it.
	closureAllocHint = 1 << 13 // 8192

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
		sysincl:                 newSysinclCtx(sysincl),
		parsers:                 parsers,
		generatedFirstClaim:     make(map[VFS]GenOwner, 2048),
		generatedNodeClaim:      make(map[NodeRef]string, 256),
		generatedENIncluderDirs: make(map[VFS][]string, 16),
		onWarn:                  onWarn,
		// Index 0 reserved so a fresh closureRef is always >= 1.
		subgraphClosures: make([][]VFS, 1, 256),
		closureArena:     newBumpAllocator[VFS](closureArenaInitial),
		childArena:       newBumpAllocator[VFS](1 << 12),
		searchTierFlat:   newIntValueMap[SearchTierResult](4096),
		sourceUnderCache: newIntValueMap[VFS](1 << 16),
		tjc:              tjc,
	}

	s.visitedIDPool.New = func() any {
		return &IdSet{}
	}

	return s
}

// markGeneratedProducerOwned records dir as the first-claim for a generated
// output of a module that auto-compiles a cc/asm sibling. Called at registration,
// so the producer is the guaranteed first writer.
func (s *IncludeScanner) markGeneratedProducerOwned(out VFS, dir string) {
	if _, ok := s.generatedFirstClaim[out]; !ok {
		s.generatedFirstClaim[out] = GenOwner{Dir: dir}
	}
}

type ScanContext struct {
	OwnAddIncl      []VFS
	PeerAddInclSet  []VFS
	BaseSearchPaths []VFS
	// OwnerModuleDir identifies the consumer module whose compile triggered this
	// scan. Populates generatedFirstClaim on the first resolve of a codegen output.
	OwnerModuleDir string

	// OwnerModuleTag is the module_tag of OwnerModuleDir (0 when none). NOT part of
	// hashScanContext — a claim side-channel never affecting the resolve.
	OwnerModuleTag STR

	// cfg is the resolved scan config, bound once by newScanContext.
	cfg *ScanConfig
}

// ScanConfig is one distinct resolve configuration: its dense id and the prebuilt
// resolve index. Deduped by resolveScanConfig.
type ScanConfig struct {
	num uint32
	ri  *CfgResolveIndex
}

// newScanContext builds a scan config and binds its resolved ScanConfig.
func newScanContext(pm *IncludeParserManager, ownAddIncl, peerAddIncl, base []VFS, ownerModuleDir string) ScanContext {
	cfg := ScanContext{
		OwnAddIncl:      ownAddIncl,
		PeerAddInclSet:  peerAddIncl,
		BaseSearchPaths: base,
		OwnerModuleDir:  ownerModuleDir,
	}
	cfg.cfg = pm.resolveScanConfig(&cfg)

	return cfg
}

// newScanCtx wraps cfg for this scanner; parser handles unregistered-extension
// files (nil = C default).
func (s *IncludeScanner) newScanCtx(cfg ScanContext, parser IncludeDirectiveParser) *ScanCtx {
	if cfg.cfg == nil {
		throwFmt("newScanCtx: ScanContext built without newScanContext")
	}

	return &ScanCtx{
		scanner:      s,
		cfg:          cfg,
		ctxNum:       cfg.cfg.num,
		resolveIndex: cfg.cfg.ri,
		parser:       parser,
	}
}

// hashScanContext fingerprints the resolve-relevant context fields for the
// in-memory config maps. Each element contributes its interned string's xxh3 lo,
// chained through mix64; length-prefixing keeps the slice boundaries unambiguous.
func hashScanContext(ctx *ScanContext) uint64 {
	// Non-zero seed so the all-empty context cannot hash to 0 (the "unsealed"
	// sentinel newScanCtx guards on).
	h := uint64(0x9e3779b97f4a7c15)

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

	// suppressCimportNames: when a `from X cimport names` module .pxd resolves, the
	// names are within it, not submodules, so their probes are skipped. A statement
	// opener clears the flag; name/fallback probes do not.
	//
	// prevProbeMissed: a fallback probe runs only when its primary was attempted and
	// did not resolve. A suppressed primary counts as not-missed.
	suppressCimportNames := false
	prevProbeMissed := false

	for _, entry := range s.parsers.parsedIncludes(vfsPath, sc.parser) {
		isName := entry.kind == includeCythonName
		isFallback := entry.kind == includeCythonFallback

		if !isName && !isFallback {
			suppressCimportNames = false
		}

		if (isName && suppressCimportNames) || (isFallback && !prevProbeMissed) {
			prevProbeMissed = false

			continue
		}

		resolved := sc.resolve(vfsPath, incDir, entry)

		for _, rabs := range resolved {
			fn(rabs)
		}

		prevProbeMissed = len(resolved) == 0

		if entry.kind == includeCythonModule && len(resolved) > 0 {
			suppressCimportNames = true
		}
	}

	sc.resolveInducedDeps(vfsPath, incDir, fn)
}

// resolveInducedDeps mixes the INDUCED_DEPS of a generated file's producing tools
// into its resolved children. Only build outputs with a codegen registry entry
// and recorded GeneratorRefs contribute.
func (sc *ScanCtx) resolveInducedDeps(vfsPath VFS, incDir VFS, fn func(rabs VFS)) {
	s := sc.scanner

	if !vfsPath.isBuild() {
		return
	}

	info := s.codegen.lookup(vfsPath)

	if info == nil {
		return
	}

	// A header output reads the Header bucket; a translation unit reads Cpp.
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

// forEachResolvedChildID returns the resolved immediate children of abs, caching
// by abs alone. See scanCache for the once-per-run invariant that makes it correct.
func (sc *ScanCtx) forEachResolvedChildID(abs VFS, fn func(VFS)) {
	s := sc.scanner

	if cached, ok := s.cachedChildren(abs); ok {
		for _, id := range cached {
			fn(id)
		}

		return
	}

	// Collect into an arena block. Nothing else touches childArena while it is open.
	block := s.childArena.alloc(closureAllocHint)
	k := 0
	sc.forEachResolvedChild(abs, func(rabs VFS) {
		block[k] = rabs
		k++
	})
	s.childArena.commit(k)

	var children []VFS

	if k > 0 {
		children = block[:k:k]
	}

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

// dfs builds the transitive include closure of any root. Most files are acyclic,
// so a flat dfs suffices: abs leads its own closure with the children's flat
// windows spliced in. On a cycle, dfsActive re-enters dfs(abs) before abs is
// cached and the guard hands abs to strongconnect, which collapses the SCC.
//
// Two passes, required by the single-pending arena: pass 1 builds and caches each
// child's closure (closureOf may allocate from closureArena); pass 2 reserves our
// block and splices each child's cached window, allocating nothing else.
func (sc *ScanCtx) dfs(abs VFS) {
	s := sc.scanner

	if s.dfsActive.has(uint32(abs)) {
		s.subgraphHits += s.tjc.runSCC(sc, abs)

		return
	}

	s.dfsActive.add(uint32(abs))

	// Pass 1: build and cache each child's closure. Skip the self-edge: abs is not
	// cached yet, and it leads the window below anyway.
	sc.forEachResolvedChildID(abs, func(ch VFS) {
		if ch == abs {
			return
		}

		sc.closureOf(ch)
	})

	// Pass 2: every child is cached now, so splice its window from the cache and
	// nothing here allocates from closureArena while the block is open.
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

	// Splice non-expanded closure leaves for every $(B) member: bare window members
	// that ride transitively but are never traversed as children.
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
// closure block under construction. Windows are transitively closed, so ch
// arriving via an earlier splice means that window contained closure(ch). The
// leafEver guard keeps this sound: a ClosureLeaf rides as a bare member whose
// presence does NOT imply its window is present, so it never short-circuits.
func (sc *ScanCtx) windowSubsumed(ch VFS) bool {
	s := sc.scanner

	if !s.tjc.closure.has(ch) {
		return false
	}

	if s.codegen.isLeafEver(ch) {
		return false
	}

	s.subgraphSubsumed++

	return true
}

// scanCtx implements closureSink so strongconnect can build SCC closures without
// depending on scanner internals.

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

// emitClosure reserves an arena block, lets fill write the deduped closure (count
// returned), then commits that prefix into subgraphClosures and caches it for
// every member.
func (sc *ScanCtx) emitClosure(members []VFS, fill func(block []VFS) int) {
	s := sc.scanner

	block := s.closureArena.alloc(closureAllocHint)
	k := fill(block)

	// Splice non-expanded closure leaves for every $(B) member (same as dfs pass-2).
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
	s := sc.scanner

	// A rooted target is already bound to its root and classifies directly, with no
	// include search, sysincl, or FS check.
	if v := d.target.vfs(); v != 0 {
		// A rooted include of a generated header is still attributed to the first
		// module that leaves it in DFS post-order, so record the first-claim. Gated
		// to build targets with an owner.
		if v.isBuild() && sc.cfg.OwnerModuleDir != "" {
			if info := s.codegen.lookupSTR(d.target); info != nil {
				s.recordFirstClaim(info.OutputPath, sc.cfg.OwnerModuleDir, sc.cfg.OwnerModuleTag)
			}
		}

		out = append(s.resolveOut[:0], v)
		s.resolveOut = out

		return out
	}

	var sysinclClaimed bool

	defer func() {
		// Cython cimport probes are best-effort: never warned.
		if d.cythonProbe() {
			return
		}

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

	if d.quotedLike() && len(searchOut) > 0 {
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
		res := s.resolveOut[:0]

	fastLoop:
		for _, abs := range mappings {
			for _, q := range res {
				if q == abs {
					continue fastLoop
				}
			}

			if !s.sourceFileExists(abs) {
				continue
			}

			res = append(res, abs)
		}

		s.resolveOut = res

		if len(res) == 0 {
			return nil
		}

		out = res

		return out
	}

	merged := s.resolveOut[:0]
	added := false

mapLoop:
	for _, abs := range mappings {
		base := searchOut

		if added {
			base = merged
		}

		for _, q := range base {
			if q == abs {
				continue mapLoop
			}
		}

		if !s.sourceFileExists(abs) {
			continue
		}

		if !added {
			merged = append(merged, searchOut...)
			added = true
		}

		merged = append(merged, abs)
	}

	s.resolveOut = merged

	if !added {
		return searchOut
	}

	out = merged

	return out
}

type CfgResolveIndex struct {
	indexable    bool
	rank         *IntValueMap[int32]
	buildEntries []CfgBuildAddincl
}

// cfgBuildAddincl is a Build-rooted addincl prefix paired with its rank in
// declaration order. prefixSrc is the pre-interned Source-rooted twin, so
// codegen.LookupSplit needs no per-resolve string work.
type CfgBuildAddincl struct {
	prefix    VFS
	prefixSrc VFS
	rank      int
}

const resolveNoRank = int(^uint(0) >> 1)

// buildCfgResolveIndex assigns a declaration-order rank to every addincl entry so
// the fast path can pick the first-wins match. Source entries keep their
// inverted-index lookup; Build entries are collected for a codegen.LookupSplit pass.
func buildCfgResolveIndex(cfg *ScanContext) *CfgResolveIndex {
	idx := &CfgResolveIndex{}

	for _, p := range cfg.OwnAddIncl {
		if p.isSource() && p.rel() == "" {
			return idx
		}
	}

	for _, p := range cfg.PeerAddInclSet {
		if p.isSource() && p.rel() == "" {
			return idx
		}
	}

	idx.indexable = true
	idx.rank = newIntValueMap[int32](2 * (len(cfg.OwnAddIncl) + len(cfg.PeerAddInclSet)))

	// Membership rides the global epoch deduper.
	deduper.reset()

	r := int32(0)
	add := func(p VFS) {
		if !deduper.add(p) {
			return
		}

		idx.rank.put(uint64(p), r)

		if p.isBuild() {
			idx.buildEntries = append(idx.buildEntries, CfgBuildAddincl{
				prefix:    p,
				prefixSrc: source(p.rel()),
				rank:      int(r),
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

// recordFirstClaim applies first-write-wins: the first include-scan resolving a
// CodegenRegistry output records the consumer module that owns it.
func (s *IncludeScanner) recordFirstClaim(out VFS, ownerModuleDir string, ownerModuleTag STR) {
	if ownerModuleDir == "" {
		return
	}

	if _, ok := s.generatedFirstClaim[out]; !ok {
		s.generatedFirstClaim[out] = GenOwner{Dir: ownerModuleDir, Tag: ownerModuleTag}
	}
}

// recordNodeClaim records, first-write-wins, the module naming an output of
// producer node `ref` in OUTPUT_INCLUDES. See generatedNodeClaim.
func (s *IncludeScanner) recordNodeClaim(ref NodeRef, ownerModuleDir string) {
	if ownerModuleDir == "" {
		return
	}

	if _, ok := s.generatedNodeClaim[ref]; !ok {
		s.generatedNodeClaim[ref] = ownerModuleDir
	}
}

// recordENIncluderDir records includerAbs's directory as an includer of EN output
// `out`. The finalize pass reads these to drift the EN node to a nested submodule
// whose directory-owned header includes it.
func (s *IncludeScanner) recordENIncluderDir(out VFS, info *GeneratedFileInfo, includerAbs VFS) {
	if info == nil || info.ProducerKvP != pkEN {
		return
	}

	dir := pathDir(includerAbs.rel())

	if dir == "" {
		return
	}

	cur := s.generatedENIncluderDirs[out]

	for _, d := range cur {
		if d == dir {
			return
		}
	}

	s.generatedENIncluderDirs[out] = append(cur, dir)
}

func (sc *ScanCtx) cacheSearchTier(targetID STR, out SearchTierResult) SearchTierResult {
	s := sc.scanner
	s.searchTierFlat.put(splitMix64(sc.ctxNum, uint32(targetID)), out)
	s.searchTierSeen.add(uint32(targetID))

	return out
}

func (sc *ScanCtx) resolveContextSearchTier(targetID STR) SearchTierResult {
	s := sc.scanner

	// Gate the hash probe on the per-target presence flag.
	if s.searchTierSeen.has(uint32(targetID)) {
		if cached := s.searchTierFlat.get(splitMix64(sc.ctxNum, uint32(targetID))); cached != nil {
			s.searchTierHits++

			return *cached
		}
	}

	s.searchTierMisses++

	// The string view is only needed on the miss path.
	target := targetID.string()

	var out SearchTierResult

	normTarget := normalisePath(target)

	addSource := func(prefix VFS) bool {
		v := s.resolveSourceUnder(prefix, target)

		if v == 0 {
			return false
		}

		out.paths = []VFS{v}
		out.found = true

		return true
	}

	buildSuffix := interned(normTarget)

	addBuild := func(prefixRel string) bool {
		if buildSuffix == 0 {
			return false
		}

		var info *GeneratedFileInfo

		if prefixRel == "" {
			info = s.codegen.lookupSTR(buildSuffix)
		} else if pid := internedPrefixed("$(S)/", prefixRel); pid != 0 {
			info = s.codegen.lookupSplit(pid.vfs(), buildSuffix)
		}

		if info == nil {
			return false
		}

		out.paths = []VFS{info.OutputPath}
		out.found = true

		s.recordFirstClaim(info.OutputPath, sc.cfg.OwnerModuleDir, sc.cfg.OwnerModuleTag)

		return true
	}

	addInclPath := func(prefix VFS) bool {
		if prefix.isBuild() {
			return addBuild(prefix.rel())
		}

		return addSource(prefix)
	}

	// The build + source roots precede the module's ADDINCL: a fully-qualified
	// target existing at a root binds there, not under a peer ADDINCL mirroring the
	// same subtree. The includer-dir arm is handled in resolveSearchPath.
	if addInclPath(bld) || addInclPath(v) {
		return sc.cacheSearchTier(targetID, out)
	}

	first, _ := firstComponent(target)

	if canRelFilter(first, target) && !strings.Contains(target, "/./") && !strings.Contains(target, "//") {
		idx := sc.resolveIndex

		if idx.indexable {
			// Source side: precomputed FS-existence inverted index; smallest rank wins.
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

			// Build side: walk the Build-rooted addincl entries via the split
			// lookup; a hit beating the Source best wins (first-match order).
			var bestBuild *GeneratedFileInfo

			if buildSuffix != 0 {
				for i := range idx.buildEntries {
					b := &idx.buildEntries[i]

					if b.rank >= bestRank {
						continue
					}

					info := s.codegen.lookupSplit(b.prefixSrc, buildSuffix)

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

					s.recordFirstClaim(bestBuild.OutputPath, sc.cfg.OwnerModuleDir, sc.cfg.OwnerModuleTag)
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

	// out doubles as the dedup set (linear scan over <= 3 elements). Backed by the
	// per-scanner scratch: the caller consumes the result before the next resolve.
	out := s.spOut[:0]

	defer func() {
		s.spOut = out[:0]
	}()

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

	searchPathFound := false

	if candidate, ok := cythonPy2SiblingOverride(includerAbs, d); ok && addPath(candidate) {
		searchPathFound = true
	}

	// No slash gate before the probe: a slashless target just misses the DenseMap,
	// cheaper than the string view a gate would cost.
	if includerAbs.isBuild() {
		if info := s.codegen.lookupSTR(d.target); info != nil && !outHas(info.OutputPath) {
			out = append(out, info.OutputPath)
			searchPathFound = true

			s.recordFirstClaim(info.OutputPath, sc.cfg.OwnerModuleDir, sc.cfg.OwnerModuleTag)
			s.recordENIncluderDir(info.OutputPath, info, includerAbs)
		}
	}

	if d.quotedLike() {
		matched := false

		// Memoize the includer-local resolve by splitMix64(incDir, target).
		suKey := splitMix64(uint32(incDir), uint32(d.target))
		var sv VFS

		if p := s.sourceUnderCache.get(suKey); p != nil {
			sv = *p
		} else {
			// 0 is the shared "does not resolve" sentinel, cached as-is.
			sv = s.resolveSourceUnder(incDir, d.target.string())
			s.sourceUnderCache.put(suKey, sv)
		}

		if sv != 0 {
			out = append(out, sv)
			searchPathFound = true
			matched = true
		}

		if !matched {
			if info := s.codegen.lookupSplit(incDir, d.target); info != nil {
				if !outHas(info.OutputPath) {
					out = append(out, info.OutputPath)
					searchPathFound = true
				}

				// Mirror addBuild: a quoted include resolving to a generated
				// path records the first consumer module for the finalize pass.
				s.recordFirstClaim(info.OutputPath, sc.cfg.OwnerModuleDir, sc.cfg.OwnerModuleTag)
				s.recordENIncluderDir(info.OutputPath, info, includerAbs)
			}
		}
	}

	if !searchPathFound {
		tier := sc.resolveContextSearchTier(d.target)

		if tier.found {
			out = append(out, tier.paths...)
			searchPathFound = true

			// Angle/full-path includes of an EN serialized header resolve here;
			// record the includer dir for EN drift.
			if len(tier.paths) > 0 && tier.paths[0].isBuild() {
				s.recordENIncluderDir(tier.paths[0], s.codegen.lookup(tier.paths[0]), includerAbs)
			}
		}
	}

	return out
}

func cythonPy2SiblingOverride(includerAbs VFS, d IncludeDirective) (string, bool) {
	if !includerAbs.isSource() || !d.quotedLike() {
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

// resolveSourceUnder resolves target relative to prefix; 0 means "no such file".
func (s *IncludeScanner) resolveSourceUnder(prefix VFS, target string) VFS {
	// IsFile keyed off the already-interned prefix VFS skips re-gating the prefix's
	// components.
	if !s.parsers.fs.isFile(prefix, target) {
		return 0
	}

	// A clean target joins in the scratch buffer — no concat, no normalise.
	if target != "" && pathIsClean(target) {
		return sourceJoined(prefix.rel(), target)
	}

	return source(normalisePath(joinRel(prefix.rel(), target)))
}

func canRelFilter(first, target string) bool {
	return first != "" && first != "." && first != ".." && !strings.Contains(target, "/..")
}
