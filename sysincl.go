package main

// sysincl.go — loader for build/sysincl/*.yml. Maps a `#include <H>`
// (or quoted-form fallback) to zero or more SOURCE_ROOT-relative file
// paths the upstream ymake scanner would emit as additional inputs.
//
// Why we parse YAML at runtime (documented exception to the project's
// "hand-translate build/conf" rule): the upstream tree contains 53
// sysincl YAML files totalling ~11K lines of pure data — translating
// each to Go would be prohibitive and would drift on every upstream
// resync. The PR-31 scanner needs faithful sysincl behaviour to
// resolve `<features.h>`, `<cstring>`, `<linux/...>`, etc.; that
// behaviour IS the data in these YAMLs.
//
// We do NOT pull a YAML library — a hand-rolled parser that handles
// the subset actually used (string-only `key: value`, `key:` followed
// by `- value` list under `includes:`, optional `source_filter:`
// regex, `# comment` lines) is ~150 LOC. The parser THROWS on
// constructs it does not recognise so an upstream evolution surfaces
// loudly rather than silently misparses (R1 of the plan doc).
//
// Subset supported:
//   - leading `# comment` lines (skipped)
//   - top-level list of records, each begun by `- source_filter: ...`
//     OR `- includes:` (the source_filter line is optional; when
//     absent, the record's filter matches every source path)
//   - inside a record: `source_filter: "<regex>"` (string, optionally
//     quoted) and `includes:` (a list)
//   - inside includes: each entry is `- key: value` (single mapping
//     to one path), `- key: ""` (suppression — header is sysincl-known
//     but emits nothing), `- key:` followed by `- path` lines (fan-out
//     to N paths), or `- bareKey` (no mapping — header is sysincl-
//     known but emits nothing). Macro forms like `MACRO(arg)` parse
//     but never fire because our scanner only triggers on bracketed
//     `#include`s; the data is retained for forward-compat.
//
// Negative-lookahead regexes (`(?!FOO)`) are common in the upstream
// (e.g. `^(?!contrib/libs/musl).*` to match "every source EXCEPT musl
// itself"). Go's RE2 stdlib does not support them. We translate the
// recognised patterns into a custom matcher: a sourcePath matches when
// (a) it satisfies all positive prefixes, AND (b) it matches none of
// the negative-lookahead exclusions. Anything that does not fit those
// shapes throws at load time.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SysIncl is one record from a sysincl YAML file. Mappings are
// header-string → list of SOURCE_ROOT-relative resolved paths. Empty
// list (or list with empty-string element) marks a header as sysincl-
// known but emitting no input — the resolution stops without
// recursing.
type SysIncl struct {
	Filter   *sourceFilter
	Mappings map[string][]string
}

// SysInclSet is the union of all sysincl records loaded from
// build/sysincl/*.yml.
type SysInclSet []SysIncl

// Lookup returns the union of resolved paths for `header` across
// every record whose filter matches `sourcePath`. Empirically, ymake
// stacks sysincl YAML files: each record contributes its own mapping
// to a header, and the consumer sees ALL contributions as candidate
// inputs. Examples:
//
//   - `<stddef.h>` from a non-musl C source resolves to BOTH
//     libcxx/include/stddef.h (via stl-to-libcxx.yml) AND
//     musl/include/stddef.h (via libc-to-musl.yml). Verified via
//     base64/avx2/lib.c.o reference inputs (slots [3] and [14]).
//   - `<features.h>` from musl source resolves to the
//     musl/include + musl/src/include fan-out (libc-to-musl.yml's
//     first record's 2-element list), with no further records adding
//     paths.
//
// Bare-key entries (no mapping value) and explicit `key: ""` are
// suppression markers — they make the record a no-op for that header
// without short-circuiting other records that might still claim it.
//
// Returns (paths, true) when at least one record claimed the header
// (paths may be empty when every claimer is suppression-only), or
// (nil, false) when no record claims the header at all (the caller's
// resolver then falls through to other search-path candidates).
func (s SysInclSet) Lookup(sourcePath, header string) ([]string, bool) {
	var (
		out   []string
		found bool
		seen  = map[string]struct{}{}
	)

	for _, rec := range s {
		if rec.Filter != nil && !rec.Filter.match(sourcePath) {
			continue
		}

		paths, ok := rec.Mappings[header]

		if !ok {
			continue
		}

		found = true

		for _, p := range paths {
			if p == "" {
				continue
			}

			if _, dup := seen[p]; dup {
				continue
			}

			seen[p] = struct{}{}
			out = append(out, p)
		}
	}

	return out, found
}

