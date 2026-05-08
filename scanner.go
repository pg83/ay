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
	"os"
	"regexp"
	"strings"
	"sync"
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
// All caches are protected by a mutex so the scanner is safe to
// invoke from concurrent walkers (PR-31's walker is single-threaded
// but PR-32+ may parallelise per-source).
type IncludeScanner struct {
	sysincl    SysInclSet
	sourceRoot string

	mu           sync.Mutex
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
	s := &IncludeScanner{
		sysincl:              sysincl,
		sourceRoot:           sourceRoot,
		parsed:               make(map[string][]includeDirective),
		exists:               make(map[string]bool, 4096),
		resolveCache:         make(map[resolveKey][]string, 4096),
		viewCache:            make(map[string]PerSourceView, 256),
		sysinclSourceCache:   make(map[sysinclSourceKey][]string, 4096),
		sysinclIncluderCache: make(map[sysinclIncluderKey][]string, 4096),
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
	srcAbs := s.sourceRoot + "/" + ctx.SourceRel
	ctxHash := hashScanContext(&ctx)

	visitedP := s.visitedPool.Get().(*map[string]struct{})
	orderP := s.orderPool.Get().(*[]string)

	visited := *visitedP
	order := (*orderP)[:0]

	s.dfs(srcAbs, &ctx, ctxHash, visited, &order)

	prefix := s.sourceRoot + "/"
	out := make([]string, 0, len(order))

	for _, abs := range order {
		// Skip the source itself; only headers are emitted.
		if abs == srcAbs {
			continue
		}

		rel := strings.TrimPrefix(abs, prefix)
		out = append(out, "$(SOURCE_ROOT)/"+rel)
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

	return out
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

// dfs walks the include closure in depth-first discovery order.
func (s *IncludeScanner) dfs(absPath string, ctx *ScanContext, ctxHash uint64, visited map[string]struct{}, order *[]string) {
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

// parseIncludes returns the parsed include directives for the file at
// `absPath`. Memoized per absolute path. Returns nil for files that
// do not exist (the caller's resolver dropped them already, but DFS
// may also reach a dangling path through a sysincl mapping that
// names a file the tree does not have).
func (s *IncludeScanner) parseIncludes(absPath string) []includeDirective {
	s.mu.Lock()
	cached, ok := s.parsed[absPath]
	s.mu.Unlock()

	if ok {
		return cached
	}

	data, err := os.ReadFile(absPath)

	if err != nil {
		s.mu.Lock()
		s.parsed[absPath] = nil
		s.mu.Unlock()

		return nil
	}

	out := make([]includeDirective, 0, 8)

	for _, line := range splitLinesNoAlloc(data) {
		m := includeRe.FindSubmatch(line)

		if m == nil {
			continue
		}

		// Determine kind by inspecting the line's bracket character
		// after the keyword. `[<"]` capture is not exposed by
		// FindSubmatch, so we re-find the bracket position.
		kind := includeSystem
		// Find the first `<` or `"` after the keyword in the line.
		idx := indexOfAny(line, []byte{'<', '"'})

		if idx >= 0 && line[idx] == '"' {
			kind = includeQuoted
		}

		next := string(m[1]) == "include_next"

		out = append(out, includeDirective{kind: kind, next: next, target: string(m[2])})
	}

	s.mu.Lock()
	s.parsed[absPath] = out
	s.mu.Unlock()

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
	includerRel := strings.TrimPrefix(includerAbs, s.sourceRoot+"/")
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

	s.mu.Lock()
	cached, ok := s.sysinclSourceCache[key]
	s.mu.Unlock()

	if ok {
		return cached
	}

	view := s.perSourceView(sourceRel)
	rels, _ := view.LookupSourceKeyed(target)

	mappings := s.absifyRels(rels)

	s.mu.Lock()
	s.sysinclSourceCache[key] = mappings
	s.mu.Unlock()

	return mappings
}

func (s *IncludeScanner) sysinclIncluderLookup(includerRel, target string) []string {
	key := sysinclIncluderKey{includerRel: includerRel, target: target}

	s.mu.Lock()
	cached, ok := s.sysinclIncluderCache[key]
	s.mu.Unlock()

	if ok {
		return cached
	}

	// PerSourceView's includerKeyed slice is identical regardless of
	// which source it was prepared for (every view derives it from the
	// same SysInclSet). Use the prepared anySrcView (initialised once
	// at NewIncludeScanner) to access the includer-keyed records
	// without going through perSourceView's per-call lock.
	rels, _ := s.anySrcView.LookupIncluderKeyed(includerRel, target)

	mappings := s.absifyRels(rels)

	s.mu.Lock()
	s.sysinclIncluderCache[key] = mappings
	s.mu.Unlock()

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

	prefix := s.sourceRoot + "/"
	out := make([]string, 0, len(rels))

	for _, rel := range rels {
		out = append(out, prefix+normalisePath(rel))
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
	s.mu.Lock()
	cached, ok := s.viewCache[sourceRel]
	s.mu.Unlock()

	if ok {
		return cached
	}

	view := s.sysincl.PreparePerSource(sourceRel)

	s.mu.Lock()
	s.viewCache[sourceRel] = view
	s.mu.Unlock()

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

	s.mu.Lock()
	cached, ok := s.resolveCache[key]
	s.mu.Unlock()

	if ok {
		return cached
	}

	var (
		out  []string
		seen = map[string]struct{}{}
	)

	addPath := func(rel string) bool {
		// Normalize `..`/`.` segments so paths like
		// `musl/src/include/../../include/features.h` collapse to
		// `musl/include/features.h`. Empirical observation: the
		// upstream scanner emits the canonical path.
		rel = normalisePath(rel)

		if _, dup := seen[rel]; dup {
			return false
		}

		abs := s.sourceRoot + "/" + rel

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
		incRel := strings.TrimPrefix(includerAbs, s.sourceRoot+"/")
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

	s.mu.Lock()
	s.resolveCache[key] = out
	s.mu.Unlock()

	return out
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
	s.mu.Lock()
	cached, ok := s.exists[absPath]
	s.mu.Unlock()

	if ok {
		return cached
	}

	info, err := os.Stat(absPath)
	val := err == nil && !info.IsDir()

	s.mu.Lock()
	s.exists[absPath] = val
	s.mu.Unlock()

	return val
}

// splitLinesNoAlloc walks `data` returning successive lines as
// byte-slices into the same backing array — no per-line allocation.
// Caller must not mutate the returned slices.
func splitLinesNoAlloc(data []byte) [][]byte {
	out := make([][]byte, 0, 64)
	start := 0

	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			line := data[start:i]
			// Strip optional trailing CR.
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}

			out = append(out, line)
			start = i + 1
		}
	}

	if start < len(data) {
		out = append(out, data[start:])
	}

	return out
}

// indexOfAny returns the index of the first occurrence of any byte
// in `chars` within `b`, or -1 when none found.
func indexOfAny(b []byte, chars []byte) int {
	for i := 0; i < len(b); i++ {
		for _, c := range chars {
			if b[i] == c {
				return i
			}
		}
	}

	return -1
}
