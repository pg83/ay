package main

// scanner.go — C/C++ #include transitive-closure scanner. Mirrors
// upstream ymake closely enough for L2-multiset acceptance: text-based
// regex match, conditional-blind, ADDINCL + peer-GLOBAL ADDINCL +
// sysincl resolution, DFS with per-source visited set, file-level
// memoization of parsed directives.
//
// Documented gaps: `#include MACRO_NAME` macro-expanded forms (handled
// case-by-case via `macroIndirectIncludes`); exact ymake scanner-order
// traversal (L2 compares as multiset, so DFS-discovery is sufficient).
//
// `stripComments` runs before regex matching so block-comment / line-
// comment / string-literal payloads are replaced with spaces (newlines
// kept for per-line `^\s*#` anchoring). Without it, a `/* ... #include
// <iostream> ... */` block inside `from_chars_integral.h` floods every
// `<charconv>` consumer with phantom `<iostream>` and its cascade.

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
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

// includeRe matches `#include` / `#include_next` directives in their
// angle-bracket and quoted-string forms, tolerating arbitrary
// whitespace between `#`, the keyword, and the bracket. Two capture
// groups: directive (`include` or `include_next`) and target.
var includeRe = regexp.MustCompile(`^\s*#\s*(include|include_next)\s*[<"]([^>"]+)[>"]`)

// macroIndirectIncludes augments parseIncludes for sources that use
// macro-indirect `#include MACRO_NAME` forms. The text-blind scanner
// cannot expand macros, so e.g. `#include OPENSSL_UNISTD` parses to
// nothing. Each entry lists the include targets the source's macro-
// indirect lines expand to on a linux-musl target — what the upstream
// scanner emits. Resolution flows through the normal resolve()/sysincl
// pipeline.
type macroIndirectInclude struct {
	target string
	kind   includeKind
}

var macroIndirectIncludes = map[string][]macroIndirectInclude{
	"contrib/libs/openssl/crypto/rand/rand_egd.c": {{target: "unistd.h", kind: includeSystem}},
	"contrib/libs/openssl/crypto/uid.c":           {{target: "unistd.h", kind: includeSystem}},
	// pugixml.hpp's header-only-mode trailer:
	//   #if defined(PUGIXML_HEADER_ONLY) && !defined(PUGIXML_SOURCE)
	//   #    define PUGIXML_SOURCE "pugixml.cpp"
	//   #    include PUGIXML_SOURCE
	// The macro-indirect `#include PUGIXML_SOURCE` expands to a quoted
	// include of pugixml.cpp, which then pulls in the standard <float.h>
	// + <setjmp.h> XPath dependencies — both reach musl-self on linux.
	"contrib/libs/pugixml/pugixml.hpp": {{target: "pugixml.cpp", kind: includeQuoted}},
}

// yasmIncludeRe matches NASM/yasm `%include` directives in `.asm` /
// `.asi` sources. Token is case-insensitive (`%include` and `%INCLUDE`
// both occur in asmlib). Both quoted and angle-bracket forms accepted;
// only quoted appears in practice. Single capture group: target.
var yasmIncludeRe = regexp.MustCompile(`(?i)^\s*%\s*include\s*[<"]([^>"]+)[>"]`)

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

// sharedParseCache holds the parse-level caches that are architecture-
// independent: file-byte parsing (parsed) and file existence (exists).
// Both depend only on the source tree, not on which sysincl YAML records
// are loaded, so target/host scanner pairs in GenWith share one cache.
//
// Full unification is not safe: sysincl resolution IS arch-dependent
// (linux-musl-aarch64.yml vs linux-musl.yml map bits/* headers to
// different paths). The resolve chain (resolveCache, subgraphCache,
// sysincl{Source,Includer}Cache) stays per-scanner.
type sharedParseCache struct {
	// parsed memoises include directives per VFS-rooted path
	// ($(S)/<rel>). 8192 pre-size covers the tools/archiver peak
	// (4354 target + 3559 host, mostly overlapping).
	parsed VFSMap[[]includeDirective]
	// exists memoises os.Stat results, keyed by SOURCE_ROOT-relative
	// tail. 16384 covers the observed peak.
	exists map[string]bool
}

// newSharedParseCache allocates a sharedParseCache with pre-sized maps
// matched to the observed peak for the tools/archiver closure.
func newSharedParseCache() *sharedParseCache {
	return &sharedParseCache{
		parsed: NewVFSMap[[]includeDirective](8192),
		exists: make(map[string]bool, 16384),
	}
}

// scannerInterner assigns scanner-local numeric IDs to repeated strings,
// VFS paths, and source-class signatures so hot cache keys stay compact
// and avoid repeated string hashing in their own maps.
type scannerInterner struct {
	stringIDs map[string]uint32
	nextStr   uint32
}

const scannerInternerBuildBit = uint32(1) << 31