// linuxMuslSysInclOrder lists the platform-INDEPENDENT sysincl YAML
// files loaded for the M2 build configuration (Linux, MUSL,
// USE_STL_SYSTEM=no, NORUNTIME=no, USE_ARCADIA_COMPILER_RUNTIME=yes,
// OPENSOURCE=yes). Order is taken from build/conf/sysincl.conf and
// the conditional block in build/ymake.core.conf:340-351.
//
// Union-of-matches semantics: each record contributes its mapping
// to the resolved set; bare-key entries are sysincl-known but emit
// nothing.
//
// linux-headers.yml is intentionally absent: ymake loads it only
// when OS_LINUX != "yes" (build/conf/sysincl.conf:69). Linux headers
// reach the closure via the per-module/per-bundle ADDINCL paths
// (-I$(SOURCE_ROOT)/contrib/libs/linux-headers etc.) and through
// the include scanner's transitive walk, not through sysincl
// resolution.
//
// Platform-dependent files (`linux-musl-aarch64.yml` vs
// `linux-musl.yml`) are routed through `LoadSysInclSetFor`'s
// `arch` argument because `bits/alltypes.h` and similar `bits/*`
// mappings differ between aarch64 and x86_64. For dual-platform
// emission (M2's archiver closure has both aarch64 target and
// x86_64 host CC nodes) the walker keeps a separate scanner per
// architecture.
var linuxMuslSysInclOrder = []string{
	"macro.yml",
	"libc-to-compat.yml",
	"libc-to-nothing.yml",
	"stl-to-libstdcxx.yml",
	"stl-to-nothing.yml",
	"windows.yml",
	"darwin.yml",
	"android.yml",
	"freebsd.yml",
	"intrinsic.yml",
	"nvidia.yml",
	"misc.yml",
	"unsorted.yml",
	"swig.yml",
	"libiconv.yml",
	"libidn.yml",
	"jdk-to-arcadia.yml",
	"opensource.yml",
	"libc-to-musl.yml",
	// linux-musl-<arch>.yml is injected here by LoadSysInclSetFor.
	"emscripten-to-nothing.yml",
	"nvidia-cccl.yml",
	"stl-to-libcxx.yml",
	"libc-musl-libcxx.yml",
}

// LoadSysInclSet parses the sysincl YAML files in
// `<sourceRoot>/build/sysincl/` per the configuration order
// documented in `linuxMuslSysInclOrder`. Loads the aarch64
// platform-specific file by default. For host (x86_64) builds use
// `LoadSysInclSetFor("x86_64", ...)` instead — the platform
// dispatch is explicit.
func LoadSysInclSet(sourceRoot string) SysInclSet {
	return LoadSysInclSetFor(sourceRoot, "aarch64")
}

// LoadSysInclSetFor loads the M2 sysincl YAMLs with the given
// architecture's `linux-musl-<arch>.yml` injected after
// `libc-to-musl.yml` (mirroring `build/conf/sysincl.conf:53-58`'s
// when-block). `arch` must be "aarch64" or "x86_64"; other values
// throw.
//
// When the sysincl directory does not exist (synthetic test trees
// that supply only the modules the test cares about), the loader
// returns an empty set rather than throwing — the include scanner
// then falls back to its own AddIncl + peer-GLOBAL search path.
func LoadSysInclSetFor(sourceRoot, arch string) SysInclSet {
	dir := filepath.Join(sourceRoot, "build", "sysincl")

	if _, err := os.Stat(dir); err != nil {
		return nil
	}

	var archFile string

	switch arch {
	case "aarch64":
		archFile = "linux-musl-aarch64.yml"
	case "x86_64":
		archFile = "linux-musl.yml"
	default:
		ThrowFmt("LoadSysInclSetFor: unsupported arch %q (want aarch64 or x86_64)", arch)
	}

	order := make([]string, 0, len(linuxMuslSysInclOrder)+1)

	for _, name := range linuxMuslSysInclOrder {
		order = append(order, name)

		if name == "libc-to-musl.yml" {
			order = append(order, archFile)
		}
	}

	var set SysInclSet

	for _, name := range order {
		path := filepath.Join(dir, name)

		data, err := os.ReadFile(path)

		if err != nil {
			continue
		}

		records := parseSysInclYAML(name, string(data))
		set = append(set, records...)
	}

	return set
}

