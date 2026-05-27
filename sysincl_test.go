package main

import (
	"testing"
)

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
	recs := parseSysInclYAML("test.yml", yaml, func(Warn) {})

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
			f := compileSourceFilter("synthetic.yml", 1, c.pat, func(Warn) {})

			for path, want := range c.match {
				got := f.match(path)
				if got != want {
					t.Errorf("%s on %q: got %v, want %v", c.pat, path, got, want)
				}
			}
		})
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
