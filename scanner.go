package main

// scanner.go — C/C++ #include transitive-closure scanner. Reproduces
// (closely enough for L2-multiset acceptance) the upstream ymake
// scanner: text-based regex match, conditional-blind, ADDINCL +
// peer-GLOBAL ADDINCL + sysincl resolution, depth-first traversal
// with per-source visited set, file-level memoization of parsed
// directives.
//
// Out of scope for PR-31 (documented gaps):
//   - `#include MACRO_NAME` macro-expanded include paths. Empirically
//     not observed in the M2 closure; emitting nothing for these is
//     the same behaviour ymake exhibits when the macro has no
//     sysincl mapping.
//   - Exact ymake scanner-order traversal. L2 compares inputs as a
//     multiset; we DFS-discovery-emit and rely on multiset semantics.
//   - `#include` lines inside multi-line C strings or block comments
//     (false positive risk). Not observed in M2 closure.

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"unsafe"
)

// includeRe matches `#include` / `#include_next` directives in their
// angle-bracket and quoted-string forms, tolerating arbitrary
// whitespace between `#`, the keyword, and the bracket. Two capture
// groups: directive (`include` or `include_next`) and target.
var includeRe = regexp.MustCompile(`^\s*#\s*(include|include_next)\s*[<"]([^>"]+)[>"]`)

// includeKind discriminates `<...>` (system) from `"..."` (quoted).
// `#include_next` retains its directive form via `next` and is
// otherwise treated as system for search-path resolution.
type includeKind int

const (
	includeSystem includeKind = iota
	includeQuoted
)

// includeDirective is one parsed `#include` from a source file.
// `next` distinguishes `#include_next` from a regular `#include`. For
// sysincl resolution `#include_next` is suppressed: the directive
// semantically asks the preprocessor to search past the current
// header's directory, never to apply YAML-driven sysincl mappings —
// the upstream ymake scanner does not synthesise sysincl entries for
// `#include_next`, and following them through libcxx's
// `__has_include_next` shadow-header pattern is the dominant L2-ceiling
// over-fan-out (PR-31-D08, PR-33-C03).
type includeDirective struct {
	kind   includeKind
	next   bool
	target string
}