func newScannerInterner() scannerInterner {
	return scannerInterner{
		stringIDs: make(map[string]uint32, 32768),
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

// IncludeScanner is the per-walker include-resolver state. It owns the
// SysInclSet, sourceRoot, the shared parse cache (parsed + exists), the
// per-scanCtx resolve/subgraph caches, scratch-buffer sync.Pools, and
// the sysincl per-half caches.
//
// The scanner is invoked exclusively from gen.go's serial walker — no
// locking. If a future change introduces per-source goroutines, every
// cache access site needs a mutex reintroduced.
type IncludeScanner struct {
	sysincl    SysInclSet
	sourceRoot string
	// sourceRootSlash is the precomputed `sourceRoot + "/"` prefix
	// used at the FS-translation boundary (parseIncludes / fileExists
	// / fsLocator.Exists) to keep the concat alloc-free.
	sourceRootSlash string

	// pc holds the parse-level caches shared between target and host
	// scanners. Never nil.
	pc *sharedParseCache
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

	visitedPool sync.Pool // *VFSSet
	orderPool   sync.Pool // *[]VFS
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
	subgraphHits    uint64
	subgraphMisses  uint64
	subgraphTainted uint64
	statsCallCount  uint64

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

// subgraphInnerKey is the per-scanCtx subgraph cache key. ctxHash is
// implicit; sourceClass stays because a single scanCtx serves many
// sources whose sysincl branches differ even within one ctxHash.
type subgraphInnerKey struct {
	abs         uint32
	sourceClass uint32
}

// scanCtx is the per-ctxHash runtime context for include resolution. It
// owns every cache whose key contains ctxHash. Two lifecycle policies
// (see gen.go): "local" (fresh per genModule call) and "interned"
// (genCtx-owned, shared across modules whose ScanContext shape matches).
type scanCtx struct {
	scanner *IncludeScanner
	cfg     ScanContext
	ctxHash uint64

	resolveCache         map[resolveInnerKey][]VFS
	subgraphCache        map[subgraphInnerKey][]VFS
	subgraphTaintedKnown map[subgraphInnerKey]struct{}
	subgraphInProgress   map[subgraphInnerKey]struct{}
}

type sysinclSourceKey struct {
	sourceClass uint32
	target      uint32
}

type sysinclIncluderKey struct {
	includer uint32
	target   uint32
}

// sysinclCacheEntry stores the resolved sysincl paths plus two flags.
// hasMultiTarget is true when any contributing record maps the queried
// header to ≥ 2 non-empty paths (used by the quoted-include gate).
// claimed is true when at least one record's filter matched and its
// Mappings contained the queried header — even with empty paths
// (bare-key suppression). Lets resolve() distinguish "sysincl knows
// this header but suppresses it" (no warning) from "sysincl doesn't
// know this header" (warn under --verbose).
type sysinclCacheEntry struct {
	paths          []VFS
	hasMultiTarget bool
	claimed        bool
}

// NewIncludeScanner constructs a scanner bound to a SysInclSet and a
// source-root absolute path. Allocates a private sharedParseCache; use
// newIncludeScannerWith to share a parse cache between scanners.
func NewIncludeScanner(sourceRoot string, sysincl SysInclSet) *IncludeScanner {
	return newIncludeScannerWith(sourceRoot, sysincl, newSharedParseCache(), func(Warn) {})
}

// newIncludeScannerWith is the internal constructor used when a
// sharedParseCache is provided externally (target/host pair in GenWith).
// pc must be non-nil; both scanners must share the same sourceRoot (the
// cache is keyed by absolute path, so a mismatched root returns stale
// results).
func newIncludeScannerWith(sourceRoot string, sysincl SysInclSet, pc *sharedParseCache, onWarn func(Warn)) *IncludeScanner {
	// Pre-sizes set to the upper bound of the observed working set for
	// tools/archiver; sysinclSourceCache reaches ~328k entries on the
	// target scanner, so pre-sizing past the peak eliminates rehashing.
	s := &IncludeScanner{
		sysincl:              sysincl,
		sourceRoot:           sourceRoot,
		sourceRootSlash:      sourceRoot + "/",
		pc:                   pc,
		interner:             newScannerInterner(),
		sourceClassCache:     make(map[string]uint32, 1024),
		sourceClassViews:     make(map[uint32]PerSourceView, 1024),
		sourceClassBuckets:   make(map[uint64][]uint32, 1024),
		sysinclSourceCache:   make(map[sysinclSourceKey]sysinclCacheEntry, 131072),
		sysinclIncluderCache: make(map[sysinclIncluderKey]sysinclCacheEntry, 8192),
		walkClosureCache:     make(map[uint64]*scanCtx, 8),
		onWarn:               onWarn,
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
	s.visitedPool.New = func() any {
		m := NewVFSSet(64)

		return &m
	}

	s.orderPool.New = func() any {
		o := make([]VFS, 0, 64)

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
// propagated GLOBAL ADDINCL, and the BaseSearchPaths baseline.
type ScanContext struct {
	SourceRel       string // SOURCE_ROOT-relative path of the primary source
	OwnAddIncl      []VFS  // module's own non-GLOBAL ADDINCL
	PeerAddInclSet  []VFS  // peer-propagated GLOBAL ADDINCL (transitive)
	BaseSearchPaths []VFS  // baseline include set (linux-headers, musl arch when applicable)
}

// NewScanCtx allocates a fresh per-context resolution object bound to
// this scanner and the given ScanContext. The returned scanCtx owns its
// own resolveCache and subgraphCache; lifetime is the caller's (see
// gen.go's local vs interned dispatch). ctxHash is computed once at
// construction.
func (s *IncludeScanner) NewScanCtx(cfg ScanContext) *scanCtx {
	// Pre-sizes cover the largest single-context working set with one
	// or two grows on M2. M3 creates many more distinct ctxHashes, so
	// over-sizing every context wastes hundreds of MB and feeds GC; keep
	// the initial buckets modest and let the few large contexts grow.
	return &scanCtx{
		scanner:              s,
		cfg:                  cfg,
		ctxHash:              hashScanContext(&cfg),
		resolveCache:         make(map[resolveInnerKey][]VFS, 1024),
		subgraphCache:        make(map[subgraphInnerKey][]VFS, 512),
		subgraphTaintedKnown: make(map[subgraphInnerKey]struct{}, 64),
		subgraphInProgress:   make(map[subgraphInnerKey]struct{}, 16),
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

// WalkClosure walks the include closure rooted at `vfsPath`. vfsPath
// MUST be VFS-rooted — $(S)/... (FS-backed) or $(B)/...
// (codegen-registry-backed). Dispatch on provenance lives in
// forEachResolvedChild's locator branch, not in callers.
//
// Returns the transitive header set excluding the root itself, in DFS-
// discovery order. The result slice is freshly allocated.
//
// For $(S)/ roots SourceRel is derived from the VFS path so cross-
// source DFS within one scanCtx keys sysincl per source. For $(B)/
// roots no sysincl fires at the root (children come from pre-resolved
// EmitsIncludes).
func (sc *scanCtx) WalkClosure(vfsPath VFS) []VFS {
	s := sc.scanner

	// scanCtx is shared across sources within a module; overwrite
	// cfg.SourceRel so resolve()'s sysinclSourceLookup keys on the
	// CURRENT source. For $(B)/ roots there is no meaningful source-rel,
	// and forEachResolvedChild's BUILD branch never consults SourceRel.
	if vfsPath.IsSource() {
		sc.cfg.SourceRel = vfsPath.Rel
	}

	visitedP := s.visitedPool.Get().(*VFSSet)
	orderP := s.orderPool.Get().(*[]VFS)

	visited := *visitedP
	order := (*orderP)[:0]

	sc.dfs(vfsPath, visited, &order)

	out := make([]VFS, 0, len(order))

	for _, abs := range order {
		// Skip the root itself; only headers are emitted.
		if abs == vfsPath {
			continue
		}

		out = append(out, abs)
	}

	// Reset and return scratch buffers to the pool. VFSSet.Clear
	// drops every entry in both buckets while retaining the
	// underlying bucket allocations for reuse.
	visited.Clear()
	*orderP = order[:0]

	s.visitedPool.Put(visitedP)
	s.orderPool.Put(orderP)

	if scannerStatsEnabled {
		s.statsCallCount++

		// SCANNER_STATS env-gated trace; emit every 500 calls. The
		// boolean check short-circuits in production.
		if s.statsCallCount%500 == 0 {
			fmt.Fprintf(os.Stderr, "scanner-stats[%d]: subgraph hits=%d misses=%d tainted=%d cache=%d\n", s.statsCallCount, s.subgraphHits, s.subgraphMisses, s.subgraphTainted, len(sc.subgraphCache))
		}
	}

	return out
}

// IncludeDirectiveTargets returns the raw `#include` directive target
// strings parsed from `vfsPath`, in source order, with no resolution
// applied. Memoised through the same parse-cache WalkClosure populates;
// dispatches on provenance: $(S)/ uses the FS parser, $(B)/ returns the
// registered EmitsIncludes list as-is. Re-opening the file with os.Open
// and re-implementing bracket extraction is forbidden.
func (s *IncludeScanner) IncludeDirectiveTargets(vfsPath VFS) []string {
	if vfsPath.IsBuild() {
		if s.codegen != nil {
			if info, ok := s.codegen.Lookup(vfsPath); ok {
				out := make([]string, len(info.EmitsIncludes))
				for i, v := range info.EmitsIncludes {
					out[i] = v.String()
				}
				return out
			}
		}
		return nil
	}

	directives := s.parseIncludes(vfsPath)
	if len(directives) == 0 {
		return nil
	}

	out := make([]string, len(directives))
	for i, d := range directives {
		out[i] = d.target
	}
	return out
}

// scannerStatsEnabled is set once at process start from $SCANNER_STATS.
// PR-34r perf instrumentation: when set, WalkClosure periodically dumps
// subgraph cache hit/miss counters to stderr. No-op when env not set.
var scannerStatsEnabled = os.Getenv("SCANNER_STATS") != ""

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

// sourceClassID returns the scanner-local numeric ID of the active
// SOURCE-keyed sysincl equivalence class for sourceRel.
func (s *IncludeScanner) sourceClassID(sourceRel string) uint32 {
	id, _ := s.sourceClass(sourceRel)

	return id
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

// dfs walks the include closure in depth-first discovery order via the
// per-includer subgraph cache. On a hit, the pre-computed canonical-
// order subgraph rooted at `absPath` is merged into the caller's
// visited+order, skipping pre-visited entries. On a miss, the subgraph
// is computed and memoised. Skipping pre-visited entries during merge
// preserves the canonical first-visit order an uncached DFS would
// produce from the same partially-populated visited state.
func (sc *scanCtx) dfs(absPath VFS, visited VFSSet, order *[]VFS) {
	if visited.Has(absPath) {
		return
	}

	// External callers invoke dfs only with source files; each source
	// compiles once, so a subgraph cache probe always misses. Skip the
	// speculative walk for source extensions and plain-dfs into the
	// caller's visited+order — per-header descendants reached
	// recursively still take the subgraph cache fast path.
	if isSourceLike(absPath) {
		sc.plainDfs(absPath, visited, order)

		return
	}
	sourceClass := sc.scanner.sourceClassID(sc.cfg.SourceRel)
	sg, ok := sc.subgraph(absPath, sourceClass)

	if ok {
		// Cached or freshly-computed clean canonical subgraph. Merge
		// into caller's visited+order, skipping pre-visited entries.
		for _, p := range sg {
			if !visited.AddIfAbsent(p) {
				continue
			}

			*order = append(*order, p)
		}

		return
	}

	// absPath is on a cycle (no cacheable canonical subgraph). Plain
	// DFS into the caller's shared visited+order; non-cycle descendants
	// reached recursively still hit the persistent subgraph cache.
	sc.plainDfs(absPath, visited, order)
}

// plainDfs walks `absPath`'s subtree using the caller's shared
// visited+order. Used as the fall-back path for headers known to be on
// a cycle (`subgraphTaintedKnown`) and recursively from `walkSubgraph`
// when a child reports it is on a cycle. Per-child dispatch goes
// through `dfs()` so non-cycle descendants benefit from the
// `subgraphCache`.
func (sc *scanCtx) plainDfs(absPath VFS, visited VFSSet, order *[]VFS) {
	if !visited.AddIfAbsent(absPath) {
		return
	}

	*order = append(*order, absPath)

	sc.forEachResolvedChild(absPath, func(rabs VFS) {
		sc.dfs(rabs, visited, order)
	})
}

// forEachResolvedChild invokes `fn` once per resolved-child VFS path of
// `vfsPath`, dispatching on path provenance:
//
//   - $(B)/ path in the per-scanner CodegenRegistry: children are the
//     entry's EmitsIncludes (already in VFS form, pre-resolved). No
//     parseIncludes/resolve — the file does not exist on disk.
//   - $(S)/ path: children come from parseIncludes(vfsPath) piped
//     through resolve().
//
// Registry children are emitted in EmitsIncludes order (sorted by the
// emitters: protoDirectImportIncludes, cfIncludeDirectives, EN
// registration). Caller handles visited-set deduplication.
func (sc *scanCtx) forEachResolvedChild(vfsPath VFS, fn func(rabs VFS)) {
	s := sc.scanner

	if vfsPath.IsBuild() {
		if s.codegen != nil {
			if info, ok := s.codegen.Lookup(vfsPath); ok {
				for _, rabs := range info.EmitsIncludes {
					fn(rabs)
				}

				return
			}
		}

		// $(B) path not in the registry: nothing to walk (either a
		// registered output reached as a leaf, or an unknown BUILD_ROOT
		// path). parseIncludes would fail os.ReadFile in either case.
		return
	}

	directives := s.parseIncludes(vfsPath)

	for _, d := range directives {
		resolved := sc.resolve(vfsPath, d)

		for _, rabs := range resolved {
			fn(rabs)
		}
	}
}

// SubgraphCacheStats reports per-includer subgraph cache traffic since
// scanner construction. Observed tools/archiver hit rate after warmup
// is ~87% (target) / ~92% (host).
func (s *IncludeScanner) SubgraphCacheStats() (hits, misses, tainted uint64) {
	return s.subgraphHits, s.subgraphMisses, s.subgraphTainted
}

// subgraph returns the canonical transitive include closure rooted at
// `absPath` for the (ctxHash, sourceClass) equivalence class — root-
// included DFS-discovery order. The returned slice is cache-owned;
// callers must only iterate.
//
// Returns (sg, true) on success; the caller merges with skip-on-already-
// visited. Returns (nil, false) when absPath is on a cycle (no cacheable
// canonical subgraph); caller falls back to plain DFS into its OWN
// visited+order. subgraphTaintedKnown short-circuits future requests so
// the wasted speculative walk is paid at most once per key.
//
// Cycle detection: a call for a key in subgraphInProgress is a back-
// edge; returns (nil, false) and propagates upward — every header on
// the SCC ends up marked taintedKnown.
func (sc *scanCtx) subgraph(absPath VFS, sourceClass uint32) ([]VFS, bool) {
	s := sc.scanner
	key := subgraphInnerKey{
		abs:         s.interner.internVFS(absPath),
		sourceClass: sourceClass,
	}

	if cached, ok := sc.subgraphCache[key]; ok {
		s.subgraphHits++

		return cached, true
	}

	if _, taintedKnown := sc.subgraphTaintedKnown[key]; taintedKnown {
		// Already discovered as on-a-cycle; tell the caller to plain-
		// DFS into its own visited+order.
		s.subgraphHits++

		return nil, false
	}

	if _, busy := sc.subgraphInProgress[key]; busy {
		// Back-edge into an ancestor's in-progress computation. The
		// caller plain-dfs into its shared visited (already contains
		// the ancestors); the cycle terminates without re-walking.
		return nil, false
	}

	s.subgraphMisses++
	sc.subgraphInProgress[key] = struct{}{}

	// Each subgraph computation needs its own isolated visited+order
	// (isolation is what makes the cache correct). Pool the throwaway
	// buffers to avoid per-call make(map)+make([]) allocs.
	visitedP := s.visitedPool.Get().(*VFSSet)
	orderP := s.orderPool.Get().(*[]VFS)

	visited := *visitedP
	order := (*orderP)[:0]

	clean := sc.walkSubgraph(absPath, sourceClass, visited, &order)

	delete(sc.subgraphInProgress, key)

	if !clean {
		// A descendant of `absPath` was on a cycle (back-edge to our
		// own sentinel, or a descendant reported tainted). This key
		// cannot be cached; future visits short-circuit via taintedKnown.
		s.subgraphTainted++
		sc.subgraphTaintedKnown[key] = struct{}{}

		// Return scratch buffers to the pool before returning.
		visited.Clear()
		*orderP = order[:0]
		s.visitedPool.Put(visitedP)
		s.orderPool.Put(orderP)

		return nil, false
	}

	// Trim any unused capacity — the slice will live in the cache for
	// the rest of the run, so paying the one-time copy avoids holding
	// over-allocated buffers across millions of cached subgraphs.
	out := make([]VFS, len(order))
	copy(out, order)

	// Return scratch buffers to the pool.
	visited.Clear()
	*orderP = order[:0]
	s.visitedPool.Put(visitedP)
	s.orderPool.Put(orderP)

	sc.subgraphCache[key] = out

	return out, true
}

// walkSubgraph is the cycle-safe core of canonical-subgraph computation.
// Returns clean=true when every descendant produced a cacheable subgraph;
// clean=false when at least one descendant reported tainted. Tainted
// children plain-dfs INTO THE LOCAL visited+order so the walk still
// enumerates reachable headers in the canonical first-visit order; the
// propagated clean=false just prevents caching. Pure-DAG paths cache
// normally.
func (sc *scanCtx) walkSubgraph(absPath VFS, sourceClass uint32, visited VFSSet, order *[]VFS) bool {
	if !visited.AddIfAbsent(absPath) {
		return true
	}

	*order = append(*order, absPath)

	clean := true

	sc.forEachResolvedChild(absPath, func(rabs VFS) {
		if visited.Has(rabs) {
			return
		}

		childSg, ok := sc.subgraph(rabs, sourceClass)

		if ok {
			// Clean child subgraph — merge into our walk.
			for _, p := range childSg {
				if !visited.AddIfAbsent(p) {
					continue
				}

				*order = append(*order, p)
			}

			return
		}

		// Tainted child. Plain-dfs into our local visited+order
		// so the walk enumerates the cycle's reachable nodes.
		// `clean=false` propagates upward.
		clean = false

		sc.walkSubgraphTainted(rabs, sourceClass, visited, order)
	})

	return clean
}

// walkSubgraphTainted is the in-walk plain-DFS used when a child
// reported tainted. Mirrors plainDfs but on the local (subgraph-
// computation) visited+order. Each child still goes through subgraph()
// so non-cycle descendants reuse the persistent cache.
func (sc *scanCtx) walkSubgraphTainted(absPath VFS, sourceClass uint32, visited VFSSet, order *[]VFS) {
	if !visited.AddIfAbsent(absPath) {
		return
	}

	*order = append(*order, absPath)

	sc.forEachResolvedChild(absPath, func(rabs VFS) {
		if visited.Has(rabs) {
			return
		}

		childSg, ok := sc.subgraph(rabs, sourceClass)

		if ok {
			for _, p := range childSg {
				if !visited.AddIfAbsent(p) {
					continue
				}

				*order = append(*order, p)
			}

			return
		}

		sc.walkSubgraphTainted(rabs, sourceClass, visited, order)
	})
}

// parseIncludes returns the parsed include directives for the $(S)/-
// rooted file `vfsPath`. The FS translation happens here at the
// os.ReadFile call. Memoised by VFS path; returns nil for missing files
// (DFS may reach dangling sysincl mappings).
//
// Callers must NOT pass a $(B)/ path — generated outputs are read via
// the CodegenRegistry. forEachResolvedChild enforces this dispatch.
//
// Dispatches on file extension: .asm/.asi use parseYasmIncludes
// (`%include`); everything else uses parseCIncludes. Both produce the
// same includeDirective shape and feed the same resolver pipeline.
func (s *IncludeScanner) parseIncludes(vfsPath VFS) []includeDirective {
	// Parsed cache is shared between target and host scanners via s.pc.
	if cached, ok := s.pc.parsed.Get(vfsPath); ok {
		return cached
	}

	// Any non-SOURCE path here is a bug (a $(B)/ should have
	// been dispatched to the registry by forEachResolvedChild).
	fsPath := s.sourceRootSlash + vfsPath.Rel

	data, err := os.ReadFile(fsPath)

	if err != nil {
		s.pc.parsed.Set(vfsPath, nil)

		return nil
	}

	var out []includeDirective

	if isYasmLike(vfsPath) {
		out = parseYasmIncludes(data)
	} else if isAntlrGrammar(vfsPath) {
		// .g4 grammars carry C++ snippets with `#include` lines
		// resolved by the generated .cpp/.h pair, not by direct
		// compilation. The grammar joins the closure as an
		// EmitsIncludes input-dep edge on the generated headers; its
		// own `#include` lines are not real C directives.
		out = nil
	} else {
		out = parseCIncludes(data)
		// Inject synthetic angle-includes for macroIndirectIncludes
		// entries (e.g. openssl's `#include OPENSSL_UNISTD` → unistd.h).
		// The text-blind parser cannot expand the macro; the upstream
		// scanner sees the post-expansion target.
		if extras, ok := macroIndirectIncludes[vfsPath.Rel]; ok {
			for _, m := range extras {
				out = append(out, includeDirective{kind: m.kind, target: m.target})
			}
		}
	}

	s.pc.parsed.Set(vfsPath, out)

	return out
}

// parseCIncludes extracts C/C++ `#include` / `#include_next` directives
// from `data`. stripComments runs first so the regex never matches
// include text inside non-code spans.
func parseCIncludes(data []byte) []includeDirective {
	data = stripComments(data)

	out := make([]includeDirective, 0, 8)

	eachLine(data, func(line []byte) {
		// Short-circuit lines without `#` before the regex.
		// stripComments fills block-comment regions with spaces, and
		// the `^\s*#` anchor would otherwise greedily match leading
		// whitespace, multiplying regex cost ~3× on the M2 closure.
		if bytes.IndexByte(line, '#') < 0 {
			return
		}

		// FindSubmatchIndex returns offsets in a stack-cap'd []int (no
		// alloc for tiny matches); the [][]byte form allocates a slice
		// header per call.
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

	return out
}

// parseYasmIncludes extracts NASM/yasm `%include` directives from
// `data`. Token matches case-insensitively; yasm's `;` line comments
// are not stripped (the anchor cannot fire from a comment line, and
// yasm has no C-style block comments). String literals are preserved
// verbatim — the directive's quoted form IS a string literal at lexer
// level. Result uses includeDirective with next=false (no
// `%include_next` exists in NASM).
func parseYasmIncludes(data []byte) []includeDirective {
	out := make([]includeDirective, 0, 4)

	eachLine(data, func(line []byte) {
		// Short-circuit lines without `%` before invoking the regex
		// engine — most yasm source lines are instruction mnemonics
		// or labels that never start with `%`.
		if bytes.IndexByte(line, '%') < 0 {
			return
		}

		m := yasmIncludeRe.FindSubmatchIndex(line)

		if m == nil {
			return
		}

		// Discriminate kind by bracket character. Practice is always
		// quoted; angle-bracket branch is kept for C-scanner parity.
		kind := includeSystem

		idx := indexOfAngleOrQuote(line)
		if idx >= 0 && line[idx] == '"' {
			kind = includeQuoted
		}

		// m[2:4] are the target capture's byte offsets (the regex has
		// only one capture group; m[0:2] is the full match span).
		target := string(line[m[2]:m[3]])

		out = append(out, includeDirective{kind: kind, next: false, target: target})
	})

	return out
}

// isYasmLike returns true when `absPath` ends with `.asm` or `.asi`
// — the NASM/yasm assembly source extensions. `.S`/`.s` files use
// GAS / AT&T syntax with C preprocessor `#include` directives and
// continue to use the C-include scanner path.
// isAntlrGrammar returns true when `absPath` ends with `.g4` — an
// ANTLR4 grammar source. The grammar contains C++ snippet sections
// with `#include` directives that are emitted into the generated
// .cpp/.h, not compiled in place.
func isAntlrGrammar(absPath VFS) bool {
	return strings.HasSuffix(string(absPath.Rel), ".g4")
}

func isYasmLike(absPath VFS) bool {
	rel := absPath.Rel
	idx := strings.LastIndexByte(rel, '.')

	if idx < 0 {
		return false
	}

	ext := rel[idx:]

	switch ext {
	case ".asm", ".asi":
		return true
	}

	return false
}

// resolve returns the absolute paths the directive resolves to, in
// declaration order, deduplicated. Memoised via resolveCache.
//
// Two-tier semantics from upstream ymake:
//
//   - Search-path candidates (samedir, own AddIncl, peer-GLOBAL, base)
//     are FIRST-MATCH-WINS, mirroring the compiler's `-I` precedence.
//   - Angle-bracket includes (`#include <X>`): every matching sysincl
//     record's paths are UNIONED on top of the search-path result.
//     Example: `<stddef.h>` from a non-musl C source legitimately
//     resolves to both libcxx/include/stddef.h and
//     musl/include/stddef.h.
//   - Quoted includes (`#include "X"`): sysincl is GATED by search-
//     path resolution and tier:
//     (a) Same-directory hit → sysincl ALWAYS suppressed
//     (`#include "elf.h"` from yasm/ targets yasm/elf.h, not
//     musl/include/elf.h).
//     (b) ADDINCL/peer/base hit + single-target sysincl → suppressed.
//     (c) ADDINCL/peer/base hit + multi-target sysincl (≥ 2 non-
//     empty paths) → sysincl IS added on top. Example:
//     `#include "cxxabi.h"` from libcxxabi-parts resolves
//     locally via OwnAddIncl but the reference also expects
//     libcxxrt/include/cxxabi.h from sysincl.
func (sc *scanCtx) resolve(includerAbs VFS, d includeDirective) (out []VFS) {
	s := sc.scanner
	ctx := &sc.cfg

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
	includerRel := includerAbs.Rel
	var mappings []VFS
	var hasMultiTarget bool
	mappings, hasMultiTarget, sysinclClaimed = s.sysinclLookup(ctx.SourceRel, includerRel, d.target)

	// Quoted-include gate. For quoted includes with at least one local
	// hit, sysincl is suppressed when:
	//   1. Same-directory hit (always, regardless of multi-target).
	//   2. ADDINCL/peer/base hit AND sysincl is single-target.
	// Bypass: ADDINCL/peer/base hit AND sysincl is multi-target —
	// e.g. `#include "cxxabi.h"` from libcxxabi-parts unions both
	// libcxxabi/include/cxxabi.h and libcxxrt/include/cxxabi.h.
	if d.kind == includeQuoted && len(searchOut) > 0 {
		// Single-target → bypass unconditionally (dominates sysincl).
		// Compute sameDirRel lazily; the concat would otherwise fire
		// ~2.4M times per M3 gen.
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
	hasMultiTarget = srcMT || incMT
	claimed = srcClaimed || incClaimed

	if len(srcMappings) == 0 {
		return incMappings, hasMultiTarget, claimed
	}

	if len(incMappings) == 0 {
		return srcMappings, hasMultiTarget, claimed
	}

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

	return out, hasMultiTarget, claimed
}

func (s *IncludeScanner) sysinclSourceLookup(sourceRel, target string) ([]VFS, bool, bool) {
	classID, view := s.sourceClass(sourceRel)
	key := sysinclSourceKey{
		sourceClass: classID,
		target:      s.interner.internString(target),
	}

	// PR-34n: lock removed (single-goroutine).
	if cached, ok := s.sysinclSourceCache[key]; ok {
		return cached.paths, cached.hasMultiTarget, cached.claimed
	}

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

	// PR-34n: lock removed (single-goroutine).
	if cached, ok := s.sysinclIncluderCache[key]; ok {
		return cached.paths, cached.hasMultiTarget, cached.claimed
	}

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

// resolveSearchPath returns the search-path-only resolved set for the
// given directive. Cached on the scanCtx by (includer, target, kind,
// next) — ctxHash is implicit in the scanCtx receiver.
func (sc *scanCtx) resolveSearchPath(includerAbs VFS, d includeDirective) []VFS {
	s := sc.scanner
	ctx := &sc.cfg
	key := resolveInnerKey{
		includer: s.interner.internVFS(includerAbs),
		target:   s.interner.internString(d.target),
		flags:    packResolveFlags(d.kind, d.next),
	}

	// PR-34n: lock removed (single-goroutine).
	if cached, ok := sc.resolveCache[key]; ok {
		return cached
	}

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

	addInclPath := func(prefix VFS, target string) bool {
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

		panic("resolveSearchPath: zero-valued search path")
	}

	// First-match-wins across the search path. Order:
	//   1. quoted-form: same directory as the includer
	//   2. module's own ADDINCL
	//   3. peer-propagated GLOBAL ADDINCL
	//   4. baseline (linux-headers, musl arch when applicable)
	searchPathFound := false

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
		for _, p := range ctx.OwnAddIncl {
			if addInclPath(p, d.target) {
				searchPathFound = true

				break
			}
		}
	}

	if !searchPathFound {
		for _, p := range ctx.PeerAddInclSet {
			if addInclPath(p, d.target) {
				searchPathFound = true

				break
			}
		}
	}

	if !searchPathFound {
		for _, p := range ctx.BaseSearchPaths {
			if addInclPath(p, d.target) {
				searchPathFound = true

				break
			}
		}
	}

	// VFS fallback tier — consult fallbackLocators (codegen registry)
	// only when every on-disk search-path candidate missed. Generated
	// files (.pb.h, _serialized.h, .ev.pb.h, ...) don't exist on disk
	// at gen time. Locator is queried with the canonical $(B)/<target>
	// form: consumer #includes use the full BUILD_ROOT-relative path,
	// never basename-only. On-disk wins over VFS — preserves M2 byte-
	// exactness (M2's closure contains no generated-file includes).
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

	// `clear()` drops every key without releasing the bucket allocation
	// — next caller starts with empty-but-prewarmed state.
	clear(seen)
	s.seenPool.Put(seenP)

	// PR-34n: lock removed (single-goroutine).
	sc.resolveCache[key] = out

	return out
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

// fileExists is a cached wrapper around os.Stat. Returns true for
// regular files only. Parameter must be $(S)/-rooted — $(B)/ paths
// belong to the codegen registry tier. Cache key is the rel-form tail,
// unified with fileExistsByRel so hot callers (resolveSearchPath, ~4.7M
// calls) skip the `$(S)/` concat.
func (s *IncludeScanner) fileExists(vfsPath VFS) bool {
	return s.fileExistsByRel(vfsPath.Rel)
}

// fileExistsByRel is the inner, rel-keyed existence check.
func (s *IncludeScanner) fileExistsByRel(rel string) bool {
	// Exists cache is shared between target and host scanners via s.pc.
	if cached, ok := s.pc.exists[rel]; ok {
		return cached
	}

	info, err := os.Stat(s.sourceRootSlash + rel)
	val := err == nil && !info.IsDir()

	s.pc.exists[rel] = val

	return val
}

// eachLine invokes `fn` for every newline-terminated record in `data`,
// passing a sub-slice (no per-line alloc). Trailing `\r` stripped.
// The callback must not retain the slice past invocation.
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
// or -1 when neither is present. Inlines.
func indexOfAngleOrQuote(b []byte) int {
	for i := 0; i < len(b); i++ {
		c := b[i]

		if c == '<' || c == '"' {
			return i
		}
	}

	return -1
}

// stripComments rewrites C/C++ source bytes so the include-directive
// regex never matches text inside non-code regions. Block and line
// comments are replaced with spaces; newlines preserved so per-line
// `^\s*#` anchoring continues to address the same lines.
//
// String and char literals are RECOGNISED but not stripped: bytes are
// walked so a `/*` or `//` inside a string body cannot enter comment
// state, but the bytes themselves stay unchanged. This matters because
// `#include "header.h"` IS a string literal at lexer level — stripping
// its payload would erase every quoted include.
//
// Raw string literals (`R"delim(...)delim"`) ARE walked transparently
// AND have their bodies blanked: protoc's `R"(#include "$path$")"`
// codegen templates would otherwise expose fake `#include` lines.
//
// Mutates `data` in place; the buffer comes from os.ReadFile and is
// not retained past parseIncludes. The state machine is intentionally
// simple: no trigraphs, no line-continuation backslash splicing, no
// alternative tokens (`%:include`).
func stripComments(data []byte) []byte {
	// Fast pre-scan: no `/`, `"`, or `'` → no comment or string state.
	hasTrigger := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if c == '/' || c == '"' || c == '\'' {
			hasTrigger = true

			break
		}
	}

	if !hasTrigger {
		return data
	}

	n := len(data)
	i := 0
	atLineStart := true

	for i < n {
		c := data[i]

		if atLineStart {
			if next, ok := scanIncludeDirectiveTarget(data, i); ok {
				i = next
				atLineStart = false
				continue
			}
		}

		// Line comment: `//` runs to end of line. Newline is preserved.
		if c == '/' && i+1 < n && data[i+1] == '/' {
			data[i] = ' '
			data[i+1] = ' '
			i += 2

			for i < n && data[i] != '\n' {
				data[i] = ' '
				i++
			}
			atLineStart = true

			continue
		}

		// Block comment: `/* ... */`. Newlines inside are preserved so
		// per-line addressing through the comment span keeps lining up.
		if c == '/' && i+1 < n && data[i+1] == '*' {
			data[i] = ' '
			data[i+1] = ' '
			i += 2

			for i < n {
				if i+1 < n && data[i] == '*' && data[i+1] == '/' {
					data[i] = ' '
					data[i+1] = ' '
					i += 2

					break
				}

				if data[i] != '\n' {
					data[i] = ' '
				}

				i++
			}
			atLineStart = true

			continue
		}

		// Raw string literal: `R"delim(...)delim"`. The body may
		// contain unescaped `"`, `\`, `/*`, `//` — must be recognised
		// to keep comment state out. Only recognise `R"` when the
		// previous byte is not an identifier byte (`myR"..."` is a
		// trailing letter, not a raw-string prefix).
		if c == 'R' && i+1 < n && data[i+1] == '"' && !isIdentByte(prevByte(data, i)) {
			// Read delimiter between `R"` and `(`.
			delimStart := i + 2
			j := delimStart

			for j < n && data[j] != '(' && data[j] != '\n' && j-delimStart < 16 {
				j++
			}

			if j >= n || data[j] != '(' {
				// Malformed (or newline before `(`) — treat `R` as
				// ordinary identifier and fall through.
				i++

				continue
			}

			// Capture delim independently — shields the close-token
			// match from future changes to the transparency policy.
			delim := make([]byte, j-delimStart)
			copy(delim, data[delimStart:j])

			i = j + 1

			// Walk to `)delim"`, blanking the body (non-newline bytes
			// replaced with space; newlines preserved). Raw strings can
			// contain `#include "X"` lines that would otherwise match
			// the directive regex anchored on `^\s*#include`.
			for i < n {
				if data[i] == ')' && i+1+len(delim)+1 <= n {
					match := true

					for k, b := range delim {
						if data[i+1+k] != b {
							match = false

							break
						}
					}

					if match && data[i+1+len(delim)] == '"' {
						for k := 0; k <= len(delim); k++ {
							data[i+k] = ' '
						}
						data[i+1+len(delim)] = ' '
						i += 1 + len(delim) + 1

						break
					}
				}

				if data[i] != '\n' {
					data[i] = ' '
				}
				i++
			}

			continue
		}

		// Double-quoted string: walk past so `/*` or `//` inside cannot
		// enter comment state. Bytes NOT modified.
		if c == '"' {
			i++

			for i < n {
				if data[i] == '\\' && i+1 < n && data[i+1] != '\n' {
					i += 2

					continue
				}

				if data[i] == '"' {
					i++

					break
				}

				if data[i] == '\n' {
					// Unterminated string at EOL — bail out so the
					// next line resets to code state; matches the C
					// compiler.
					break
				}

				i++
			}

			continue
		}

		// Single-quoted char literal: same handling as double-quoted.
		if c == '\'' {
			i++

			for i < n {
				if data[i] == '\\' && i+1 < n && data[i+1] != '\n' {
					i += 2

					continue
				}

				if data[i] == '\'' {
					i++

					break
				}

				if data[i] == '\n' {
					break
				}

				i++
			}

			continue
		}

		i++
		atLineStart = c == '\n'
	}

	return data
}

func scanIncludeDirectiveTarget(data []byte, i int) (int, bool) {
	n := len(data)
	j := i
	for j < n {
		switch data[j] {
		case ' ', '\t':
			j++
		default:
			goto nonSpace
		}
	}
	return 0, false

nonSpace:
	if data[j] != '#' {
		return 0, false
	}
	j++
	for j < n {
		switch data[j] {
		case ' ', '\t':
			j++
		default:
			goto directive
		}
	}
	return 0, false

directive:
	switch {
	case bytes.HasPrefix(data[j:], []byte("include_next")):
		j += len("include_next")
	case bytes.HasPrefix(data[j:], []byte("include")):
		j += len("include")
	default:
		return 0, false
	}
	for j < n {
		switch data[j] {
		case ' ', '\t':
			j++
		default:
			goto target
		}
	}
	return 0, false

target:
	if j >= n {
		return 0, false
	}
	var close byte
	switch data[j] {
	case '<':
		close = '>'
	case '"':
		close = '"'
	default:
		return 0, false
	}
	j++
	for j < n {
		if data[j] == '\\' && close == '"' && j+1 < n && data[j+1] != '\n' {
			j += 2
			continue
		}
		if data[j] == close {
			return j + 1, true
		}
		if data[j] == '\n' {
			return 0, false
		}
		j++
	}
	return 0, false
}

// prevByte returns the byte immediately before index `i` in `data`, or
// 0 when `i == 0`. Used by stripComments to discriminate token-starting
// `R` (the C++11 raw-string-literal prefix) from a trailing identifier
// letter.
func prevByte(data []byte, i int) byte {
	if i == 0 {
		return 0
	}

	return data[i-1]
}

// isIdentByte reports whether `b` is part of a C/C++ identifier
// (`[A-Za-z0-9_]`). stripComments uses it to recognise that an `R"`
// preceded by an identifier byte is NOT the raw-string-literal prefix
// but the trailing letter of a longer identifier.
func isIdentByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}
