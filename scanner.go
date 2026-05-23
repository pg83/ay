package main

// scanner.go — C/C++ #include transitive-closure scanner. Text-based
// regex match, conditional-blind, ADDINCL + peer-GLOBAL ADDINCL +
// sysincl resolution, DFS with per-source visited set.
//
// `#include MACRO_NAME` macro-expanded forms handled case-by-case via
// `macroIndirectIncludes`. `stripComments` (in parsers.go) blanks
// comment / string-literal payloads before regex matching — without it
// a `/* ... #include <iostream> ... */` block inside
// `from_chars_integral.h` would flood every `<charconv>` consumer with
// phantom `<iostream>`.

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"unsafe"
)

// The scanner core works on typed VFS paths:
//
//   - `Source(rel)` — real on-disk file under sourceRoot
//   - `Build(rel)` — generated output tracked by the per-scanner
//     CodegenRegistry
//
// String materialization is kept only at the boundaries that still need
// the canonical `$(S)/...` / `$(B)/...` spelling (serializer and a few
// compatibility interfaces).

// includeKind discriminates `<...>` (system) from `"..."` (quoted).
// `#include_next` retains its directive form via `next` and is
// otherwise treated as system for search-path resolution.
type includeKind int

const (
	includeSystem includeKind = iota
	includeQuoted
)

// includeDirective is one parsed `#include` from a source file. `next`
// distinguishes `#include_next` (treated as resolving to nothing — the
// upstream scanner does not synthesise sysincl entries for it, and
// following libcxx's `__has_include_next` shadow-header pattern is the
// dominant over-fan-out source).
type includeDirective struct {
	kind   includeKind
	next   bool
	target string
}

// scannerInterner assigns scanner-local numeric IDs to repeated strings,
// VFS paths, and source-class signatures so hot cache keys stay compact
// and avoid repeated string hashing in their own maps.
type scannerInterner struct {
	stringIDs map[string]uint32
	strings   []string
	nextStr   uint32
}

const scannerInternerBuildBit = uint32(1) << 31

func newScannerInterner() scannerInterner {
	return scannerInterner{
		stringIDs: make(map[string]uint32, 32768),
		strings:   make([]string, 1, 32769),
	}
}

func (si *scannerInterner) internString(s string) uint32 {
	if id, ok := si.stringIDs[s]; ok {
		return id
	}

	if si.nextStr == scannerInternerBuildBit-1 {
		panic("scannerInterner: exhausted 31-bit string ID space")
	}

	si.nextStr++
	id := si.nextStr
	si.stringIDs[s] = id
	si.strings = append(si.strings, s)

	return id
}

func (si *scannerInterner) internVFS(v VFS) uint32 {
	relID := si.internString(v.Rel)

	switch v.Root {
	case VFSRootSource:
		return relID
	case VFSRootBuild:
		return relID | scannerInternerBuildBit
	}

	panic("scannerInterner.internVFS: zero-valued VFS")
}

// relIDBound returns an exclusive upper bound on relIDs interned so far,
// suitable for sizing a relID-indexed idSet.
func (si *scannerInterner) relIDBound() uint32 {
	return si.nextStr + 1
}

// vfsByID reconstructs a VFS previously interned through internVFS.
// The caller must pass an ID produced by internVFS, not a raw string ID.
func (si *scannerInterner) vfsByID(id uint32) VFS {
	root := VFSRootSource
	if id&scannerInternerBuildBit != 0 {
		root = VFSRootBuild
		id &^= scannerInternerBuildBit
	}

	if id == 0 || id >= uint32(len(si.strings)) {
		panic("scannerInterner.vfsByID: out-of-range VFS ID")
	}

	return VFS{Root: root, Rel: si.strings[id]}
}