// IncludeScanner is the per-walker include-resolver state. It owns:
//
//   - sysincl: the loaded SysInclSet (one for the whole walker).
//   - sourceRoot: absolute path used to stat candidate header files
//     and read their text for transitive parsing.
//   - parsed: per-file include-directive cache, keyed by absolute
//     path. Memoized once per scanner — libcxx's __config (≈1180
//     lines) is parsed once even though ~3000 CC nodes transitively
//     include it.
//   - exists: per-absolute-path file-existence cache. Stat'ing a
//     candidate path is the per-resolution hot loop; we cache the
//     boolean to avoid hammering the kernel for negative results.
//   - resolveCache: per-(ctx, includer, target, kind) resolved-set
//     cache. Modules contribute the same ctx to many CC nodes, and
//     CC nodes share most of their includer transitive graph; caching
//     resolve() results across that overlap turns the scan from O(N
//     CC × header-graph) into approximately O(unique ctx × header-
//     graph). The ctx-hash is computed once per WalkClosure call.
//   - visitedPool / orderPool: per-WalkClosure scratch buffers reused
//     across calls (PR-34d). The profiler showed WalkClosure's fresh
//     `visited` map and `order` slice as the largest single allocator
//     (~1.94 GB flat across the tools/archiver run). Both are scratch
//     state — once WalkClosure copies the result into the returned
//     `[]string`, the buffers can be cleared and returned to the pool.
//
// PR-34n: removed sync.Mutex per re-profile — single-goroutine; M3
// streaming may need to reintroduce. The scanner is invoked exclusively
// from gen.go's serial walker; profiling at HEAD f5fef1c showed the
// `sync.Mutex.Lock`+`Unlock` pair (no contention, just runtime overhead)
// at 7.8% of CPU across 13 lock pairs on hot paths. Removing them
// turns each cache op into a plain map read/write. If M3 introduces
// per-source goroutines, the locks must be reintroduced — every Lock
// site is replaced by a comment marker `// PR-34n: lock removed`.
type IncludeScanner struct {
	sysincl    SysInclSet
	sourceRoot string
	// sourceRootSlash is the precomputed `sourceRoot + "/"` prefix.
	// Hot paths build absolute paths by `sourceRootSlash + rel` (one
	// 2-string concat = one alloc) instead of `sourceRoot + "/" + rel`
	// (which Go's runtime resolves via concatstring3 — still one alloc,
	// but the literal `"/"` segment forces the string-table to allocate
	// the joined `sourceRoot+"/"` prefix on every call). Caching it
	// once removes the per-call prefix alloc that PR-34k's profile
	// flagged inside `addPath` and `resolve`'s `TrimPrefix(... ,
	// s.sourceRoot+"/")` call sites.
	sourceRootSlash string

	parsed       map[string][]includeDirective
	exists       map[string]bool
	resolveCache map[resolveKey][]string
	// anySrcView is a PerSourceView prepared with an empty source path.
	// Its `includerKeyed` slice is the canonical includer-keyed record
	// list (every view derives the same slice); the `activeSourceKeyed`
	// half is empty (no source-keyed filter accepts ""). Used as a
	// lock-free shortcut by sysinclIncluderLookup.
	anySrcView PerSourceView
	// viewCache caches per-source PerSourceViews so repeat WalkClosure
	// calls (and the multi-source dfs in joinSrcsIncludeClosure) reuse
	// the precomputed source-keyed filter results. Keyed by SourceRel.
	viewCache map[string]PerSourceView
	// sysinclSourceCache memoises the source-keyed half across
	// (sourceRel, target). The result is includer-INdependent for
	// source-keyed records (the source filter was satisfied when the
	// view was constructed); two CCs sharing a sourceRel reach the
	// same set of source-keyed paths for any (includer, target). Most
	// CC sources visit a few hundred distinct targets; the cache hits
	// on every repeat target within one source's closure.
	sysinclSourceCache map[sysinclSourceKey][]string
	// sysinclIncluderCache memoises the includer-keyed half across
	// (includerRel, target). The result is source-INdependent
	// (includer-keyed records' filters depend only on the includer);
	// every CC reaching the same (includer, target) shares this entry.
	sysinclIncluderCache map[sysinclIncluderKey][]string

	visitedPool sync.Pool // *map[string]struct{}
	orderPool   sync.Pool // *[]string
	// seenPool reuses the per-resolveSearchPath dedup map across calls.
	// Each resolve produces 1-6 candidate paths so the map fills to a
	// handful of entries; the bucket allocation (~256 B) is what we
	// were paying per call before pooling.
	seenPool sync.Pool // *map[string]struct{}

	// emittedRelCache memoises the per-output `$(SOURCE_ROOT)/<rel>`
	// string built by WalkClosure for every header in the closure. The
	// same header appears in many CC nodes' closures (libcxx's
	// __config is included by 3000+ CCs), so interning the formatted
	// path string once and reusing it saves the per-element string
	// concat — 30 MB / run pre-PR-34k.
	//
	// PR-34n: the dedicated emittedRelMu is gone (single-goroutine).
	emittedRelCache map[string]string

	// subgraphCache memoises the transitive include-closure rooted at
	// `(absPath, ctxHash, srcClassHash)` (PR-34r). The cached value is
	// a list of absolute paths in DFS-discovery order, including the
	// root itself, that an UNCACHED dfs starting from `absPath` with an
	// empty visited set would emit. `srcClassHash` identifies the
	// equivalence class of source-keyed sysincl records active for the
	// caller's source — two sources whose `activeSourceKeyed` slice
	// shares the same record-pointer set produce identical sysincl
	// resolutions for any (includer, target), and therefore identical
	// subgraphs. ctxHash captures search-path resolution; the pair of
	// (ctxHash, srcClassHash) plus the root path uniquely determines
	// the ordered subgraph.
	//
	// On a cache hit, dfs iterates the cached list and merges entries
	// into the caller's visited+order, skipping already-visited paths.
	// This preserves uncached-DFS semantics: the cached list IS the
	// canonical first-visit order, and skipping pre-visited entries
	// during merge yields the same final order as uncached DFS would
	// have produced from the partially-populated visited state. Cached
	// slices are immutable strings — callers iterate, never mutate.
	//
	// Header-graph reuse drives the hit rate: libcxx's __config.h is
	// reached by ~3000 CC closures across the tools/archiver run, so
	// (libcxx/__config, ctxHash_X, srcClass_Y) computes once and serves
	// every later visit in that equivalence class. PR-34p tried keying
	// (srcAbs, ctxHash) and saw 0% — every srcAbs is unique. The
	// per-includer (header) form has high reuse because the header
	// graph is many-to-many: many sources reach the same header, and
	// each header in turn carries a deep transitive subgraph.
	subgraphCache map[subgraphKey][]string

	// subgraphTaintedKnown records subgraph keys whose computation
	// hit a cycle on first attempt — the persistent `subgraphCache`
	// cannot hold them (the cycle-incomplete result depends on
	// ancestor stack context). On every later visit, dfs() short-
	// circuits the costly fresh-walk-and-discard via this set and
	// uses plain in-place DFS over the caller's visited+order. The
	// in-place fall-back is still O(|subtree|) per call, but it
	// avoids the per-call fresh `visited`+`order` allocation and
	// reuses the caller's already-populated visited so paths visited
	// via parallel branches are skipped — exactly the dedup the
	// original DFS provided.
	subgraphTaintedKnown map[subgraphKey]struct{}

	// subgraphInProgress holds the keys of canonical subgraphs that are
	// currently being computed (the recursion sandwich between
	// `subgraph()`'s entry and its cache write). When a child header
	// `r` reaches a back edge into a header whose subgraph computation
	// is already on the stack, the child's `subgraph(r)` call would
	// otherwise infinitely recurse (the cache write happens AFTER the
	// recursion returns). The sentinel lets such calls bail out
	// immediately, leaving the back-edge child to be discovered by the
	// outer computation's own visited set when the cycle closes.
	subgraphInProgress map[subgraphKey]struct{}

	// subgraphHits/subgraphMisses count cache traffic for verification.
	// Plain uint64; single-goroutine like the rest of scanner.go.
	subgraphHits    uint64
	subgraphMisses  uint64
	subgraphTainted uint64
	statsCallCount  uint64
}

// subgraphKey identifies a memoised transitive include subgraph. See
// `subgraphCache` for the equivalence rationale.
type subgraphKey struct {
	abs          string
	ctxHash      uint64
	srcClassHash uint64
}

type sysinclSourceKey struct {
	sourceRel string
	target    string
}

type sysinclIncluderKey struct {
	includerRel string
	target      string
}

type resolveKey struct {
	ctxHash  uint64
	includer string
	target   string
	kind     includeKind
	next     bool
}

