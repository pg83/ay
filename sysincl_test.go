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
	recs := parseSysInclYAML("test.yml", []byte(yaml), func(Warn) {})

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

	if got, _ := recMapping(r, "bar.h"); len(got) != 1 || got[0] != source("contrib/libs/foo/bar.h") {
		t.Errorf("bar.h: got %v, want [contrib/libs/foo/bar.h]", got)
	}

	if got, _ := recMapping(r, "baz.h"); len(got) != 2 {
		t.Errorf("baz.h: got %v, want 2-element fan-out", got)
	}

	got, ok := recMapping(r, "quux.h")

	if !ok {
		t.Errorf("quux.h: missing")
	}

	if len(got) != 0 {
		t.Errorf("quux.h suppression: got %v, want no paths", got)
	}

	got, ok = recMapping(r, "bare.h")

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

func TestSysInclMuslGating(t *testing.T) {
	muslFiles := []string{
		"libc-to-musl.yml",
		"linux-musl.yml",
		"linux-musl-aarch64.yml",
		"libc-musl-libcxx.yml",
	}

	selected := func(env SysInclEnv) map[string]bool {
		out := map[string]bool{}

		for _, e := range sysInclYamlSequence {
			if e.predicate == nil || e.predicate(env) {
				out[e.file] = true
			}
		}

		return out
	}

	for _, arch := range []string{"x86_64", "aarch64"} {
		got := selected(SysInclEnv{arch: arch, musl: false})

		for _, f := range muslFiles {
			if got[f] {
				t.Errorf("musl=off arch=%s: %s selected, want gated out", arch, f)
			}
		}
	}

	gotX := selected(SysInclEnv{arch: "x86_64", musl: true})

	for _, f := range []string{"libc-to-musl.yml", "linux-musl.yml", "libc-musl-libcxx.yml"} {
		if !gotX[f] {
			t.Errorf("musl=on x86_64: %s not selected, want selected", f)
		}
	}

	if gotX["linux-musl-aarch64.yml"] {
		t.Errorf("musl=on x86_64: linux-musl-aarch64.yml selected, want x86_64 variant only")
	}

	gotA := selected(SysInclEnv{arch: "aarch64", musl: true})

	if !gotA["linux-musl-aarch64.yml"] {
		t.Errorf("musl=on aarch64: linux-musl-aarch64.yml not selected, want selected")
	}

	if gotA["linux-musl.yml"] {
		t.Errorf("musl=on aarch64: linux-musl.yml selected, want aarch64 variant only")
	}
}

func TestSysInclInternalGating(t *testing.T) {
	fs := newMemFS(map[string]string{
		"build/sysincl/macro.yml": "# empty\n",

		"build/internal/sysincl/actions_zephyr.yml": "" +
			"- source_filter: \"^util\"\n" +
			"  includes:\n" +
			"  - zephyr_only.h: smart_devices/platforms/monocle_common/firmware/zephyr/zephyr_only.h\n",

		"build/internal/sysincl/smart_devices_linux.yml": "" +
			"- source_filter: \"^util\"\n" +
			"  includes:\n" +
			"  - sd_linux.h: smart_devices/linux/sd_linux.h\n",
	})

	set := loadSysInclSetForFS(fs, "x86_64", false, false, OSLinux, func(Warn) {})
	ctx := newSysinclCtx(set)

	claims := func(includer, header string) ([]VFS, bool) {
		paths, _, claimed := ctx.lookup(includer, internStr(header))

		return paths, claimed
	}

	if paths, claimed := claims("util/foo.cpp", "zephyr_only.h"); claimed {
		t.Errorf("actions_zephyr.yml gated out: util/foo.cpp acquired zephyr_only.h -> %v", paths)
	}

	paths, claimed := claims("util/foo.cpp", "sd_linux.h")

	if !claimed {
		t.Fatalf("smart_devices_linux.yml gated in: util/foo.cpp did not acquire sd_linux.h")
	}

	if len(paths) != 1 || paths[0] != source("smart_devices/linux/sd_linux.h") {
		t.Errorf("sd_linux.h for util/foo.cpp: got %v, want [smart_devices/linux/sd_linux.h]", paths)
	}

	if paths, claimed := claims("adfox/bar.cpp", "sd_linux.h"); claimed {
		t.Errorf("source_filter ^util: adfox/bar.cpp acquired sd_linux.h -> %v", paths)
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

func recMapping(r SysIncl, k string) ([]VFS, bool) {
	id := internStr(k)

	for i := len(r.pairs) - 1; i >= 0; i-- {
		if r.pairs[i].key == id {
			return r.pairs[i].paths, true
		}
	}

	return nil, false
}