// IncludeScanner is the per-walker include-resolver state. It owns the
// SysInclSet, the parser manager (SOURCE_ROOT FS access + raw scan),
// the per-scanCtx resolve/subgraph caches, scratch-buffer sync.Pools,
// and the sysincl per-half caches.
//
// The scanner is invoked exclusively from gen.go's serial walker — no
// locking. If a future change introduces per-source goroutines, every
// cache access site needs a mutex reintroduced.
type IncludeScanner struct {
	sysincl SysInclSet
	// parsers owns SOURCE_ROOT FS access, parse/existence caches, and
	// ext-dispatch for raw include scanning. Shared between target/host
	// scanners so they reuse the same source-tree work.
	parsers *includeParserManager
	// interner assigns scanner-local numeric IDs for hot cache keys.
	interner scannerInterner
	// anySrcView is a PerSourceView prepared with an empty source path.
	// Its `includerKeyed` slice is the canonical includer-keyed record
	// list (every view derives the same slice); the `activeSourceKeyed`
	// half is empty (no source-keyed filter accepts ""). Used as a
	// lock-free shortcut by sysinclIncluderLookup.
	anySrcView PerSourceView
	// sourceClassCache maps a concrete source path to its SOURCE-keyed
	// sysincl equivalence class ID.
	sourceClassCache map[string]uint32
	// sourceClassViews stores one source-only view per equivalence class.
	// Unlike PerSourceView from PreparePerSource, these views keep only
	// activeSourceKeyed; includer-side state stays solely in anySrcView.
	sourceClassViews map[uint32]PerSourceView
	// sourceClassBuckets guards against sourceClassSignature collisions:
	// equal signatures only reuse an ID after the active record pointer
	// lists compare equal.
	sourceClassBuckets map[uint64][]uint32
	nextSourceClass    uint32
	sourceKeyedCount   int
	// sysinclSourceCache memoises the source-keyed sysincl half by
	// (sourceClass, target). Source-keyed records are includer-
	// independent, so every source in the same active-record class hits
	// the same entry.
	sysinclSourceCache map[sysinclSourceKey]sysinclCacheEntry
	// sysinclIncluderCache memoises the includer-keyed half by
	// (includerRel, target). Includer-keyed records are source-
	// independent.
	sysinclIncluderCache map[sysinclIncluderKey]sysinclCacheEntry

	// subgraphCache / childrenCache are GLOBAL to the scanner (per run),
	// keyed by absID, NOT per scanCtx. Upstream ymake resolves each file's
	// includes once — in the context of whichever module first reaches it —
	// and reuses that result everywhere via the shared dep graph; we mirror
	// that: the first scanCtx to resolve a file populates these caches with
	// its ADDINCL ctx, and every later module (different ctxHash) reuses it.
	// This collapses the per-ctxHash duplication (one closure per file for
	// the whole run instead of one per module-include-config).
	//   subgraphCache: full transitive include closure (incl. the node),
	//     deduplicated, computed by closureOf via Tarjan SCC — cyclic
	//     closures cache exactly like acyclic ones (SCC members share a
	//     slice); no node is re-walked. Order is irrelevant (normalize sorts).
	//   childrenCache: forEachResolvedChildID's resolved-child ID list,
	//     read twice per node (edge classification + SCC-finalize union).
	subgraphCache map[uint32][]uint32
	childrenCache map[uint32][]uint32

	// Tarjan SCC scratch, shared across closure explorations (gen scanning
	// is single-goroutine). closureOf clears index/low/onStack and resets
	// stack/next per top-level exploration; tjClosure + tjBuf are the
	// reusable dedup set + accumulator for an SCC's merged closure.
	tjIndex   map[uint32]int32
	tjLow     map[uint32]int32
	tjOnStack map[uint32]bool
	tjStack   []uint32
	tjNext    int32
	tjClosure idSet
	tjBuf     []uint32

	visitedIDPool sync.Pool // *idSet
	orderIDPool   sync.Pool // *[]uint32
	// seenPool reuses the per-resolveSearchPath dedup map across calls.
	// Keys are rel-form strings — dedup never crosses VFS roots, and
	// rel keys are slightly cheaper than VFS-keyed.
	seenPool sync.Pool // *map[string]struct{}

	// walkClosureCache interns scanCtx instances created via the
	// top-level WalkClosure entry (test-facing path) so repeat calls on
	// the same ctxHash hit shared resolve / subgraph caches. Production
	// callers intern through genCtx.getScanCtx instead.
	walkClosureCache map[uint64]*scanCtx

	// onWarn surfaces resolve-time diagnostics — primarily include
	// directives that found no match in source tree, build tree, OR
	// sysincl mappings. Caller-supplied; no-op in the default-quiet
	// CLI, stderr printer under `--verbose`.
	onWarn func(Warn)

	// subgraphHits/subgraphMisses count cache traffic for verification.
	// Plain uint64; single-goroutine.
	walkClosureCalls       uint64
	dfsCalls               uint64
	plainDfsCalls          uint64
	subgraphHits           uint64
	subgraphMisses         uint64
	subgraphTainted        uint64
	searchTierHits         uint64
	searchTierMisses       uint64
	resolveSearchPathCalls uint64
	resolveCacheHits       uint64
	resolveCacheMisses     uint64
	sysinclSourceHits      uint64
	sysinclSourceMisses    uint64
	sysinclIncluderHits    uint64
	sysinclIncluderMisses  uint64
	statsCallCount         uint64

	// codegen is the per-scanner registry of codegen-emitted file
	// metadata. Nil means the registry is not active (tests that
	// construct scanners directly via NewIncludeScanner). GenWith wires
	// it; resolveSearchPath consults it via codegenLocator.
	codegen *CodegenRegistry

	// fallbackLocators holds the VFS-codegen tier (and any future non-FS
	// resolution tier). Consulted by resolveSearchPath only AFTER the
	// regular on-disk search-path walk fails for every candidate. The FS
	// tier stays inline because each search-path tier prepends a
	// different prefix; the codegen tier resolves on the target name
	// alone (path lives under $(B)/) and runs once as fallback.
	fallbackLocators []pathLocator
}

// resolveInnerKey is the per-scanCtx resolve cache key. ctxHash is NOT
// part of the key — the scanCtx is bound to a single ctxHash, so every
// entry in its resolveCache is implicitly that-ctxHash-only.
type resolveInnerKey struct {
	includer uint32
	target   uint32
	flags    uint8
}

// scanCtx is the per-ctxHash runtime context for include resolution. It
// owns every cache whose key contains ctxHash. Two lifecycle policies
// (see gen.go): "local" (fresh per genModule call) and "interned"
// (genCtx-owned, shared across modules whose ScanContext shape matches).
type scanCtx struct {
	scanner *IncludeScanner
	cfg     ScanContext
	ctxHash uint64

	resolveCache    map[resolveInnerKey][]VFS
	searchTierCache map[uint32]searchTierResult
}

// idSet is a membership set over interned scanner IDs, used as the DFS
// `visited` set. Interned IDs are NOT dense (build-VFS IDs carry
// scannerInternerBuildBit), but the underlying relID space is, so the set
// is backed by two generation-stamped slices indexed by relID — one for
// source roots, one for build roots. Membership is O(1) array indexing
// with no hashing, and reset is an O(1) epoch bump instead of an O(n) map
// clear. Reused across walks via the scanner's idSet pool; large graphs
// re-walk tainted subgraphs millions of times, so the per-access constant
// dominates. Pass by pointer: add() may reallocate the backing slices.
type idSet struct {
	srcGen []uint32
	bldGen []uint32
	epoch  uint32
}

// reset clears the set in O(1) by bumping the epoch, ensuring backing
// capacity for relIDs in [0, size). On epoch wraparound (every 2^32
// resets) the slices are zeroed so stale stamps cannot alias the new
// epoch.
func (s *idSet) reset(size uint32) {
	if uint32(len(s.srcGen)) < size {
		s.srcGen = make([]uint32, size)
		s.bldGen = make([]uint32, size)
		s.epoch = 1

		return
	}

	s.epoch++
	if s.epoch == 0 {
		for i := range s.srcGen {
			s.srcGen[i] = 0
			s.bldGen[i] = 0
		}

		s.epoch = 1
	}
}

func (s *idSet) has(id uint32) bool {
	if id&scannerInternerBuildBit != 0 {
		rel := id &^ scannerInternerBuildBit

		return rel < uint32(len(s.bldGen)) && s.bldGen[rel] == s.epoch
	}

	return id < uint32(len(s.srcGen)) && s.srcGen[id] == s.epoch
}

// add records id as a member, growing the backing slices if a freshly
// interned relID outran the size pinned at reset.
func (s *idSet) add(id uint32) {
	rel := id
	build := id&scannerInternerBuildBit != 0
	if build {
		rel = id &^ scannerInternerBuildBit
	}

	if rel >= uint32(len(s.srcGen)) {
		grown := uint32(len(s.srcGen)) * 2
		if grown <= rel {
			grown = rel + 1
		}

		src := make([]uint32, grown)
		bld := make([]uint32, grown)
		copy(src, s.srcGen)
		copy(bld, s.bldGen)
		s.srcGen = src
		s.bldGen = bld
	}

	if build {
		s.bldGen[rel] = s.epoch
	} else {
		s.srcGen[rel] = s.epoch
	}
}

type searchTierResult struct {
	paths []VFS
	found bool
}

