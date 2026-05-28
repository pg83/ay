package main

import (
	"regexp"
	"strings"
	"testing"
)

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