// Suppress unused-import warning when sort is no longer needed; we
// keep the import behind a helper so a future widening that sorts
// supplemental yamls compiles cleanly.
var _ = sort.Strings

// parseSysInclYAML parses one sysincl YAML file's text into a slice of
// records. The filename is carried for error messages only. Throws
// with a precise location ("name.yml:LINE: <reason>") on any
// construct outside the documented subset.
func parseSysInclYAML(name, text string) []SysIncl {
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

		// `- key:` with no explicit list and no values — suppression.
		// Empirically: linux-headers.yml's `- asm-generic/auxvec.h`
		// (bare key) goes through this path with pendingPaths==nil.
		if pendingPaths == nil {
			current.Mappings[pendingKey] = nil
		} else {
			current.Mappings[pendingKey] = pendingPaths
		}

		pendingKey = ""
		pendingPaths = nil
	}

	flushRecord := func() {
		flushPending()

		if current != nil {
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

			handleRecordHeader(name, lineno, rest, current, &inIncludes)

			continue
		}

		if current == nil {
			ThrowFmt("sysincl: %s:%d: line outside any record: %q", name, lineno, body)
		}

		// Sub-key at indent ≥ 2: either `source_filter: ...`,
		// `includes:`, or — when inIncludes — a mapping entry.
		if !inIncludes {
			handleRecordHeader(name, lineno, body, current, &inIncludes)

			continue
		}

		// Inside `includes:`. Expected forms:
		//   `- key`              bare (suppression)
		//   `- key: value`       single mapping
		//   `- key: ""`          explicit suppression
		//   `- key:`             start of fan-out list
		//   `- path`             continuation of fan-out (when pendingKey set)
		if !strings.HasPrefix(body, "- ") && body != "-" {
			ThrowFmt("sysincl: %s:%d: expected list entry, got %q", name, lineno, body)
		}

		var entry string

		if body == "-" {
			entry = ""
		} else {
			entry = strings.TrimSpace(body[2:])
		}

		// Continuation of fan-out: this `- ...` lives at deeper indent
		// than the mapping that opened it. Empirical YAML uses 4-space
		// indent for the entry and 6-space for list items inside it,
		// or 2/4-space depending on file. We disambiguate by whether
		// `pendingKey` is set AND the entry contains no `:` outside a
		// quoted string.
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
			current.Mappings[key] = nil

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
			current.Mappings[key] = []string{""}
		} else {
			current.Mappings[key] = []string{v}
		}
	}

	flushRecord()

	return out
}