type sysinclSourceKey struct {
	sourceClass uint32
	target      uint32
}

type sysinclIncluderKey struct {
	includer uint32
	target   uint32
}

// sysinclCacheEntry stores resolved sysincl paths plus two flags.
// hasMultiTarget: any contributing record maps the header to ≥ 2 non-
// empty paths (drives the quoted-include gate). claimed: at least one
// record's filter matched and listed the header, even with empty paths
// (bare-key suppression) — lets resolve() distinguish "known but
// suppressed" (no warning) from "unknown" (warn under --verbose).
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
	resolveCacheHits       uint64
	resolveCacheMisses     uint64
	sysinclSourceHits      uint64
	sysinclSourceMisses    uint64
	sysinclIncluderHits    uint64
	sysinclIncluderMisses  uint64
}

// NewIncludeScanner constructs a scanner bound to a SysInclSet and a
// source-root absolute path. Allocates a private parser manager; use
// newIncludeScannerWith to share one between scanners.
func NewIncludeScanner(sourceRoot string, sysincl SysInclSet) *IncludeScanner {
	return newIncludeScannerWith(newIncludeParserManager(sourceRoot), sysincl, func(Warn) {})
}

// newIncludeScannerWith is the internal constructor used when a parser
// manager is provided externally (target/host pair in GenWith).
// parsers must be non-nil and tied to the same source root consumed by
// both scanners.
func newIncludeScannerWith(parsers *includeParserManager, sysincl SysInclSet, onWarn func(Warn)) *IncludeScanner {
	// Pre-sizes set to the upper bound of the observed working set for
	// tools/archiver; sysinclSourceCache reaches ~328k entries on the
	// target scanner, so pre-sizing past the peak eliminates rehashing.
	s := &IncludeScanner{
		sysincl:              sysincl,
		parsers:              parsers,
		interner:             newScannerInterner(),
		sourceClassCache:     make(map[string]uint32, 1024),
		sourceClassViews:     make(map[uint32]PerSourceView, 1024),
		sourceClassBuckets:   make(map[uint64][]uint32, 1024),
		sysinclSourceCache:   make(map[sysinclSourceKey]sysinclCacheEntry, 131072),
		sysinclIncluderCache: make(map[sysinclIncluderKey]sysinclCacheEntry, 8192),
		walkClosureCache:     make(map[uint64]*scanCtx, 8),
		onWarn:               onWarn,
		subgraphCache:        make(map[uint32][]uint32, 65536),
		childrenCache:        make(map[uint32][]uint32, 65536),
		tjIndex:              make(map[uint32]int32, 4096),
		tjLow:                make(map[uint32]int32, 4096),
		tjOnStack:            make(map[uint32]bool, 4096),
	}
	for i := range sysincl {
		if sysincl[i].KeyBySource {
			s.sourceKeyedCount++
		}
	}
	s.anySrcView = s.sysincl.PreparePerSource("")

	// Pool factories preallocate the same capacity that the
	// non-pooled WalkClosure used (64 entries). Pooled items are
	// returned as pointers to keep `Pool.Put` from boxing the
	// value (a plain map or slice would box-allocate on Put).
	s.visitedIDPool.New = func() any {
		return &idSet{}
	}

	s.orderIDPool.New = func() any {
		o := make([]uint32, 0, 64)

		return &o
	}

	// Per-resolve dedup maps are tiny (1-6 entries typical); start
	// with a small bucket and let it grow once for the rare large
	// resolution.
	s.seenPool.New = func() any {
		m := make(map[string]struct{}, 8)

		return &m
	}

	return s
}

// ScanContext carries the per-CC-node resolution context: the effective
// ADDINCL search path and the source-relative path of the primary input
// (for sysincl source_filter matching). The search path concatenates:
// source's own directory (quoted only), module's own ADDINCL, peer-
// propagated GLOBAL ADDINCL, and the BaseSearchPaths fallback baseline.
type ScanContext struct {
	SourceRel       string // SOURCE_ROOT-relative path of the primary source
	OwnAddIncl      []VFS  // module's own non-GLOBAL ADDINCL
	PeerAddInclSet  []VFS  // peer-propagated GLOBAL ADDINCL (transitive)
	BaseSearchPaths []VFS  // bundled fallback include set (repo-root/linux-headers)
}

// NewScanCtx allocates a fresh per-context resolution object bound to
// this scanner and the given ScanContext. The returned scanCtx owns its
// own resolveCache and subgraphCache; lifetime is the caller's (see
// gen.go's local vs interned dispatch). ctxHash is computed once at
// construction.
func (s *IncludeScanner) NewScanCtx(cfg ScanContext) *scanCtx {
	// Modest pre-sizes: many distinct ctxHashes exist, so over-sizing
	// every context wastes memory; the few large contexts grow.
	return &scanCtx{
		scanner:         s,
		cfg:             cfg,
		ctxHash:         hashScanContext(&cfg),
		resolveCache:    make(map[resolveInnerKey][]VFS, 1024),
		searchTierCache: make(map[uint32]searchTierResult, 256),
	}
}

// WalkClosure returns the SOURCE_ROOT-prefixed transitive-header set
// for the given source file (excluding the source itself), in DFS-
// discovery order. Suitable for `node.Inputs[1:]`. Test-facing entry —
// production callers in gen.go hold a scanCtx and call WalkSource so
// multiple sources within a module share caches.
//
// visited/order are pulled from sync.Pools; the returned slice is freshly
// allocated, so the caller may keep it past Pool.Put.
func (s *IncludeScanner) WalkClosure(cfg ScanContext) []VFS {
	// Intern per (scanner, ctxHash) so repeat calls on the same context
	// hit the previous call's resolve/subgraph caches.
	ctxHash := hashScanContext(&cfg)

	sc, ok := s.walkClosureCache[ctxHash]

	if !ok {
		sc = s.NewScanCtx(cfg)
		s.walkClosureCache[ctxHash] = sc
	}

	return sc.WalkSource(cfg.SourceRel)
}

// WalkSource walks the include closure starting from `sourceRel` using
// the receiver's already-bound context. Used by gen.go's per-module
// dispatch: one scanCtx per (scanner, ctxHash), reused for every source
// in the module. Thin shim over WalkClosure so callers that already
// hold a VFS path hit the uniform entry point.
func (sc *scanCtx) WalkSource(sourceRel string) []VFS {
	return sc.WalkClosure(Source(sourceRel))
}

