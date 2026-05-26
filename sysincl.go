package main

// sysincl.go — loader for build/sysincl/*.yml. Maps `#include <H>` to
// zero or more SOURCE_ROOT-relative paths emitted as additional inputs.
//
// Runtime YAML parse: documented exception to the "hand-translate
// build/conf" rule — 53 files / ~11K lines of pure data would drift
// on every upstream resync. Hand-rolled parser THROWS on unrecognised
// constructs so upstream evolution surfaces loudly.
//
// Negative-lookahead regexes (`(?!FOO)`) common upstream are not
// supported by Go's RE2: translated into a custom matcher (positive
// prefix AND none of the exclusion prefixes). Shapes outside the
// recognised subset throw at load time.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// SysIncl is one record from a sysincl YAML file. Mappings are
// header → list of SOURCE_ROOT-relative resolved paths. Empty list (or
// empty-string element) marks the header sysincl-known but emitting
// no input — resolution stops without recursing.
//
// `KeyBySource`: filters with negative lookahead (`(?!...)`) key by
// compile-unit source so libcxx/musl replacement headers fire only on
// non-musl/non-yasm consumers. Filters without negative lookahead key
// by immediate includer so records like `^contrib/libs/glibcasm` fire
// when a musl-source's includer chain reaches a glibcasm header.
//
// `HasMultiTarget` is true when ≥1 header in Mappings fans out to ≥2
// distinct non-empty paths. Used by scanner.go's resolveDirective to
// decide whether the quoted-include gate yields to sysincl.
type SysIncl struct {
	Filter         *sourceFilter
	KeyBySource    bool
	HasMultiTarget bool
	// CaseInsensitive marks records loaded with `case_sensitive: false`
	// (windows.yml). Keys are stored lower-cased at parse time, and
	// Lookup lower-cases the query header before probing this record's
	// Mappings. Other records (CaseInsensitive=false) probe verbatim.
	CaseInsensitive bool
	Mappings        map[string][]string
}

// SysInclSet is the union of all sysincl records loaded from
// build/sysincl/*.yml.
type SysInclSet []SysIncl

// recordKey returns the case-folded form of `k` for a record's
// Mappings map. Records loaded with `case_sensitive: false` store
// lower-cased keys; all others store verbatim.
func recordKey(rec *SysIncl, k string) string {
	if rec.CaseInsensitive {
		return strings.ToLower(k)
	}
	return k
}

// recordQuery is recordKey's read-side twin: folds the query header
// to match the record's storage convention.
func recordQuery(rec *SysIncl, header string) string {
	if rec.CaseInsensitive {
		return strings.ToLower(header)
	}
	return header
}

// Lookup returns the union of resolved paths for `header` across every
// record whose filter matches the appropriate path key. ymake stacks
// YAML files: each record contributes its mapping; consumers see ALL
// contributions (e.g. `<stddef.h>` from a non-musl C source resolves
// to BOTH libcxx/include/stddef.h AND musl/include/stddef.h).
//
// Bare-key entries and explicit `key: ""` are suppression markers —
// they no-op the record for that header without short-circuiting
// other records that might still claim it.
//
// Returns (paths, true) when ≥1 record claims (paths may be empty for
// suppression-only claims), or (nil, false) when no record claims.
func (s SysInclSet) Lookup(sourcePath, includerPath, header string) ([]string, bool) {
	view := s.PreparePerSource(sourcePath)

	return view.Lookup(includerPath, header)
}

// PerSourceView is a sysincl set with SOURCE-keyed filters pre-resolved
// against a fixed source path. Source-keyed records have a fixed
// accept-set for one CC's WalkClosure; includer-keyed records still
// need per-call filter match against the includer.
type PerSourceView struct {
	// activeSourceKeyed: source-keyed records whose filter accepted the
	// source — fire on every Lookup against this view.
	activeSourceKeyed []*SysIncl
	// includerKeyed: every KeyBySource=false record; filters tested
	// per Lookup against the includer.
	includerKeyed []*SysIncl
	// includerFilterCache memoises filter-match for a given includerPath.
	// Header-independent; pointer-typed so value-type PerSourceView
	// copies share one map. sync.Map was slower in experiments — many
	// small subsets, frequent reads.
	includerFilterCache *includerFilterCache
}