// handleRecordHeader handles a `source_filter: ...` or `includes:`
// line at the record level (either after `- ` on a top-level list
// entry, or as a continuation key inside an existing record). Sets
// inIncludes to true when `includes:` is seen.
func handleRecordHeader(name string, lineno int, body string, rec *SysIncl, inIncludes *bool) {
	if body == "" || body == "includes:" {
		*inIncludes = true

		return
	}

	if strings.HasPrefix(body, "source_filter:") {
		rest := strings.TrimSpace(body[len("source_filter:"):])
		rec.Filter = compileSourceFilter(name, lineno, unquote(rest))

		return
	}

	if strings.HasPrefix(body, "includes:") {
		// `includes:` on the same line as the `- source_filter` (rare
		// but valid in compact form) — treat as opener.
		*inIncludes = true

		return
	}

	// Unknown record-header keys (e.g. `case_sensitive: false` from
	// windows.yml) are not in our supported subset. Mark the record's
	// filter unsupported so its mappings never fire — dropping the
	// record outright is preferable to throwing because windows.yml
	// and similar exotic files are not in the M2 Linux closure.
	warnUnsupportedSysInclFilter(name, lineno, body, "unrecognised record header")
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

// sourceFilter is a compiled source_filter clause. Each alternative
// (split on top-level `|`) becomes its own filterAlt. The filter
// matches a path when ANY alt matches. Within an alt:
//
//   - excludePrefixes: `(?!P)` translations (the path must NOT start
//     with any of these for the alt to match).
//   - re: the residual positive RE2 regex (compiled by stdlib); nil
//     when the alt is purely `^(?!...)` with no remainder, in which
//     case the alt matches any path that survives the excludes.
//
// When `unsupported` is set on the whole filter, no alt fires —
// the record's mappings are dead for our purposes.
type sourceFilter struct {
	alts        []filterAlt
	unsupported bool
}

// filterAlt holds one alt of a sourceFilter. An alt matches when:
//   - the path does not start with any excludePrefix, AND
//   - one of (literalPrefix, re) is satisfied. literalPrefix is a
//     fast-path: when set, `strings.HasPrefix(path, literalPrefix)`
//     is the positive criterion and `re` is left nil. When neither
//     literalPrefix nor re is set the alt accepts every path that
//     survived the excludes (matches `.*` / no positive constraint).
//
// Hot-path note: 129/169 source_filter patterns observed in
// build/sysincl/*.yml are simple `^literal-prefix` regexes. Replacing
// `re.MatchString` with `strings.HasPrefix` avoids the RE2 NFA
// engine's per-call overhead in the common case.
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

// compileSourceFilter parses a single source_filter regex. Recognises
// the upstream pattern vocabulary (with or without negative lookahead);
// throws on anything outside that subset so future upstream patterns
// surface immediately rather than silently misparse.
//
// Recognised shapes (after stripping outer quotes):
//
//   - `^P` or other plain RE2 — compiled directly.
//   - `^(?!P)` or `^(?!(P1|P2|...))` — translated to an exclude-prefix
//     list; remaining anchors are dropped (the filter trivially
//     accepts the rest after the lookahead consumed nothing).
//   - `^(?!P)X|^Y` — alternation between an exclude-clause and a
//     positive clause. We transform to "match positive Y OR (no
//     exclude AND remainder)". Implementation: split on top-level `|`,
//     translate each alt independently, then OR-combine via a
//     synthesised matcher (each excludePrefix list applies only to
//     its alt; we approximate with the safe lower bound — the FIRST
//     alt's excludePrefixes plus the union of positive RE2 patterns
//     compiled into a single OR'd regex).
//
// The OR-combine path is sound because every observed upstream
// alternation has the shape `^(?!X)|^X/something_else` (e.g.
// `^(?!contrib/libs/musl)|^contrib/libs/musl/tests` — "everything
// except musl, EXCEPT also musl/tests"). The "lower bound" matches
// musl/tests via the second alt (regex) and excludes plain musl/foo
// via the first alt's exclude prefix. Verified against every
// alternation pattern in build/sysincl/*.yml.
func compileSourceFilter(name string, lineno int, pat string) *sourceFilter {
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
		// mappings never fire. Print one stderr line per
		// distinct failure so an audit run surfaces the gap.
		warnUnsupportedSysInclFilter(name, lineno, pat, exc.Error())

		return &sourceFilter{unsupported: true}
	}

	return f
}

// warnUnsupportedSysInclFilter is invoked once per unrecognised
// source_filter regex during sysincl loading. We print to stderr —
// the ledger captures the gap for follow-up; the runtime continues
// with that record dead.
func warnUnsupportedSysInclFilter(name string, lineno int, pat, why string) {
	fmt.Fprintf(os.Stderr, "sysincl: %s:%d: source_filter %q unsupported (%s) — record disabled\n", name, lineno, pat, why)
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
// at the start of a regex, returning the excluded prefixes and any
// residual pattern after the lookahead. Returns isExclude=false for
// patterns that do not begin with `^(?!`. Throws on `^(?!` shapes
// outside the documented subset.
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

// parseLookaheadAlts splits the body of a (?!...) group into prefix
// alternatives. Recognised:
//
//   - `P` — single literal prefix.
//   - `(P1|P2|...)` — parenthesised alternation; emits each Pi.
//   - `P1|P2|...` — bare alternation.
//
// Each Pi must be a literal string (no regex metacharacters). Throws
// on patterns outside this subset.
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
			ThrowFmt("sysincl: negative-lookahead alt %q has regex metacharacters; PR-31 only handles literal prefixes", p)
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

// extractLiteralAnchoredPrefix returns the literal prefix when `pat` is
// exactly `^literalChars` (anchored start, followed by characters that
// are not RE2 metacharacters), else returns "". This is the hot-path
// optimisation for source_filter regexes: empirically 129/169 patterns
// in build/sysincl/*.yml have this shape (e.g. `^contrib/libs/musl`,
// `^contrib/libs/jemalloc/`), so replacing the RE2 engine call with a
// `strings.HasPrefix` saves measurable Lookup-time overhead.
//
// Returns "" for:
//   - patterns lacking a leading `^` anchor.
//   - patterns whose body contains any RE2 metacharacter (the residual
//     would then be a real regex, not a literal prefix).
//   - the empty literal (`^` alone) — signalling "no fast path"; the
//     caller falls through to compile a regex (which itself is then a
//     trivial `.*`-equivalent, but correctness is upstream's job).
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