// WalkClosure walks the include closure rooted at `vfsPath` ($(S)/ or
// $(B)/-rooted). Returns the transitive header set excluding the root,
// in DFS-discovery order; the result slice is freshly allocated.
//
// For $(S)/ roots SourceRel is overwritten from the VFS path so cross-
// source DFS within one scanCtx keys sysincl per source. $(B)/ roots
// pull children from pre-resolved EmitsIncludes — no sysincl at root.
func (sc *scanCtx) WalkClosure(vfsPath VFS) []VFS {
	s := sc.scanner
	s.walkClosureCalls++

	// scanCtx is shared across sources within a module; overwrite
	// cfg.SourceRel so resolve()'s sysinclSourceLookup keys on the
	// CURRENT source. For $(B)/ roots there is no meaningful source-rel,
	// and forEachResolvedChild's BUILD branch never consults SourceRel.
	if vfsPath.IsSource() {
		sc.cfg.SourceRel = vfsPath.Rel
	}

	visited := s.visitedIDPool.Get().(*idSet)
	visited.reset(s.interner.relIDBound())
	orderP := s.orderIDPool.Get().(*[]uint32)

	order := (*orderP)[:0]
	rootID := s.interner.internVFS(vfsPath)

	sc.dfsID(rootID, visited, &order)

	out := make([]VFS, 0, len(order))

	for _, absID := range order {
		// Skip the root itself; only headers are emitted.
		if absID == rootID {
			continue
		}

		out = append(out, s.interner.vfsByID(absID))
	}

	// Return scratch buffers to the pool. The idSet is cleared lazily by
	// the next reset()'s epoch bump, not here.
	*orderP = order[:0]

	s.visitedIDPool.Put(visited)
	s.orderIDPool.Put(orderP)

	if scannerStatsEnabled {
		s.statsCallCount++

		// SCANNER_STATS env-gated trace; emit every 500 calls. The
		// boolean check short-circuits in production.
		if s.statsCallCount%500 == 0 {
			fmt.Fprintf(os.Stderr, "scanner-stats[%d]: subgraph hits=%d misses=%d tainted=%d cache=%d\n", s.statsCallCount, s.subgraphHits, s.subgraphMisses, s.subgraphTainted, len(s.subgraphCache))
		}
	}

	return out
}

// IncludeDirectiveTargets returns the raw include-directive target
// strings scanned from `vfsPath`, in source order, with no resolution
// applied. Memoised through the same parse-cache WalkClosure populates.
func (s *IncludeScanner) IncludeDirectiveTargets(vfsPath VFS) []string {
	entries := s.parsers.parsedIncludes(vfsPath)
	if len(entries) == 0 {
		return nil
	}

	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.target)
	}
	return out
}

// scannerStatsEnabled is set once at process start from $SCANNER_STATS.
// When set, WalkClosure periodically dumps subgraph cache hit/miss
// counters to stderr.
var scannerStatsEnabled = os.Getenv("SCANNER_STATS") != ""

// perfStatsEnabled is set once at process start from
// $YATOOL_PERF_STATS. When enabled, Gen prints a final scanner/parser
// summary to stderr after each root walk.
var perfStatsEnabled = os.Getenv("YATOOL_PERF_STATS") != ""

// emittedRel returns the VFS-rooted form that the scanner emits for a
// header path. Internal paths are already VFS-rooted, so this is now an
// identity passthrough.
func (s *IncludeScanner) emittedRel(abs string) string {
	return abs
}

// sourceClassSignature returns an FNV-1a digest of the pointer
// addresses of the source-keyed sysincl records active for `sourceRel`.
// Two sources sharing this digest belong to the same equivalence class:
// identical source-keyed mappings, identical resolve() outputs,
// identical subgraphs.
func sourceClassSignature(view PerSourceView) uint64 {
	const (
		offset uint64 = 1469598103934665603
		prime  uint64 = 1099511628211
	)

	active := view.activeSourceKeyed

	h := offset

	// Order independence: the active subset always preserves the
	// sysincl-load iteration order (which is stable across runs of
	// PreparePerSource on the same set). Two sources with the same
	// active subset see the records in the same order, so we hash the
	// address sequence directly without sorting.
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
	sig := sourceClassSignature(view)

	for _, id := range s.sourceClassBuckets[sig] {
		cached := s.sourceClassViews[id]
		if sameSourceClassView(cached, view) {
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

func sameSourceClassView(a, b PerSourceView) bool {
	if len(a.activeSourceKeyed) != len(b.activeSourceKeyed) {
		return false
	}

	for i, rec := range a.activeSourceKeyed {
		if rec != b.activeSourceKeyed[i] {
			return false
		}
	}

	return true
}

// hashScanContext is an FNV-1a hash over OwnAddIncl + PeerAddInclSet +
// BaseSearchPaths. SourceRel is intentionally NOT in the hash because
// search-path resolution is source-independent; sysincl resolution IS
// source-dependent and is handled outside resolveCache via the per-half
// sysincl caches.
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

	mixVFS := func(v VFS) {
		h ^= uint64(v.Root)
		h *= prime
		mix(v.Rel)
	}

	mixSlice := func(ss []VFS) {
		for _, s := range ss {
			mixVFS(s)
		}

		h ^= 0xfe
		h *= prime
	}

	mixSlice(ctx.OwnAddIncl)
	mixSlice(ctx.PeerAddInclSet)
	mixSlice(ctx.BaseSearchPaths)

	return h
}

// dfsID merges the full transitive closure of `absID` into the caller's
// visited+order, skipping pre-visited entries. closureOf returns the
// node's complete reachable set (cycle-safe, cached), so unlike a plain
// DFS this never recurses per child — the whole subtree arrives in one
// merge. Source-like roots are not cached (each compiles once); they
// plain-DFS so their per-header descendants still hit closureOf.
func (sc *scanCtx) dfsID(absID uint32, visited *idSet, order *[]uint32) {
	sc.scanner.dfsCalls++

	if visited.has(absID) {
		return
	}

	absPath := sc.scanner.interner.vfsByID(absID)

	if isSourceLike(absPath) {
		sc.plainDfsID(absID, visited, order)

		return
	}

	// Merge the cached/computed closure (includes absID), skipping
	// pre-visited entries.
	for _, id := range sc.closureOf(absID) {
		if visited.has(id) {
			continue
		}

		visited.add(id)
		*order = append(*order, id)
	}
}

// plainDfsID walks `absID` into the caller's shared visited+order without
// caching absID itself — used for source-like roots, which compile once.
// Per-child dispatch goes through dfsID(), so each header descendant still
// resolves to a cached closureOf() set.
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

// forEachResolvedChild invokes `fn` once per resolved-child VFS path
// of `vfsPath`. Parsing is delegated to the parser layer: per-extension
// parser for source files, parser manager for generated $(B) outputs
// (which serve the emitter-mounted include list). Each parser entry
// then goes through resolve().
func (sc *scanCtx) forEachResolvedChild(vfsPath VFS, fn func(rabs VFS)) {
	s := sc.scanner

	for _, entry := range s.parsers.parsedIncludes(vfsPath) {
		resolved := sc.resolve(vfsPath, entry)
		for _, rabs := range resolved {
			fn(rabs)
		}
	}
}

// forEachResolvedChildID invokes `fn` once per resolved-child ID of
// `absID`. The resolved-child ID list is memoised in the scanner-global
// childrenCache (keyed by absID): the first scanCtx to reach a file resolves
// its children in that module's ADDINCL ctx and every later module reuses
// the result, mirroring upstream's resolve-once-per-file model. Tainted-
// subgraph re-walks also reuse it instead of re-running resolve().
func (sc *scanCtx) forEachResolvedChildID(absID uint32, fn func(uint32)) {
	s := sc.scanner
	if cached, ok := s.childrenCache[absID]; ok {
		for _, id := range cached {
			fn(id)
		}

		return
	}

	vfsPath := s.interner.vfsByID(absID)
	var children []uint32
	sc.forEachResolvedChild(vfsPath, func(rabs VFS) {
		children = append(children, s.interner.internVFS(rabs))
	})
	s.childrenCache[absID] = children

	for _, id := range children {
		fn(id)
	}
}

// SubgraphCacheStats reports per-includer subgraph cache traffic since
// scanner construction. Observed tools/archiver hit rate after warmup
// is ~87% (target) / ~92% (host).
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
		resolveCacheHits:       s.resolveCacheHits,
		resolveCacheMisses:     s.resolveCacheMisses,
		sysinclSourceHits:      s.sysinclSourceHits,
		sysinclSourceMisses:    s.sysinclSourceMisses,
		sysinclIncluderHits:    s.sysinclIncluderHits,
		sysinclIncluderMisses:  s.sysinclIncluderMisses,
	}
}

