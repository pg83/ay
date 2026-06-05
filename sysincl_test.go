package main

import (
	"regexp"
	"strings"
	"testing"
)

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

	if recs[1].Filter != nil {
		t.Errorf("second record: expected nil filter")
	}
}

func TestSourceFilter_NegativeLookahead(t *testing.T) {
	cases := []struct {
		pat   string
		match map[string]bool
	}{
		{
			pat: `^(?!contrib/libs/foolib)|^contrib/libs/foolib/tests`,
			match: map[string]bool{
				"contrib/libs/foo/x.c":          true,
				"contrib/libs/foolib/src/y.c":   false,
				"contrib/libs/foolib/tests/z.c": true,
				"contrib/libs/foolib-other/w.c": false,
			},
		},
		{
			pat: `^(?!(contrib/libs/foolib|contrib/tools/yasm)).*|^contrib/libs/foolib/tests`,
			match: map[string]bool{
				"contrib/libs/foo/x.c":          true,
				"contrib/libs/foolib/src/y.c":   false,
				"contrib/tools/yasm/main.c":     false,
				"contrib/libs/foolib/tests/z.c": true,
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

func TestLiteralAltsFromRegex_ParityWithRegex(t *testing.T) {
	expandable := []string{
		`^contrib/(deprecated/onednn|libs/intel/onednn)`,
		`^contrib/(libs/(ffmpeg-3|kyotocabinet)|tools/ag)`,
		`^(contrib/libs/cxxsupp/openmp|catboost/cuda/cuda_lib)`,
		`^contrib/libs/(apache/apr|openssl)`,
		`^contrib/libs/(kyotocabinet|minilzo)`,
		`^(contrib/libs/foolib|contrib/libs/cxxsupp/libcxx/include/__config)`,
	}

	paths := []string{
		"",
		"contrib/deprecated/onednn/src/x.cpp",
		"contrib/libs/intel/onednn/y.c",
		"contrib/libs/ffmpeg-3/a.c",
		"contrib/libs/kyotocabinet/b.c",
		"contrib/tools/ag/c.c",
		"contrib/libs/openssl/ssl.c",
		"contrib/libs/apache/apr/x.c",
		"contrib/libs/minilzo/m.c",
		"contrib/libs/foolib/src/string/strlen.c",
		"contrib/libs/cxxsupp/libcxx/include/__config",
		"util/generic/string.h",
		"library/cpp/foo/bar.cpp",
		"xcontrib/libs/openssl/ssl.c",
	}

	for _, pat := range expandable {
		prefixes, ok := literalAltsFromRegex(pat)
		if !ok {
			t.Errorf("literalAltsFromRegex(%q) = not expandable, want expandable", pat)

			continue
		}

		re := regexp.MustCompile(pat)
		for _, p := range paths {
			want := re.MatchString(p)

			got := false
			for _, pre := range prefixes {
				if strings.HasPrefix(p, pre) {
					got = true

					break
				}
			}

			if got != want {
				t.Errorf("pattern %q path %q: HasPrefix-any%v = %v, regex = %v", pat, p, prefixes, got, want)
			}
		}
	}
}

func TestLiteralAltsFromRegex_BailsOnNonLiteral(t *testing.T) {
	keepRegex := []string{
		`.*contrib.*`,
		`[.]swg([.](h|c(c|pp|xx)?))?$`,
		`^contrib/.*`,
		`^contrib/[a-z]+`,
		`contrib/(a|b)`,
		`^contrib/(a|b)$`,
	}

	for _, pat := range keepRegex {
		if _, ok := literalAltsFromRegex(pat); ok {
			t.Errorf("literalAltsFromRegex(%q) = expandable, want kept as regex", pat)
		}
	}
}
