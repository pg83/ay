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
		"/usr/bin/clang":                        "/usr/bin/clang",
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

	throw(os.WriteFile(in, []byte("cherry\napple\nbanana\napple\ndate\n"), 0o644))

	if exc := try(func() { cmdDumpSort(GlobalFlags{}, []string{"--in", in, "--out", out, "--chunk-bytes", "8"}) }); exc != nil {
		t.Fatalf("dump sort: %v", exc)
	}

	got := string(throw2(os.ReadFile(out)))
	want := "apple\napple\nbanana\ncherry\ndate\n"

	if got != want {
		t.Fatalf("sorted = %q, want %q", got, want)
	}
}

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
	throw(os.WriteFile(raw, []byte(graphJSON), 0o644))

	if exc := try(func() { cmdDumpNormalize(GlobalFlags{}, []string{"--in", raw, "--target", target, "--out", norm}) }); exc != nil {
		t.Fatalf("normalize %s: %v", name, exc)
	}

	if exc := try(func() { cmdDumpSort(GlobalFlags{}, []string{"--in", norm, "--out", sorted}) }); exc != nil {
		t.Fatalf("sort %s: %v", name, exc)
	}

	return string(throw2(os.ReadFile(sorted)))
}

func TestDumpNormalizeSemanticEquivalence(t *testing.T) {
	dir := t.TempDir()
	target := "pkg/app"

	a := graph(
		node("u_ld", "LD", "$(B)/pkg/app/app", []string{"u_cc"}, []string{"$(B)/pkg/app/main.o"}, `,"env":{"X":"$(CLANG-123)/lib"}`),
		node("u_cc", "CC", "$(B)/pkg/app/main.o", nil, []string{"$(S)/pkg/app/main.c"}, `,"env":{}`),
	)

	b := graph(
		node("a2", "CC", "$(BUILD_ROOT)/pkg/app/main.o", nil, []string{"$(SOURCE_ROOT)/pkg/app/main.c"}, `,"env":{},"stats_uid":"deadbeef"`),
		node("a1", "LD", "$(BUILD_ROOT)/pkg/app/app", []string{"a2"}, []string{"$(BUILD_ROOT)/pkg/app/main.o"}, `,"env":{"X":"$(CLANG-999)/lib"}`),
	)

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

	throw(os.WriteFile(left, []byte(
		`{"self_uid":"A","outputs":["/x"]}`+"\n"+
			`{"self_uid":"B","outputs":["/y"]}`+"\n"+
			`{"self_uid":"C","outputs":["/shared"]}`+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(
		`{"self_uid":"A","outputs":["/x"]}`+"\n"+
			`{"self_uid":"D","outputs":["/z"]}`+"\n"+
			`{"self_uid":"E","outputs":["/shared"]}`+"\n"), 0o644))

	if exc := try(func() { cmdDumpDiff(GlobalFlags{}, []string{"--left", left, "--right", right, "--out", out}) }); exc != nil {
		t.Fatalf("dump diff: %v", exc)
	}

	got := string(throw2(os.ReadFile(out)))

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
	f := throw2(os.CreateTemp(t.TempDir(), "stdout"))
	os.Stdout = f
	exc := try(fn)
	os.Stdout = old
	throw(f.Close())

	if exc != nil {
		t.Fatalf("captured call threw: %v", exc)
	}

	return string(throw2(os.ReadFile(f.Name())))
}

func TestDumpGrep(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "g.jsonl")
	throw(os.WriteFile(in, []byte(
		`{"self_uid":"AA","outputs":["$(B)/p/a.o"],"kv":{"p":"CC"}}`+"\n"+
			`{"self_uid":"BB","outputs":["$(B)/p/b.o"],"kv":{"p":"CC"}}`+"\n"), 0o644))

	byOut := captureStdout(t, func() { cmdDumpGrep(GlobalFlags{}, []string{"--in", in, "$(BUILD_ROOT)/p/a.o"}) })

	if !strings.Contains(byOut, `"AA"`) || strings.Contains(byOut, `"BB"`) {
		t.Fatalf("grep by output: want AA only, got:\n%s", byOut)
	}

	bySU := captureStdout(t, func() { cmdDumpGrep(GlobalFlags{}, []string{"--in", in, "BB"}) })

	if !strings.Contains(bySU, `"BB"`) || strings.Contains(bySU, `"AA"`) {
		t.Fatalf("grep by self_uid: want BB only, got:\n%s", bySU)
	}
}