// closureOf returns the full transitive include closure of absID
// (INCLUDING absID): a deduplicated, cache-owned []uint32 (iterate only).
// Cycle-safe via Tarjan SCC — each node is explored at most once for the
// whole run (the closure cache is scanner-global), and every member of a
// strongly-connected component shares one closure slice, so include cycles
// cost no more than acyclic fan-out. Element order is the deterministic
// first-exploration order; downstream callers treat the result as a set
// (dump normalize sorts inputs), so no sort.
func (sc *scanCtx) closureOf(absID uint32) []uint32 {
	s := sc.scanner
	if cached, ok := s.subgraphCache[absID]; ok {
		s.subgraphHits++

		return cached
	}

	// Fresh exploration. Tarjan scratch is scanner-shared (gen scanning is
	// single-goroutine) and reset per top-level miss; uncached descendants
	// are reached via strongconnect recursion, not another closureOf call.
	clear(s.tjIndex)
	clear(s.tjLow)
	clear(s.tjOnStack)
	s.tjStack = s.tjStack[:0]
	s.tjNext = 0

	sc.strongconnect(absID)

	return s.subgraphCache[absID]
}

// strongconnect is the recursive Tarjan core. It finalizes every SCC it
// discovers into subgraphCache in reverse-topological order, so when an
// SCC's closure is built each of its external successors is already
// cached. A child already in subgraphCache is an external successor of a
// previously-finalized SCC (or an earlier run) and is not re-explored.
func (sc *scanCtx) strongconnect(v uint32) {
	s := sc.scanner

	s.tjNext++
	s.tjIndex[v] = s.tjNext
	s.tjLow[v] = s.tjNext
	s.tjStack = append(s.tjStack, v)
	s.tjOnStack[v] = true

	sc.forEachResolvedChildID(v, func(w uint32) {
		if _, cached := s.subgraphCache[w]; cached {
			s.subgraphHits++ // reuse of a previously-finalized node's closure

			return
		}

		if s.tjIndex[w] == 0 {
			sc.strongconnect(w)
			if s.tjLow[w] < s.tjLow[v] {
				s.tjLow[v] = s.tjLow[w]
			}
		} else if s.tjOnStack[w] {
			if s.tjIndex[w] < s.tjLow[v] {
				s.tjLow[v] = s.tjIndex[w]
			}
		}
	})

	if s.tjLow[v] != s.tjIndex[v] {
		return // not an SCC root; members stay on the stack
	}

	// v roots an SCC. Its members are the stack suffix back through v;
	// every member's children are now either members (still on stack) or
	// external nodes already present in subgraphCache.
	sccStart := len(s.tjStack) - 1
	for s.tjStack[sccStart] != v {
		sccStart--
	}
	members := s.tjStack[sccStart:]

	s.tjClosure.reset(s.interner.relIDBound())
	buf := s.tjBuf[:0]

	for _, u := range members {
		if !s.tjClosure.has(u) {
			s.tjClosure.add(u)
			buf = append(buf, u)
		}
	}

	for _, u := range members {
		sc.forEachResolvedChildID(u, func(w uint32) {
			if s.tjOnStack[w] {
				return // same SCC; already added as a member
			}

			for _, id := range s.subgraphCache[w] {
				if !s.tjClosure.has(id) {
					s.tjClosure.add(id)
					buf = append(buf, id)
				}
			}
		})
	}

	out := make([]uint32, len(buf))
	copy(out, buf)
	s.tjBuf = buf[:0]

	s.subgraphMisses += uint64(len(members)) // nodes whose closure was computed
	if len(members) > 1 {
		s.subgraphTainted++ // count non-trivial SCCs (genuine include cycles)
	}

	for _, u := range members {
		s.subgraphCache[u] = out
		s.tjOnStack[u] = false
	}

	s.tjStack = s.tjStack[:sccStart]
}

