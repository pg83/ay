package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadSysInclSet_RealTree loads the production tree's sysincl
// directory and checks (a) the parser does not throw on any file,
// (b) a few well-known mappings resolve as expected.
func TestLoadSysInclSet_RealTree(t *testing.T) {
	const sourceRoot = "/home/pg/monorepo/yatool_orig"

	if _, err := os.Stat(filepath.Join(sourceRoot, "build", "sysincl")); err != nil {
		t.Skipf("sysincl tree %s not present: %v", sourceRoot, err)
	}

	set := LoadSysInclSet(sourceRoot)

	if len(set) == 0 {
		t.Fatalf("expected non-zero records loaded")
	}

	// Empirical check: union over all sysincl records that claim
	// `<string.h>` for a non-musl source. libc-to-musl.yml record 2
	// contributes musl/include/string.h; stl-to-libcxx.yml record 1
	// contributes libcxx/include/string.h. Both records' filters
	// match base64/avx2/lib.c. PR-35e: Lookup takes (source, includer,
	// header). The base64 source IS its own includer for a top-level
	// `#include <string.h>`, so we pass the same path twice.
	got, ok := set.Lookup("contrib/libs/base64/avx2/lib.c", "contrib/libs/base64/avx2/lib.c", "string.h")

	if !ok {
		t.Fatalf("expected string.h mapping for non-musl source, got none")
	}

	wantMembers := map[string]bool{
		"contrib/libs/musl/include/string.h":           true,
		"contrib/libs/cxxsupp/libcxx/include/string.h": true,
	}

	for _, p := range got {
		delete(wantMembers, p)
	}

	if len(wantMembers) != 0 {
		t.Fatalf("string.h union missing %v; got %v", wantMembers, got)
	}

	// Empirical check: features.h fan-out for musl source. The musl
	// source is its own immediate includer for a top-level
	// `#include <features.h>`, so source and includer paths coincide.
	got, ok = set.Lookup("contrib/libs/musl/src/string/strlen.c", "contrib/libs/musl/src/string/strlen.c", "features.h")

	if !ok {
		t.Fatalf("expected features.h mapping for musl source, got none")
	}

	wantPaths := map[string]bool{
		"contrib/libs/musl/include/features.h":     true,
		"contrib/libs/musl/src/include/features.h": true,
	}

	for _, p := range got {
		delete(wantPaths, p)
	}

	if len(wantPaths) != 0 {
		t.Fatalf("features.h fan-out missing paths %v; got %v", wantPaths, got)
	}
}

// TestParseSysInclYAML_Synthetic exercises the parser on a hand-built
// YAML that covers every supported construct: filter present/absent,
// single mapping, fan-out, suppression, bare key.
func TestParseSysInclYAML_Synthetic(t *testing.T) {
	const yaml = `# leading comment
- source_filter: "^contrib/libs/foo"
  includes:
  - bar.h: contrib/libs/foo/bar.h
  - baz.h:
    - contrib/libs/foo/baz.h
    - contrib/libs/foo/baz_extra.h
  - quux.h: ""
  - bare.h
- includes:
  - any.h: contrib/libs/any/any.h
`
	recs := parseSysInclYAML("test.yml", yaml)

	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}

	// First record: filter-bound mappings.
	r := recs[0]

	if r.Filter == nil {
		t.Fatalf("expected filter on first record")
	}

	if !r.Filter.match("contrib/libs/foo/bar.c") {
		t.Errorf("filter should match contrib/libs/foo/bar.c")
	}

	if r.Filter.match("contrib/libs/other/bar.c") {
		t.Errorf("filter should not match contrib/libs/other/bar.c")
	}

	if got := r.Mappings["bar.h"]; len(got) != 1 || got[0] != "contrib/libs/foo/bar.h" {
		t.Errorf("bar.h: got %v, want [contrib/libs/foo/bar.h]", got)
	}

	if got := r.Mappings["baz.h"]; len(got) != 2 {
		t.Errorf("baz.h: got %v, want 2-element fan-out", got)
	}

	got, ok := r.Mappings["quux.h"]

	if !ok {
		t.Errorf("quux.h: missing")
	}

	if len(got) != 1 || got[0] != "" {
		t.Errorf("quux.h suppression: got %v, want [\"\"]", got)
	}

	got, ok = r.Mappings["bare.h"]

	if !ok {
		t.Errorf("bare.h: missing")
	}

	if got != nil {
		t.Errorf("bare.h: got %v, want nil (bare-key suppression)", got)
	}

	// Second record: no filter.
	if recs[1].Filter != nil {
		t.Errorf("second record: expected nil filter")
	}
}