// includerFilterCache memoises filter-match results across a SysInclSet's
// includer-keyed records. Pointer-typed so multiple PerSourceView
// instances built from the same SysInclSet share the table. RWMutex
// permits concurrent reads from parallel scanner workers.
type includerFilterCache struct {
	mu sync.RWMutex
	// active maps includerPath → subset of includerKeyed records
	// whose filters accepted that path. nil-slice value distinguishes
	// "cached, no records match" from "not cached".
	active map[string][]*SysIncl
}

func newIncluderFilterCache() *includerFilterCache {
	return &includerFilterCache{active: make(map[string][]*SysIncl, 64)}
}

// PreparePerSource returns a Lookup view with SOURCE-keyed filters
// pre-resolved against the given source path. Safe to reuse for every
// Lookup call within one WalkClosure. A fresh includerFilterCache is
// allocated per view.
func (s SysInclSet) PreparePerSource(sourcePath string) PerSourceView {
	view := PerSourceView{
		activeSourceKeyed:   make([]*SysIncl, 0, len(s)),
		includerKeyed:       make([]*SysIncl, 0, len(s)),
		includerFilterCache: newIncluderFilterCache(),
	}

	for i := range s {
		rec := &s[i]

		if rec.KeyBySource {
			if rec.Filter == nil || rec.Filter.match(sourcePath) {
				view.activeSourceKeyed = append(view.activeSourceKeyed, rec)
			}

			continue
		}

		view.includerKeyed = append(view.includerKeyed, rec)
	}

	return view
}

// Lookup returns the union of resolved paths for `header` across every
// record applicable to the given includer. SOURCE-keyed records were
// pre-filtered when the view was constructed; INCLUDER-keyed records
// are filter-checked here.
func (v PerSourceView) Lookup(includerPath, header string) ([]string, bool) {
	srcOut, srcFound, _ := v.LookupSourceKeyed(header)
	incOut, incFound, _ := v.LookupIncluderKeyed(includerPath, header)

	if !srcFound && !incFound {
		return nil, false
	}

	if len(srcOut) == 0 {
		return incOut, true
	}

	if len(incOut) == 0 {
		return srcOut, true
	}

	out := make([]string, 0, len(srcOut)+len(incOut))
	seen := make(map[string]struct{}, len(srcOut)+len(incOut))

	for _, p := range srcOut {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		out = append(out, p)
	}

	for _, p := range incOut {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		out = append(out, p)
	}

	return out, true
}

// LookupSourceKeyed returns the union of paths contributed by the
// view's active source-keyed records. Source-dependent but
// includer-INdependent (filters already satisfied at view construction).
//
// `hasMultiTarget` is true when any contributing record has
// HasMultiTarget=true AND maps `header` to ≥2 non-empty paths. Used
// by scanner.go's resolveDirective to gate the quoted-include path.
func (v PerSourceView) LookupSourceKeyed(header string) ([]string, bool, bool) {
	var (
		out            []string
		found          bool
		hasMultiTarget bool
		seen           map[string]struct{}
	)

	for _, rec := range v.activeSourceKeyed {
		paths, ok := rec.Mappings[recordQuery(rec, header)]

		if !ok {
			continue
		}

		found = true

		if rec.HasMultiTarget {
			count := 0

			for _, p := range paths {
				if p != "" {
					count++
				}
			}

			if count >= 2 {
				hasMultiTarget = true
			}
		}

		for _, p := range paths {
			if p == "" {
				continue
			}

			if seen == nil {
				seen = make(map[string]struct{}, 4)
			}

			if _, dup := seen[p]; dup {
				continue
			}

			seen[p] = struct{}{}
			out = append(out, p)
		}
	}

	return out, found, hasMultiTarget
}