// resolve returns the paths the directive resolves to, in declaration
// order, deduplicated. Memoised via resolveCache.
//
// Two-tier semantics from upstream ymake:
//   - Search-path candidates (samedir, own AddIncl, peer-GLOBAL, base)
//     are FIRST-MATCH-WINS — compiler `-I` precedence.
//   - `#include <X>`: every matching sysincl record's paths are
//     UNIONED on top of the search-path result (`<stddef.h>` from non-
//     musl C unions libcxx and musl `stddef.h`).
//   - `#include "X"`: sysincl is gated. Same-directory hit always
//     suppresses sysincl. ADDINCL/peer/base hit suppresses single-
//     target sysincl, but multi-target sysincl (≥ 2 non-empty paths)
//     IS unioned on top (e.g. `"cxxabi.h"` from libcxxabi-parts unions
//     libcxxabi and libcxxrt).
func (sc *scanCtx) resolve(includerAbs VFS, d includeDirective) (out []VFS) {
	s := sc.scanner

	// Unresolved-include diagnostic: surface every directive with no
	// hit in source dir / build dir / search path AND not claimed by
	// any sysincl record (bare-key suppression is an intentional empty
	// result). Skipped when d.next is set.
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
				includerAbs.String(), open, d.target, close),
		})
	}()
	// `#include_next` directives resolve to nothing. Every observed
	// live use is the libcxx shadow-header pattern (libcxx/X.h does
	// `#include_next <X.h>` to chain to the system's X.h); the chained
	// header is always reachable via the parallel C++ wrapper, which
	// resolves via sysincl. Following `#include_next` adds no new
	// inputs in the live case and adds spurious ones when it sits
	// inside an `#elif` the preprocessor never takes (e.g.
	// __mbstate_t.h's dead branch under _LIBCPP_HAS_MUSL_LIBC).
	// `#include_next` is NOT surfaced to onWarn — empty is intended.
	if d.next {
		return nil
	}

	// Search-path resolution is source-independent and uses resolveCache
	// (ctxHash + includer + target + kind) for cross-source reuse.
	// Sysincl is source-dependent and uses per-half caches: source-keyed
	// by (sourceRel, target), includer-keyed by (includer, target).
	searchOut := sc.resolveSearchPath(includerAbs, d)

	// Sysincl: per-record source-vs-includer keying. Each SysIncl record
	// carries a KeyBySource flag compiled from its source_filter shape
	// (negative-lookahead `(?!...)` → source-keyed, otherwise includer-
	// keyed). includerAbs is $(S)/-rooted here (BUILD_ROOT dispatches
	// in forEachResolvedChild); strip to the sysincl-keyed rel form.
	//
	// Both halves key on the IMMEDIATE INCLUDER (the file that holds the
	// directive), matching upstream ymake: TModuleResolver resolves a file's
	// includes with src = that file, and Conf.Sysincl.Resolve(src, ...) keys
	// the source_filter on it — never on the originating compile root. That
	// makes a file's resolution a pure function of (file, ADDINCL ctx),
	// independent of which target pulled it in — including generated $(B)
	// includers, which key on their own build path just like upstream.
	includerRel := includerAbs.Rel
	var mappings []VFS
	var hasMultiTarget bool
	mappings, hasMultiTarget, sysinclClaimed = s.sysinclLookup(includerRel, includerRel, d.target)

	// Quoted-include gate. For quoted includes with at least one local
	// hit, sysincl is suppressed when:
	//   1. Same-directory hit (always, regardless of multi-target).
	//   2. ADDINCL/peer/base hit AND sysincl is single-target.
	// Bypass: ADDINCL/peer/base hit AND sysincl is multi-target —
	// e.g. `#include "cxxabi.h"` from libcxxabi-parts unions both
	// libcxxabi/include/cxxabi.h and libcxxrt/include/cxxabi.h.
	if d.kind == includeQuoted && len(searchOut) > 0 {
		// Single-target → bypass unconditionally (dominates sysincl).
		// Compute sameDirRel lazily — the concat fires millions of
		// times per gen.
		bypass := !hasMultiTarget
		if !bypass && searchOut[0].IsSource() {
			incDir := pathDir(includerRel)

			var sameDirRel string

			if incDir != "" {
				sameDirRel = normalisePath(incDir + "/" + d.target)
			} else {
				sameDirRel = d.target
			}

			bypass = searchOut[0].Rel == sameDirRel
		}

		if bypass {
			return searchOut
		}
	}

	if len(mappings) == 0 {
		return searchOut
	}

	// Layer sysincl mappings on top of the search-path result. Each
	// mapping is file-checked (sysincl YAMLs may name files the tree
	// lacks). When no entries stick, return searchOut directly to avoid
	// the copy. Fast path: searchOut empty (common for system includes
	// hitting only sysincl) — use `mappings` directly with linear-scan
	// dedup (lists are 1-3 entries, cheaper than a map).
	if len(searchOut) == 0 {
	fastLoop:
		for _, abs := range mappings {
			for _, q := range out {
				if q == abs {
					continue fastLoop
				}
			}

			if !s.fileExistsByRel(abs.Rel) {
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

		if !s.fileExistsByRel(abs.Rel) {
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

// sysinclLookup unions the source-keyed and includer-keyed halves of
// the sysincl Lookup, each memoised independently. The split lets the
// includer-keyed half be reused across every CC reaching the same
// (includer, target), while the source-keyed half is reused within a
// single source's closure.
//
// hasMultiTarget is true when any contributing record maps `target` to
// ≥ 2 non-empty paths — used by resolve()'s quoted-include gate.
// Dedup uses linear scan because mapping lists are 1-3 entries.
func (s *IncludeScanner) sysinclLookup(sourceRel, includerRel, target string) (paths []VFS, hasMultiTarget, claimed bool) {
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

	// Multi-target: a single record maps to ≥2 files (e.g. cxxabi.h →
	// libcxxabi+libcxxrt) OR the union across distinct matching records
	// resolves to ≥2 files (e.g. quoted "math.h" → musl + libcxx via
	// libc-to-musl + stl-to-libcxx). Upstream's sysincl resolver unions
	// every matching rule, so the quoted-include bypass must treat the
	// cross-record union as multi-target too.
	hasMultiTarget = srcMT || incMT || len(paths) >= 2

	return paths, hasMultiTarget, claimed
}

func (s *IncludeScanner) sysinclSourceLookup(sourceRel, target string) ([]VFS, bool, bool) {
	classID, view := s.sourceClass(sourceRel)
	key := sysinclSourceKey{
		sourceClass: classID,
		target:      s.interner.internString(target),
	}

	if cached, ok := s.sysinclSourceCache[key]; ok {
		s.sysinclSourceHits++
		return cached.paths, cached.hasMultiTarget, cached.claimed
	}

	s.sysinclSourceMisses++

	rels, claimed, hasMultiTarget := view.LookupSourceKeyed(target)

	entry := sysinclCacheEntry{
		paths:          s.absifyRels(rels),
		hasMultiTarget: hasMultiTarget,
		claimed:        claimed,
	}
	s.sysinclSourceCache[key] = entry

	return entry.paths, entry.hasMultiTarget, entry.claimed
}

func (s *IncludeScanner) sysinclIncluderLookup(includerRel, target string) ([]VFS, bool, bool) {
	key := sysinclIncluderKey{
		includer: s.interner.internString(includerRel),
		target:   s.interner.internString(target),
	}

	if cached, ok := s.sysinclIncluderCache[key]; ok {
		s.sysinclIncluderHits++
		return cached.paths, cached.hasMultiTarget, cached.claimed
	}

	s.sysinclIncluderMisses++

	// PerSourceView's includerKeyed slice is identical regardless of
	// which source it was prepared for. anySrcView (initialised once)
	// gives access without going through perSourceView.
	rels, claimed, hasMultiTarget := s.anySrcView.LookupIncluderKeyed(includerRel, target)

	entry := sysinclCacheEntry{
		paths:          s.absifyRels(rels),
		hasMultiTarget: hasMultiTarget,
		claimed:        claimed,
	}
	s.sysinclIncluderCache[key] = entry

	return entry.paths, entry.hasMultiTarget, entry.claimed
}

// absifyRels converts SOURCE_ROOT-relative paths (sysincl YAMLs) into
// VFS-rooted paths, normalising `..`/`.` segments. Cached at the per-
// half sysinclCache level so the hot path skips per-mapping concat.
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

// resolveContextSearchTier resolves the source-independent search tiers
// for one target within a bound scanCtx:
//  1. module's own ADDINCL
//  2. peer-propagated GLOBAL ADDINCL
//  3. baseline fallback (repo-root/linux-headers)
//
// The result is keyed only by target because the receiver scanCtx
// already binds the relevant OwnAddIncl/PeerAddInclSet/BaseSearchPaths.
// Same-directory quoted lookup and BUILD-root direct handling stay in
// resolveSearchPath because they depend on the includer.
func (sc *scanCtx) resolveContextSearchTier(targetID uint32, target string) searchTierResult {
	s := sc.scanner

	if cached, ok := sc.searchTierCache[targetID]; ok {
		s.searchTierHits++
		return cached
	}

	s.searchTierMisses++

	var out searchTierResult

	addPath := func(rel string) bool {
		rel = normalisePath(rel)
		if !s.fileExistsByRel(rel) {
			return false
		}

		out.paths = []VFS{Source(rel)}
		out.found = true

		return true
	}

	addBuildPath := func(rel string) bool {
		rel = normalisePath(rel)
		if s.codegen == nil {
			return false
		}

		v := Build(rel)
		if _, ok := s.codegen.Lookup(v); !ok {
			return false
		}

		out.paths = []VFS{v}
		out.found = true

		return true
	}

	addInclPath := func(prefix VFS) bool {
		switch prefix.Root {
		case VFSRootBuild:
			if prefix.Rel == "" {
				return addBuildPath(target)
			}

			return addBuildPath(prefix.Rel + "/" + target)
		case VFSRootSource:
			if prefix.Rel == "" {
				return addPath(target)
			}

			return addPath(prefix.Rel + "/" + target)
		}

		panic("resolveContextSearchTier: zero-valued search path")
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

	switch includerAbs.Rel {
	case "util/generic/string.pxd":
		if d.target == "libcpp/string.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/libcpp/string.pxd", true
		}
	case "util/generic/hash.pxd":
		if d.target == "libcpp/pair.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/libcpp/pair.pxd", true
		}
	case "util/system/types.pxd":
		if d.target == "libc/stdint.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/libc/stdint.pxd", true
		}
	}

	if strings.HasPrefix(includerAbs.Rel, "contrib/tools/cython_py2/Cython/Includes/") {
		switch d.target {
		case "libc/string.pxd", "libcpp/string.pxd", "libcpp/pair.pxd", "libcpp/utility.pxd":
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target, true
		}
	}

	return "", false
}

// resolveSearchPath returns the search-path-only resolved set for the
// given directive. Cached on the scanCtx by (includer, target, kind,
// next) — ctxHash is implicit in the scanCtx receiver.
func (sc *scanCtx) resolveSearchPath(includerAbs VFS, d includeDirective) []VFS {
	s := sc.scanner
	s.resolveSearchPathCalls++
	key := resolveInnerKey{
		includer: s.interner.internVFS(includerAbs),
		target:   s.interner.internString(d.target),
		flags:    packResolveFlags(d.kind, d.next),
	}

	if cached, ok := sc.resolveCache[key]; ok {
		s.resolveCacheHits++
		return cached
	}

	s.resolveCacheMisses++

	var out []VFS

	// Pool the per-resolve dedup map. Keys are rel-form strings — what
	// fileExistsByRel keys on too, so no extra VFS construction.
	seenP := s.seenPool.Get().(*map[string]struct{})
	seen := *seenP

	addPath := func(rel string) bool {
		// Normalize `..`/`.` segments — the upstream scanner emits
		// the canonical form.
		rel = normalisePath(rel)

		if _, dup := seen[rel]; dup {
			return false
		}

		if !s.fileExistsByRel(rel) {
			return false
		}

		seen[rel] = struct{}{}
		// Append as typed VFS; the "$(S)/..." string is only
		// materialised by the JSON serializer.
		out = append(out, Source(rel))

		return true
	}

	addBuildPath := func(rel string) bool {
		rel = normalisePath(rel)

		if s.codegen == nil {
			return false
		}

		v := Build(rel)
		if _, ok := s.codegen.Lookup(v); !ok {
			return false
		}

		dedupKey := "B:" + rel
		if _, dup := seen[dedupKey]; dup {
			return false
		}

		seen[dedupKey] = struct{}{}
		out = append(out, v)

		return true
	}

	// First-match-wins across the search path. Order:
	//   1. quoted-form: same directory as the includer
	//   2. BUILD-root BUILD-only check (generated header in codegen registry)
	//   3. module's own ADDINCL
	//   4. peer-propagated GLOBAL ADDINCL
	//   5. baseline fallback (repo-root/linux-headers)
	//   6. BUILD-root Source fallback (after all search paths fail)
	searchPathFound := false

	// buildRootFallbackRel is set when the includer is a BUILD-root file
	// and the target contains a path separator, but the BUILD-root codegen
	// registry didn't claim it. We defer the Source(rel) fallback until
	// after the ADDINCL tier so that headers like llvm/IR/Value.h resolve
	// to $(S)/contrib/libs/llvm16/include/llvm/IR/Value.h (via the GLOBAL
	// ADDINCL for that module) rather than the spurious $(S)/llvm/IR/Value.h.
	var buildRootFallbackRel string

	if candidate, ok := cythonPy2SiblingOverride(includerAbs, d); ok && addPath(candidate) {
		searchPathFound = true
	}

	if includerAbs.IsBuild() && strings.Contains(d.target, "/") {
		rel := normalisePath(d.target)

		if addBuildPath(rel) {
			searchPathFound = true
		} else {
			buildRootFallbackRel = rel
		}
	}

	if candidate, ok := resolveCythonPy2Override(includerAbs, d); ok && addPath(candidate) {
		searchPathFound = true
	}

	if d.kind == includeQuoted {
		// includerAbs is SOURCE-rooted here (BUILD_ROOT dispatches in
		// forEachResolvedChild before reaching resolveSearchPath).
		incDir := pathDir(includerAbs.Rel)

		var candidate string

		if incDir != "" {
			candidate = incDir + "/" + d.target
		} else {
			candidate = d.target
		}

		if addPath(candidate) {
			searchPathFound = true
		} else if addBuildPath(candidate) {
			searchPathFound = true
		}
	}

	if !searchPathFound {
		tier := sc.resolveContextSearchTier(key.target, d.target)
		if tier.found {
			out = append(out, tier.paths...)
			searchPathFound = true
		}
	}

	// VFS fallback tier — consult fallbackLocators (codegen registry)
	// only when every on-disk search-path candidate missed. Generated
	// files (.pb.h, _serialized.h, .ev.pb.h, ...) don't exist on disk
	// at gen time. Locator is queried with the canonical $(B)/<target>
	// form; consumers always use the full BUILD_ROOT-relative path.
	if !searchPathFound && len(s.fallbackLocators) > 0 {
		// BUILD-rooted candidate. The Exists locator is the codegen
		// registry; its API still takes the string form.
		abs := Build(d.target)

		for _, loc := range s.fallbackLocators {
			if !loc.Exists(abs) {
				continue
			}

			// Use a distinct dedup key for BUILD-rooted entries (the
			// rel-keyed `seen` would collide with a SOURCE rel of the
			// same name). Prefix with "B:" so it's unique.
			dedupKey := "B:" + d.target
			if _, dup := seen[dedupKey]; !dup {
				seen[dedupKey] = struct{}{}
				out = append(out, abs)
			}

			break
		}
	}

	// BUILD-root Source fallback: after all on-disk search paths (ADDINCL,
	// VFS locators) failed, emit Source(rel) unconditionally. The upstream
	// scanner does the same for BUILD-root generated-header includes that
	// cannot be verified on disk at graph-gen time.
	if !searchPathFound && buildRootFallbackRel != "" {
		if _, dup := seen[buildRootFallbackRel]; !dup {
			seen[buildRootFallbackRel] = struct{}{}
			out = append(out, Source(buildRootFallbackRel))
		}
		searchPathFound = true
	}

	// `clear()` drops every key without releasing the bucket allocation
	// — next caller starts with empty-but-prewarmed state.
	clear(seen)
	s.seenPool.Put(seenP)

	sc.resolveCache[key] = out

	return out
}

func cythonPy2SiblingOverride(includerAbs VFS, d includeDirective) (string, bool) {
	if !includerAbs.IsSource() || d.kind != includeQuoted {
		return "", false
	}

	if hasPrefix(includerAbs.Rel, "contrib/tools/cython_py2/Cython/Includes/") {
		if hasPrefix(d.target, "libc/") || hasPrefix(d.target, "libcpp/") {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target, true
		}

		return "", false
	}

	switch includerAbs.Rel {
	case "util/generic/string.pxd":
		if d.target == "libcpp/string.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target, true
		}
	case "util/generic/hash.pxd", "util/generic/hash_set.pxd":
		if d.target == "libcpp/pair.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target, true
		}
	case "util/system/types.pxd":
		if d.target == "libc/stdint.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target, true
		}
	}

	return "", false
}