// NewIncludeScanner constructs a scanner bound to a SysInclSet and a
// source-root absolute path.
func NewIncludeScanner(sourceRoot string, sysincl SysInclSet) *IncludeScanner {
	// PR-34n: pre-sizes set to the upper bound of the observed working
	// set for tools/archiver (target+host scanners combined; instrumented
	// run reports below). Under-pre-sizing was the actual finding from
	// the re-profile — sysinclSourceCache reaches ~328k entries on the
	// target scanner (the 131072 prior pre-size triggered ~2 rehash
	// chains; bucket re-grow + rehash-and-copy was the dominant flat
	// alloc). Pre-sizing past the observed peak eliminates rehashing.
	//
	// Observed peak per-scanner:
	//   parsed=4354 / 3559   exists=14195 / 14494
	//   resolveCache=97921 / 48054   viewCache=2063 / 1767
	//   sysinclSourceCache=328510 / 128091
	//   sysinclIncluderCache=22520 / 16089
	//   emittedRelCache=2137 / 1691
	s := &IncludeScanner{
		sysincl:              sysincl,
		sourceRoot:           sourceRoot,
		sourceRootSlash:      sourceRoot + "/",
		parsed:               make(map[string][]includeDirective, 8192),
		exists:               make(map[string]bool, 16384),
		resolveCache:         make(map[resolveKey][]string, 131072),
		viewCache:            make(map[string]PerSourceView, 4096),
		emittedRelCache:      make(map[string]string, 4096),
		sysinclSourceCache:   make(map[sysinclSourceKey][]string, 524288),
		sysinclIncluderCache: make(map[sysinclIncluderKey][]string, 32768),
		// subgraphCache: header graph has ~16k unique nodes in the
		// tools/archiver closure; with a handful of (ctxHash,
		// srcClassHash) equivalence classes the cache populates to
		// O(headers × classes). Pre-size generously to elide rehash.
		subgraphCache:        make(map[subgraphKey][]string, 65536),
		subgraphTaintedKnown: make(map[subgraphKey]struct{}, 8192),
		subgraphInProgress:   make(map[subgraphKey]struct{}, 64),
	}
	s.anySrcView = s.sysincl.PreparePerSource("")

	// Pool factories preallocate the same capacity that the
	// non-pooled WalkClosure used (64 entries). Pooled items are
	// returned as pointers to keep `Pool.Put` from boxing the
	// value (a plain map or slice would box-allocate on Put).
	s.visitedPool.New = func() any {
		m := make(map[string]struct{}, 64)

		return &m
	}

	s.orderPool.New = func() any {
		o := make([]string, 0, 64)

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

// ScanContext carries the per-CC-node resolution context: the
// effective ADDINCL search path and the source-relative path of the
// CC's primary input (used for sysincl source_filter matching). The
// search path is the concatenation of:
//
//   - the source's own directory (only consulted for quoted includes)
//   - the module's own ADDINCL paths
//   - the module's effective peer-propagated GLOBAL ADDINCL paths
//   - the standard cc-include set (BUILD_ROOT, SOURCE_ROOT,
//     linux-headers, plus musl arch/include set when applicable —
//     these come in via cmd_args and the scanner mirrors them via
//     `BaseSearchPaths`).
//
// All paths are SOURCE_ROOT-relative.
type ScanContext struct {
	SourceRel       string   // SOURCE_ROOT-relative path of the primary source
	OwnAddIncl      []string // module's own non-GLOBAL ADDINCL
	PeerAddInclSet  []string // peer-propagated GLOBAL ADDINCL (transitive)
	BaseSearchPaths []string // baseline include set (linux-headers, musl arch when applicable)
}

// WalkClosure returns the SOURCE_ROOT-prefixed transitive-header set
// for the given source file (excluding the source itself), in DFS-
// discovery order. The result list is suitable for use as
// `node.Inputs[1:]`.
//
// The `visited` map and `order` slice are pulled from per-scanner
// `sync.Pool`s (PR-34d). The result `out` slice is freshly allocated
// each call — the caller stores it on the node and the scanner does
// not retain it — so returning `order` to the pool cannot corrupt the
// caller's data. `clear()` resets the map in place; `order[:0]`
// retains backing capacity for the next call. Pool items are
// pointer-typed (`*map`, `*[]string`) so `Pool.Put` does not box.
func (s *IncludeScanner) WalkClosure(ctx ScanContext) []string {
	srcAbs := s.sourceRootSlash + ctx.SourceRel
	ctxHash := hashScanContext(&ctx)

	visitedP := s.visitedPool.Get().(*map[string]struct{})
	orderP := s.orderPool.Get().(*[]string)

	visited := *visitedP
	order := (*orderP)[:0]

	s.dfs(srcAbs, &ctx, ctxHash, visited, &order)

	out := make([]string, 0, len(order))

	for _, abs := range order {
		// Skip the source itself; only headers are emitted.
		if abs == srcAbs {
			continue
		}

		out = append(out, s.emittedRel(abs))
	}

	// Reset and return scratch buffers to the pool. `clear()`
	// (Go 1.21+) drops every key without releasing the bucket
	// allocation. `order` is reset by writing back the trimmed
	// slice header so the next caller sees length 0 with the
	// existing capacity. The contents of `order` are not zeroed
	// (string headers retained), but they are unreachable through
	// the empty slice and will be overwritten on the next dfs.
	clear(visited)
	*orderP = order[:0]

	s.visitedPool.Put(visitedP)
	s.orderPool.Put(orderP)

	if scannerStatsEnabled {
		s.statsCallCount++

		// SCANNER_STATS env-gated tracing for the PR-34r perf
		// instrumentation. Emit once every 500 calls — enough cadence
		// to watch hit-rate evolve, infrequent enough not to overwhelm
		// stderr. Production builds run with the env unset; the check
		// short-circuits at the boolean.
		if s.statsCallCount%500 == 0 {
			fmt.Fprintf(os.Stderr, "scanner-stats[%d]: subgraph hits=%d misses=%d tainted=%d cache=%d\n", s.statsCallCount, s.subgraphHits, s.subgraphMisses, s.subgraphTainted, len(s.subgraphCache))
		}
	}

	return out
}

// scannerStatsEnabled is set once at process start from $SCANNER_STATS.
// PR-34r perf instrumentation: when set, WalkClosure periodically dumps
// subgraph cache hit/miss counters to stderr. No-op when env not set.
var scannerStatsEnabled = os.Getenv("SCANNER_STATS") != ""

// emittedRel converts an absolute path under sourceRoot into the
// `$(SOURCE_ROOT)/<rel>` form used in graph-node Inputs, interning the
// result so repeat calls (libcxx's __config.h is reached by 3000+ CC
// closures) return the same string instance instead of re-allocating
// the concat per caller.
//
// PR-34n: lock removed (single-goroutine).
func (s *IncludeScanner) emittedRel(abs string) string {
	if cached, ok := s.emittedRelCache[abs]; ok {
		return cached
	}

	rel := strings.TrimPrefix(abs, s.sourceRootSlash)
	out := "$(SOURCE_ROOT)/" + rel

	s.emittedRelCache[abs] = out

	return out
}

// sourceClassHash returns an FNV-1a digest of the pointer addresses of
// the source-keyed sysincl records active for `sourceRel`. Two sources
// whose `activeSourceKeyed` slice contains the same record pointers (in
// any order — we sort by sorting the address sequence) belong to the
// same equivalence class: every Lookup against them yields identical
// source-keyed mappings, and therefore identical resolve() outputs and
// identical subgraphs.
//
// PR-34r uses this digest as the third component of the subgraph cache
// key. Reusing per-equivalence-class subgraphs (rather than per-source)
// preserves cross-source reuse — sources sharing a class hash share
// every cached subgraph rooted at any header.
//
// Computation is one-shot per WalkClosure (and goes through perSourceView
// so the activeSourceKeyed slice itself is also cached). Cost is O(|set|)
// per call, ~25 records typical.
func (s *IncludeScanner) sourceClassHash(sourceRel string) uint64 {
	const (
		offset uint64 = 1469598103934665603
		prime  uint64 = 1099511628211
	)

	view := s.perSourceView(sourceRel)
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

// hashScanContext is an FNV-1a hash over the context fields the
// search-path resolve cache keys on (OwnAddIncl + PeerAddInclSet +
// BaseSearchPaths). SourceRel is intentionally NOT part of the hash:
// search-path resolution is source-independent (only the includer's
// directory plus the module's ADDINCL/peer/Base search path is
// consulted), so two CCs with different sources but the same module
// configuration can share search-path results. Sysincl resolution
// IS source-dependent (PR-35e introduces per-record source-vs-includer
// keying) and is bypass-cached: computed per call and merged into the
// final result without using resolveCache. The split keeps the
// cross-source cache hit rate that PR-34's pooling refactor delivered.
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

	mixSlice := func(ss []string) {
		for _, s := range ss {
			mix(s)
		}

		h ^= 0xfe
		h *= prime
	}

	mixSlice(ctx.OwnAddIncl)
	mixSlice(ctx.PeerAddInclSet)
	mixSlice(ctx.BaseSearchPaths)

	return h
}

// dfs walks the include closure in depth-first discovery order. PR-34r:
// dispatches on the per-includer subgraph cache. On a cache hit the
// pre-computed canonical-order subgraph rooted at `absPath` is iterated
// and merged into the caller's visited+order, with already-visited
// entries skipped. On a miss the subgraph is computed (with its own
// fresh visited+order) and memoised before merging. Skipping pre-visited
// entries during merge yields the same final order as an uncached DFS
// would produce from the same partially-populated visited state — the
// cached list IS the canonical first-visit order from `absPath`.
//
// PR-34r preserves the pre-existing 4-arg signature so external callers
// (gen.go's joinSrcsIncludeClosure) keep their call shape. The
// srcClassHash needed for the subgraph cache key is derived from
// ctx.SourceRel inside dfs itself; it routes through perSourceView's
// cache so the per-call cost is one map probe.
func (s *IncludeScanner) dfs(absPath string, ctx *ScanContext, ctxHash uint64, visited map[string]struct{}, order *[]string) {
	if _, seen := visited[absPath]; seen {
		return
	}

	// External callers (WalkClosure, gen.go's joinSrcsIncludeClosure)
	// only invoke `dfs` with SOURCE files (`*.cpp`/`*.cc`/`*.c`/`*.S`
	// /…). Each source compiles exactly once across the walker, so a
	// subgraph cache check at the source key would always miss and
	// the speculative canonical-subgraph walk would re-walk the
	// source's entire closure for nothing. Skip the subgraph attempt
	// for files with source-style extensions; plain-dfs into the
	// caller's visited+order so per-header descendants still take the
	// `subgraph()` cache fast path on the recursive dfs() calls inside
	// plainDfs.
	if isSourceLike(absPath) {
		s.plainDfs(absPath, ctx, ctxHash, visited, order)

		return
	}

	srcClassHash := s.sourceClassHash(ctx.SourceRel)
	sg, ok := s.subgraph(absPath, ctx, ctxHash, srcClassHash)

	if ok {
		// Cached or freshly-computed clean canonical subgraph. Merge
		// into caller's visited+order, skipping pre-visited entries.
		for _, p := range sg {
			if _, seen := visited[p]; seen {
				continue
			}

			visited[p] = struct{}{}
			*order = append(*order, p)
		}

		return
	}

	// `absPath` is on a cycle (no cacheable canonical subgraph). Plain
	// DFS into the caller's shared visited+order so already-visited
	// paths skip naturally — this is the original pre-PR-34r dfs, with
	// the only addition being that non-cycle descendants reached via
	// the recursive `dfs` call still hit the persistent subgraph
	// cache.
	s.plainDfs(absPath, ctx, ctxHash, visited, order)
}

// plainDfs walks `absPath`'s subtree using the caller's shared
// visited+order. Used as the fall-back path for headers known to be on
// a cycle (`subgraphTaintedKnown`) and recursively from `walkSubgraph`
// when a child reports it is on a cycle. Per-child dispatch goes
// through `dfs()` so non-cycle descendants benefit from the
// `subgraphCache`.
func (s *IncludeScanner) plainDfs(absPath string, ctx *ScanContext, ctxHash uint64, visited map[string]struct{}, order *[]string) {
	if _, seen := visited[absPath]; seen {
		return
	}

	visited[absPath] = struct{}{}
	*order = append(*order, absPath)

	directives := s.parseIncludes(absPath)

	for _, d := range directives {
		resolved := s.resolve(absPath, d, ctx, ctxHash)

		for _, rabs := range resolved {
			s.dfs(rabs, ctx, ctxHash, visited, order)
		}
	}
}

// SubgraphCacheStats reports the per-includer subgraph cache traffic
// for verification during PR-34r perf measurement. Returns hits, misses,
// and the count of `tainted` (cycle-on-stack) outcomes since scanner
// construction. Hit-rate target ≥30% to make the cache worth keeping
// (the brief's self-recover threshold). On the tools/archiver M2 closure
// the observed hit rate after warmup is 87% (target scanner) / 92%
// (host scanner).
func (s *IncludeScanner) SubgraphCacheStats() (hits, misses, tainted uint64) {
	return s.subgraphHits, s.subgraphMisses, s.subgraphTainted
}

// subgraph returns the canonical transitive include closure rooted at
// `absPath` for the given (ctxHash, srcClassHash) equivalence class —
// the DFS-discovery order an uncached DFS starting at `absPath` with
// empty visited would emit (root included). The returned slice is owned
// by the cache and must NOT be mutated by callers; dfs and the
// recursive walk only iterate.
//
// Cache key is `(abs, ctxHash, srcClassHash)`. ctxHash collapses
// search-path-equivalent ScanContexts; srcClassHash collapses sources
// whose `activeSourceKeyed` set is identical (so they take the same
// sysincl branches). Two scans whose triple matches share the same
// canonical subgraph by construction.
//
// Returns `(sg, ok)`:
//   - `ok=true`: `sg` is the canonical subgraph of `absPath` from clean
//     visited (root-included DFS-discovery order). Caller merges with
//     skip-on-already-visited.
//   - `ok=false`: `absPath` is on a cycle and cannot have a cacheable
//     canonical subgraph (the cycle's content depends on which
//     ancestors are on the stack). `sg` is nil; caller MUST fall back
//     to plain DFS using its OWN visited+order. The `subgraphTaintedKnown`
//     set short-circuits future requests so the wasted speculative
//     walk is paid at most once per (key) globally.
//
// Cycle detection: a call for a key already in `subgraphInProgress`
// (a recursion higher up the call stack is computing the same key) is
// a back-edge. The call returns `(nil, false)`. Caller propagates that
// signal upward by returning ok=false from its own walk; every header
// on the SCC ends up marked taintedKnown (set when subgraph returns
// ok=false to its top-level invoker).
func (s *IncludeScanner) subgraph(absPath string, ctx *ScanContext, ctxHash, srcClassHash uint64) ([]string, bool) {
	key := subgraphKey{abs: absPath, ctxHash: ctxHash, srcClassHash: srcClassHash}

	if cached, ok := s.subgraphCache[key]; ok {
		s.subgraphHits++

		return cached, true
	}

	if _, taintedKnown := s.subgraphTaintedKnown[key]; taintedKnown {
		// We have already discovered this header is on a cycle.
		// Don't waste the speculative walk; tell the caller to plain
		// DFS into its own visited+order.
		s.subgraphHits++

		return nil, false
	}

	if _, busy := s.subgraphInProgress[key]; busy {
		// Back-edge into an ancestor's in-progress computation. The
		// caller will see ok=false, fall back to plain-dfs into its
		// shared visited (which already contains this `absPath`'s
		// ancestors), and the cycle terminates naturally without
		// re-walking.
		return nil, false
	}

	s.subgraphMisses++
	s.subgraphInProgress[key] = struct{}{}

	visited := make(map[string]struct{}, 32)
	order := make([]string, 0, 32)

	clean := s.walkSubgraph(absPath, ctx, ctxHash, srcClassHash, visited, &order)

	delete(s.subgraphInProgress, key)

	if !clean {
		// At least one descendant of `absPath` was on a cycle. Either
		// the back-edge bounced into our own in-progress sentinel
		// (absPath itself is on a cycle) or a descendant's computation
		// reported tainted. Either way, this key cannot be cached and
		// future visits will short-circuit via `taintedKnown`.
		s.subgraphTainted++
		s.subgraphTaintedKnown[key] = struct{}{}

		return nil, false
	}

	// Trim any unused capacity — the slice will live in the cache for
	// the rest of the run, so paying the one-time copy avoids holding
	// over-allocated buffers across millions of cached subgraphs.
	out := make([]string, len(order))
	copy(out, order)

	s.subgraphCache[key] = out

	return out, true
}

// walkSubgraph is the cycle-safe core of canonical-subgraph computation.
// Returns `clean=true` when every recursive descendant produced a clean
// (cacheable) canonical subgraph; returns `clean=false` when at least
// one descendant reported tainted. Tainted children fall back to plain-
// dfs INTO THE LOCAL visited+order so the walk continues to enumerate
// reachable headers in the right order, but the propagated `clean=false`
// prevents the caller from caching its own result.
//
// Crucially the LOCAL visited+order means the order recorded here is
// still the canonical first-visit order from `absPath` — every header
// reachable from `absPath` is added to `order` exactly once, regardless
// of whether it was reached via a cached, tainted, or freshly-walked
// child. The parent of `absPath` (if any) gets a complete-but-uncached
// result and merges it into ITS visited+order; if that parent itself
// fails the cleanliness check, its parent gets the same treatment.
//
// Pure-DAG paths (no cycle in any descendant) cache normally because
// every recursive descendant returns clean=true.
func (s *IncludeScanner) walkSubgraph(absPath string, ctx *ScanContext, ctxHash, srcClassHash uint64, visited map[string]struct{}, order *[]string) bool {
	if _, seen := visited[absPath]; seen {
		return true
	}

	visited[absPath] = struct{}{}
	*order = append(*order, absPath)

	directives := s.parseIncludes(absPath)
	clean := true

	for _, d := range directives {
		resolved := s.resolve(absPath, d, ctx, ctxHash)

		for _, rabs := range resolved {
			if _, seen := visited[rabs]; seen {
				continue
			}

			childSg, ok := s.subgraph(rabs, ctx, ctxHash, srcClassHash)

			if ok {
				// Clean child subgraph — merge into our walk.
				for _, p := range childSg {
					if _, seen := visited[p]; seen {
						continue
					}

					visited[p] = struct{}{}
					*order = append(*order, p)
				}

				continue
			}

			// Tainted child. Plain-dfs into our local visited+order
			// so the walk enumerates the cycle's reachable nodes.
			// `clean=false` propagates upward.
			clean = false

			s.walkSubgraphTainted(rabs, ctx, ctxHash, srcClassHash, visited, order)
		}
	}

	return clean
}

// walkSubgraphTainted is the in-walk plain-DFS used when a child
// reported tainted. It mirrors `plainDfs` but operates on the local
// (subgraph-computation) visited+order rather than the dfs caller's
// shared state. Each child of a tainted-walk node still goes through
// `subgraph()` so non-cycle descendants reuse the persistent cache.
func (s *IncludeScanner) walkSubgraphTainted(absPath string, ctx *ScanContext, ctxHash, srcClassHash uint64, visited map[string]struct{}, order *[]string) {
	if _, seen := visited[absPath]; seen {
		return
	}

	visited[absPath] = struct{}{}
	*order = append(*order, absPath)

	directives := s.parseIncludes(absPath)

	for _, d := range directives {
		resolved := s.resolve(absPath, d, ctx, ctxHash)

		for _, rabs := range resolved {
			if _, seen := visited[rabs]; seen {
				continue
			}

			childSg, ok := s.subgraph(rabs, ctx, ctxHash, srcClassHash)

			if ok {
				for _, p := range childSg {
					if _, seen := visited[p]; seen {
						continue
					}

					visited[p] = struct{}{}
					*order = append(*order, p)
				}

				continue
			}

			s.walkSubgraphTainted(rabs, ctx, ctxHash, srcClassHash, visited, order)
		}
	}
}

// parseIncludes returns the parsed include directives for the file at
// `absPath`. Memoized per absolute path. Returns nil for files that
// do not exist (the caller's resolver dropped them already, but DFS
// may also reach a dangling path through a sysincl mapping that
// names a file the tree does not have).
func (s *IncludeScanner) parseIncludes(absPath string) []includeDirective {
	// PR-34n: lock removed (single-goroutine).
	if cached, ok := s.parsed[absPath]; ok {
		return cached
	}

	data, err := os.ReadFile(absPath)

	if err != nil {
		s.parsed[absPath] = nil

		return nil
	}

	out := make([]includeDirective, 0, 8)

	eachLine(data, func(line []byte) {
		// `FindSubmatchIndex` returns a flat `[]int` of byte offsets
		// (start1,end1, ..., startN,endN). The stdlib internally uses
		// a 4-int dst-cap on the stack, so a tiny match returns
		// without allocating; the [][]byte form of FindSubmatch wraps
		// the same offsets in a freshly-allocated slice header per
		// call (~2 MB flat across the M2 closure pre-PR-34k).
		m := includeRe.FindSubmatchIndex(line)

		if m == nil {
			return
		}

		// Determine kind by inspecting the line's bracket character
		// after the keyword.
		kind := includeSystem
		idx := indexOfAngleOrQuote(line)

		if idx >= 0 && line[idx] == '"' {
			kind = includeQuoted
		}

		// m[2:4] are start/end offsets of the directive keyword
		// (`include` or `include_next`). Comparing on length avoids
		// `string(line[m[2]:m[3]])` allocation per matched line.
		next := (m[3] - m[2]) == len("include_next")

		// m[4:6] are the target capture's byte offsets. The single
		// remaining string allocation per match is converting the
		// target bytes to a string for the cache value.
		target := string(line[m[4]:m[5]])

		out = append(out, includeDirective{kind: kind, next: next, target: target})
	})

	s.parsed[absPath] = out

	return out
}

// resolve returns the absolute paths the include directive resolves
// to, in declaration order, deduplicated within this resolution.
// Memoized via resolveCache: the resolution depends only on the
// (ctxHash, includer, target, kind) tuple — two scans of the same
// includer in the same effective context return the same set.
//
// Two-tier semantics observed in upstream ymake:
//   - Search-path candidates (samedir, own AddIncl, peer-GLOBAL,
//     base linux-headers / musl arch set) are FIRST-MATCH-WINS,
//     mirroring the compiler's `-I` precedence. Once the first
//     existing file is found, no further search-path candidates
//     are tried.
//   - Sysincl candidates are UNION-ON-TOP: every record matching
//     the includer's path adds its mapped paths to the result, on
//     top of whatever the search path produced. This is because
//     `<stddef.h>` from a non-musl C source legitimately resolves
//     to BOTH libcxx/include/stddef.h (via stl-to-libcxx.yml) AND
//     musl/include/stddef.h (via libc-to-musl.yml) — both records
//     are active and both contribute to the input set.
func (s *IncludeScanner) resolve(includerAbs string, d includeDirective, ctx *ScanContext, ctxHash uint64) []string {
	// `#include_next` directives resolve to nothing in the upstream
	// reference scan: every observed live use is the libcxx
	// "shadow-header" pattern where libcxx/X.h does
	// `#include_next <X.h>` to chain through to the system's X.h
	// (e.g. libcxx/wchar.h chaining to musl/include/wchar.h). The
	// chained header is ALWAYS reachable via the parallel C++ wrapper
	// (cwchar, cuchar, cstring, …) which does a regular
	// `#include <X.h>` that resolves via sysincl to BOTH the libcxx
	// and the musl shadow. Following `#include_next` adds no new
	// inputs in those cases — and adds spurious inputs when the
	// `#include_next` lives inside an `#elif` branch the live
	// preprocessor never takes (PR-31-D08, PR-33-C03:
	// __mbstate_t.h's `#elif __has_include_next(<uchar.h>)` is dead
	// when `_LIBCPP_HAS_MUSL_LIBC` is set, but our text-blind scanner
	// followed it through both the search path and sysincl, doubling
	// in 422 libcxx + 23 JS-derived CC consumers).
	//
	// Returning early here is the surgical fix for that ceiling. We
	// do not attempt to evaluate `#elif` chains (out of scope for
	// PR-35e); the heuristic is conservative — real `#include_next`
	// chains in our M2 closure all duplicate paths the parallel
	// regular-include path already supplies.
	if d.next {
		return nil
	}

	// Two-level resolve. Search-path resolution is source-independent
	// and goes through resolveCache (keyed by ctxHash + includer +
	// target + kind) for cross-source reuse. Sysincl resolution is
	// source-dependent (PR-35e per-record keying) and goes through
	// per-half caches: source-keyed by (sourceRel, target) reused
	// within one source's closure, includer-keyed by (includer,
	// target) reused across every source reaching that includer.
	searchOut := s.resolveSearchPath(includerAbs, d, ctx, ctxHash)

	// Sysincl: add EVERY matching record's contribution on top of the
	// search-path result. PR-35e: per-record source-vs-includer
	// keying — each SysIncl record carries a `KeyBySource` flag
	// (compiled from its `source_filter` shape: negative-lookahead
	// `(?!...)` → key by source, otherwise key by includer). The
	// SysInclSet.Lookup signature takes BOTH paths; per-record
	// dispatch picks which to test against. PR-33 D05 attempted a
	// blanket source-keyed lookup and lost the glibcasm closure for
	// 125 musl CC nodes (filter `^contrib/libs/glibcasm` had to fire
	// on glibcasm-rooted includer chains reached from musl sources);
	// per-record dispatch keeps that includer-keyed branch intact
	// while flipping the negative-lookahead records (stl-to-libcxx,
	// libc-to-musl line 75, libc-to-compat) to source-keyed so they
	// no longer fire on libcxx-internal includer chains reaching
	// uchar.h/wchar.h via `__has_include_next` shadow patterns.
	includerRel := strings.TrimPrefix(includerAbs, s.sourceRootSlash)
	mappings := s.sysinclLookup(ctx.SourceRel, includerRel, d.target)

	if len(mappings) == 0 {
		return searchOut
	}

	// Layer sysincl mappings on top of the search-path result.
	// `mappings` already carry absolute paths (the per-half cache
	// pre-converts via `absifyRels`); we still file-check each because
	// some sysincl entries point at files the tree may lack. When no
	// new entries stick we return searchOut directly to avoid the
	// make/copy.
	//
	// Fast path: when searchOut is empty (the common case for system
	// includes hitting only sysincl) we can use `mappings` directly,
	// applying file-check + dedup in place to a fresh slice without
	// copying searchOut. Linear-scan dedup beats the map alloc since
	// mapping lists are 1-3 entries long.
	if len(searchOut) == 0 {
		var out []string

	fastLoop:
		for _, abs := range mappings {
			for _, q := range out {
				if q == abs {
					continue fastLoop
				}
			}

			if !s.fileExists(abs) {
				continue
			}

			if out == nil {
				out = make([]string, 0, len(mappings))
			}

			out = append(out, abs)
		}

		return out
	}

	var out []string

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

		if !s.fileExists(abs) {
			continue
		}

		if out == nil {
			out = make([]string, len(searchOut), len(searchOut)+len(mappings))
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
// (includer, target) — the cross-source cache hit rate that PR-34d's
// pooling refactor preserved — while the source-keyed half is reused
// within a single source's closure for repeat targets.
//
// Returns either srcMappings (when incMappings is empty), incMappings
// (when srcMappings is empty), or a freshly-allocated union slice. The
// dedup uses a linear scan over `out` because typical sysincl mapping
// lists are 1-3 entries long; a map allocation per call would dominate
// the per-resolve cost.
func (s *IncludeScanner) sysinclLookup(sourceRel, includerRel, target string) []string {
	srcMappings := s.sysinclSourceLookup(sourceRel, target)
	incMappings := s.sysinclIncluderLookup(includerRel, target)

	if len(srcMappings) == 0 {
		return incMappings
	}

	if len(incMappings) == 0 {
		return srcMappings
	}

	out := make([]string, 0, len(srcMappings)+len(incMappings))
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

	return out
}

func (s *IncludeScanner) sysinclSourceLookup(sourceRel, target string) []string {
	key := sysinclSourceKey{sourceRel: sourceRel, target: target}

	// PR-34n: lock removed (single-goroutine).
	if cached, ok := s.sysinclSourceCache[key]; ok {
		return cached
	}

	view := s.perSourceView(sourceRel)
	rels, _ := view.LookupSourceKeyed(target)

	mappings := s.absifyRels(rels)
	s.sysinclSourceCache[key] = mappings

	return mappings
}

func (s *IncludeScanner) sysinclIncluderLookup(includerRel, target string) []string {
	key := sysinclIncluderKey{includerRel: includerRel, target: target}

	// PR-34n: lock removed (single-goroutine).
	if cached, ok := s.sysinclIncluderCache[key]; ok {
		return cached
	}

	// PerSourceView's includerKeyed slice is identical regardless of
	// which source it was prepared for (every view derives it from the
	// same SysInclSet). Use the prepared anySrcView (initialised once
	// at NewIncludeScanner) to access the includer-keyed records
	// without going through perSourceView.
	rels, _ := s.anySrcView.LookupIncluderKeyed(includerRel, target)

	mappings := s.absifyRels(rels)
	s.sysinclIncluderCache[key] = mappings

	return mappings
}

// absifyRels converts a list of SOURCE_ROOT-relative paths (as produced
// by sysincl YAMLs) into absolute paths under the scanner's source
// root, normalising `..`/`.` segments at the same time. Cached at the
// per-half sysinclCache level so the per-resolve hot path can skip
// the per-mapping `prefix + rel` string concatenation that dominated
// the alloc profile pre-PR-35e perf tuning.
func (s *IncludeScanner) absifyRels(rels []string) []string {
	if len(rels) == 0 {
		return nil
	}

	out := make([]string, 0, len(rels))

	for _, rel := range rels {
		out = append(out, s.sourceRootSlash+normalisePath(rel))
	}

	return out
}

// perSourceView returns a cached SysInclSet view with SOURCE-keyed
// filters pre-resolved against `sourceRel`. Computed once per source
// and reused for every per-include resolve in that source's closure;
// cross-source reuse on top of that is safe — `viewCache` is keyed by
// SourceRel — so two CCs with the same SourceRel (rare but possible
// in dual-platform host/target emission) share the same view.
func (s *IncludeScanner) perSourceView(sourceRel string) PerSourceView {
	// PR-34n: lock removed (single-goroutine).
	if cached, ok := s.viewCache[sourceRel]; ok {
		return cached
	}

	view := s.sysincl.PreparePerSource(sourceRel)
	s.viewCache[sourceRel] = view

	return view
}

// resolveSearchPath returns the search-path-only resolved set for the
// given directive. Cached by (ctxHash, includer, target, kind) — the
// result is source-independent.
func (s *IncludeScanner) resolveSearchPath(includerAbs string, d includeDirective, ctx *ScanContext, ctxHash uint64) []string {
	key := resolveKey{
		ctxHash:  ctxHash,
		includer: includerAbs,
		target:   d.target,
		kind:     d.kind,
		next:     d.next,
	}

	// PR-34n: lock removed (single-goroutine).
	if cached, ok := s.resolveCache[key]; ok {
		return cached
	}

	var out []string

	// Pool the per-resolve dedup map. PR-34k's profile showed
	// `resolveSearchPath`'s map literal as a per-call ~256 B alloc
	// fired ~40k times across the tools/archiver run. The map is
	// cleared and returned to the pool before we exit.
	seenP := s.seenPool.Get().(*map[string]struct{})
	seen := *seenP

	addPath := func(rel string) bool {
		// Normalize `..`/`.` segments so paths like
		// `musl/src/include/../../include/features.h` collapse to
		// `musl/include/features.h`. Empirical observation: the
		// upstream scanner emits the canonical path.
		rel = normalisePath(rel)

		if _, dup := seen[rel]; dup {
			return false
		}

		abs := s.sourceRootSlash + rel

		if !s.fileExists(abs) {
			return false
		}

		seen[rel] = struct{}{}
		out = append(out, abs)

		return true
	}

	// First-match-wins across the search path. Order:
	//   1. quoted-form: same directory as the includer
	//   2. module's own ADDINCL
	//   3. peer-propagated GLOBAL ADDINCL
	//   4. baseline (linux-headers, musl arch when applicable)
	searchPathFound := false

	if d.kind == includeQuoted {
		incRel := strings.TrimPrefix(includerAbs, s.sourceRootSlash)
		incDir := pathDir(incRel)

		var candidate string

		if incDir != "" {
			candidate = incDir + "/" + d.target
		} else {
			candidate = d.target
		}

		if addPath(candidate) {
			searchPathFound = true
		}
	}

	if !searchPathFound {
		for _, p := range ctx.OwnAddIncl {
			if addPath(p + "/" + d.target) {
				searchPathFound = true

				break
			}
		}
	}

	if !searchPathFound {
		for _, p := range ctx.PeerAddInclSet {
			if addPath(p + "/" + d.target) {
				searchPathFound = true

				break
			}
		}
	}

	if !searchPathFound {
		for _, p := range ctx.BaseSearchPaths {
			// An empty prefix represents SOURCE_ROOT itself: resolve
			// the target directly (no prefix + separator) so that
			// `<util/foo.h>` tries $(sourceRoot)/util/foo.h rather
			// than $(sourceRoot)//util/foo.h.
			var candidate string

			if p == "" {
				candidate = d.target
			} else {
				candidate = p + "/" + d.target
			}

			if addPath(candidate) {
				break
			}
		}
	}

	// Reset and release the dedup map to the pool. `clear()` (Go 1.21+)
	// drops every key without releasing the bucket allocation, so the
	// next caller starts with empty-but-prewarmed state.
	clear(seen)
	s.seenPool.Put(seenP)

	// PR-34n: lock removed (single-goroutine).
	s.resolveCache[key] = out

	return out
}

// isSourceLike returns true when `absPath` ends with a compile-unit
// extension — `.cpp`, `.cc`, `.cxx`, `.c`, `.S`, `.s`, `.m`, `.mm`. The
// scanner uses this to skip the subgraph-cache speculation at top-level
// dfs entry points, where the absPath is always a source. The
// extensions enumerated here cover the M2 closure's compile-unit set
// (cc.go / as.go / r6.go produce these); headers (`.h`, `.hh`, `.hpp`,
// `.inl`, `.ipp`, `.tcc`) and ragel/protobuf intermediate sources
// (`.rl`, `.proto`, `.pb.cc`) all return false and go through the
// subgraph cache path.
func isSourceLike(absPath string) bool {
	// Look only at the final segment; sysincl-resolved paths can have
	// multiple `.` separators (e.g. `foo/bar.pb.cc`).
	idx := strings.LastIndexByte(absPath, '.')

	if idx < 0 {
		return false
	}

	ext := absPath[idx:]

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
	if !strings.Contains(p, "..") && !strings.Contains(p, "./") {
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

// fileExists is a thin cached wrapper around os.Stat. Returns true
// for regular files only (directories return false).
func (s *IncludeScanner) fileExists(absPath string) bool {
	// PR-34n: lock removed (single-goroutine).
	if cached, ok := s.exists[absPath]; ok {
		return cached
	}

	info, err := os.Stat(absPath)
	val := err == nil && !info.IsDir()

	s.exists[absPath] = val

	return val
}

// eachLine invokes `fn` for every newline-terminated record in `data`,
// passing a sub-slice of `data` (no per-line slice allocation, no
// `[][]byte` accumulator). The optional trailing `\r` is stripped to
// match POSIX-vs-Windows line conventions. The callback must not retain
// the slice past its invocation: the next iteration may reuse the same
// backing memory for a different sub-slice. PR-34k replaced the prior
// `splitLinesNoAlloc` (which allocated a per-file `make([][]byte, 0,
// 64)` — ~74 MB across the tools/archiver run) with this iterator.
func eachLine(data []byte, fn func(line []byte)) {
	start := 0

	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			line := data[start:i]
			// Strip optional trailing CR.
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}

			fn(line)
			start = i + 1
		}
	}

	if start < len(data) {
		fn(data[start:])
	}
}

// indexOfAngleOrQuote returns the index of the first `<` or `"` in `b`,
// or -1 when neither is present. Specialised for include-directive
// parsing — replaces the generic `indexOfAny` two-byte loop, which
// allocated nothing but ran a length-2 inner loop per byte; the
// specialised form is straight-line and inlines.
func indexOfAngleOrQuote(b []byte) int {
	for i := 0; i < len(b); i++ {
		c := b[i]

		if c == '<' || c == '"' {
			return i
		}
	}

	return -1
}