// LookupIncluderKeyed returns the union of paths contributed by the
// view's includer-keyed records (source-INdependent).
//
// Filter-match (`rec.Filter.match(includerPath)`) is memoised via
// includerFilterCache: many (includerPath, header) probes share the
// same includerPath, so the linear filter walk runs once per unique
// includerPath; per-header probes iterate the accepting subset.
//
// `hasMultiTarget` is true when any contributing record has
// HasMultiTarget=true AND maps `header` to ≥2 non-empty paths.
func (v PerSourceView) LookupIncluderKeyed(includerPath, header string) ([]string, bool, bool) {
	return unionIncluderMappings(v.activeIncluderRecords(includerPath), header)
}

// unionIncluderMappings unions the paths every record in `active` maps `header`
// to (deduped, suppression-markers dropped). The result is a pure function of
// (active, header) — which is exactly why the scanner keys its cache on the
// active set's equivalence class rather than the includer path.
func unionIncluderMappings(active []*SysIncl, header string) ([]string, bool, bool) {
	var (
		out            []string
		found          bool
		hasMultiTarget bool
		seen           map[string]struct{}
	)

	for _, rec := range active {
		paths, ok := rec.Mappings[recordQuery(rec, header)]

		if !ok {
			continue
		}

		found = true

		if rec.HasMultiTarget {
			count := 0

			for _, p := range paths {
				if p != "" {
					count++
				}
			}

			if count >= 2 {
				hasMultiTarget = true
			}
		}

		for _, p := range paths {
			if p == "" {
				continue
			}

			if seen == nil {
				seen = make(map[string]struct{}, 4)
			}

			if _, dup := seen[p]; dup {
				continue
			}

			seen[p] = struct{}{}
			out = append(out, p)
		}
	}

	return out, found, hasMultiTarget
}

// activeIncluderRecords returns the subset of `v.includerKeyed` whose
// filters accept `includerPath`. Memoised on includerFilterCache;
// header-INdependent — every header probe sharing the same includer
// pays one map probe instead of a fresh ~25-record filter walk.
func (v PerSourceView) activeIncluderRecords(includerPath string) []*SysIncl {
	if v.includerFilterCache == nil {
		// Defensive: a hand-constructed PerSourceView without the
		// cache field still works correctly, just without memo.
		return v.computeActiveIncluderRecords(includerPath)
	}

	c := v.includerFilterCache

	c.mu.RLock()
	cached, ok := c.active[includerPath]
	c.mu.RUnlock()

	if ok {
		return cached
	}

	active := v.computeActiveIncluderRecords(includerPath)

	c.mu.Lock()
	// Re-check after the upgrade: a concurrent reader may have populated
	// the entry between our RUnlock and Lock. First-writer-wins keeps
	// the slice identity stable for any reader that already saw it.
	if existing, dup := c.active[includerPath]; dup {
		c.mu.Unlock()

		return existing
	}

	c.active[includerPath] = active
	c.mu.Unlock()

	return active
}

// computeActiveIncluderRecords walks v.includerKeyed once, returning
// the records whose filters accept includerPath. Returns nil when none
// match (distinct from an unset map entry — the cache stores the nil
// slice so a "no match" probe also takes the fast path).
func (v PerSourceView) computeActiveIncluderRecords(includerPath string) []*SysIncl {
	var active []*SysIncl

	for _, rec := range v.includerKeyed {
		if rec.Filter != nil && !rec.Filter.match(includerPath) {
			continue
		}

		active = append(active, rec)
	}

	return active
}

// sysInclEnv is the read-only environment passed to sysInclEntry
// predicates. Mirrors the var-bag ymake evaluates `when (...)` blocks
// against in build/conf/sysincl.conf; for now the only varying axis is
// target ISA (used by linux-musl-<arch>.yml selection at conf:53-58).
type sysInclEnv struct {
	arch string
}

// sysInclEntry is one YAML in the load order. nil `predicate` means
// "always include" (mirrors `SYSINCL+=...` outside any `when` block).
type sysInclEntry struct {
	file      string
	predicate func(sysInclEnv) bool
}

func archIs(want string) func(sysInclEnv) bool {
	return func(e sysInclEnv) bool { return e.arch == want }
}