func packResolveFlags(kind includeKind, next bool) uint8 {
	flags := uint8(kind)

	if next {
		flags |= 1 << 7
	}

	return flags
}

// isSourceLike returns true for compile-unit extensions (.cpp, .cc,
// .cxx, .c, .S, .s, .m, .mm). The scanner skips subgraph-cache
// speculation at top-level dfs entry points (always a source). Headers
// and intermediate sources (.h, .hpp, .rl, .proto, .pb.cc, ...) return
// false and use the subgraph cache.
func isSourceLike(absPath VFS) bool {
	// VFS.Rel never contains the $(S)/ or $(B)/ prefix.
	rel := absPath.Rel
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

// pathDir returns the directory portion of a forward-slash path
// (the part before the last "/"). For paths without "/" returns "".
func pathDir(p string) string {
	idx := strings.LastIndexByte(p, '/')

	if idx < 0 {
		return ""
	}

	return p[:idx]
}

// normalisePath resolves "." and ".." segments in a forward-slash
// path. Empty result implies the path normalised away to the source
// root itself. Does not consult the filesystem.
func normalisePath(p string) string {
	if !strings.Contains(p, "..") && !strings.Contains(p, "./") && !strings.Contains(p, "//") {
		return p
	}

	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))

	for _, seg := range parts {
		switch seg {
		case "", ".":
			// "" appears when leading "/" exists (shouldn't here)
			// or trailing "/"; skip.
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

// pathLocator answers whether a VFS-rooted path refers to a reachable
// resource — a real file under sourceRoot (fsLocator) or a registered
// generated output (codegenLocator). The FS tier runs inline through
// fileExists; codegenLocator runs as a fallback for $(B)/ candidates.
// The locator boundary is where VFS dispatches to its backing store.
type pathLocator interface {
	// Exists reports whether `vfsPath` is reachable through this
	// locator. Each locator answers for one VFS root only (FS returns
	// false for $(B)/, codegen returns false for $(S)/).
	Exists(vfsPath VFS) bool
}

// fsLocator answers Exists for $(S)/-rooted paths via the shared
// parse-cache exists map. Returns false for $(B)/-prefixed paths.
// Cache key is the rel-form tail, shared with fileExistsByRel.
type fsLocator struct {
	scanner *IncludeScanner
}

func (f fsLocator) Exists(vfsPath VFS) bool {
	if !vfsPath.IsSource() {
		return false
	}

	return f.scanner.fileExistsByRel(vfsPath.Rel)
}

// codegenLocator answers Exists for $(B)/-rooted paths via the per-
// scanner CodegenRegistry. Returns false for $(S)/ paths and for any
// $(B)/ path not Register()ed. Lookup is O(1).
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

	_, ok := c.reg.Lookup(vfsPath)

	return ok
}

// fileExistsByRel is the inner, rel-keyed existence check.
func (s *IncludeScanner) fileExistsByRel(rel string) bool {
	return s.parsers.fileExistsByRel(rel)
}
