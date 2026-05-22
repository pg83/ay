package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormPath(t *testing.T) {
	cases := map[string]string{
		"$(BUILD_ROOT)/a/b.o":            "$(B)/a/b.o",
		"$(SOURCE_ROOT)/a/b.c":           "$(S)/a/b.c",
		"$(CLANG-243881345)/bin/clang":   "$(CLANG)/bin/clang",
		"$(LLD_ROOT-12)/x $(YMAKE_PYTHON3-9)/p": "$(LLD_ROOT)/x $(YMAKE_PYTHON3)/p",
		"/usr/bin/clang":                 "/usr/bin/clang", // no markers, untouched
	}
	for in, want := range cases {
		if got := normPath(in); got != want {
			t.Errorf("normPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDumpSortMergesChunks(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.txt")
	out := filepath.Join(dir, "out.txt")

	// Duplicates preserved, mixed order; tiny chunk size forces a real
	// multi-chunk k-way merge rather than a single in-memory sort.
	Throw(os.WriteFile(in, []byte("cherry\napple\nbanana\napple\ndate\n"), 0o644))

	if exc := Try(func() { cmdDumpSort([]string{"--in", in, "--out", out, "--chunk-bytes", "8"}) }); exc != nil {
		t.Fatalf("dump sort: %v", exc)
	}

	got := string(Throw2(os.ReadFile(out)))
	want := "apple\napple\nbanana\ncherry\ndate\n"
	if got != want {
		t.Fatalf("sorted = %q, want %q", got, want)
	}
}

// node builds a minimal graph node JSON object literal.
func node(uid, p, out string, deps, inputs []string, extra string) string {
	q := func(ss []string) string {
		parts := make([]string, len(ss))
		for i, s := range ss {
			parts[i] = `"` + s + `"`
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	return `{"uid":"` + uid + `","kv":{"p":"` + p + `"},"outputs":["` + out + `"],"deps":` +
		q(deps) + `,"inputs":` + q(inputs) + `,"cmds":[],"tags":[],"requirements":{},` +
		`"target_properties":{},"platform":"linux"` + extra + `}`
}

func graph(nodes ...string) string {
	return `{"conf":{},"graph":[` + strings.Join(nodes, ",") + `],"result":[]}`
}

func runNormalizeSort(t *testing.T, dir, name, graphJSON, target string) string {
	t.Helper()
	raw := filepath.Join(dir, name+".json")
	norm := filepath.Join(dir, name+".norm.jsonl")
	sorted := filepath.Join(dir, name+".sorted.jsonl")
	Throw(os.WriteFile(raw, []byte(graphJSON), 0o644))

	if exc := Try(func() { cmdDumpNormalize([]string{"--in", raw, "--target", target, "--out", norm}) }); exc != nil {
		t.Fatalf("normalize %s: %v", name, exc)
	}
	if exc := Try(func() { cmdDumpSort([]string{"--in", norm, "--out", sorted}) }); exc != nil {
		t.Fatalf("sort %s: %v", name, exc)
	}
	return string(Throw2(os.ReadFile(sorted)))
}

func TestDumpNormalizeSemanticEquivalence(t *testing.T) {
	dir := t.TempDir()
	target := "pkg/app"

	// A: our-form ($(B)/$(S)), order [ld, cc], versioned CLANG-123 in env.
	a := graph(
		node("u_ld", "LD", "$(B)/pkg/app/app", []string{"u_cc"}, []string{"$(B)/pkg/app/main.o"}, `,"env":{"X":"$(CLANG-123)/lib"}`),
		node("u_cc", "CC", "$(B)/pkg/app/main.o", nil, []string{"$(S)/pkg/app/main.c"}, `,"env":{}`),
	)
	// B: ref-form (long roots), reversed order, different uids, extra
	// stats_uid (dropped), different CLANG version — must normalize equal to A.
	b := graph(
		node("a2", "CC", "$(BUILD_ROOT)/pkg/app/main.o", nil, []string{"$(SOURCE_ROOT)/pkg/app/main.c"}, `,"env":{},"stats_uid":"deadbeef"`),
		node("a1", "LD", "$(BUILD_ROOT)/pkg/app/app", []string{"a2"}, []string{"$(BUILD_ROOT)/pkg/app/main.o"}, `,"env":{"X":"$(CLANG-999)/lib"}`),
	)
	// C: semantically different — cc compiles a different source.
	c := graph(
		node("c_ld", "LD", "$(B)/pkg/app/app", []string{"c_cc"}, []string{"$(B)/pkg/app/main.o"}, `,"env":{"X":"$(CLANG-123)/lib"}`),
		node("c_cc", "CC", "$(B)/pkg/app/main.o", nil, []string{"$(S)/pkg/app/other.c"}, `,"env":{}`),
	)

	na := runNormalizeSort(t, dir, "a", a, target)
	nb := runNormalizeSort(t, dir, "b", b, target)
	nc := runNormalizeSort(t, dir, "c", c, target)

	if na != nb {
		t.Fatalf("isomorphic graphs A and B normalized differently:\nA=%s\nB=%s", na, nb)
	}
	if na == nc {
		t.Fatalf("semantically different graph C matched A:\n%s", na)
	}
	if n := strings.Count(na, "\n"); n != 2 {
		t.Fatalf("expected 2 nodes in closure, got %d lines:\n%s", n, na)
	}
}