// sysInclYamlSequence mirrors the SYSINCL+= ordering from
// build/conf/sysincl.conf for our build config (Linux, MUSL=yes,
// USE_STL_SYSTEM=no, USE_ARCADIA_COMPILER_RUNTIME=yes, OPENSOURCE=yes).
//
// linux-headers.yml is absent: upstream loads it only when OS_LINUX !=
// "yes". The two `linux-musl-<arch>.yml` entries differ per ISA because
// `bits/*` mappings differ.
var sysInclYamlSequence = []sysInclEntry{
	{file: "macro.yml"},
	{file: "libc-to-compat.yml"},
	{file: "libc-to-nothing.yml"},
	{file: "stl-to-libstdcxx.yml"},
	{file: "stl-to-nothing.yml"},
	{file: "windows.yml"},
	{file: "darwin.yml"},
	{file: "android.yml"},
	{file: "freebsd.yml"},
	{file: "freertos.yml"},
	{file: "intrinsic.yml"},
	{file: "nvidia.yml"},
	{file: "misc.yml"},
	{file: "unsorted.yml"},
	{file: "swig.yml"},
	{file: "libiconv.yml"},
	{file: "libidn.yml"},
	{file: "jdk-to-arcadia.yml"},
	{file: "opensource.yml"},
	{file: "libc-to-musl.yml"},
	{file: "linux-musl-aarch64.yml", predicate: archIs("aarch64")},
	{file: "linux-musl.yml", predicate: archIs("x86_64")},
	{file: "emscripten-to-nothing.yml"},
	{file: "nvidia-cccl.yml"},
	{file: "stl-to-libcxx.yml"},
	{file: "libc-musl-libcxx.yml"},
	// python.conf:244-245 — gated by $OPENSOURCE. Bare-key suppression
	// for `<contrib/tools/python/src/Include/*>` shims in
	// `contrib/libs/python/Include/*.h`'s `#else` (USE_PYTHON3=no) branches.
	{file: "python-2-disable.yml"},
	{file: "python-2-disable-numpy.yml"},
}

var supportedSysInclArchs = map[string]struct{}{
	"aarch64": {},
	"x86_64":  {},
}

// LoadSysInclSetFor walks sysInclYamlSequence for the given ISA,
// loading every YAML whose predicate matches. `arch` must be "aarch64"
// or "x86_64"; other values throw.
//
// When the sysincl directory does not exist (synthetic test trees),
// returns an empty set rather than throwing — the include scanner
// falls back to its AddIncl + peer-GLOBAL search path.
func LoadSysInclSetFor(sourceRoot, arch string, onWarn func(Warn)) SysInclSet {
	return LoadSysInclSetForFS(NewFS(sourceRoot), arch, onWarn)
}

// LoadSysInclSetForFS is the FS-based variant used by production code
// so the directory-listing cache is shared with the rest of the run.
func LoadSysInclSetForFS(fs *FS, arch string, onWarn func(Warn)) SysInclSet {
	if !fs.IsDir("build/sysincl") {
		return nil
	}

	if _, ok := supportedSysInclArchs[arch]; !ok {
		ThrowFmt("LoadSysInclSetFor: unsupported arch %q (want aarch64 or x86_64)", arch)
	}

	env := sysInclEnv{arch: arch}

	var set SysInclSet

	for _, entry := range sysInclYamlSequence {
		if entry.predicate != nil && !entry.predicate(env) {
			continue
		}

		rel := "build/sysincl/" + entry.file
		if !fs.IsFile(rel) {
			continue // optional per-platform YAML absent from this checkout
		}

		records := parseSysInclYAML(entry.file, string(fs.Read(rel)), onWarn)
		set = append(set, records...)
	}

	return set
}

// Suppress unused-import warning when sort is no longer needed; we
// keep the import behind a helper so a future widening that sorts
// supplemental yamls compiles cleanly.
var _ = sort.Strings

