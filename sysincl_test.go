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
	// match base64/avx2/lib.c.
	got, ok := set.Lookup("contrib/libs/base64/avx2/lib.c", "string.h")

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

	// Empirical check: features.h fan-out for musl source.
	got, ok = set.Lookup("contrib/libs/musl/src/string/strlen.c", "features.h")

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
