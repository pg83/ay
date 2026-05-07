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
// `#include_next` is treated as system for resolution purposes (the
// distinction matters only for which directory in the search path
// the next match resumes from; since L2 is multiset-based and our
// scanner does the union of all matches anyway, the kind suffices).
type includeKind int

const (
	includeSystem includeKind = iota
	includeQuoted
)

// includeDirective is one parsed `#include` from a source file.
type includeDirective struct {
	kind   includeKind
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
}

type resolveKey struct {
	ctxHash  uint64
	includer string
	target   string
	kind     includeKind
}

// NewIncludeScanner constructs a scanner bound to a SysInclSet and a
// source-root absolute path.
func NewIncludeScanner(sourceRoot string, sysincl SysInclSet) *IncludeScanner {
	return &IncludeScanner{
		sysincl:      sysincl,
		sourceRoot:   sourceRoot,
		parsed:       make(map[string][]includeDirective),
		exists:       make(map[string]bool, 4096),
		resolveCache: make(map[resolveKey][]string, 4096),
	}
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
func (s *IncludeScanner) WalkClosure(ctx ScanContext) []string {
	srcAbs := s.sourceRoot + "/" + ctx.SourceRel
	ctxHash := hashScanContext(&ctx)

	visited := make(map[string]struct{}, 64)
	order := make([]string, 0, 64)

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

	return out
}

// hashScanContext is an FNV-1a hash over the context fields the
// resolve cache keys on (OwnAddIncl + PeerAddInclSet + BaseSearchPaths).
// SourceRel is intentionally NOT part of the hash because it does not
// affect resolve() behaviour beyond the includer-rel sysincl key (which
// IS part of the resolveKey itself). Two CCs in the same module with
// different sources but the same ADDINCL/peer-GLOBAL/Base sets share
// the same ctxHash and reuse cached resolves for shared transitive
// includers.
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

		out = append(out, includeDirective{kind: kind, target: string(m[2])})
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
	key := resolveKey{
		ctxHash:  ctxHash,
		includer: includerAbs,
		target:   d.target,
		kind:     d.kind,
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

	// Sysincl: add EVERY matching record's contribution on top of
	// the search-path result. The source_filter key is the IMMEDIATE
	// includer's relative path (not the compile-unit source path) —
	// empirically required by the reference graph: glibcasm-mapping
	// records (filter `^contrib/libs/glibcasm`) fire when a musl
	// header transitively reaches a glibcasm-based includer in the
	// closure, which would not happen with a compile-unit-keyed
	// match. PR-33 D05 attempted ctx.SourceRel as the key but lost
	// 125 musl CC nodes' glibcasm closure (regressed L2 from 83.94%
	// to 79.60%). Both keys give wrong answers for some axis: the
	// per-includer key over-fans-out for stl-to-libcxx on
	// non-yasm/non-musl headers reached via a yasm chain
	// (libc_compat/reallocarray/stdlib.h is the canonical case).
	// Resolution lives in a sysincl follow-up that gates per-record
	// by source-class (the upstream's actual mechanism — recorded as
	// a PR-33 deferred follow-up defect).
	includerRel := strings.TrimPrefix(includerAbs, s.sourceRoot+"/")
	mappings, _ := s.sysincl.Lookup(includerRel, d.target)

	for _, p := range mappings {
		addPath(p)
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