// parseSysInclYAML parses one sysincl YAML file. Throws with a precise
// location ("name.yml:LINE: <reason>") on any construct outside the
// documented subset.
func parseSysInclYAML(name, text string, onWarn func(Warn)) []SysIncl {
	lines := strings.Split(text, "\n")

	var (
		out     []SysIncl
		current *SysIncl
		// pendingKey: when we're inside a fan-out list (`- key:` then
		// `  - p1` then `  - p2`), pendingKey holds "key" and we
		// accumulate pendingPaths until the next non-list entry.
		pendingKey   string
		pendingPaths []string
		// inIncludes: true when we're inside the current record's
		// `includes:` list (so subsequent `- ...` lines are mapping
		// entries rather than new top-level records).
		inIncludes bool
	)

	flushPending := func() {
		if pendingKey == "" {
			return
		}

		if current == nil {
			ThrowFmt("sysincl: %s: pending key %q with no active record", name, pendingKey)
		}

		// `- key:` with no list and no values — suppression. E.g.
		// linux-headers.yml's `- asm-generic/auxvec.h` (bare key)
		// with pendingPaths==nil.
		key := recordKey(current, pendingKey)
		if pendingPaths == nil {
			current.Mappings[key] = nil
		} else {
			current.Mappings[key] = pendingPaths
		}

		pendingKey = ""
		pendingPaths = nil
	}

	flushRecord := func() {
		flushPending()

		if current != nil {
			// HasMultiTarget: true when any header mapping has ≥2
			// non-empty resolution targets (fan-out). Used by
			// scanner.go's resolveDirective to bypass the quoted-include
			// gate when the include was resolved via OwnAddIncl.
			for _, paths := range current.Mappings {
				count := 0

				for _, p := range paths {
					if p != "" {
						count++
					}
				}

				if count >= 2 {
					current.HasMultiTarget = true

					break
				}
			}

			out = append(out, *current)
			current = nil
			inIncludes = false
		}
	}

	for i, raw := range lines {
		lineno := i + 1
		// Strip trailing inline comments (only safe outside quoted
		// strings; the upstream YAMLs have no inline `#` inside
		// values, verified across all 53 files).
		stripped := stripComment(raw)
		trimmed := strings.TrimRight(stripped, " \t\r")

		if strings.TrimSpace(trimmed) == "" {
			continue
		}

		indent := leadingSpaces(trimmed)
		body := trimmed[indent:]

		// Top-level entry: `- source_filter: "..."` or `- includes:`.
		if indent == 0 && strings.HasPrefix(body, "- ") {
			flushRecord()

			current = &SysIncl{Mappings: make(map[string][]string)}

			rest := strings.TrimSpace(body[2:])

			handleRecordHeader(name, lineno, rest, current, &inIncludes, onWarn)

			continue
		}

		if current == nil {
			ThrowFmt("sysincl: %s:%d: line outside any record: %q", name, lineno, body)
		}

		// Sub-key at indent ≥ 2: either `source_filter: ...`,
		// `includes:`, or — when inIncludes — a mapping entry.
		if !inIncludes {
			handleRecordHeader(name, lineno, body, current, &inIncludes, onWarn)

			continue
		}

		// Inside `includes:`. Forms: `- key` (suppression),
		// `- key: value` (single), `- key: ""` (suppression),
		// `- key:` (fan-out opener), `- path` (fan-out continuation).
		if !strings.HasPrefix(body, "- ") && body != "-" {
			ThrowFmt("sysincl: %s:%d: expected list entry, got %q", name, lineno, body)
		}

		var entry string

		if body == "-" {
			entry = ""
		} else {
			entry = strings.TrimSpace(body[2:])
		}

		// Fan-out continuation: deeper-indent `- ...`. Disambiguated by
		// pendingKey set AND entry has no `:` outside quoted strings.
		if pendingKey != "" && !strings.Contains(entry, ":") {
			pendingPaths = append(pendingPaths, unquote(entry))

			continue
		}

		// Otherwise this is a new mapping entry — flush any pending one.
		flushPending()

		key, val, hasMapping := splitKeyValue(entry)

		if !hasMapping {
			// Bare key — sysincl-known, no mapping. Stored as nil
			// to signal "consume but emit nothing".
			current.Mappings[recordKey(current, key)] = nil

			continue
		}

		if val == "" {
			// `- key:` opens a fan-out list. pendingKey accumulates.
			pendingKey = key
			pendingPaths = nil

			continue
		}

		// `- key: value` — single mapping. The value "" (after
		// unquote) is suppression: explicit `key: ""`.
		v := unquote(val)

		if v == "" {
			current.Mappings[recordKey(current, key)] = []string{""}
		} else {
			current.Mappings[recordKey(current, key)] = []string{v}
		}
	}

	flushRecord()

	return out
}