// TestSourceFilter_NegativeLookahead pins the ^(?!P) and
// ^(?!(P1|P2|...)) translations.
func TestSourceFilter_NegativeLookahead(t *testing.T) {
	cases := []struct {
		pat   string
		match map[string]bool
	}{
		{
			pat: `^(?!contrib/libs/musl)|^contrib/libs/musl/tests`,
			match: map[string]bool{
				"contrib/libs/foo/x.c":        true,
				"contrib/libs/musl/src/y.c":   false,
				"contrib/libs/musl/tests/z.c": true,
				"contrib/libs/musl-other/w.c": false, // exclude prefix is just "contrib/libs/musl"
			},
		},
		{
			pat: `^(?!(contrib/libs/musl|contrib/tools/yasm)).*|^contrib/libs/musl/tests`,
			match: map[string]bool{
				"contrib/libs/foo/x.c":        true,
				"contrib/libs/musl/src/y.c":   false,
				"contrib/tools/yasm/main.c":   false,
				"contrib/libs/musl/tests/z.c": true,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.pat, func(t *testing.T) {
			f := compileSourceFilter("synthetic.yml", 1, c.pat)

			for path, want := range c.match {
				got := f.match(path)
				if got != want {
					t.Errorf("%s on %q: got %v, want %v", c.pat, path, got, want)
				}
			}
		})
	}
}

// TestSysIncl_PerRecordKeying pins the PR-35e source-vs-includer
// dispatch. Negative-lookahead filters (`(?!...)`) must key against
// the SOURCE path; positive filters key against the IMMEDIATE
// includer. Two scenarios exercise both branches:
//
//   - libcxx-source reaching uchar.h via a libcxx includer chain:
//     stl-to-libcxx's `^(?!(contrib/libs/musl|contrib/tools/yasm)).*`
//     filter is negative-lookahead and must key by source. Both
//     records (stl-to-libcxx + libc-to-musl line 75) accept libcxx
//     sources, so the libcxx-uchar + musl-uchar mappings DO fire here
//     under per-record keying — the L2-ceiling fix that closes the
//     uchar.h over-fan-out comes from skipping `#include_next`
//     resolution entirely (TestScanner_IncludeNextSuppressed below)
//     rather than from filter discrimination alone. This test only
//     pins the keying mechanism's correctness, not the L2 outcome.
//   - musl-source reaching `<stdc-predef.h>` via a glibcasm features.h
//     includer: libc-to-musl line 258's filter
//     `^(contrib/libs/glibcasm/glibc/include/features\.h)` is positive
//     and must key by includer (the source is musl, not glibcasm; the
//     filter matches the includer file path). Per-record keying lets
//     this record continue to fire — the PR-33 D05 deferred regression
//     it documented.
func TestSysIncl_PerRecordKeying(t *testing.T) {
	const sourceRoot = "/home/pg/monorepo/yatool_orig"

	if _, err := os.Stat(filepath.Join(sourceRoot, "build", "sysincl")); err != nil {
		t.Skipf("sysincl tree %s not present: %v", sourceRoot, err)
	}

	set := LoadSysInclSet(sourceRoot)

	// Source-keyed branch: libcxx-source reaching uchar.h via a
	// libcxx-internal includer (__mbstate_t.h).
	got, ok := set.Lookup(
		"contrib/libs/cxxsupp/libcxx/src/algorithm.cpp",
		"contrib/libs/cxxsupp/libcxx/include/__mbstate_t.h",
		"uchar.h",
	)

	if !ok {
		t.Fatalf("expected uchar.h sysincl mapping for libcxx source; got none")
	}

	// Both libcxx and musl uchar.h are mapped because both records'
	// source-keyed filters accept libcxx-source. The L2 fix comes from
	// the scanner suppressing `#include_next` resolution (see
	// TestScanner_IncludeNextSuppressed); this test pins the YAML-
	// level dispatch only.
	wantUchar := map[string]bool{
		"contrib/libs/cxxsupp/libcxx/include/uchar.h": true,
		"contrib/libs/musl/include/uchar.h":           true,
	}

	for _, p := range got {
		delete(wantUchar, p)
	}

	if len(wantUchar) != 0 {
		t.Errorf("uchar.h mapping for libcxx source: missing %v, got %v", wantUchar, got)
	}

	// Includer-keyed branch: musl-source reaching stdc-predef.h via
	// glibcasm features.h. The libc-to-musl line 258 record has a
	// positive filter on the includer path (the literal
	// `glibcasm/glibc/include/features.h`); per-record keying picks
	// includer for non-(?!) filters and the mapping fires.
	got, ok = set.Lookup(
		"contrib/libs/musl/src/multibyte/c16rtomb.c",
		"contrib/libs/glibcasm/glibc/include/features.h",
		"stdc-predef.h",
	)

	if !ok {
		t.Fatalf("expected stdc-predef.h mapping for glibcasm includer; got none")
	}

	foundMusl := false

	for _, p := range got {
		if p == "contrib/libs/musl/include/stdc-predef.h" {
			foundMusl = true

			break
		}
	}

	if !foundMusl {
		t.Errorf("stdc-predef.h includer-keyed mapping: musl/include/stdc-predef.h missing; got %v", got)
	}
}

// TestSysIncl_KeyBySourceCompiledFromFilter checks that the
// PR-35e parser sets KeyBySource based on filter shape: negative
// lookahead → true; everything else → false. Empirical: stl-to-libcxx
// has `^(?!...)` and must come out source-keyed; misc.yml glibcasm
// has `^contrib/libs/glibcasm` and must come out includer-keyed.
func TestSysIncl_KeyBySourceCompiledFromFilter(t *testing.T) {
	const sourceRoot = "/home/pg/monorepo/yatool_orig"

	if _, err := os.Stat(filepath.Join(sourceRoot, "build", "sysincl")); err != nil {
		t.Skipf("sysincl tree %s not present: %v", sourceRoot, err)
	}

	set := LoadSysInclSet(sourceRoot)

	srcKeyed := 0
	incKeyed := 0
	noFilter := 0

	for _, r := range set {
		switch {
		case r.Filter == nil:
			noFilter++
		case r.KeyBySource:
			srcKeyed++
		default:
			incKeyed++
		}
	}

	t.Logf("sysincl per-record keying: %d source-keyed, %d includer-keyed, %d no-filter", srcKeyed, incKeyed, noFilter)

	// Sanity: each class has at least a handful of records.
	if srcKeyed < 3 {
		t.Errorf("expected ≥3 source-keyed records (negative-lookahead filters); got %d", srcKeyed)
	}

	if incKeyed < 50 {
		t.Errorf("expected ≥50 includer-keyed records (positive prefix filters); got %d", incKeyed)
	}
}

// TestSysIncl_IncluderFilterCache_HitProducesEqualResult verifies the
// PR-34j memo: repeated LookupIncluderKeyed calls with the same
// includerPath but different headers must return the same set of
// matching records — and the cached fast path must not change the
// observable result. Builds a view, calls the lookup once to warm the
// cache, then again with a different header, and asserts the second
// call's result matches an uncached recompute.
func TestSysIncl_IncluderFilterCache_HitProducesEqualResult(t *testing.T) {
	const sourceRoot = "/home/pg/monorepo/yatool_orig"

	if _, err := os.Stat(filepath.Join(sourceRoot, "build", "sysincl")); err != nil {
		t.Skipf("sysincl tree %s not present: %v", sourceRoot, err)
	}

	set := LoadSysInclSet(sourceRoot)
	view := set.PreparePerSource("contrib/libs/musl/src/string/strlen.c")

	// First call warms the cache for this includerPath.
	got1, _ := view.LookupIncluderKeyed("contrib/libs/musl/src/string/strlen.c", "features.h")

	// Second call with a different header — should reuse the cached
	// active-records subset for the includer.
	got2, _ := view.LookupIncluderKeyed("contrib/libs/musl/src/string/strlen.c", "string.h")

	// Independent recompute via a fresh view (its cache starts empty,
	// so the result here is the uncached path's output).
	fresh := set.PreparePerSource("contrib/libs/musl/src/string/strlen.c")
	want2, _ := fresh.LookupIncluderKeyed("contrib/libs/musl/src/string/strlen.c", "string.h")

	if !stringSlicesEqualUnordered(got2, want2) {
		t.Fatalf("cache hit returned different result from uncached recompute:\n got=%v\nwant=%v", got2, want2)
	}

	// And the first warmer call's result must also be reproducible.
	freshFeatures, _ := fresh.LookupIncluderKeyed("contrib/libs/musl/src/string/strlen.c", "features.h")

	if !stringSlicesEqualUnordered(got1, freshFeatures) {
		t.Fatalf("first call result not reproducible across views:\n got=%v\nwant=%v", got1, freshFeatures)
	}
}

// stringSlicesEqualUnordered compares two []string ignoring order. The
// LookupIncluderKeyed result order is deterministic per call (driven
// by includerKeyed iteration order) but the test treats it as a set —
// any record-ordering refactor inside the cache layer should not break
// this assertion.
func stringSlicesEqualUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	seen := make(map[string]int, len(a))

	for _, s := range a {
		seen[s]++
	}

	for _, s := range b {
		seen[s]--

		if seen[s] < 0 {
			return false
		}
	}

	return true
}

// TestLoadSysInclSet_Stats prints a one-line stat header used in
// PR-31 acceptance reporting. Emitted via t.Logf so `go test -v`
// shows it.
func TestLoadSysInclSet_Stats(t *testing.T) {
	const sourceRoot = "/home/pg/monorepo/yatool_orig"

	if _, err := os.Stat(filepath.Join(sourceRoot, "build", "sysincl")); err != nil {
		t.Skipf("sysincl tree %s not present: %v", sourceRoot, err)
	}

	set := LoadSysInclSet(sourceRoot)

	mapCount := 0
	suppress := 0
	multi := 0

	for _, r := range set {
		mapCount += len(r.Mappings)

		for _, paths := range r.Mappings {
			if len(paths) == 0 || (len(paths) == 1 && paths[0] == "") {
				suppress++
			}

			if len(paths) > 1 {
				multi++
			}
		}
	}

	t.Logf("sysincl: %d records, %d mappings (%d suppress, %d fan-out)", len(set), mapCount, suppress, multi)

	// Sanity: at least 1000 mappings (production tree has > 5000).
	if mapCount < 1000 {
		t.Fatalf("mapping count %d unexpectedly low; YAML loader may be silently dropping entries", mapCount)
	}
}
