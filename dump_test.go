package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormPath(t *testing.T) {
	cases := map[string]string{
		"$(BUILD_ROOT)/a/b.o":                   "$(B)/a/b.o",
		"$(SOURCE_ROOT)/a/b.c":                  "$(S)/a/b.c",
		"$(CLANG-243881345)/bin/clang":          "$(CLANG)/bin/clang",
		"$(LLD_ROOT-12)/x $(YMAKE_PYTHON3-9)/p": "$(LLD_ROOT)/x $(YMAKE_PYTHON3)/p",
		"/usr/bin/clang":                        "/usr/bin/clang", // no markers, untouched
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

func TestDumpDiff(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")
	out := filepath.Join(dir, "d.txt")

	Throw(os.WriteFile(left, []byte(
		`{"self_uid":"A","outputs":["/x"]}`+"\n"+
			`{"self_uid":"B","outputs":["/y"]}`+"\n"+
			`{"self_uid":"C","outputs":["/shared"]}`+"\n"), 0o644))
	Throw(os.WriteFile(right, []byte(
		`{"self_uid":"A","outputs":["/x"]}`+"\n"+
			`{"self_uid":"D","outputs":["/z"]}`+"\n"+
			`{"self_uid":"E","outputs":["/shared"]}`+"\n"), 0o644))

	if exc := Try(func() { cmdDumpDiff([]string{"--left", left, "--right", right, "--out", out}) }); exc != nil {
		t.Fatalf("dump diff: %v", exc)
	}

	got := string(Throw2(os.ReadFile(out)))
	for _, want := range []string{
		"=== self_uid only in LEFT (2) ===\nB\nC\n",
		"=== self_uid only in RIGHT (2) ===\nD\nE\n",
		"=== outputs only in LEFT (1) ===\n/y\n",
		"=== outputs only in RIGHT (1) ===\n/z\n",
		"=== outputs in both with mismatched self_uid (1) ===\n/shared  left=[C] right=[E]\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("diff output missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	f := Throw2(os.CreateTemp(t.TempDir(), "stdout"))
	os.Stdout = f
	exc := Try(fn)
	os.Stdout = old
	Throw(f.Close())
	if exc != nil {
		t.Fatalf("captured call threw: %v", exc)
	}
	return string(Throw2(os.ReadFile(f.Name())))
}

func TestDumpGrep(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "g.jsonl")
	Throw(os.WriteFile(in, []byte(
		`{"self_uid":"AA","outputs":["$(B)/p/a.o"],"kv":{"p":"CC"}}`+"\n"+
			`{"self_uid":"BB","outputs":["$(B)/p/b.o"],"kv":{"p":"CC"}}`+"\n"), 0o644))

	// match by output, given in long form — normPath canonicalizes both sides
	byOut := captureStdout(t, func() { cmdDumpGrep([]string{"--in", in, "$(BUILD_ROOT)/p/a.o"}) })
	if !strings.Contains(byOut, `"AA"`) || strings.Contains(byOut, `"BB"`) {
		t.Fatalf("grep by output: want AA only, got:\n%s", byOut)
	}

	// match by self_uid
	bySU := captureStdout(t, func() { cmdDumpGrep([]string{"--in", in, "BB"}) })
	if !strings.Contains(bySU, `"BB"`) || strings.Contains(bySU, `"AA"`) {
		t.Fatalf("grep by self_uid: want BB only, got:\n%s", bySU)
	}
}

func TestDumpGrepSubstrRegex(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "g.jsonl")
	Throw(os.WriteFile(in, []byte(
		`{"self_uid":"AA","outputs":["/a.o"],"cmds":[{"cmd_args":["clang","${SSE41_CFLAGS}"]}]}`+"\n"+
			`{"self_uid":"BB","outputs":["/b.o"],"cmds":[{"cmd_args":["clang","-O2"]}]}`+"\n"), 0o644))

	// --substr searches the whole node, so it finds a cmd_args token
	sub := captureStdout(t, func() { cmdDumpGrep([]string{"--in", in, "--substr", "${SSE41_CFLAGS}"}) })
	if !strings.Contains(sub, `"AA"`) || strings.Contains(sub, `"BB"`) {
		t.Fatalf("grep --substr: want AA only, got:\n%s", sub)
	}
	rx := captureStdout(t, func() { cmdDumpGrep([]string{"--in", in, "--regex", "SSE[0-9]+_CFLAGS"}) })
	if !strings.Contains(rx, `"AA"`) || strings.Contains(rx, `"BB"`) {
		t.Fatalf("grep --regex: want AA only, got:\n%s", rx)
	}
}

func TestDumpDiffModes(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")

	ln := func(su, cmds, tags string) string {
		return `{"self_uid":"` + su + `","uid":"` + su + `","outputs":["/a.o"],"deps":[],"inputs":["/a.c"],` +
			`"cmds":[{"cmd_args":` + cmds + `}],"tags":` + tags +
			`,"kv":{"p":"CC"},"env":{},"platform":"linux","requirements":{},"target_properties":{}}`
	}
	Throw(os.WriteFile(left, []byte(ln("L1", `["clang","-c","${SSE}"]`, `["x"]`)+"\n"), 0o644))
	Throw(os.WriteFile(right, []byte(ln("R1", `["clang","-c","-fno-omit-frame-pointer"]`, `[]`)+"\n"), 0o644))

	run := func(mode ...string) string {
		out := filepath.Join(dir, "o.txt")
		args := append([]string{"--left", left, "--right", right, "--out", out}, mode...)
		if exc := Try(func() { cmdDumpDiff(args) }); exc != nil {
			t.Fatalf("diff %v: %v", mode, exc)
		}
		return string(Throw2(os.ReadFile(out)))
	}

	if bf := run("--by-field"); !strings.Contains(bf, "cmds") || !strings.Contains(bf, "tags") {
		t.Fatalf("by-field missing cmds/tags:\n%s", bf)
	}
	if bt := run("--by-token"); !strings.Contains(bt, "${SSE}") || !strings.Contains(bt, "-fno-omit-frame-pointer") {
		t.Fatalf("by-token missing tokens:\n%s", bt)
	}
	if pr := run("--pair", "/a.o"); !strings.Contains(pr, "+${SSE}") || !strings.Contains(pr, "+-fno-omit-frame-pointer") {
		t.Fatalf("pair missing token diffs:\n%s", pr)
	}
}

func TestDumpDiffModes_PairDuplicateOutputsByVariant(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")

	line := func(uid string, host bool) string {
		hostField := ""
		tags := "[]"
		if host {
			hostField = `,"host_platform":true`
			tags = `["tool"]`
		}
		return `{"self_uid":"` + uid + `","uid":"` + uid + `","outputs":["/dup"],"deps":[],"inputs":[],"cmds":[],"tags":` + tags +
			`,"kv":{"p":"R6"},"env":{},"platform":"linux","requirements":{},"target_properties":{}` + hostField + `}`
	}

	Throw(os.WriteFile(left, []byte(line("L-host", true)+"\n"+line("L-target", false)+"\n"), 0o644))
	Throw(os.WriteFile(right, []byte(line("R-host", true)+"\n"+line("R-target", false)+"\n"), 0o644))

	run := func(mode ...string) string {
		out := filepath.Join(dir, "o.txt")
		args := append([]string{"--left", left, "--right", right, "--out", out}, mode...)
		if exc := Try(func() { cmdDumpDiff(args) }); exc != nil {
			t.Fatalf("diff %v: %v", mode, exc)
		}
		return string(Throw2(os.ReadFile(out)))
	}

	byField := run("--by-field")
	if strings.Contains(byField, "host_platform") || strings.Contains(byField, "tags") {
		t.Fatalf("duplicate output pairing leaked variant-only diffs:\n%s", byField)
	}

	byKind := run("--by-kind")
	if strings.Contains(byKind, "host_platform:") || !strings.Contains(byKind, "R6") {
		t.Fatalf("by-kind did not pair duplicate variants cleanly:\n%s", byKind)
	}
}

func TestDumpDiffPair_PrefersDivergentDuplicateVariant(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")
	out := filepath.Join(dir, "o.txt")

	line := func(selfUID, uid, mode string, host bool) string {
		hostField := ""
		tags := "[]"
		if host {
			hostField = `,"host_platform":true`
			tags = `["tool"]`
		}
		return `{"self_uid":"` + selfUID + `","uid":"` + uid + `","outputs":["/dup"],"deps":[],"inputs":[],"cmds":[{"cmd_args":["clang","` + mode + `"]}],"tags":` + tags +
			`,"kv":{"p":"R6"},"env":{},"platform":"linux","requirements":{},"target_properties":{}` + hostField + `}`
	}

	Throw(os.WriteFile(left, []byte(line("same-host", "L-host", "host-clean", true)+"\n"+line("left-target", "L-target", "target-ours", false)+"\n"), 0o644))
	Throw(os.WriteFile(right, []byte(line("same-host", "R-host", "host-clean", true)+"\n"+line("right-target", "R-target", "target-ref", false)+"\n"), 0o644))

	if exc := Try(func() { cmdDumpDiff([]string{"--left", left, "--right", right, "--out", out, "--pair", "/dup"}) }); exc != nil {
		t.Fatalf("pair duplicate outputs: %v", exc)
	}
	got := string(Throw2(os.ReadFile(out)))
	if !strings.Contains(got, "[field cmds differs]") || !strings.Contains(got, "+target-ours") || !strings.Contains(got, "+target-ref") {
		t.Fatalf("pair should report the divergent duplicate variant:\n%s", got)
	}
}

func TestDumpDiffRoots(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")
	out := filepath.Join(dir, "o.txt")

	node := func(su, uid, output, deps, tags string) string {
		return `{"self_uid":"` + su + `","uid":"` + uid + `","outputs":["` + output + `"],"deps":` + deps +
			`,"inputs":[],"cmds":[],"tags":` + tags + `,"kv":{},"env":{},"platform":"x","requirements":{},"target_properties":{}}`
	}
	// /p depends on /c; both content-differ from right. /c is a leaf (no
	// divergent child); /p is not (its child /c diverges).
	Throw(os.WriteFile(left, []byte(node("Ps", "Pu", "/p", `["Cu"]`, `["a"]`)+"\n"+node("Cs", "Cu", "/c", `[]`, `["a"]`)+"\n"), 0o644))
	Throw(os.WriteFile(right, []byte(node("Ps2", "Pu2", "/p", `["Cu2"]`, `[]`)+"\n"+node("Cs2", "Cu2", "/c", `[]`, `[]`)+"\n"), 0o644))

	if exc := Try(func() { cmdDumpDiff([]string{"--left", left, "--right", right, "--out", out, "--roots"}) }); exc != nil {
		t.Fatalf("roots: %v", exc)
	}
	got := string(Throw2(os.ReadFile(out)))
	if !strings.Contains(got, "\n/c\n") {
		t.Fatalf("roots should list /c as a leaf:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if line == "/p" {
			t.Fatalf("roots should NOT list /p (child /c diverges):\n%s", got)
		}
	}
}

func TestDumpDiffRoots_DedupDuplicateOutputsByVariant(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")
	out := filepath.Join(dir, "o.txt")

	line := func(selfUID, uid string, host bool) string {
		hostField := ""
		tags := "[]"
		if host {
			hostField = `,"host_platform":true`
			tags = `["tool"]`
		}
		return `{"self_uid":"` + selfUID + `","uid":"` + uid + `","outputs":["/dup"],"deps":[],"inputs":[],"cmds":[],"tags":` + tags +
			`,"kv":{"p":"R6"},"env":{},"platform":"linux","requirements":{},"target_properties":{}` + hostField + `}`
	}

	Throw(os.WriteFile(left, []byte(line("same-host", "L-host", true)+"\n"+line("left-target", "L-target", false)+"\n"), 0o644))
	Throw(os.WriteFile(right, []byte(line("same-host", "R-host", true)+"\n"+line("right-target", "R-target", false)+"\n"), 0o644))

	if exc := Try(func() { cmdDumpDiff([]string{"--left", left, "--right", right, "--out", out, "--roots"}) }); exc != nil {
		t.Fatalf("roots duplicate outputs: %v", exc)
	}
	got := string(Throw2(os.ReadFile(out)))
	if !strings.Contains(got, "=== roots: 1 leaf-most divergent outputs (of 1 divergent) ===") {
		t.Fatalf("roots should report one divergent duplicate output:\n%s", got)
	}
	count := 0
	for _, line := range strings.Split(got, "\n") {
		if line == "/dup" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("roots should list /dup exactly once, got %d:\n%s", count, got)
	}
}

func TestDumpDiffRoots_PartialOverlapMultiOutputNode(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")
	out := filepath.Join(dir, "o.txt")

	leftNode := `{"self_uid":"left","uid":"L","outputs":["/a","/b"],"deps":[],"inputs":[],"cmds":[],"tags":[],"kv":{"p":"R6"},"env":{},"platform":"linux","requirements":{},"target_properties":{}}`
	rightNode := `{"self_uid":"right","uid":"R","outputs":["/a"],"deps":[],"inputs":[],"cmds":[],"tags":[],"kv":{"p":"R6"},"env":{},"platform":"linux","requirements":{},"target_properties":{}}`

	Throw(os.WriteFile(left, []byte(leftNode+"\n"), 0o644))
	Throw(os.WriteFile(right, []byte(rightNode+"\n"), 0o644))

	if exc := Try(func() { cmdDumpDiff([]string{"--left", left, "--right", right, "--out", out, "--roots"}) }); exc != nil {
		t.Fatalf("roots partial-overlap multi-output: %v", exc)
	}
	got := string(Throw2(os.ReadFile(out)))
	if !strings.Contains(got, "=== roots: 1 leaf-most divergent outputs (of 1 divergent) ===") {
		t.Fatalf("roots should only count the matched divergent output:\n%s", got)
	}
	if !strings.Contains(got, "\n/a\n") {
		t.Fatalf("roots should list /a as the matched divergent output:\n%s", got)
	}
	if strings.Contains(got, "\n/b\n") {
		t.Fatalf("roots should not list unmatched /b as divergent:\n%s", got)
	}
}