// handleRecordHeader handles a `source_filter: ...` or `includes:`
// line at the record level (either after `- ` on a top-level list
// entry, or as a continuation key inside an existing record). Sets
// inIncludes to true when `includes:` is seen.
func handleRecordHeader(name string, lineno int, body string, rec *SysIncl, inIncludes *bool, onWarn func(Warn)) {
	if body == "" || body == "includes:" {
		*inIncludes = true

		return
	}

	if strings.HasPrefix(body, "source_filter:") {
		rest := strings.TrimSpace(body[len("source_filter:"):])
		pat := unquote(rest)
		rec.Filter = compileSourceFilter(name, lineno, pat, onWarn)
		// Negative-lookahead filters key by source: using the includer
		// instead causes libcxx/musl replacement headers (uchar.h,
		// ctype.h, ...) to fire on every libcxx-source consumer
		// reaching them via libcxx internal chains. Records without
		// negative-lookahead key by includer so `^contrib/libs/glibcasm`
		// fires on glibcasm-rooted chains from musl sources.
		rec.KeyBySource = strings.Contains(pat, "(?!")

		return
	}

	if strings.HasPrefix(body, "includes:") {
		// `includes:` on the same line as the `- source_filter` (rare
		// but valid in compact form) — treat as opener.
		*inIncludes = true

		return
	}

	// `case_sensitive: <bool>` (windows.yml): Win32 headers are
	// referenced in mixed case (`<Windows.h>`, `<WinSock2.h>`); the
	// YAML stores them lower-case and asks the scanner to fold the
	// query. When `false`, keys are stored lower-cased and Lookup
	// lower-cases the query header before probing.
	if strings.HasPrefix(body, "case_sensitive:") {
		val := strings.TrimSpace(body[len("case_sensitive:"):])
		if val == "false" {
			rec.CaseInsensitive = true
		}
		return
	}

	// Unknown record-header keys are dropped — the record's filter is
	// marked unsupported so its mappings never fire.
	onWarn(Warn{
		Kind:    WarnSysIncl,
		Message: fmt.Sprintf("%s:%d: source_filter %q unsupported (unrecognised record header) — record disabled", name, lineno, body),
	})
	rec.Filter = &sourceFilter{unsupported: true}
}

// stripComment trims a trailing `# ...` comment. Conservative: only
// strips when `#` is preceded by whitespace OR is at column 0.
// Upstream YAMLs do not embed `#` inside string values (verified), so
// this is safe.
func stripComment(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '#' {
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				return s[:i]
			}
		}
	}

	return s
}

// leadingSpaces counts spaces (not tabs — upstream YAMLs use spaces).
func leadingSpaces(s string) int {
	i := 0

	for i < len(s) && s[i] == ' ' {
		i++
	}

	return i
}

// splitKeyValue splits "key: value" or "key:value" or "key" into
// (key, value, hasMapping). Returns hasMapping=false for bare keys
// (no colon). Macro shapes like `MACRO(arg)` count as bare keys
// (the colon-check is on the LAST `:` outside parentheses).
func splitKeyValue(s string) (string, string, bool) {
	depth := 0
	colon := -1

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ':':
			if depth == 0 {
				colon = i

				break
			}
		}

		if colon >= 0 {
			break
		}
	}

	if colon < 0 {
		return s, "", false
	}

	key := strings.TrimSpace(s[:colon])
	val := strings.TrimSpace(s[colon+1:])

	return key, val, true
}

// unquote removes a single layer of double or single quotes around s.
// No-op when s is not quoted. Empirical YAMLs use double quotes only.
func unquote(s string) string {
	if len(s) >= 2 {
		first := s[0]
		last := s[len(s)-1]

		if first == last && (first == '"' || first == '\'') {
			return s[1 : len(s)-1]
		}
	}

	return s
}