func TestDumpGrepSubstrRegex(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "g.jsonl")
	throw(os.WriteFile(in, []byte(
		`{"self_uid":"AA","outputs":["/a.o"],"cmds":[{"cmd_args":["clang","${SSE41_CFLAGS}"]}]}`+"\n"+
			`{"self_uid":"BB","outputs":["/b.o"],"cmds":[{"cmd_args":["clang","-O2"]}]}`+"\n"), 0o644))

	sub := captureStdout(t, func() { cmdDumpGrep(GlobalFlags{}, []string{"--in", in, "--substr", "${SSE41_CFLAGS}"}) })

	if !strings.Contains(sub, `"AA"`) || strings.Contains(sub, `"BB"`) {
		t.Fatalf("grep --substr: want AA only, got:\n%s", sub)
	}

	rx := captureStdout(t, func() { cmdDumpGrep(GlobalFlags{}, []string{"--in", in, "--regex", "SSE[0-9]+_CFLAGS"}) })

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
	throw(os.WriteFile(left, []byte(ln("L1", `["clang","-c","${SSE}"]`, `["x"]`)+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(ln("R1", `["clang","-c","-fno-omit-frame-pointer"]`, `[]`)+"\n"), 0o644))

	run := func(mode ...string) string {
		out := filepath.Join(dir, "o.txt")
		args := append([]string{"--left", left, "--right", right, "--out", out}, mode...)

		if exc := try(func() { cmdDumpDiff(GlobalFlags{}, args) }); exc != nil {
			t.Fatalf("diff %v: %v", mode, exc)
		}

		return string(throw2(os.ReadFile(out)))
	}

	if bf := run("--by-field"); !strings.Contains(bf, "cmds") {
		t.Fatalf("by-field missing cmds:\n%s", bf)
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

	throw(os.WriteFile(left, []byte(line("L-host", true)+"\n"+line("L-target", false)+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(line("R-host", true)+"\n"+line("R-target", false)+"\n"), 0o644))

	run := func(mode ...string) string {
		out := filepath.Join(dir, "o.txt")
		args := append([]string{"--left", left, "--right", right, "--out", out}, mode...)

		if exc := try(func() { cmdDumpDiff(GlobalFlags{}, args) }); exc != nil {
			t.Fatalf("diff %v: %v", mode, exc)
		}

		return string(throw2(os.ReadFile(out)))
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

	throw(os.WriteFile(left, []byte(line("same-host", "L-host", "host-clean", true)+"\n"+line("left-target", "L-target", "target-ours", false)+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(line("same-host", "R-host", "host-clean", true)+"\n"+line("right-target", "R-target", "target-ref", false)+"\n"), 0o644))

	if exc := try(func() {
		cmdDumpDiff(GlobalFlags{}, []string{"--left", left, "--right", right, "--out", out, "--pair", "/dup"})
	}); exc != nil {
		t.Fatalf("pair duplicate outputs: %v", exc)
	}

	got := string(throw2(os.ReadFile(out)))

	if !strings.Contains(got, "[field cmds differs]") || !strings.Contains(got, "+target-ours") || !strings.Contains(got, "+target-ref") {
		t.Fatalf("pair should report the divergent duplicate variant:\n%s", got)
	}
}

func TestDumpDiffPair_DuplicateOutputsExactCounterpartsNoFieldDiff(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")
	out := filepath.Join(dir, "o.txt")

	line := func(selfUID, mode string) string {
		return `{"self_uid":"` + selfUID + `","uid":"` + selfUID + `","outputs":["/dup"],"deps":[],"inputs":[],"cmds":[{"cmd_args":["ragel","` + mode + `"]}],"tags":[]` +
			`,"kv":{"p":"R6"},"env":{},"platform":"linux","requirements":{},"target_properties":{}}`
	}

	throw(os.WriteFile(left, []byte(line("L-cg2", "-CG2")+"\n"+line("L-ct0", "-CT0")+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(line("R-cg2", "-CG2")+"\n"+line("R-ct0", "-CT0")+"\n"), 0o644))

	if exc := try(func() {
		cmdDumpDiff(GlobalFlags{}, []string{"--left", left, "--right", right, "--out", out, "--pair", "/dup"})
	}); exc != nil {
		t.Fatalf("pair duplicate outputs: %v", exc)
	}

	got := string(throw2(os.ReadFile(out)))

	if !strings.Contains(got, "=== pair diff for /dup ===") {
		t.Fatalf("pair should print the header:\n%s", got)
	}

	if strings.Contains(got, "[field cmds differs]") {
		t.Fatalf("exact counterparts exist on both sides; --pair must not report a spurious cmds delta:\n%s", got)
	}
}

func TestDumpDiffPair_DuplicateOutputsOneSiblingDiffersReportsResidual(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")
	out := filepath.Join(dir, "o.txt")

	line := func(selfUID, mode string) string {
		return `{"self_uid":"` + selfUID + `","uid":"` + selfUID + `","outputs":["/dup"],"deps":[],"inputs":[],"cmds":[{"cmd_args":["ragel","` + mode + `"]}],"tags":[]` +
			`,"kv":{"p":"R6"},"env":{},"platform":"linux","requirements":{},"target_properties":{}}`
	}

	throw(os.WriteFile(left, []byte(line("L-a", "-CG2")+"\n"+line("L-b", "-Bours")+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(line("R-a", "-CG2")+"\n"+line("R-c", "-Cref")+"\n"), 0o644))

	if exc := try(func() {
		cmdDumpDiff(GlobalFlags{}, []string{"--left", left, "--right", right, "--out", out, "--pair", "/dup"})
	}); exc != nil {
		t.Fatalf("pair duplicate outputs control: %v", exc)
	}

	got := string(throw2(os.ReadFile(out)))

	if !strings.Contains(got, "[field cmds differs]") || !strings.Contains(got, "+-Bours") || !strings.Contains(got, "+-Cref") {
		t.Fatalf("a genuinely divergent duplicate sibling must still be reported:\n%s", got)
	}
}

func TestDumpDiffAggregate_DuplicateOutputsExactCounterpartsNoDrift(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")

	line := func(selfUID, mode string) string {
		return `{"self_uid":"` + selfUID + `","uid":"` + selfUID + `","outputs":["/dup"],"deps":[],"inputs":[],"cmds":[{"cmd_args":["ragel","` + mode + `"]}],"tags":[]` +
			`,"kv":{"p":"R6"},"env":{},"platform":"linux","requirements":{},"target_properties":{}}`
	}

	throw(os.WriteFile(left, []byte(line("L-cg2", "-CG2")+"\n"+line("L-ct0", "-CT0")+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(line("R-cg2", "-CG2")+"\n"+line("R-ct0", "-CT0")+"\n"), 0o644))

	run := func(mode ...string) string {
		out := filepath.Join(dir, "o.txt")
		args := append([]string{"--left", left, "--right", right, "--out", out}, mode...)

		if exc := try(func() { cmdDumpDiff(GlobalFlags{}, args) }); exc != nil {
			t.Fatalf("diff %v: %v", mode, exc)
		}

		return string(throw2(os.ReadFile(out)))
	}

	if bf := run("--by-field"); strings.Contains(bf, "cmds") {
		t.Fatalf("exact counterparts exist; --by-field must not count a cmds drift:\n%s", bf)
	}

	if bk := run("--by-kind"); strings.Contains(bk, "cmds:") {
		t.Fatalf("exact counterparts exist; --by-kind must not count a cmds drift:\n%s", bk)
	}

	if bt := run("--by-token"); strings.Contains(bt, "-CG2") || strings.Contains(bt, "-CT0") {
		t.Fatalf("exact counterparts exist; --by-token must not surface mode tokens:\n%s", bt)
	}
}

func TestDumpDiffAggregate_DuplicateOutputsOneSiblingDiffersReportsResidual(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")

	line := func(selfUID, mode string) string {
		return `{"self_uid":"` + selfUID + `","uid":"` + selfUID + `","outputs":["/dup"],"deps":[],"inputs":[],"cmds":[{"cmd_args":["ragel","` + mode + `"]}],"tags":[]` +
			`,"kv":{"p":"R6"},"env":{},"platform":"linux","requirements":{},"target_properties":{}}`
	}

	throw(os.WriteFile(left, []byte(line("L-a", "-CG2")+"\n"+line("L-b", "-Bours")+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(line("R-a", "-CG2")+"\n"+line("R-c", "-Cref")+"\n"), 0o644))

	run := func(mode ...string) string {
		out := filepath.Join(dir, "o.txt")
		args := append([]string{"--left", left, "--right", right, "--out", out}, mode...)

		if exc := try(func() { cmdDumpDiff(GlobalFlags{}, args) }); exc != nil {
			t.Fatalf("diff %v: %v", mode, exc)
		}

		return string(throw2(os.ReadFile(out)))
	}

	bt := run("--by-token")

	if !strings.Contains(bt, "-Bours") || !strings.Contains(bt, "-Cref") {
		t.Fatalf("residual sibling drift must surface in --by-token:\n%s", bt)
	}

	if strings.Contains(bt, "-CG2") {
		t.Fatalf("the exact-counterpart sibling -CG2 must be cancelled, not reported:\n%s", bt)
	}

	if bf := run("--by-field"); !strings.Contains(bf, "cmds") {
		t.Fatalf("residual sibling drift must surface in --by-field:\n%s", bf)
	}

	if bk := run("--by-kind"); !strings.Contains(bk, "cmds:1") {
		t.Fatalf("--by-kind must count exactly the one residual cmds divergence:\n%s", bk)
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

	throw(os.WriteFile(left, []byte(node("Ps", "Pu", "/p", `["Cu"]`, `["a"]`)+"\n"+node("Cs", "Cu", "/c", `[]`, `["a"]`)+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(node("Ps2", "Pu2", "/p", `["Cu2"]`, `[]`)+"\n"+node("Cs2", "Cu2", "/c", `[]`, `[]`)+"\n"), 0o644))

	if exc := try(func() {
		cmdDumpDiff(GlobalFlags{}, []string{"--left", left, "--right", right, "--out", out, "--roots"})
	}); exc != nil {
		t.Fatalf("roots: %v", exc)
	}

	got := string(throw2(os.ReadFile(out)))

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

	throw(os.WriteFile(left, []byte(line("same-host", "L-host", true)+"\n"+line("left-target", "L-target", false)+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(line("same-host", "R-host", true)+"\n"+line("right-target", "R-target", false)+"\n"), 0o644))

	if exc := try(func() {
		cmdDumpDiff(GlobalFlags{}, []string{"--left", left, "--right", right, "--out", out, "--roots"})
	}); exc != nil {
		t.Fatalf("roots duplicate outputs: %v", exc)
	}

	got := string(throw2(os.ReadFile(out)))

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

	throw(os.WriteFile(left, []byte(leftNode+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(rightNode+"\n"), 0o644))

	if exc := try(func() {
		cmdDumpDiff(GlobalFlags{}, []string{"--left", left, "--right", right, "--out", out, "--roots"})
	}); exc != nil {
		t.Fatalf("roots partial-overlap multi-output: %v", exc)
	}

	got := string(throw2(os.ReadFile(out)))

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

func TestDumpDiffByTokenRoots(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")
	out := filepath.Join(dir, "o.txt")

	n := func(su, uid, output, deps, tok string) string {
		return `{"self_uid":"` + su + `","uid":"` + uid + `","outputs":["` + output + `"],"deps":` + deps +
			`,"inputs":[],"cmds":[{"cmd_args":["cc","` + tok + `"]}],"tags":[],"kv":{"p":"CC"},` +
			`"env":{},"platform":"linux","requirements":{},"target_properties":{}}`
	}

	throw(os.WriteFile(left, []byte(
		n("LP", "P", "/p", `["C"]`, "PARENT_OURS")+"\n"+n("LC", "C", "/c", `[]`, "CHILD_OURS")+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(
		n("RP", "P", "/p", `["C"]`, "PARENT_REF")+"\n"+n("RC", "C", "/c", `[]`, "CHILD_REF")+"\n"), 0o644))

	if exc := try(func() {
		cmdDumpDiff(GlobalFlags{}, []string{"--left", left, "--right", right, "--out", out, "--by-token", "--roots"})
	}); exc != nil {
		t.Fatalf("by-token --roots: %v", exc)
	}

	got := string(throw2(os.ReadFile(out)))

	if !strings.Contains(got, "CHILD_OURS") || !strings.Contains(got, "CHILD_REF") {
		t.Fatalf("by-token --roots should count the leaf child tokens:\n%s", got)
	}

	if strings.Contains(got, "PARENT_OURS") || strings.Contains(got, "PARENT_REF") {
		t.Fatalf("by-token --roots leaked non-leaf parent tokens:\n%s", got)
	}
}

func TestDumpDiffByTokenGroup(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")
	out := filepath.Join(dir, "o.txt")

	n := func(su, kind, output, tok string) string {
		return `{"self_uid":"` + su + `","uid":"` + su + `","outputs":["` + output + `"],"deps":[],` +
			`"inputs":[],"cmds":[{"cmd_args":["cc","` + tok + `"]}],"tags":[],"kv":{"p":"` + kind + `"},` +
			`"env":{},"platform":"linux","requirements":{},"target_properties":{}}`
	}

	throw(os.WriteFile(left, []byte(
		n("LA", "CC", "$(B)/dirA/a.o", "TOKA_OURS")+"\n"+n("LB", "PB", "$(B)/dirB/b.o", "TOKB_OURS")+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(
		n("RA", "CC", "$(B)/dirA/a.o", "TOKA_REF")+"\n"+n("RB", "PB", "$(B)/dirB/b.o", "TOKB_REF")+"\n"), 0o644))

	if exc := try(func() {
		cmdDumpDiff(GlobalFlags{}, []string{"--left", left, "--right", right, "--out", out, "--by-token", "--group", "kind,dir"})
	}); exc != nil {
		t.Fatalf("by-token --group: %v", exc)
	}

	got := string(throw2(os.ReadFile(out)))

	for _, want := range []string{"kind=CC dir=dirA", "kind=PB dir=dirB", "TOKA_OURS", "TOKB_REF"} {
		if !strings.Contains(got, want) {
			t.Fatalf("by-token --group missing %q:\n%s", want, got)
		}
	}

	ccIdx := strings.Index(got, "kind=CC dir=dirA")
	pbIdx := strings.Index(got, "kind=PB dir=dirB")
	tokAIdx := strings.Index(got, "TOKA_OURS")
	tokBIdx := strings.Index(got, "TOKB_OURS")
	lo, hi := ccIdx, pbIdx

	if hi < lo {
		lo, hi = hi, lo
	}

	inFirst := func(i int) bool { return i > lo && i < hi }

	if inFirst(tokAIdx) == inFirst(tokBIdx) {
		t.Fatalf("group sections did not separate TOKA from TOKB:\n%s", got)
	}
}

func TestDumpDiffPairStructuredCmds(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "l.jsonl")
	right := filepath.Join(dir, "r.jsonl")
	out := filepath.Join(dir, "o.txt")

	n := func(su, cwd, args string) string {
		return `{"self_uid":"` + su + `","uid":"` + su + `","outputs":["/s"],"deps":[],"inputs":[],` +
			`"cmds":[{"cmd_args":` + args + `,"cwd":"` + cwd + `"}],"tags":[],"kv":{"p":"CC"},` +
			`"env":{},"platform":"linux","requirements":{},"target_properties":{}}`
	}

	throw(os.WriteFile(left, []byte(n("L", "/wd_ours", `["cc","-c","a","b"]`)+"\n"), 0o644))
	throw(os.WriteFile(right, []byte(n("R", "/wd_ref", `["cc","-c","b","a"]`)+"\n"), 0o644))

	if exc := try(func() {
		cmdDumpDiff(GlobalFlags{}, []string{"--left", left, "--right", right, "--out", out, "--pair", "/s"})
	}); exc != nil {
		t.Fatalf("pair structured cmds: %v", exc)
	}

	got := string(throw2(os.ReadFile(out)))

	if !strings.Contains(got, "[field cmds differs]") {
		t.Fatalf("pair should report cmds differ:\n%s", got)
	}

	if !strings.Contains(got, "cwd: ours=/wd_ours ref=/wd_ref") {
		t.Fatalf("pair should expose structured cwd difference:\n%s", got)
	}

	if !strings.Contains(got, "arg order") || !strings.Contains(got, "ours: cc -c a b") || !strings.Contains(got, "ref:  cc -c b a") {
		t.Fatalf("pair should expose per-cmd arg ordering difference:\n%s", got)
	}
}

func TestCanonInputs_ArchiveByKeysIgnoresKeyListBasename(t *testing.T) {
	node := &RawNode{
		Kv: map[string]any{"p": "AR"},
		Inputs: []string{
			"$(B)/mod/a.raw",
			"$(B)/mod/sub/b.raw",
			"$(S)/mod/a.lua",
			"$(S)/mod/sub/b.lua",
			"$(B)/tools/archiver/archiver",
		},
		Cmds: []any{map[string]any{"cmd_args": []any{
			"$(B)/tools/archiver/archiver", "-q", "-x", "-p",
			"$(B)/mod/a.raw", "$(B)/mod/sub/b.raw",
			"-k", "a.lua:sub/b.lua",
			"-o", "$(B)/mod/LuaScripts.inc",
		}}},
	}

	got := canonInputs(node, true)
	has := func(s string) bool {
		for _, g := range got {
			if g == s {
				return true
			}
		}

		return false
	}

	for _, want := range []string{"$(B)/mod/a.raw", "$(B)/mod/sub/b.raw", "$(B)/tools/archiver/archiver"} {
		if !has(want) {
			t.Errorf("canonInputs dropped command-named member %q; got %v", want, got)
		}
	}

	for _, drop := range []string{"$(S)/mod/a.lua", "$(S)/mod/sub/b.lua"} {
		if has(drop) {
			t.Errorf("canonInputs kept source lua %q named only via the -k key list; got %v", drop, got)
		}
	}
}
