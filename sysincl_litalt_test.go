package main

import (
	"regexp"
	"strings"
	"testing"
)

// TestLiteralAltsFromRegex_ParityWithRegex verifies that the prefix set an
// anchored literal-alternation expands to matches exactly what the original
// RE2 pattern matches, for a spread of paths — the property the source-filter
// optimisation relies on.
func TestLiteralAltsFromRegex_ParityWithRegex(t *testing.T) {
	expandable := []string{
		`^contrib/(deprecated/onednn|libs/intel/onednn)`,
		`^contrib/(libs/(ffmpeg-3|kyotocabinet)|tools/ag)`,
		`^(contrib/libs/cxxsupp/openmp|catboost/cuda/cuda_lib)`,
		`^contrib/libs/(apache/apr|openssl)`,
		`^contrib/libs/(kyotocabinet|minilzo)`,
		`^(contrib/libs/musl|contrib/libs/cxxsupp/libcxx/include/__config)`,
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
		"contrib/libs/musl/src/string/strlen.c",
		"contrib/libs/cxxsupp/libcxx/include/__config",
		"util/generic/string.h",
		"library/cpp/foo/bar.cpp",
		"xcontrib/libs/openssl/ssl.c", // not at start
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

// TestLiteralAltsFromRegex_BailsOnNonLiteral pins the forms that must keep RE2.
func TestLiteralAltsFromRegex_BailsOnNonLiteral(t *testing.T) {
	keepRegex := []string{
		`.*contrib.*`,                  // unanchored substring
		`[.]swg([.](h|c(c|pp|xx)?))?$`, // char class + $ anchor
		`^contrib/.*`,                  // repetition
		`^contrib/[a-z]+`,              // char class
		`contrib/(a|b)`,                // not ^-anchored
		`^contrib/(a|b)$`,              // $ anchor → full match, not prefix
	}

	for _, pat := range keepRegex {
		if _, ok := literalAltsFromRegex(pat); ok {
			t.Errorf("literalAltsFromRegex(%q) = expandable, want kept as regex", pat)
		}
	}
}