// sourceFilter is a compiled source_filter. Top-level `|` splits into
// alts; the filter matches when ANY alt matches. Within an alt:
//   - excludePrefixes: `(?!P)` translations (path must NOT start with).
//   - re: residual positive RE2 regex; nil for pure `^(?!...)` (the
//     alt matches any path surviving the excludes).
//
// `unsupported` kills the filter (no alt fires).
type sourceFilter struct {
	alts        []filterAlt
	unsupported bool
}

// filterAlt holds one alt of a sourceFilter. Matches when path has no
// excludePrefix AND one of (literalPrefix, re) is satisfied. Empty
// literalPrefix+re accepts every path surviving the excludes.
//
// Fast-path: 129/169 source_filter patterns in build/sysincl/*.yml are
// simple `^literal-prefix`; `strings.HasPrefix` avoids RE2 overhead.
type filterAlt struct {
	excludePrefixes []string
	literalPrefix   string
	re              *regexp.Regexp
}

// match returns true when the source path satisfies any alt.
func (f *sourceFilter) match(sourcePath string) bool {
	if f.unsupported {
		return false
	}

	for i := range f.alts {
		if f.alts[i].matches(sourcePath) {
			return true
		}
	}

	return false
}

func (a *filterAlt) matches(sourcePath string) bool {
	for _, p := range a.excludePrefixes {
		if strings.HasPrefix(sourcePath, p) {
			return false
		}
	}

	if a.literalPrefix != "" {
		return strings.HasPrefix(sourcePath, a.literalPrefix)
	}

	if a.re == nil {
		return true
	}

	return a.re.MatchString(sourcePath)
}

// compileSourceFilter parses a single source_filter regex. Throws on
// anything outside the recognised subset so future upstream patterns
// surface immediately rather than silently misparse.
//
// Recognised shapes (after stripping outer quotes):
//   - `^P` or plain RE2 — compiled directly.
//   - `^(?!P)` or `^(?!(P1|P2|...))` — exclude-prefix list.
//   - `^(?!P)X|^Y` — alternation between exclude-clause and positive;
//     each alt translated independently.
func compileSourceFilter(name string, lineno int, pat string, onWarn func(Warn)) *sourceFilter {
	if pat == "" {
		return nil
	}

	f := &sourceFilter{}

	exc := Try(func() {
		altStrs := splitTopLevelOr(pat)

		for _, altStr := range altStrs {
			excludes, residual, isExclude := extractNegativeLookahead(altStr)

			alt := filterAlt{}

			if isExclude {
				alt.excludePrefixes = excludes

				if residual != "" {
					if strings.Contains(residual, "(?!") {
						ThrowFmt("sysincl: %s:%d: unsupported negative lookahead position in %q (residual after ^(?!): %q)", name, lineno, altStr, residual)
					}

					// `.*` after the lookahead is a no-op positive
					// constraint — the lookahead already gated the path
					// and the rest accepts anything.
					if residual == ".*" {
						// alt.re stays nil; alt.literalPrefix stays "".
						// The exclude prefixes are the only constraint.
					} else if lit := extractLiteralAnchoredPrefix(residual); lit != "" {
						alt.literalPrefix = lit
					} else {
						re, err := regexp.Compile(residual)

						if err != nil {
							ThrowFmt("sysincl: %s:%d: cannot compile alt residual %q: %v", name, lineno, residual, err)
						}

						alt.re = re
					}
				}
			} else {
				if strings.Contains(altStr, "(?!") {
					ThrowFmt("sysincl: %s:%d: unsupported negative lookahead position in %q", name, lineno, altStr)
				}

				if lit := extractLiteralAnchoredPrefix(altStr); lit != "" {
					alt.literalPrefix = lit
				} else {
					re, err := regexp.Compile(altStr)

					if err != nil {
						ThrowFmt("sysincl: %s:%d: cannot compile alt %q: %v", name, lineno, altStr, err)
					}

					alt.re = re
				}
			}

			f.alts = append(f.alts, alt)
		}
	})

	if exc != nil {
		// Fail soft: mark the filter as unsupported so the record's
		// mappings never fire. Surface one diagnostic per distinct
		// failure through `onWarn` — caller-supplied (no-op for the
		// quiet default, stderr for `--verbose`).
		onWarn(Warn{
			Kind:    WarnSysIncl,
			Message: fmt.Sprintf("%s:%d: source_filter %q unsupported (%s) — record disabled", name, lineno, pat, exc.Error()),
		})

		return &sourceFilter{unsupported: true}
	}

	return f
}

// splitTopLevelOr splits a regex on top-level `|` (not inside
// parentheses or character classes). Returns the original string in a
// 1-element slice when no top-level `|` exists.
func splitTopLevelOr(pat string) []string {
	depth := 0
	bracket := false
	out := []string{}
	last := 0

	for i := 0; i < len(pat); i++ {
		c := pat[i]

		switch {
		case c == '\\':
			i++ // skip escaped char
		case c == '[':
			bracket = true
		case c == ']':
			bracket = false
		case c == '(' && !bracket:
			depth++
		case c == ')' && !bracket:
			depth--
		case c == '|' && depth == 0 && !bracket:
			out = append(out, pat[last:i])
			last = i + 1
		}
	}

	out = append(out, pat[last:])

	return out
}

// extractNegativeLookahead recognises `^(?!P)` and `^(?!(P1|P2|...))`
// at the start of a regex. Returns excluded prefixes + residual after
// the lookahead, or isExclude=false for patterns not starting with
// `^(?!`. Throws on `^(?!` shapes outside the documented subset.
func extractNegativeLookahead(pat string) ([]string, string, bool) {
	const prefix = "^(?!"

	if !strings.HasPrefix(pat, prefix) {
		return nil, "", false
	}

	// Find matching `)` for the (?! group.
	rest := pat[len(prefix):]
	depth := 1
	end := -1

	for i := 0; i < len(rest); i++ {
		c := rest[i]

		switch c {
		case '\\':
			i++
		case '(':
			depth++
		case ')':
			depth--

			if depth == 0 {
				end = i
			}
		}

		if end >= 0 {
			break
		}
	}

	if end < 0 {
		ThrowFmt("sysincl: malformed negative lookahead in %q", pat)
	}

	inner := rest[:end]
	residual := rest[end+1:]

	excludes := parseLookaheadAlts(inner)

	return excludes, residual, true
}

// parseLookaheadAlts splits a (?!...) body into prefix alternatives.
// Recognised: `P`, `(P1|P2|...)`, `P1|P2|...`. Each Pi must be literal
// (no regex metacharacters); throws on patterns outside this subset.
func parseLookaheadAlts(body string) []string {
	body = strings.TrimSpace(body)

	if strings.HasPrefix(body, "(") && strings.HasSuffix(body, ")") {
		body = body[1 : len(body)-1]
	}

	parts := splitTopLevelOr(body)

	out := make([]string, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(p)

		if p == "" {
			continue
		}

		if containsRegexMeta(p) {
			ThrowFmt("sysincl: negative-lookahead alt %q has regex metacharacters; only literal prefixes are supported", p)
		}

		out = append(out, p)
	}

	return out
}

// containsRegexMeta returns true when s contains any RE2 metacharacter
// that would make it non-literal. Used to gate the negative-lookahead
// translation: a literal prefix is safe to translate; a regex alt
// inside `(?!...)` is not.
func containsRegexMeta(s string) bool {
	const meta = `\.+*?[]{}()|^$`

	for i := 0; i < len(s); i++ {
		for j := 0; j < len(meta); j++ {
			if s[i] == meta[j] {
				return true
			}
		}
	}

	return false
}

// extractLiteralAnchoredPrefix returns the literal prefix when `pat`
// is exactly `^literalChars` (no RE2 metacharacters after `^`), else
// "". 129/169 source_filter patterns have this shape; HasPrefix saves
// the RE2 NFA cost. Returns "" when no leading `^`, when body has
// metacharacters, or when body is empty.
func extractLiteralAnchoredPrefix(pat string) string {
	if !strings.HasPrefix(pat, "^") {
		return ""
	}

	body := pat[1:]

	if body == "" {
		return ""
	}

	if containsRegexMeta(body) {
		return ""
	}

	return body
}
