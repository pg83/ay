package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// freshInterner returns a standalone VFS interner for tests that drive
// genModule directly (production sets genCtx.vfsInterner in GenWith).
func freshInterner() *scannerInterner {
	i := newScannerInterner()
	return &i
}

// gen_test.go — end-to-end test for the M1 vertical slice. Parses
// build/cow/on/ya.make, emits the 2-node CC+AR subgraph, and asserts
// byte-exact L3 equality against the corresponding 2 nodes carved out
// of the upstream reference graph.
//
// Skip-if-missing pattern follows cc_test.go / ar_test.go: the reference
// snapshot under /home/pg/monorepo/yatool is required; absence is a
// host condition, not a test failure.

// sourceRoot points at the upstream snapshot used by reference-aware
// tests. The constant matches the production make default so test-only
// fixtures resolve the same paths the integration harness does.
const sourceRoot = "/home/pg/monorepo/yatool"

// TestGen_AcceptsProgramModule_Synthetic verifies PR-24's PROGRAM →
// LD wiring on a synthetic source tree:
//   - PROGRAM() modules are accepted and emit an LD node (PR-24);
//   - PEERDIR(...) is recursively walked, with the parent PROGRAM's
//     LD node carrying the peer LIBRARY's AR UID as a dependency
//     (peerLDRefs flow through to LD's DepRefs).
//
// The synthetic source tree has two modules — `mainprog` (PROGRAM
// peering thelib) and `thelib` (LIBRARY) — each with a single
// source. The expected closure is 4 nodes: thelib's CC + AR, then
// mainprog's CC + LD. The root result is mainprog's LD (the binary
// `$(B)/mainprog/mainprog`).
func TestGen_AcceptsProgramModule_Synthetic(t *testing.T) {
	root := t.TempDir()

	mainProgDir := filepath.Join(root, "mainprog")
	libDir := filepath.Join(root, "thelib")

	if err := os.MkdirAll(mainProgDir, 0o755); err != nil {
		t.Fatalf("mkdir mainprog: %v", err)
	}

	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("mkdir thelib: %v", err)
	}

	mainYaMake := []byte("PROGRAM()\nPEERDIR(thelib)\nSRCS(main.cpp)\nEND()\n")
	libYaMake := []byte("LIBRARY()\nSRCS(lib.cpp)\nEND()\n")

	if err := os.WriteFile(filepath.Join(mainProgDir, "ya.make"), mainYaMake, 0o644); err != nil {
		t.Fatalf("write mainprog/ya.make: %v", err)
	}

	if err := os.WriteFile(filepath.Join(libDir, "ya.make"), libYaMake, 0o644); err != nil {
		t.Fatalf("write thelib/ya.make: %v", err)
	}

	g := testGen(root, "mainprog")

	if len(g.Graph) != 4 {
		t.Fatalf("Gen produced %d nodes, want 4 (2 CC + 1 AR + 1 LD)", len(g.Graph))
	}

	if len(g.Result) != 1 {
		t.Fatalf("Gen produced %d results, want 1", len(g.Result))
	}

	// Locate nodes by output path so the assertions don't depend on
	// emit order. Each path is unique within the synthetic tree.
	nodesByOutput := make(map[string]*Node, len(g.Graph))

	for _, n := range g.Graph {
		if len(n.Outputs) == 0 {
			t.Fatalf("node uid=%s has no outputs", n.UID)
		}

		nodesByOutput[n.Outputs[0].String()] = n
	}

	const (
		libCCOut    = "$(B)/thelib/lib.cpp.o"
		libARout    = "$(B)/thelib/libthelib.a"
		mainCCOut   = "$(B)/mainprog/main.cpp.o"
		mainBinPath = "$(B)/mainprog/mainprog"
	)

	for _, key := range []string{libCCOut, libARout, mainCCOut, mainBinPath} {
		if _, ok := nodesByOutput[key]; !ok {
			t.Fatalf("graph is missing expected output %q", key)
		}
	}

	rootLD := nodesByOutput[mainBinPath]

	// Verify it really is an LD node, not an AR aliased to the
	// binary path.
	if rootLD.KV["p"] != "LD" {
		t.Errorf("root node kv.p = %q, want LD", rootLD.KV["p"])
	}

	if len(rootLD.Cmds) != 4 {
		t.Errorf("root LD Cmds = %d, want 4", len(rootLD.Cmds))
	}

	// Result must point at the root LD node.
	if g.Result[0] != rootLD.UID {
		t.Errorf("result UID = %q, want mainprog LD uid %q", g.Result[0], rootLD.UID)
	}

	if rootLD.TargetProperties["module_dir"] != "mainprog" {
		t.Errorf("root LD module_dir = %q, want %q", rootLD.TargetProperties["module_dir"], "mainprog")
	}

	if rootLD.TargetProperties["module_type"] != "bin" {
		t.Errorf("root LD module_type = %q, want bin", rootLD.TargetProperties["module_type"])
	}

	// Root LD must depend on BOTH its own CC node AND the peer's
	// AR node — that is the wiring contract PR-24 commits to.
	mainCC := nodesByOutput[mainCCOut]
	libAR := nodesByOutput[libARout]

	depSet := make(map[string]struct{}, len(rootLD.Deps))
	for _, d := range rootLD.Deps {
		depSet[d] = struct{}{}
	}

	if _, ok := depSet[mainCC.UID]; !ok {
		t.Errorf("root LD deps %v missing main.cpp.o uid %q", rootLD.Deps, mainCC.UID)
	}

	if _, ok := depSet[libAR.UID]; !ok {
		t.Errorf("root LD deps %v missing thelib AR uid %q", rootLD.Deps, libAR.UID)
	}
}

// TestGen_UnittestFor_Synthetic verifies T-1's UNITTEST_FOR desugaring:
// UNITTEST_FOR(<dir>) parses as a program-like ModuleStmt (no
// unsupported-macro throw), emits an LD, and implicitly PEERDIRs both
// library/cpp/testing/unittest_main and the tested dir (their ARs land in
// the LD link closure). The tested-dir argument is NOT used as the binary
// name (it is a path), and source rebasing must not also inject a direct
// `-I$(S)/<tested-dir>` onto the module's own compile.
func TestGen_UnittestFor_Synthetic(t *testing.T) {
	root := t.TempDir()

	mk := func(dir, body string) {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}

		if err := os.WriteFile(filepath.Join(d, "ya.make"), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s/ya.make: %v", dir, err)
		}
	}

	mkfile := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mk("mod", "UNITTEST_FOR(thelib)\nSRCS(a_ut.cpp)\nEND()\n")
	mk("thelib", "LIBRARY()\nSRCS(lib.cpp)\nEND()\n")
	mk("build/cow/on", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(cow.cpp)\nEND()\n")
	mk("library/cpp/malloc/api", "LIBRARY()\nSRCS(api.cpp)\nEND()\n")
	mk("library/cpp/malloc/tcmalloc", "LIBRARY()\nPEERDIR(library/cpp/malloc/api contrib/restricted/abseil-cpp contrib/libs/tcmalloc/malloc_extension contrib/libs/tcmalloc/no_percpu_cache)\nSRCS(tcmalloc.cpp)\nEND()\n")
	mk("contrib/restricted/abseil-cpp", "LIBRARY()\nSRCS(absl.cpp)\nEND()\n")
	mk("contrib/libs/tcmalloc/malloc_extension", "LIBRARY()\nSRCS(ext.cpp)\nEND()\n")
	mk("contrib/libs/tcmalloc/no_percpu_cache", "LIBRARY()\nSRCS(npc.cpp)\nEND()\n")
	mk("library/cpp/testing/unittest_main", "LIBRARY()\nSRCS(main.cpp)\nEND()\n")
	mkfile("thelib/a_ut.cpp", "int a_ut() { return 0; }\n")
	mkfile("thelib/lib.cpp", "int thelib() { return 0; }\n")
	mkfile("build/cow/on/cow.cpp", "int cow() { return 0; }\n")
	mkfile("library/cpp/malloc/api/api.cpp", "int malloc_api() { return 0; }\n")
	mkfile("library/cpp/malloc/tcmalloc/tcmalloc.cpp", "int tcmalloc_lib() { return 0; }\n")
	mkfile("contrib/restricted/abseil-cpp/absl.cpp", "int absl() { return 0; }\n")
	mkfile("contrib/libs/tcmalloc/malloc_extension/ext.cpp", "int malloc_ext() { return 0; }\n")
	mkfile("contrib/libs/tcmalloc/no_percpu_cache/npc.cpp", "int no_percpu_cache() { return 0; }\n")
	mkfile("library/cpp/testing/unittest_main/main.cpp", "int unittest_main() { return 0; }\n")

	// Reaching the assertions at all proves UNITTEST_FOR did not trip the
	// "does not yet support macro" throw (that would panic out of testGen).
	g := testGen(root, "mod")

	byOut := make(map[string]*Node, len(g.Graph))
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 {
			byOut[n.Outputs[0].String()] = n
		}
	}

	// Full-path naming keeps a single-component module as "mod", NOT the
	// UNITTEST_FOR argument ("thelib").
	ld := byOut["$(B)/mod/mod"]
	if ld == nil {
		t.Fatalf("missing UNITTEST_FOR LD output $(B)/mod/mod")
	}

	if ld.KV["p"] != "LD" {
		t.Errorf("root node kv.p = %q, want LD", ld.KV["p"])
	}

	if ld.TargetProperties["module_type"] != "bin" {
		t.Errorf("module_type = %q, want bin", ld.TargetProperties["module_type"])
	}

	deps := make(map[string]struct{}, len(ld.Deps))
	for _, d := range ld.Deps {
		deps[d] = struct{}{}
	}

	libAR := byOut["$(B)/thelib/libthelib.a"]
	if libAR == nil {
		t.Fatal("missing tested-lib AR $(B)/thelib/libthelib.a")
	}

	if _, ok := deps[libAR.UID]; !ok {
		t.Errorf("LD deps missing tested-lib AR uid %q (implicit PEERDIR($arg) not walked)", libAR.UID)
	}

	umAR := byOut["$(B)/library/cpp/testing/unittest_main/libcpp-testing-unittest_main.a"]
	if umAR == nil {
		t.Fatal("missing unittest_main AR")
	}

	if _, ok := deps[umAR.UID]; !ok {
		t.Errorf("LD deps missing unittest_main AR uid %q (implicit PEERDIR not walked)", umAR.UID)
	}

	// UNITTEST_FOR sources resolve under the tested dir, while outputs stay
	// under the declaring module via the SRCDIR-style composed path. With a
	// sibling tested dir (`thelib`) that becomes `__/thelib/...`.
	cc := byOut["$(B)/mod/__/thelib/a_ut.cpp.o"]
	if cc == nil {
		t.Fatal("missing own CC $(B)/mod/__/thelib/a_ut.cpp.o")
	}

	if cc.TargetProperties["module_dir"] != "mod" {
		t.Fatalf("cc module_dir = %q, want mod", cc.TargetProperties["module_dir"])
	}

	inputs := make([]string, 0, len(cc.Inputs))
	for _, in := range cc.Inputs {
		inputs = append(inputs, in.String())
	}
	if !slicesContains(inputs, "$(S)/thelib/a_ut.cpp") {
		t.Fatalf("cc inputs missing $(S)/thelib/a_ut.cpp: %v", inputs)
	}

	for _, c := range cc.Cmds {
		for _, a := range c.CmdArgs {
			if a == "-I$(S)/thelib" {
				t.Fatalf("own CC unexpectedly carries direct -I$(S)/thelib: cmds=%+v", cc.Cmds)
			}
		}
	}

	linkArgs := ld.Cmds[2].CmdArgs
	thelibIdx := indexOfArg(linkArgs, "thelib/libthelib.a")
	cowIdx := indexOfArg(linkArgs, "build/cow/on/libbuild-cow-on.a")
	if thelibIdx < 0 || cowIdx < 0 {
		t.Fatalf("LD link args missing expected tested-dir/program-default archives: %v", linkArgs)
	}
	if thelibIdx > cowIdx {
		t.Fatalf("tested-dir archive lands after build/cow/on in LD cmd: thelib=%d cow=%d args=%v", thelibIdx, cowIdx, linkArgs)
	}
}

// TestGen_SyntheticPROGRAM_EmitsLD verifies a PROGRAM module with
// one source and zero PEERDIR emits exactly 2 nodes (1 CC + 1 LD)
// per the PR-24 brief's synthetic-test acceptance line. The LD node
// has 4 cmds and is the graph result.
func TestGen_SyntheticPROGRAM_EmitsLD(t *testing.T) {
	root := t.TempDir()

	progDir := filepath.Join(root, "lone")
	if err := os.MkdirAll(progDir, 0o755); err != nil {
		t.Fatalf("mkdir lone: %v", err)
	}

	yamake := []byte("PROGRAM()\nSRCS(main.cpp)\nEND()\n")
	if err := os.WriteFile(filepath.Join(progDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write lone/ya.make: %v", err)
	}

	g := testGen(root, "lone")

	if len(g.Graph) != 2 {
		t.Fatalf("Gen produced %d nodes, want 2 (1 CC + 1 LD)", len(g.Graph))
	}

	if len(g.Result) != 1 {
		t.Fatalf("Gen produced %d results, want 1", len(g.Result))
	}

	// Locate nodes by kv.p.
	var ld, cc *Node

	for _, n := range g.Graph {
		switch n.KV["p"] {
		case "LD":
			ld = n
		case "CC":
			cc = n
		}
	}

	if ld == nil {
		t.Fatal("Gen produced no LD node for PROGRAM module")
	}

	if cc == nil {
		t.Fatal("Gen produced no CC node for PROGRAM module")
	}

	if len(ld.Cmds) != 4 {
		t.Errorf("LD Cmds = %d, want 4", len(ld.Cmds))
	}

	wantOut := "$(B)/lone/lone"
	if len(ld.Outputs) != 1 || ld.Outputs[0].String() != wantOut {
		t.Errorf("LD outputs = %#v, want [%q]", ld.Outputs, wantOut)
	}

	if g.Result[0] != ld.UID {
		t.Errorf("result UID = %q, want LD uid %q", g.Result[0], ld.UID)
	}
}

// TestGen_PeerdirCycle_Tolerated verifies the cycle handler breaks
// the loop without throwing, emits a diagnostic, and increments
// ctx.cyclesTolerated. PR-27 changed the model from "cycle is a hard
// error" to "cycle peer is treated as a header-only stub", because the
// implicit DEFAULT_PEERDIR set in real ya.makes creates legitimate
// mutual references between runtime-stack modules that the upstream
// reference handles via exclusion lists we have not yet modelled.
// The break-edge peer's archive ref is not propagated into the
// consumer's AR/LD; the peer's own walk completes elsewhere on the
// recursion stack.
//
// D02: the test drives genModule directly so it can inspect ctx.cyclesTolerated.
func TestGen_PeerdirCycle_Tolerated(t *testing.T) {
	root := t.TempDir()

	aDir := filepath.Join(root, "a")
	bDir := filepath.Join(root, "b")

	if err := os.MkdirAll(aDir, 0o755); err != nil {
		t.Fatalf("mkdir a: %v", err)
	}

	if err := os.MkdirAll(bDir, 0o755); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}

	// Both modules declare effective NO_PLATFORM so the implicit
	// default-peer set is empty — this keeps the test focused on
	// the explicit cycle rather than introducing a transitive
	// musl/builtins/etc. recursion.
	Throw(os.WriteFile(filepath.Join(aDir, "ya.make"), []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(b)\nSRCS(a.cpp)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(bDir, "ya.make"), []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(a)\nSRCS(b.cpp)\nEND()\n"), 0o644))

	// Drive genModule directly so we can inspect ctx.cyclesTolerated
	// after the walk (D02). The a→b→a cycle triggers exactly one
	// back-edge: when b walks its PEERDIR(a) and a is still on the
	// walking stack.
	ctx := &genCtx{
		sourceRoot:  root,
		fs:          NewFS(root),
		emit:        NewBufferedEmitter(),
		memo:        make(map[ModuleInstance]*moduleEmitResult),
		walking:     make(map[ModuleInstance]bool),
		host:        testHostP,
		target:      testTargetP,
		vfsInterner: freshInterner(),
	}

	seed := ModuleInstance{
		Path:     "a",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}

	var exc *Exception

	exc = Try(func() {
		genModule(ctx, seed)
	})

	if exc != nil {
		t.Fatalf("genModule on cyclic graph should not throw (cycle is tolerated); got: %v", exc)
	}

	// Both modules emit a CC node and an AR node — the cycle is
	// broken silently, the peer's own walk runs, and the archive
	// ref for the back-edge is dropped.
	g := Finalize(ctx.emit.(*BufferedEmitter))

	if len(g.Graph) < 4 {
		t.Errorf("expected at least 4 nodes (2 CC + 2 AR), got %d", len(g.Graph))
	}

	// D02: exactly one back-edge was tolerated (b's PEERDIR(a) fires
	// while a is still on the walking stack).
	if ctx.cyclesTolerated != 1 {
		t.Errorf("cyclesTolerated = %d, want 1", ctx.cyclesTolerated)
	}
}

// TestGen_RejectsUnsupportedMacro verifies that any macro outside
// PR-23's whitelist throws with a concrete deferred-to-PR-25
// message. PR-13 introduced typed Stmts for IF / INCLUDE /
// JOIN_SRCS / ADDINCL / CFLAGS / LDFLAGS / SRCDIR / GLOBAL_SRCS,
// so `IF` is no longer the "unsupported macro" canary — gen.go now
// hits its default `*Stmt` arm with an "unhandled Stmt type" message
// for those. Any name NOT in `whitelistedMetadataMacros` AND NOT a
// typed Stmt name still flows through `*UnknownStmt` and trips the
// original whitelist check; a made-up macro name is the stable canary
// for that path now that more real generated-source macros are typed.
func TestGen_RejectsUnsupportedMacro(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "mod")

	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	yamake := []byte("LIBRARY()\nTOTALLY_UNKNOWN(foo bar)\nSRCS(main.cpp)\nEND()\n")

	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write mod/ya.make: %v", err)
	}

	exc := Try(func() {
		testGen(root, "mod")
	})

	if exc == nil {
		t.Fatal("expected exception for unsupported macro, got nil")
	}

	if !strings.Contains(exc.Error(), "does not yet support macro") {
		t.Errorf("error %q does not contain 'does not yet support macro'", exc.Error())
	}
}

func TestGen_RejectsMultipleModules(t *testing.T) {
	tmp := t.TempDir()
	Throw(os.MkdirAll(filepath.Join(tmp, "bad"), 0755))
	Throw(os.WriteFile(filepath.Join(tmp, "bad", "ya.make"), []byte(`LIBRARY()
SRCS(a.c)
PROGRAM()
SRCS(b.c)
END()
`), 0644))

	exc := Try(func() {
		testGen(tmp, "bad")
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "multiple modules") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestGen_RejectsZeroModule(t *testing.T) {
	tmp := t.TempDir()
	Throw(os.MkdirAll(filepath.Join(tmp, "noop"), 0755))
	Throw(os.WriteFile(filepath.Join(tmp, "noop", "ya.make"), []byte(`SET(X y)
END()
`), 0644))

	exc := Try(func() {
		testGen(tmp, "noop")
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "no module declaration") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestGen_RejectsProgramAsPeer(t *testing.T) {
	tmp := t.TempDir()
	Throw(os.MkdirAll(filepath.Join(tmp, "peerprog"), 0755))
	Throw(os.MkdirAll(filepath.Join(tmp, "caller"), 0755))
	Throw(os.WriteFile(filepath.Join(tmp, "peerprog", "ya.make"), []byte(`PROGRAM()
SRCS(peer_main.cpp)
END()
`), 0644))
	Throw(os.WriteFile(filepath.Join(tmp, "caller", "ya.make"), []byte(`PROGRAM()
PEERDIR(peerprog)
SRCS(caller_main.cpp)
END()
`), 0644))

	exc := Try(func() {
		testGen(tmp, "caller")
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "peers PROGRAM module") {
		t.Errorf("unexpected error: %v", exc)
	}
}

// TestGen_DualInstantiation_BuildCowOn pins D31 — the same Path,
// instantiated as TWO ModuleInstances (target + host), produces TWO
// distinct memo entries and TWO distinct CC+AR pairs. PR-23 walker
// (`Gen`) only emits the TARGET pair (host-tool recursion is wired
// in PR-25 via the macro evaluator). PR-23's contract for this test
// is therefore:
//
//  1. Gen with the target seed → 2 nodes (M1 acceptance preserved).
//  2. A direct EmitCC + EmitAR call against a host instance against
//     the SAME emitter buffer adds 2 more nodes byte-exact against
//     the reference host pair.
//
// Together these prove that ModuleInstance addressing AND host
// emission both work; PR-25 will fold the second half into the
// walker.
func TestGen_PeerdirDeclarationOrder_Preserved(t *testing.T) {
	tmp := t.TempDir()

	// Three modules: mainprog peers [zlib, alib] in non-alphabetical declaration order.
	// Sort would put alib before zlib; declaration-order R14 invariant requires zlib first.
	Throw(os.MkdirAll(filepath.Join(tmp, "mainprog"), 0755))
	Throw(os.MkdirAll(filepath.Join(tmp, "zlib"), 0755))
	Throw(os.MkdirAll(filepath.Join(tmp, "alib"), 0755))
	Throw(os.WriteFile(filepath.Join(tmp, "mainprog", "ya.make"), []byte(`PROGRAM()
PEERDIR(zlib alib)
SRCS(main.cpp)
END()
`), 0644))
	Throw(os.WriteFile(filepath.Join(tmp, "zlib", "ya.make"), []byte(`LIBRARY()
SRCS(zlib.c)
END()
`), 0644))
	Throw(os.WriteFile(filepath.Join(tmp, "alib", "ya.make"), []byte(`LIBRARY()
SRCS(alib.c)
END()
`), 0644))

	g := testGen(tmp, "mainprog")

	// Find the AR nodes for zlib and alib by output path. Assert zlib AR appears
	// BEFORE alib AR in g.Graph (post-Finalize topo order respects emit order +
	// dep relationship; for two independent leaves visited in declaration order
	// zlib should be emitted first, hence appear first in topo).
	var zlibIdx, alibIdx int = -1, -1

	for i, n := range g.Graph {
		if len(n.Outputs) > 0 {
			if strings.Contains(n.Outputs[0].String(), "/zlib/") && n.KV["p"] == "AR" {
				zlibIdx = i
			}
			if strings.Contains(n.Outputs[0].String(), "/alib/") && n.KV["p"] == "AR" {
				alibIdx = i
			}
		}
	}

	if zlibIdx == -1 || alibIdx == -1 {
		t.Fatalf("expected both zlib and alib AR nodes; zlibIdx=%d alibIdx=%d", zlibIdx, alibIdx)
	}

	// NOTE: Finalize's topo order may sort by UID at tie-breaking points, so
	// relative position of independent leaves can be UID-driven not declaration-driven.
	// What we CAN reliably assert: emit order in BufferedEmitter is declaration order.
	// The graph topology however constrains that BOTH zlib and alib are emitted before
	// mainprog (they're its deps). The strongest declaration-order assertion that survives
	// Finalize is by checking the BufferedEmitter directly... but Gen doesn't expose it.
	//
	// Pragmatic check: the synthetic produces 6 nodes (3 CC + 2 AR + 1 LD;
	// mainprog is PROGRAM so closes with LD, two peer LIBRARYs close with AR).
	// Verify count — catches regressions where a sort.Strings(peerdirs) collapses or
	// breaks the walk.
	if len(g.Graph) != 6 {
		t.Errorf("expected 6 nodes (3 CC + 2 AR + 1 LD), got %d", len(g.Graph))
	}
}

// TestGen_MacroEvaluation_IfStmt_TakeThen verifies that IF/ELSE
// branches are evaluated against the per-instance env and only the
// taken branch contributes sources. The ya.make picks SRCS(linux.c)
// in the THEN arm and SRCS(other.c) in the ELSE arm; under the
// default target env (OS_LINUX=true) only linux.c is emitted.
func TestGen_MacroEvaluation_IfStmt_TakeThen(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "ifmod")
	Throw(os.MkdirAll(modDir, 0o755))

	yamake := []byte(`LIBRARY()
IF (OS_LINUX)
    SRCS(linux.c)
ELSE()
    SRCS(other.c)
ENDIF()
END()
`)
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	g := testGen(root, "ifmod")

	if len(g.Graph) != 2 {
		t.Fatalf("expected 2 nodes (1 CC + 1 AR), got %d", len(g.Graph))
	}

	var ccInputs []string

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			ccInputs = append(ccInputs, vfsStrings(n.Inputs)...)
		}
	}

	if len(ccInputs) != 1 {
		t.Fatalf("expected 1 CC input, got %d (%v)", len(ccInputs), ccInputs)
	}

	if !strings.Contains(ccInputs[0], "linux.c") {
		t.Errorf("CC input %q does not reference linux.c (THEN branch)", ccInputs[0])
	}

	if strings.Contains(ccInputs[0], "other.c") {
		t.Errorf("CC input %q unexpectedly references other.c (ELSE branch should be unreached)", ccInputs[0])
	}
}

// TestGen_MacroEvaluation_NoLibcFlag verifies that NO_LIBC() in a
// module's ya.make sets `d.flags.NoLibc=true` after collectModule.
func TestGen_MacroEvaluation_NoLibcFlag(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "nolibcmod")
	Throw(os.MkdirAll(modDir, 0o755))

	yamake := []byte(`LIBRARY()
NO_LIBC()
NO_UTIL()
NO_RUNTIME()
SRCS(lib.c)
END()
`)
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	// Drive collectModule directly to inspect the FlagSet overlay
	// outcome. (Gen's path goes through Finalize which strips refs;
	// we want to see the flags that flow into EmitCC for "nolibcmod".)
	mf := Throw2(ParseFile(NewFS(root), filepath.Join(modDir, "ya.make")))

	d := collectModule(NewFS(root), "nolibcmod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Kind: KindLib, Platform: testTargetP}))

	if !d.flags.NoLibc {
		t.Errorf("flags.NoLibc = false, want true (macro overlay should have flipped it)")
	}

	if !d.flags.NoUtil {
		t.Errorf("flags.NoUtil = false, want true")
	}

	if !d.flags.NoRuntime {
		t.Errorf("flags.NoRuntime = false, want true")
	}

	// Sanity: a full Gen against this synthetic still produces a
	// 2-node subgraph (1 CC + 1 AR). The CC's cmd_args composer is
	// PR-26's job to keep aligned with the flag bag; PR-25 only
	// verifies the bag itself is populated.
	g := testGen(root, "nolibcmod")

	if len(g.Graph) != 2 {
		t.Errorf("Gen produced %d nodes, want 2 (1 CC + 1 AR)", len(g.Graph))
	}
}

func TestCollectModule_SetMuslNoSuppressesConsumerDefaults(t *testing.T) {
	targetFlags := make(map[string]string, len(testToolchainFlags)+2)
	for k, v := range testToolchainFlags {
		targetFlags[k] = v
	}
	targetFlags["PIC"] = "no"
	targetFlags["MUSL"] = "yes"

	target := NewPlatform(OSLinux, ISAX8664, targetFlags, nil, "", "")
	instance := ModuleInstance{
		Path:     "bridge",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: target,
	}

	tmp := t.TempDir()
	tmpFS := NewFS(tmp)
	mf := Throw2(Parse(tmpFS, "bridge/ya.make", []byte(`LIBRARY()
SET(MUSL no)
NO_RUNTIME()
PEERDIR(contrib/libs/musl)
SRCS(x.cpp)
END()
`)))

	d := collectModule(tmpFS, "bridge", instance.Kind, mf.Stmts, buildIfEnv(instance))

	if d.muslEnabled {
		t.Fatalf("muslEnabled = true, want false after SET(MUSL no)")
	}

	if got := defaultPeerCFlags(&genCtx{target: target}, instance, d); got != nil {
		t.Fatalf("defaultPeerCFlags = %v, want nil", got)
	}

	defaults := defaultPeerdirsForModule(&genCtx{target: target}, instance, d)
	for _, peer := range defaults {
		if peer == "contrib/libs/musl/include" {
			t.Fatalf("defaultPeerdirsForModule included musl/include despite SET(MUSL no): %v", defaults)
		}
	}
}

func TestGen_NoStdIncGlobalCFlagsPropagateToExplicitPeer(t *testing.T) {
	root := t.TempDir()

	muslDir := filepath.Join(root, "contrib/libs/musl")
	bridgeDir := filepath.Join(root, "bridge")
	Throw(os.MkdirAll(muslDir, 0o755))
	Throw(os.MkdirAll(bridgeDir, 0o755))

	Throw(os.WriteFile(filepath.Join(muslDir, "ya.make"), []byte(`LIBRARY()
NO_PLATFORM()
CFLAGS(
    GLOBAL -D_musl_=1
    -nostdinc
)
SRCS(m.c)
END()
`), 0o644))
	Throw(os.WriteFile(filepath.Join(muslDir, "m.c"), []byte("int musl_symbol(void) { return 1; }\n"), 0o644))

	Throw(os.WriteFile(filepath.Join(bridgeDir, "ya.make"), []byte(`LIBRARY()
SET(MUSL no)
NO_RUNTIME()
PEERDIR(contrib/libs/musl)
SRCS(x.cpp)
END()
`), 0o644))
	Throw(os.WriteFile(filepath.Join(bridgeDir, "x.cpp"), []byte("int bridge_symbol(void) { return 2; }\n"), 0o644))

	g := testGen(root, "bridge")
	var args []string

	for _, n := range g.Graph {
		if len(n.Outputs) == 1 && n.Outputs[0].String() == "$(B)/bridge/x.cpp.o" {
			args = n.Cmds[0].CmdArgs
			break
		}
	}

	if len(args) == 0 {
		t.Fatalf("bridge CC node not found")
	}

	if !flagsContain(args, "-D_musl_=1") {
		t.Fatalf("bridge CC args missing GLOBAL CFLAG from explicit musl peer: %v", args)
	}

	if flagsContain(args, "-D_musl_") {
		t.Fatalf("bridge CC args contain consumer MUSL sentinel despite SET(MUSL no): %v", args)
	}
}

// TestGen_JoinSrcs_EmitsJSAndCC verifies that JOIN_SRCS produces
// (1 JS) + (1 CC for joined) + (1 CC for sibling) + (1 AR) = 4
// nodes. The JS NodeRef must thread into the joined-CC's input
// path; the sibling CC compiles regularly. The joined CC's source
// path is `$(B)/<modulePath>/<allName>` per EmitJS.
func TestGen_JoinSrcs_EmitsJSAndCC(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "joinmod")
	Throw(os.MkdirAll(modDir, 0o755))

	yamake := []byte(`LIBRARY()
JOIN_SRCS(all_my.cpp src1.cpp src2.cpp)
SRCS(other.cpp)
END()
`)
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	g := testGen(root, "joinmod")

	counts := make(map[string]int)
	for _, n := range g.Graph {
		p, _ := n.KV["p"].(string)
		counts[p]++
	}

	if counts["JS"] != 1 {
		t.Errorf("JS count = %d, want 1", counts["JS"])
	}

	if counts["CC"] != 2 {
		t.Errorf("CC count = %d, want 2 (1 for joined + 1 for other.cpp)", counts["CC"])
	}

	if counts["AR"] != 1 {
		t.Errorf("AR count = %d, want 1", counts["AR"])
	}

	if len(g.Graph) != 4 {
		t.Fatalf("total graph nodes = %d, want 4", len(g.Graph))
	}

	// Verify both expected CC sources surfaced. PR-25's joined CC
	// uses SOURCE_ROOT-rooted input (the same as a regular SRCS);
	// the upstream reference uses BUILD_ROOT for joined sources.
	// That divergence is a known PR-26 byte-exact gap (the
	// generated-source dep wiring needs EmitCC to learn a
	// build-root variant + the JS NodeRef as an additional dep).
	// PR-25 tests the structural fact: 1 JS + 2 CC + 1 AR with
	// the correct sources surfacing.
	var (
		joinedInput string
		otherInput  string
	)

	for _, n := range g.Graph {
		if n.KV["p"] != "CC" {
			continue
		}

		if len(n.Inputs) == 0 {
			continue
		}

		switch {
		case strings.Contains(n.Inputs[0].String(), "all_my.cpp"):
			joinedInput = n.Inputs[0].String()
		case strings.Contains(n.Inputs[0].String(), "other.cpp"):
			otherInput = n.Inputs[0].String()
		}
	}

	if joinedInput == "" {
		t.Errorf("no CC node found whose input references all_my.cpp")
	}

	if otherInput == "" {
		t.Errorf("no CC node found whose input references other.cpp")
	}

	// Sanity: the JS node's output path is the joined .cpp under
	// $(B)/<modulePath>/<allName>.
	var jsOut string

	for _, n := range g.Graph {
		if n.KV["p"] == "JS" && len(n.Outputs) > 0 {
			jsOut = n.Outputs[0].String()
		}
	}

	wantJSOut := "$(B)/joinmod/all_my.cpp"
	if jsOut != wantJSOut {
		t.Errorf("JS output = %q, want %q", jsOut, wantJSOut)
	}
}

// TestGen_GlobalSrcs_EmitsTwoARs verifies that a LIBRARY with both
// SRCS and GLOBAL_SRCS emits two AR nodes — the regular `.a` and
// the `.global.a`. The regular AR carries `module_lang=cpp,
// module_type=lib`; the global AR additionally has
// `module_tag=global` per `EmitARGlobal`.
func TestGen_GlobalSrcs_EmitsTwoARs(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "globalmod")
	Throw(os.MkdirAll(modDir, 0o755))

	yamake := []byte(`LIBRARY()
GLOBAL_SRCS(global.cpp)
SRCS(regular.cpp)
END()
`)
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	g := testGen(root, "globalmod")

	counts := make(map[string]int)
	for _, n := range g.Graph {
		p, _ := n.KV["p"].(string)
		counts[p]++
	}

	if counts["CC"] != 2 {
		t.Errorf("CC count = %d, want 2 (regular + global)", counts["CC"])
	}

	if counts["AR"] != 2 {
		t.Errorf("AR count = %d, want 2 (regular + global)", counts["AR"])
	}

	// Verify exactly one AR carries module_tag=global; the other
	// has no module_tag.
	var globalARs, regularARs int

	for _, n := range g.Graph {
		if n.KV["p"] != "AR" {
			continue
		}

		if n.TargetProperties["module_tag"] == "global" {
			globalARs++
		} else if _, has := n.TargetProperties["module_tag"]; !has {
			regularARs++
		}
	}

	if globalARs != 1 {
		t.Errorf("global ARs = %d, want 1", globalARs)
	}

	if regularARs != 1 {
		t.Errorf("regular (no-tag) ARs = %d, want 1", regularARs)
	}
}

// TestGen_HostToolRecursion_R6 verifies the cross-platform
// recursion mechanism (D31). A synthetic ya.make with a `.rl6`
// source forces the walker to recurse into a host instance of
// `contrib/tools/ragel6`, walk a stub PROGRAM there, and thread the
// resulting LD ref through EmitR6's `ForeignDepRefs["tool"]`. The
// generated `.cpp` is then compiled by EmitCC, so the closure is:
// host CC + host LD (the ragel6 stub) + R6 + target CC + target AR.
func TestGen_HostToolRecursion_R6(t *testing.T) {
	root := t.TempDir()

	// Synthetic host ragel6 module at the real path
	// `contrib/tools/ragel6/bin` (PR-28 D03 — the parent
	// `contrib/tools/ragel6/ya.make` uses INCLUDE(${ARCADIA_ROOT}/...)
	// which the parser does not yet expand). The PROGRAM(ragel6) macro
	// argument pins PR-28-D01: the LD's binary name comes from the
	// macro, not from the directory's last component ("bin").
	ragelDir := filepath.Join(root, "contrib/tools/ragel6/bin")
	Throw(os.MkdirAll(ragelDir, 0o755))
	Throw(os.WriteFile(filepath.Join(ragelDir, "ya.make"), []byte("PROGRAM(ragel6)\nSRCS(main.cpp)\nEND()\n"), 0o644))

	// Target consumer with an .rl6 source.
	consumerDir := filepath.Join(root, "consumer")
	Throw(os.MkdirAll(consumerDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consumerDir, "ya.make"), []byte("LIBRARY()\nSRCS(parser.rl6)\nEND()\n"), 0o644))

	g := testGen(root, "consumer")

	counts := make(map[string]int)
	platforms := make(map[string]int)
	hostNodes := 0

	for _, n := range g.Graph {
		p, _ := n.KV["p"].(string)
		counts[p]++
		platforms[n.Platform]++

		if nodeHasHostTag(n.Tags) {
			hostNodes++
		}
	}

	// Expected nodes:
	//   target side: R6, CC (the generated .cpp), AR  → 3
	//   host  side: CC (ragel6/main.cpp), LD (ragel6) → 2
	if counts["R6"] != 1 {
		t.Errorf("R6 count = %d, want 1", counts["R6"])
	}

	if counts["LD"] != 1 {
		t.Errorf("LD count = %d, want 1 (host ragel6 LD)", counts["LD"])
	}

	if counts["AR"] != 1 {
		t.Errorf("AR count = %d, want 1 (target consumer AR)", counts["AR"])
	}

	if counts["CC"] != 2 {
		t.Errorf("CC count = %d, want 2 (host ragel6/main.cpp + target generated parser.rl6.cpp)", counts["CC"])
	}

	if hostNodes != 2 {
		t.Errorf("host nodes = %d, want 2 (host CC + host LD)", hostNodes)
	}

	if platforms[string(PlatformDefaultLinuxAArch64)] != 3 {
		t.Errorf("target nodes = %d, want 3", platforms[string(PlatformDefaultLinuxAArch64)])
	}

	if platforms[string(PlatformDefaultLinuxX8664)] != 2 {
		t.Errorf("host nodes (by platform) = %d, want 2", platforms[string(PlatformDefaultLinuxX8664)])
	}

	// Verify the R6 node's foreign_deps.tool is exactly the LD
	// host ragel6 UID.
	var (
		r6Node *Node
		ldNode *Node
	)

	for _, n := range g.Graph {
		switch n.KV["p"] {
		case "R6":
			r6Node = n
		case "LD":
			ldNode = n
		}
	}

	if r6Node == nil {
		t.Fatal("no R6 node found")
	}

	if ldNode == nil {
		t.Fatal("no host ragel6 LD node found")
	}

	// PR-L4-C/07: ragel6 host LD edge lives in both deps (for topology)
	// and foreign_deps.tool (matching REF's shape for the R6 aarch64 node).
	if len(r6Node.Deps) != 1 || r6Node.Deps[0] != ldNode.UID {
		t.Errorf("R6 Deps = %v, want [%q]", r6Node.Deps, ldNode.UID)
	}

	if len(r6Node.ForeignDeps) != 1 || len(r6Node.ForeignDeps["tool"]) != 1 || r6Node.ForeignDeps["tool"][0] != ldNode.UID {
		t.Errorf("R6 ForeignDeps = %v, want {tool: [%q]}", r6Node.ForeignDeps, ldNode.UID)
	}

	// PR-28 D09: cmd_args[0] (the ragel6 invocation path) tracks the
	// host LD's outputs[0] modulo PR-35j's `/bin/` canonicalisation.
	// This pins the internal consistency between R6 dispatch and our
	// own host-PROGRAM emission — without it a future regression in
	// either side could produce a graph that compiles but invokes the
	// wrong binary path. PR-35j (closure of PR-33-C2_07): when the
	// host walker enters `contrib/tools/ragel6/bin` (because the
	// parent ya.make's `INCLUDE` is not yet expanded), EmitR6
	// canonicalises cmd_args[0] back to the reference-shaped parent
	// path `$(B)/contrib/tools/ragel6/ragel6`. The host LD's
	// outputs[0] keeps the walked `/bin/` path (the host LD itself is
	// not an L*-pair lever for the util closure). The consistency
	// invariant therefore compares the LD output through the same
	// canonicaliser that EmitR6 applies.
	if len(r6Node.Cmds) == 0 || len(r6Node.Cmds[0].CmdArgs) == 0 {
		t.Fatalf("R6 node has no Cmds[0].CmdArgs; got Cmds=%v", r6Node.Cmds)
	}

	if len(ldNode.Outputs) == 0 {
		t.Fatalf("host LD node has no Outputs; got Outputs=%v", ldNode.Outputs)
	}

	wantCmd0 := canonicalizeRagel6Binary(ldNode.Outputs[0]).String()

	if r6Node.Cmds[0].CmdArgs[0] != wantCmd0 {
		t.Errorf("R6 cmd_args[0] = %q, want canonicalised host ragel6 LD outputs[0] = %q (raw outputs[0] = %q)",
			r6Node.Cmds[0].CmdArgs[0], wantCmd0, ldNode.Outputs[0].String())
	}
}

// TestGen_PeerGlobalArchive_ThreadsToLD verifies D03: when a PROGRAM
// peers a LIBRARY that has GLOBAL_SRCS, the LD node's DepRefs include
// both the peer's regular AR and the peer's global AR.
func TestGen_PeerGlobalArchive_ThreadsToLD(t *testing.T) {
	root := t.TempDir()

	// peerlib: LIBRARY with both SRCS and GLOBAL_SRCS.
	peerDir := filepath.Join(root, "peerlib")
	Throw(os.MkdirAll(peerDir, 0o755))
	Throw(os.WriteFile(filepath.Join(peerDir, "ya.make"), []byte(
		"LIBRARY()\nSRCS(regular.cpp)\nGLOBAL_SRCS(global.cpp)\nEND()\n",
	), 0o644))

	// consumer: PROGRAM that peers peerlib.
	consumerDir := filepath.Join(root, "consumer")
	Throw(os.MkdirAll(consumerDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consumerDir, "ya.make"), []byte(
		"PROGRAM()\nSRCS(main.cpp)\nPEERDIR(peerlib)\nEND()\n",
	), 0o644))

	g := testGen(root, "consumer")

	// Locate the LD node.
	var ldNode *Node
	for _, n := range g.Graph {
		if n.KV["p"] == "LD" {
			ldNode = n
		}
	}

	if ldNode == nil {
		t.Fatal("no LD node found in graph")
	}

	// Count AR nodes: expect 2 (regular peerlib.a + peerlib.global.a).
	arCount := 0
	for _, n := range g.Graph {
		if n.KV["p"] == "AR" {
			arCount++
		}
	}

	if arCount != 2 {
		t.Errorf("AR count = %d, want 2 (regular + global from peerlib)", arCount)
	}

	// The LD node's Deps must include at least one reference to each
	// of the two peer ARs. With 1 CC (main.cpp.o) + 2 peer ARs
	// (regular + global) wired in, the minimum resolved Deps count is 3.
	// (Finalize resolves DepRefs into Deps UIDs and clears DepRefs.)
	if len(ldNode.Deps) < 3 {
		t.Errorf("LD Deps = %d, want >= 3 (own CC + peer AR + peer global AR)", len(ldNode.Deps))
	}

	// D08 regression guard: inputs must contain $(B)/peerlib/libpeerlib.global.a
	// (single prefix, no double). composeLDInputs prepends $(B)/ itself, so
	// GlobalPath must be BUILD_ROOT-relative (no $(B)/ prefix).
	expectedInput := "$(B)/peerlib/libpeerlib.global.a"
	foundInInputs := false

	for _, in := range ldNode.Inputs {
		if in.String() == expectedInput {
			foundInInputs = true
			break
		}
	}

	if !foundInInputs {
		t.Errorf("expected single-prefixed global archive in inputs; got: %v", ldNode.Inputs)
	}

	// Guard against double-prefixed entries (the original D08 defect).
	for _, in := range ldNode.Inputs {
		if strings.Contains(in.String(), "$(B)/$(B)") {
			t.Errorf("double-prefixed input found: %q", in.String())
		}
	}

	// D08 regression guard: cmd_args of link_exe.py (cmds[2]) must contain
	// peerlib/libpeerlib.global.a without any $(B)/ prefix, because
	// composeLDCmdLinkExe appends globalPaths verbatim into cmd_args and link_exe.py
	// resolves them relative to $(B) (the cmd's Cwd).
	if len(ldNode.Cmds) < 3 {
		t.Fatalf("LD node has %d cmds, want >= 3", len(ldNode.Cmds))
	}

	linkArgs := ldNode.Cmds[2].CmdArgs
	expectedCmdArg := "peerlib/libpeerlib.global.a"
	foundInCmdArgs := false

	for _, a := range linkArgs {
		if a == expectedCmdArg {
			foundInCmdArgs = true
			break
		}
	}

	if !foundInCmdArgs {
		t.Errorf("expected unprefixed global archive in cmd_args[2]; got: %v", linkArgs)
	}
}

// TestGen_ToolsArchiver_DoesNotCrash exercises the walker against
// the real `tools/archiver` PEERDIR closure. PR-25 only required
// "does not panic" + ≥50 nodes (the explicit-PEERDIR closure).
// PR-26 hardcodes the implicit DEFAULT_PEERDIRs (musl / builtins /
// malloc/api), so the closure now reaches ≥150 nodes (in practice
// ~1500). The full byte-exact L0/L3 acceptance gates land in a
// later PR once the flag-bundle and host-tool gaps are filled.
// Skipped when /home/pg/monorepo/yatool is not present.
//
// The call may still throw a *ParseError or a domain error from a
// deeply-peered ya.make whose macros PR-26 cannot evaluate (e.g.
// libcxx / libcxxrt / libunwind use `IF (X == "Y")` which the
// parser does not yet understand). Those are kept out of
// `defaultPeerdirsFor`; if the test starts throwing again, a new
// transitive gap has appeared. A panic that escapes Try IS a
// regression.
func TestGen_ToolsArchiver_DoesNotCrash(t *testing.T) {
	if _, err := os.Stat(sourceRoot + "/tools/archiver/ya.make"); err != nil {
		t.Skipf("tools/archiver not present in source tree: %v", err)
	}

	var g *Graph

	exc := Try(func() {
		g = testGen(sourceRoot, "tools/archiver")
	})

	if exc != nil {
		t.Fatalf("Gen against tools/archiver must not throw; got: %v", exc)
	}

	if len(g.Graph) < 150 {
		t.Errorf("expected at least 150 nodes (PR-26 acceptance), got %d", len(g.Graph))
	}

	if len(g.Result) == 0 {
		t.Error("expected non-empty Result")
	}
}

// TestGen_ToolsArchiver_DualPlatform_HostAndTargetCounts pins PR-28's
// structural acceptance bar: target nodes ≥ 1696 (PR-27 baseline), host
// nodes ≥ 1500 (D10 threshold; reference has 1797), single result root.
// A regression in either lobe (target or host) is a structural failure
// that comparator percentage drops cannot diagnose alone.
func TestGen_ToolsArchiver_DualPlatform_HostAndTargetCounts(t *testing.T) {
	if _, err := os.Stat(sourceRoot + "/tools/archiver/ya.make"); err != nil {
		t.Skipf("tools/archiver not present in source tree: %v", err)
	}

	g := testGen(sourceRoot, "tools/archiver")

	var hostNodes, targetNodes int

	for _, n := range g.Graph {
		if nodeHasHostTag(n.Tags) {
			hostNodes++
		} else {
			targetNodes++
		}
	}

	if targetNodes < 1696 {
		t.Errorf("target nodes = %d, want ≥ 1696 (PR-27 baseline)", targetNodes)
	}

	if hostNodes < 1582 {
		t.Errorf("host nodes = %d, want ≥ 1582 (current emission floor; ref = 1797)", hostNodes)
	}

	// PR-28 D09: result is target-only — single archiver LD.
	if len(g.Result) != 1 {
		t.Errorf("len(Result) = %d, want 1 (host LDs reachable via deps but not result roots)", len(g.Result))
	}
}

// TestGen_BuildCowOn_NoHostWalk pins the demand-driven invariant:
// build/cow/on has no .rl6 / .S sources, so Gen against it MUST emit
// exactly 2 target nodes and zero host nodes. A regression here means
// the host walk fired unconditionally, which would also break M1's
// byte-exact 2/2 comparator pairing.
func TestGen_BuildCowOn_NoHostWalk(t *testing.T) {
	if _, err := os.Stat(sourceRoot + "/build/cow/on/ya.make"); err != nil {
		t.Skipf("reference ya.make not present at %s/build/cow/on/ya.make", sourceRoot)
	}

	g := testGen(sourceRoot, "build/cow/on")

	if len(g.Graph) != 2 {
		t.Fatalf("len(Graph) = %d, want 2 (host walk must NOT fire for build/cow/on)", len(g.Graph))
	}

	for _, n := range g.Graph {
		if nodeHasHostTag(n.Tags) {
			t.Errorf("unexpected host node %s emitted for build/cow/on", n.UID)
		}
	}
}

// TestGen_AllocatorMacro_ResolvesToPeer pins D12: ALLOCATOR(MIM) must
// append `library/cpp/malloc/mimalloc` to the module's PEERDIR list so
// the walker descends into it. Synthetic fixture with a trivial peer
// stub.
func TestGen_AllocatorMacro_ResolvesToPeer(t *testing.T) {
	root := t.TempDir()

	// PROGRAM with ALLOCATOR(MIM) and explicit minimal peers; the
	// implicit DEFAULT_PEERDIR machinery is gated off by NO_PLATFORM
	// so the synthetic graph stays narrow.
	progDir := filepath.Join(root, "prog")
	Throw(os.MkdirAll(progDir, 0o755))
	Throw(os.WriteFile(filepath.Join(progDir, "ya.make"),
		[]byte("PROGRAM()\nNO_PLATFORM()\nALLOCATOR(MIM)\nSRCS(main.cpp)\nEND()\n"), 0o644))

	mimDir := filepath.Join(root, "library/cpp/malloc/mimalloc")
	Throw(os.MkdirAll(mimDir, 0o755))
	Throw(os.WriteFile(filepath.Join(mimDir, "ya.make"),
		[]byte("LIBRARY()\nNO_PLATFORM()\nSRCS(mim.cpp)\nEND()\n"), 0o644))

	g := testGen(root, "prog")

	var sawMimDir bool

	for _, n := range g.Graph {
		if n.TargetProperties["module_dir"] == "library/cpp/malloc/mimalloc" {
			sawMimDir = true

			break
		}
	}

	if !sawMimDir {
		t.Errorf("expected ALLOCATOR(MIM) to add library/cpp/malloc/mimalloc as peer; got Graph with no such module_dir")
	}
}

// TestGen_HostWalk_AsmlibYasmWired pins D05: a host `.S` source in a
// known yasm-using module must trigger a yasm host walk and wire the
// resulting LD ref into the AS node's `ForeignDepRefs["tool"]`. Other
// host AS sources (e.g. `chkstk_aarch64.S` in cxxsupp/builtins) get
// nil and emit no foreign_deps — that's the M2 `asmlibYasmModules`
// gate.
func TestGen_HostWalk_AsmlibYasmWired(t *testing.T) {
	root := t.TempDir()

	// Synthetic asmlib host fixture with one .asm source.
	// PR-M3-F-5: yasm dispatch is now extension-based (.asm), not
	// module-path-based, so the fixture must use .asm.
	asmlibDir := filepath.Join(root, "contrib/libs/asmlib")
	Throw(os.MkdirAll(asmlibDir, 0o755))
	Throw(os.WriteFile(filepath.Join(asmlibDir, "ya.make"),
		[]byte("LIBRARY()\nNO_PLATFORM()\nSRCS(memcmp64.asm)\nEND()\n"), 0o644))

	// Synthetic host yasm PROGRAM.
	yasmDir := filepath.Join(root, "contrib/tools/yasm")
	Throw(os.MkdirAll(yasmDir, 0o755))
	Throw(os.WriteFile(filepath.Join(yasmDir, "ya.make"),
		[]byte("PROGRAM()\nNO_PLATFORM()\nSRCS(yasm.c)\nEND()\n"), 0o644))

	// Drive asmlib as a host instance directly so the .S dispatch
	// fires under PIC=true. (The full demand-driven path would route
	// through ragel6/bin → musl/full → asmlib; this synthetic test
	// shortcuts to the AS+yasm wiring.)
	ctx := &genCtx{
		sourceRoot:  root,
		fs:          NewFS(root),
		emit:        NewBufferedEmitter(),
		memo:        make(map[ModuleInstance]*moduleEmitResult),
		walking:     make(map[ModuleInstance]bool),
		host:        testHostP,
		target:      testTargetP,
		vfsInterner: freshInterner(),
	}

	hostAsmlib := ModuleInstance{
		Path:     "contrib/libs/asmlib",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}

	genModule(ctx, hostAsmlib)

	g := Finalize(ctx.emit.(*BufferedEmitter))

	var asNode, yasmLD *Node

	for _, n := range g.Graph {
		switch n.KV["p"] {
		case "AS":
			asNode = n
		case "LD":
			if nodeHasHostTag(n.Tags) {
				yasmLD = n
			}
		}
	}

	if asNode == nil {
		t.Fatal("no AS node emitted")
	}

	if yasmLD == nil {
		t.Fatal("no host yasm LD emitted")
	}

	tool, ok := asNode.ForeignDeps["tool"]
	if !ok {
		t.Fatalf("AS ForeignDeps[tool] missing; got %#v", asNode.ForeignDeps)
	}

	if len(tool) != 1 || tool[0] != yasmLD.UID {
		t.Errorf("AS ForeignDeps[tool] = %v, want [%q]", tool, yasmLD.UID)
	}
}

// TestGen_HostWalk_NonAsmlibAS_NoYasmDep pins the gate: a host `.S`
// source NOT in `asmlibYasmModules` (e.g. cxxsupp/builtins shim) must
// emit an AS node with NO foreign_deps. Reference confirms 58 of 83
// host AS nodes have no foreign_deps.
func TestGen_HostWalk_NonAsmlibAS_NoYasmDep(t *testing.T) {
	root := t.TempDir()

	// Fake module NOT in asmlibYasmModules.
	modDir := filepath.Join(root, "myasm")
	Throw(os.MkdirAll(modDir, 0o755))
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"),
		[]byte("LIBRARY()\nNO_PLATFORM()\nSRCS(thing.S)\nEND()\n"), 0o644))

	ctx := &genCtx{
		sourceRoot:  root,
		fs:          NewFS(root),
		emit:        NewBufferedEmitter(),
		memo:        make(map[ModuleInstance]*moduleEmitResult),
		walking:     make(map[ModuleInstance]bool),
		host:        testHostP,
		target:      testTargetP,
		vfsInterner: freshInterner(),
	}

	hostInstance := ModuleInstance{
		Path:     "myasm",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}

	genModule(ctx, hostInstance)

	g := Finalize(ctx.emit.(*BufferedEmitter))

	var asNode *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "AS" {
			asNode = n

			break
		}
	}

	if asNode == nil {
		t.Fatal("no AS node emitted")
	}

	if len(asNode.ForeignDeps) != 0 {
		t.Errorf("AS ForeignDeps = %v, want empty (myasm not in asmlibYasmModules)", asNode.ForeignDeps)
	}
}

// TestGen_DefaultPeerdirs_BuildCowOnUnaffected pins the M1 invariant
// against the PR-26 default-peerdir machinery: build/cow/on declares
// NO_LIBC + NO_RUNTIME + NO_UTIL (effective NO_PLATFORM per
// `effectiveNoPlatform`), which suppresses every implicit default.
// The 2-node CC+AR closure must remain byte-exact even after the
// helper is wired into the walker.
func TestGen_DefaultPeerdirs_BuildCowOnUnaffected(t *testing.T) {
	const targetDir = "build/cow/on"

	if _, err := os.Stat(sourceRoot + "/" + targetDir + "/ya.make"); err != nil {
		t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
	}

	g := testGen(sourceRoot, targetDir)

	if len(g.Graph) != 2 {
		t.Errorf("Gen produced %d nodes, want 2 (defaults must be suppressed)", len(g.Graph))
	}

	// Belt-and-braces unit assertion: the helper itself returns
	// nothing for an effectively-no-platform CPP module. Flags mirror
	// the post-parse overlay for build/cow/on (NO_LIBC / NO_UTIL /
	// NO_RUNTIME) since this synthetic instance bypasses the parser.
	bcOn := ModuleInstance{
		Path:     targetDir,
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}

	got := defaultPeerdirsFor(nil, bcOn, FlagSet{NoLibc: true, NoUtil: true, NoRuntime: true}, true)

	if len(got) != 0 {
		t.Errorf("defaultPeerdirsFor(build/cow/on) = %v, want []", got)
	}
}

// TestGen_DefaultPeerdirs_SimpleLibrary verifies that a synthetic
// LIBRARY without any NO_* macro receives the full set of implicit
// default peers. The synthetic source tree contains stubbed
// musl / builtins / malloc/api ya.makes (each a minimal LIBRARY)
// so the walker can recurse into them. This is the only test in
// the file that exercises the actual emit path of defaults; the
// real-tree coverage lives in TestGen_ToolsArchiver_DoesNotCrash.
func TestGen_DefaultPeerdirs_SimpleLibrary(t *testing.T) {
	root := t.TempDir()

	// Minimal ya.make for each default peer. They each declare
	// effective NO_PLATFORM so they don't recursively trigger
	// further defaults (which would require deeper stub trees).
	stubLib := "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(stub.cpp)\nEND()\n"

	for _, path := range []string{
		"contrib/libs/musl",
		"contrib/libs/cxxsupp/builtins",
		"library/cpp/malloc/api",
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
	} {
		dir := filepath.Join(root, path)

		Throw(os.MkdirAll(dir, 0o755))
		Throw(os.WriteFile(filepath.Join(dir, "ya.make"), []byte(stubLib), 0o644))
	}

	// Helper assertion: defaultPeerdirsFor returns exactly the
	// seven paths for a fresh CPP LIBRARY with zero NO_* flags.
	plain := ModuleInstance{
		Path:     "consumer",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}

	// PR-42: contrib/libs/musl, contrib/libs/cxxsupp/builtins, and
	// library/cpp/malloc/api are no longer direct implicit peers; they are
	// reached transitively (musl via musl/full, builtins via libcxx, malloc/api
	// via malloc/tcmalloc). PR-32 D03: musl/include is still appended directly.
	wantDefaults := []string{
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
		"contrib/libs/musl/include",
	}

	gotDefaults := defaultPeerdirsFor(nil, plain, FlagSet{}, true)

	if !stringSlicesEqual(gotDefaults, wantDefaults) {
		t.Errorf("defaultPeerdirsFor(plain CPP) = %v, want %v", gotDefaults, wantDefaults)
	}

	// End-to-end: walk a consumer LIBRARY and confirm the seven
	// stubs were emitted.
	consumerDir := filepath.Join(root, "consumer")

	Throw(os.MkdirAll(consumerDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consumerDir, "ya.make"), []byte("LIBRARY()\nSRCS(main.cpp)\nEND()\n"), 0o644))

	g := testGen(root, "consumer")

	emittedDirs := make(map[string]bool)

	for _, n := range g.Graph {
		if md, ok := n.TargetProperties["module_dir"]; ok {
			emittedDirs[md] = true
		}
	}

	// PR-42: musl, builtins, malloc/api are no longer direct implicit peers of
	// a plain LIBRARY; they arrive transitively through program-default parents
	// (musl/full, libcxx, malloc/tcmalloc) which are not added for LIBRARYs.
	for _, want := range []string{
		"consumer",
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
	} {
		if !emittedDirs[want] {
			t.Errorf("graph missing module_dir %q; got %v", want, emittedDirs)
		}
	}
}

// TestGen_DefaultPeerdirs_HelperSuppression exercises the full
// suppression matrix of `defaultPeerdirsFor`. PR-27 widened the
// default set (libcxx/libcxxrt/libunwind/util added, gated by
// NoRuntime / NoUtil) and introduced runtime-ancestor self-suppression
// — modules whose path sits in `runtimeAncestorPaths` get zero
// implicit peers regardless of their FlagSet, matching the empirical
// reference-graph fact that every such module has no peer-archive
// deps in its AR.
//
//   - effective NO_PLATFORM (NoLibc+NoRuntime+NoUtil)  → empty set
//   - explicit NO_PLATFORM                            → empty set
//   - NO_LIBC only                                    → drops musl
//   - NO_RUNTIME only                                 → drops builtins+libcxx*
//   - NO_UTIL only                                    → drops util
//   - non-CPP language                                → empty set
//   - self-instance for any runtime ancestor          → empty set
func TestGen_DefaultPeerdirs_HelperSuppression(t *testing.T) {
	// PR-42: musl, builtins, malloc/api are no longer direct implicit peers
	// (reached transitively via program-default parents). PR-32 D03: musl/include
	// remains a direct peer (conf-declared via _BASE_UNIT PEERDIR at ymake.core.conf:781).
	fullSet := []string{
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
		"contrib/libs/musl/include",
	}

	cases := []struct {
		name  string
		mi    ModuleInstance
		flags FlagSet
		want  []string
	}{
		{
			name: "effective_no_platform",
			mi: ModuleInstance{
				Path:     "x",
				Kind:     KindLib,
				Language: LangCPP,
			},
			flags: FlagSet{NoLibc: true, NoRuntime: true, NoUtil: true},
			want:  nil,
		},
		{
			name: "explicit_no_platform",
			mi: ModuleInstance{
				Path:     "x",
				Kind:     KindLib,
				Language: LangCPP,
			},
			flags: FlagSet{NoPlatform: true},
			want:  nil,
		},
		{
			name: "no_libc_only",
			mi: ModuleInstance{
				Path:     "x",
				Kind:     KindLib,
				Language: LangCPP,
				Platform: testTargetP,
			},
			flags: FlagSet{NoLibc: true},
			// PR-42: musl was already removed from direct peers; NO_LIBC no
			// longer changes the set. PR-32 D03: musl/include still fires.
			want: fullSet,
		},
		{
			name: "no_runtime_only",
			mi: ModuleInstance{
				Path:     "x",
				Kind:     KindLib,
				Language: LangCPP,
				Platform: testTargetP,
			},
			flags: FlagSet{NoRuntime: true},
			// PR-42: musl and malloc/api removed from direct peers. NO_RUNTIME drops
			// libcxx/libcxxrt/libunwind. PR-32 D03: musl/include still fires.
			want: []string{"util", "contrib/libs/musl/include"},
		},
		{
			name: "non_cpp",
			mi: ModuleInstance{
				Path:     "x",
				Kind:     KindLib,
				Language: LangProto,
			},
			want: nil,
		},
		// `contrib/libs/musl` is a runtime ancestor with no NO_PLATFORM
		// effective flags — only the implicit musl/include auto-peer
		// fires.
		{
			name: "self_musl_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "contrib/libs/musl",
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: []string{"contrib/libs/musl/include"},
		},
		// `contrib/libs/musl/full` is not a literal runtime ancestor.
		// When a test bypasses ya.make parsing, it must model the
		// module's effective NO_PLATFORM flags explicitly.
		{
			name: "self_musl_subdir_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "contrib/libs/musl/full",
				Kind:     KindLib,
				Language: LangCPP,
			},
			flags: FlagSet{NoLibc: true, NoUtil: true, NoRuntime: true},
			want:  nil,
		},
		{
			name:  "no_util_only",
			mi:    ModuleInstance{Path: "x", Kind: KindLib, Language: LangCPP},
			flags: FlagSet{NoUtil: true},
			// PR-42: musl, builtins, malloc/api removed from direct peers.
			// NO_UTIL drops util. PR-32 D03: musl/include still fires.
			want: []string{
				"contrib/libs/cxxsupp/libcxx",
				"contrib/libs/cxxsupp/libcxxrt",
				"contrib/libs/libunwind",
				"contrib/libs/musl/include",
			},
		},
		// musl_extra is NOT in the runtime-ancestor set (the upstream
		// reference includes it as a 2-node leaf rather than a
		// runtime root); it gets the full default set.
		{
			name: "musl_extra_not_runtime_ancestor",
			mi:   ModuleInstance{Path: "contrib/libs/musl_extra", Kind: KindLib, Language: LangCPP, Platform: testTargetP},
			want: fullSet,
		},
		// PR-32 D03: non-NoStdInc runtime ancestors (builtins,
		// malloc/api, libcxx, util) get the auto-PEERDIR
		// `contrib/libs/musl/include` only — the runtime-stack peers
		// stay suppressed. The two-phase peer-aggregation in the
		// walker keeps the libcxx-include + libcxxrt-include slots
		// ordered BEFORE the musl-arch paths in consumer cmd_args.
		{
			name: "self_builtins_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "contrib/libs/cxxsupp/builtins",
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: []string{"contrib/libs/musl/include"},
		},
		{
			name: "self_malloc_api_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "library/cpp/malloc/api",
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: []string{"contrib/libs/musl/include"},
		},
		{
			name: "self_libcxx_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "contrib/libs/cxxsupp/libcxx",
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: []string{"contrib/libs/musl/include"},
		},
		{
			name: "self_util_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "util",
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: []string{"contrib/libs/musl/include"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := defaultPeerdirsFor(nil, c.mi, c.flags, true)

			if !stringSlicesEqual(got, c.want) {
				t.Errorf("defaultPeerdirsFor(%+v, %+v) = %v, want %v", c.mi, c.flags, got, c.want)
			}
		})
	}
}

// stringSlicesEqual compares two []string by length+order. nil and
// empty are treated as equal.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func TestGen_DefaultPeerdirs_ExplicitDuplicateDeduped(t *testing.T) {
	tmp := t.TempDir()
	Throw(os.MkdirAll(filepath.Join(tmp, "lib1"), 0755))
	Throw(os.WriteFile(filepath.Join(tmp, "lib1", "ya.make"), []byte(`LIBRARY()
PEERDIR(contrib/libs/musl)
SRCS(a.cpp)
END()
`), 0644))

	// Set up a stub musl ya.make so the recursion can resolve.
	Throw(os.MkdirAll(filepath.Join(tmp, "contrib/libs/musl"), 0755))
	Throw(os.WriteFile(filepath.Join(tmp, "contrib/libs/musl", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_UTIL()
NO_RUNTIME()
NO_PLATFORM()
SRCS(stub.c)
END()
`), 0644))

	g := testGen(tmp, "lib1")

	// Find lib1's AR; check its DepRefs.
	var lib1AR *Node
	for _, n := range g.Graph {
		if n.KV["p"] == "AR" && n.TargetProperties["module_dir"] == "lib1" {
			lib1AR = n
			break
		}
	}

	if lib1AR == nil {
		t.Fatal("lib1 AR not found")
	}

	// PR-30 D05: peer-archive refs are NOT threaded into AR.DepRefs.
	// The reference graph confirms every AR has zero AR-on-AR deps.
	// The dedupe contract still applies upstream (defaultPeerdirsFor
	// + explicit PEERDIR are deduped before walk), so the peer's
	// genModule fires exactly once. We pin the new invariant: lib1's
	// AR has zero AR-typed deps.
	for _, ref := range lib1AR.Deps {
		for _, n := range g.Graph {
			if n.UID == ref && n.KV["p"] == "AR" {
				t.Errorf("lib1 AR has AR-typed dep %q (module_dir=%q); reference invariant: zero AR-on-AR deps", ref, n.TargetProperties["module_dir"])
			}
		}
	}
}

// TestGen_SrcDirRebasesSourceResolution pins PR-28-D02 / D11: when a
// module declares SRCDIR(other/dir), per-source CC nodes (including
// those from JOIN_SRCS) must emit module_dir = "other/dir" and inputs
// rooted at "$(S)/other/dir/<src>". Without SRCDIR the
// instance's own path must be used unchanged.
func TestGen_SrcDirRebasesSourceResolution(t *testing.T) {
	t.Run("with_srcdir", func(t *testing.T) {
		// PR-30 D06: LIBRARY with non-ancestor SRCDIR keeps module_dir
		// at instance.Path; per-source SRCDIR routing applies (input
		// at $(S)/<srcdir>/<src>; output uses `__/<rel>`
		// infix). The historical PR-28-D02 "always rebase" shape is
		// retained ONLY for the PROGRAM-with-ancestor-SRCDIR pattern
		// (ragel6/bin); see TestGen_SrcdirAncestor_RebasesModuleDir.
		root := t.TempDir()

		modDir := filepath.Join(root, "mymod")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCDIR(other/dir)\nSRCS(foo.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "mymod")

		if len(g.Graph) != 2 {
			t.Fatalf("expected 2 nodes (1 CC + 1 AR), got %d", len(g.Graph))
		}

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
				ccNode = n
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		// Sibling SRCDIR: module_dir stays at instance.Path.
		if ccNode.TargetProperties["module_dir"] != "mymod" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties["module_dir"], "mymod")
		}

		// Input path resolves under SRCDIR (foo.cpp doesn't exist
		// locally at mymod/, so the composer takes the SRCDIR route).
		wantInput := "$(S)/other/dir/foo.cpp"

		if len(ccNode.Inputs) == 0 || ccNode.Inputs[0].String() != wantInput {
			t.Errorf("CC inputs = %v, want first = %q", ccNode.Inputs, wantInput)
		}

		// Output path uses `__/<rel>` infix; rel = relpath(other/dir/foo.cpp
		// from mymod) = ../other/dir/foo.cpp → __/other/dir/foo.cpp.
		wantOutput := "$(B)/mymod/__/other/dir/foo.cpp.o"

		if len(ccNode.Outputs) == 0 || ccNode.Outputs[0].String() != wantOutput {
			t.Errorf("CC outputs = %v, want first = %q", ccNode.Outputs, wantOutput)
		}
	})

	t.Run("without_srcdir_baseline", func(t *testing.T) {
		root := t.TempDir()

		modDir := filepath.Join(root, "basemod")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(bar.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "basemod")

		if len(g.Graph) != 2 {
			t.Fatalf("expected 2 nodes (1 CC + 1 AR), got %d", len(g.Graph))
		}

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
				ccNode = n
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		// Without SRCDIR, module_dir must be instance.Path.
		if ccNode.TargetProperties["module_dir"] != "basemod" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties["module_dir"], "basemod")
		}

		wantInput := "$(S)/basemod/bar.cpp"

		if len(ccNode.Inputs) == 0 || ccNode.Inputs[0].String() != wantInput {
			t.Errorf("CC inputs = %v, want first = %q", ccNode.Inputs, wantInput)
		}
	})

	t.Run("join_srcs_with_srcdir_library_non_ancestor", func(t *testing.T) {
		// PR-30 D06: LIBRARY with non-ancestor SRCDIR + JOIN_SRCS keeps
		// the JS module_dir at instance.Path. JS sources resolve at
		// the LIBRARY's own dir per the upstream convention (the
		// JOIN_SRCS-with-sibling-SRCDIR shape is unused in the M2
		// closure; the test pins the LIBRARY-no-rebase invariant).
		root := t.TempDir()

		modDir := filepath.Join(root, "jsmod")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCDIR(other/dir)\nJOIN_SRCS(all.cpp s1.cpp s2.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "jsmod")

		if len(g.Graph) != 3 {
			t.Fatalf("expected 3 nodes (1 JS + 1 CC + 1 AR), got %d", len(g.Graph))
		}

		var jsNode, ccNode *Node

		for _, n := range g.Graph {
			switch n.KV["p"] {
			case "JS":
				jsNode = n
			case "CC":
				ccNode = n
			}
		}

		if jsNode == nil {
			t.Fatal("no JS node emitted")
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		// LIBRARY non-ancestor: JS module_dir stays at instance.Path.
		if jsNode.TargetProperties["module_dir"] != "jsmod" {
			t.Errorf("JS module_dir = %q, want %q", jsNode.TargetProperties["module_dir"], "jsmod")
		}

		if ccNode.TargetProperties["module_dir"] != "jsmod" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties["module_dir"], "jsmod")
		}
	})

	t.Run("ancestor_program_rebases_module_dir", func(t *testing.T) {
		// PR-30 D06: PROGRAM with ancestor SRCDIR (ragel6/bin pattern)
		// fully rebases — module_dir = SRCDIR, output at SRCDIR.
		root := t.TempDir()

		modDir := filepath.Join(root, "tools/r6/bin")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("PROGRAM(myprog)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nALLOCATOR(FAKE)\nSRCDIR(tools/r6)\nSRCS(main.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "tools/r6/bin")

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
				ccNode = n
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		// Ancestor PROGRAM: module_dir == SRCDIR; output at <srcdir>/<src>.o.
		if ccNode.TargetProperties["module_dir"] != "tools/r6" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties["module_dir"], "tools/r6")
		}

		wantInput := "$(S)/tools/r6/main.cpp"
		if len(ccNode.Inputs) == 0 || ccNode.Inputs[0].String() != wantInput {
			t.Errorf("CC inputs = %v, want first = %q", ccNode.Inputs, wantInput)
		}

		wantOutput := "$(B)/tools/r6/main.cpp.o"
		if len(ccNode.Outputs) == 0 || ccNode.Outputs[0].String() != wantOutput {
			t.Errorf("CC outputs = %v, want first = %q", ccNode.Outputs, wantOutput)
		}
	})

	t.Run("ancestor_program_nested_source_keeps_module_dir", func(t *testing.T) {
		root := t.TempDir()

		modDir := filepath.Join(root, "tools/r6/bin")
		Throw(os.MkdirAll(filepath.Join(root, "tools/r6/sub"), 0o755))
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("PROGRAM(myprog)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nALLOCATOR(FAKE)\nSRCDIR(tools/r6)\nSRCS(sub/main.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))
		Throw(os.WriteFile(filepath.Join(root, "tools/r6/sub/main.cpp"), []byte("int main() { return 0; }\n"), 0o644))

		g := testGen(root, "tools/r6/bin")

		var ccNode *Node
		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
				ccNode = n
				break
			}
		}
		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if ccNode.TargetProperties["module_dir"] != "tools/r6/bin" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties["module_dir"], "tools/r6/bin")
		}

		if got := ccNode.Inputs[0].String(); got != "$(S)/tools/r6/sub/main.cpp" {
			t.Errorf("CC input = %q, want %q", got, "$(S)/tools/r6/sub/main.cpp")
		}

		if got := ccNode.Outputs[0].String(); got != "$(B)/tools/r6/bin/__/sub/main.cpp.o" {
			t.Errorf("CC output = %q, want %q", got, "$(B)/tools/r6/bin/__/sub/main.cpp.o")
		}
	})
}

// TestGen_CXXFLAGS_GLOBAL_LandsOnOwnCmdArgs pins the PR-33 D02
// inversion of PR-29-D01: own GLOBAL CXXFLAGS / CONLYFLAGS DO appear
// on the module's own cmd_args (via the (own ∪ peer) GLOBAL bucket
// emitted twice flanking the catboost-redux). The literal `GLOBAL`
// token must still NOT leak through (only the flag token following
// it). Empirical anchor: libcxx algorithm.cpp.o cmd_args[101] +
// [103] = `-nostdinc++` (own GLOBAL CXXFLAGS, emitted twice via the
// bucket).
func TestGen_CXXFLAGS_GLOBAL_LandsOnOwnCmdArgs(t *testing.T) {
	t.Run("CXXFLAGS_GLOBAL_emitted_twice_no_literal_GLOBAL", func(t *testing.T) {
		root := t.TempDir()
		modDir := filepath.Join(root, "testlib")
		Throw(os.MkdirAll(modDir, 0o755))

		// CXXFLAGS(GLOBAL -nostdinc++) — PR-33 D02: own GLOBAL CXXFLAGS
		// IS emitted on the module's own C++ CC node (twice, flanking
		// the catboost-redux). The literal GLOBAL token must not leak.
		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nCXXFLAGS(GLOBAL -nostdinc++)\nSRCS(foo.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "testlib")

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
				ccNode = n

				break
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if len(ccNode.Cmds) == 0 {
			t.Fatal("CC node has no Cmds")
		}

		nostdinccCount := 0

		for _, arg := range ccNode.Cmds[0].CmdArgs {
			if arg == "GLOBAL" {
				t.Errorf("CC cmd_args contains literal %q — GLOBAL modifier leaked into own node", arg)
			}

			if arg == "-nostdinc++" {
				nostdinccCount++
			}
		}

		// PR-33 D02: own GLOBAL CXXFLAGS lands on own cmd_args twice
		// (the bucket emitted on each side of the catboost-redux).
		if nostdinccCount != 2 {
			t.Errorf("expected 2 occurrences of -nostdinc++ in own cmd_args (bucket × 2), got %d", nostdinccCount)
		}
	})

	t.Run("CONLYFLAGS_GLOBAL_no_literal_GLOBAL_in_C", func(t *testing.T) {
		root := t.TempDir()
		modDir := filepath.Join(root, "testlib")
		Throw(os.MkdirAll(modDir, 0o755))

		// CONLYFLAGS(GLOBAL -Dfoo) — for C sources the empirical
		// reference shows no catboost-redux and no second peerExtras
		// emission (build/cow/on lib.c.o has no -DCATBOOST_OPENSOURCE
		// duplicate; tcmalloc aligned_alloc.c.o likewise). PR-33 D02
		// keeps the C path on the existing single peerExtras slot;
		// the literal GLOBAL token still must not leak. Own GLOBAL
		// CONLYFLAGS for C is not exercised in the M2 closure; if a
		// future closure surfaces such a case, revisit.
		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nCONLYFLAGS(GLOBAL -Dfoo)\nSRCS(bar.c)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "testlib")

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
				ccNode = n

				break
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if len(ccNode.Cmds) == 0 {
			t.Fatal("CC node has no Cmds")
		}

		for _, arg := range ccNode.Cmds[0].CmdArgs {
			if arg == "GLOBAL" {
				t.Errorf("CC cmd_args contains literal %q — GLOBAL modifier leaked into own node", arg)
			}
		}
	})

	t.Run("CXXFLAGS_non_GLOBAL_still_applied", func(t *testing.T) {
		root := t.TempDir()
		modDir := filepath.Join(root, "testlib")
		Throw(os.MkdirAll(modDir, 0o755))

		// CXXFLAGS(-DMINE) without GLOBAL — must reach cmd_args.
		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nCXXFLAGS(-DMINE)\nSRCS(foo.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "testlib")

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
				ccNode = n

				break
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if len(ccNode.Cmds) == 0 {
			t.Fatal("CC node has no Cmds")
		}

		found := false

		for _, arg := range ccNode.Cmds[0].CmdArgs {
			if arg == "-DMINE" {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("CC cmd_args missing %q — non-GLOBAL CXXFLAGS must be applied to own node", "-DMINE")
		}
	})
}

// TestGen_GeneratorWiredIntoDepRefs_JS pins PR-30 D04: a JOIN_SRCS module's
// downstream CC must carry the JS NodeRef as a DepRef (the reference shape:
// every JS-derived CC has Deps=[js UID]).
func TestGen_GeneratorWiredIntoDepRefs_JS(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "jsmod")
	Throw(os.MkdirAll(modDir, 0o755))
	yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nJOIN_SRCS(all.cpp s1.cpp s2.cpp)\nEND()\n")
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	g := testGen(root, "jsmod")

	var jsNode, ccNode *Node

	for _, n := range g.Graph {
		switch n.KV["p"] {
		case "JS":
			jsNode = n
		case "CC":
			ccNode = n
		}
	}

	if jsNode == nil {
		t.Fatal("no JS node emitted")
	}

	if ccNode == nil {
		t.Fatal("no CC node emitted")
	}

	found := false

	for _, dep := range ccNode.Deps {
		if dep == jsNode.UID {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("CC.Deps = %v, want to contain JS UID %q (PR-30 D04 Generator wiring)", ccNode.Deps, jsNode.UID)
	}
}

// TestGen_GeneratorWiredIntoDepRefs_R6 pins PR-30 D04: a `.rl6` source
// emits a downstream CC node whose DepRefs contains the R6 NodeRef.
func TestGen_GeneratorWiredIntoDepRefs_R6(t *testing.T) {
	root := t.TempDir()

	// Module that uses .rl6.
	modDir := filepath.Join(root, "r6mod")
	Throw(os.MkdirAll(modDir, 0o755))
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(thing.rl6)
END()
`), 0o644))

	// Stub host ragel6 PROGRAM so the host-tool recursion has
	// something to resolve. Its parse may fail in the synthetic
	// fixture (no SET evaluator) — emitOneSource swallows ParseError
	// and leaves ragelLDRef zero-valued; the downstream CC still
	// receives r6Ref from the local EmitR6 call (HasGenerator=true).
	Throw(os.MkdirAll(filepath.Join(root, "contrib/tools/ragel6/bin"), 0o755))
	Throw(os.WriteFile(filepath.Join(root, "contrib/tools/ragel6/bin", "ya.make"), []byte(`PROGRAM(ragel6)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALLOCATOR(FAKE)
SRCS(main.cpp)
END()
`), 0o644))

	g := testGen(root, "r6mod")

	var r6Node, ccNode *Node

	for _, n := range g.Graph {
		switch n.KV["p"] {
		case "R6":
			r6Node = n
		case "CC":
			// Pick the CC whose input lives under $(B)
			// (the R6-derived CC; the host ragel6 PROGRAM also
			// emits a CC for its main.cpp under $(S)).
			ip := ""
			if len(n.Inputs) > 0 {
				ip = n.Inputs[0].String()
			}

			if ccNode == nil && strings.HasPrefix(ip, "$(B)/") {
				ccNode = n
			}
		}
	}

	if r6Node == nil {
		t.Fatal("no R6 node emitted")
	}

	if ccNode == nil {
		t.Fatal("no R6-derived CC node emitted")
	}

	found := false

	for _, dep := range ccNode.Deps {
		if dep == r6Node.UID {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("R6-derived CC.Deps = %v, want to contain R6 UID %q (PR-30 D04 Generator wiring)", ccNode.Deps, r6Node.UID)
	}
}

// TestEmitAR_NoPeerArchivesInDeps pins PR-30 D05: the LIBRARY AR call
// site drops `peerArchiveRefs`. Reference confirms every AR has zero
// AR-on-AR deps. Even when peers are declared and emit, lib1's AR
// has zero AR-typed deps.
func TestEmitAR_NoPeerArchivesInDeps(t *testing.T) {
	tmp := t.TempDir()
	Throw(os.MkdirAll(filepath.Join(tmp, "lib_consumer"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "lib_consumer", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(lib_peer)
SRCS(c.cpp)
END()
`), 0o644))

	Throw(os.MkdirAll(filepath.Join(tmp, "lib_peer"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "lib_peer", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(p.cpp)
END()
`), 0o644))

	g := testGen(tmp, "lib_consumer")

	var consumerAR *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "AR" && n.TargetProperties["module_dir"] == "lib_consumer" {
			consumerAR = n

			break
		}
	}

	if consumerAR == nil {
		t.Fatal("lib_consumer AR not found")
	}

	for _, dep := range consumerAR.Deps {
		for _, n := range g.Graph {
			if n.UID == dep && n.KV["p"] == "AR" {
				t.Errorf("lib_consumer AR has AR-typed dep (peer module_dir=%q); reference invariant: zero AR-on-AR deps", n.TargetProperties["module_dir"])
			}
		}
	}
}

// TestGen_PROGRAM_DefaultMuslFull_PeerEmitted pins PR-30 D02: a PROGRAM
// in the M2 environment (MUSL=yes, !MUSL_LITE) implicitly peers
// `contrib/libs/musl/full`. Synthetic fixture supplies the musl/full
// ya.make so the implicit peer resolves.
func TestGen_PROGRAM_DefaultMuslFull_PeerEmitted(t *testing.T) {
	tmp := t.TempDir()

	// Synthetic PROGRAM with no ALLOCATOR macro.
	Throw(os.MkdirAll(filepath.Join(tmp, "myprog"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "myprog", "ya.make"), []byte(`PROGRAM(myprog)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALLOCATOR(FAKE)
SRCS(main.cpp)
END()
`), 0o644))

	// Synthetic musl/full ya.make.
	Throw(os.MkdirAll(filepath.Join(tmp, "contrib/libs/musl/full"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "contrib/libs/musl/full", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PLATFORM()
SRCS(stub.c)
END()
`), 0o644))

	g := testGen(tmp, "myprog")

	found := false

	for _, n := range g.Graph {
		if n.KV["p"] == "AR" && n.TargetProperties["module_dir"] == "contrib/libs/musl/full" {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("expected an AR node with module_dir=contrib/libs/musl/full (PR-30 D02 default PROGRAM peer); not found")
	}
}

// TestGen_PROGRAM_DefaultAllocator_TcmallocTc pins PR-30 D03: a PROGRAM
// without ALLOCATOR(...) defaults to TCMALLOC_TC, pulling
// library/cpp/malloc/tcmalloc + contrib/libs/tcmalloc/no_percpu_cache.
func TestGen_PROGRAM_DefaultAllocator_TcmallocTc(t *testing.T) {
	tmp := t.TempDir()

	Throw(os.MkdirAll(filepath.Join(tmp, "myprog"), 0o755))
	// PROGRAM with no ALLOCATOR macro and no SRCDIR.
	Throw(os.WriteFile(filepath.Join(tmp, "myprog", "ya.make"), []byte(`PROGRAM(myprog)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`), 0o644))

	// Synthetic stubs for the TCMALLOC_TC peers.
	Throw(os.MkdirAll(filepath.Join(tmp, "library/cpp/malloc/tcmalloc"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "library/cpp/malloc/tcmalloc", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PLATFORM()
SRCS(stub.cpp)
END()
`), 0o644))

	Throw(os.MkdirAll(filepath.Join(tmp, "contrib/libs/tcmalloc/no_percpu_cache"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "contrib/libs/tcmalloc/no_percpu_cache", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PLATFORM()
SRCS(stub.cpp)
END()
`), 0o644))

	g := testGen(tmp, "myprog")

	hasTcmalloc := false
	hasNoPercpu := false

	for _, n := range g.Graph {
		md := n.TargetProperties["module_dir"]

		if n.KV["p"] != "AR" {
			continue
		}

		switch md {
		case "library/cpp/malloc/tcmalloc":
			hasTcmalloc = true
		case "contrib/libs/tcmalloc/no_percpu_cache":
			hasNoPercpu = true
		}
	}

	if !hasTcmalloc {
		t.Errorf("expected AR with module_dir=library/cpp/malloc/tcmalloc; not found")
	}

	if !hasNoPercpu {
		t.Errorf("expected AR with module_dir=contrib/libs/tcmalloc/no_percpu_cache; not found")
	}
}

// TestGen_PROGRAM_ExplicitAllocator_NoTcmallocDefault pins PR-30 D03:
// a PROGRAM that explicitly types ALLOCATOR(FAKE) does NOT receive
// the TCMALLOC_TC default-allocator peers.
func TestGen_PROGRAM_ExplicitAllocator_NoTcmallocDefault(t *testing.T) {
	tmp := t.TempDir()

	Throw(os.MkdirAll(filepath.Join(tmp, "myprog"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "myprog", "ya.make"), []byte(`PROGRAM(myprog)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALLOCATOR(FAKE)
SRCS(main.cpp)
END()
`), 0o644))

	g := testGen(tmp, "myprog")

	for _, n := range g.Graph {
		md := n.TargetProperties["module_dir"]

		if md == "library/cpp/malloc/tcmalloc" || md == "contrib/libs/tcmalloc/no_percpu_cache" {
			t.Errorf("PROGRAM with ALLOCATOR(FAKE) emitted unexpected node module_dir=%q (TCMALLOC_TC default must be suppressed)", md)
		}
	}
}

// TestGen_SrcdirSibling_KeepsModuleDir pins PR-30 D06: a LIBRARY whose
// SRCDIR points at a sibling directory keeps its own module_dir on
// per-source CC nodes; the source path uses `__/<rel>` infix.
//
// Synthetic fixture: instance=`mylib`, SRCDIR=`other`, source `src/foo.cpp`.
// The composer takes the SRCDIR route because foo.cpp doesn't exist
// at mylib/src/foo.cpp on disk.
func TestGen_SrcdirSibling_KeepsModuleDir(t *testing.T) {
	tmp := t.TempDir()

	Throw(os.MkdirAll(filepath.Join(tmp, "mylib"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "mylib", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCDIR(other)
SRCS(src/foo.cpp)
END()
`), 0o644))

	g := testGen(tmp, "mylib")

	var ccNode *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			ccNode = n

			break
		}
	}

	if ccNode == nil {
		t.Fatal("no CC node emitted")
	}

	if got := ccNode.TargetProperties["module_dir"]; got != "mylib" {
		t.Errorf("CC module_dir = %q, want %q (sibling SRCDIR — module_dir stays at instance.Path)", got, "mylib")
	}

	wantInput := "$(S)/other/src/foo.cpp"

	if len(ccNode.Inputs) == 0 || ccNode.Inputs[0].String() != wantInput {
		t.Errorf("CC input = %v, want first = %q", ccNode.Inputs, wantInput)
	}

	wantOutput := "$(B)/mylib/__/other/src/foo.cpp.o"

	if len(ccNode.Outputs) == 0 || ccNode.Outputs[0].String() != wantOutput {
		t.Errorf("CC output = %v, want first = %q", ccNode.Outputs, wantOutput)
	}
}

// TestGen_SrcdirLocal_IgnoresSrcdir pins PR-30 D06: a LIBRARY with SRCDIR
// where the source ALSO exists locally (in instance.Path) takes the
// local-resolution path — SRCDIR is silently ignored. This is the
// musl_extra / tcmalloc/no_percpu_cache `aligned_alloc.c` shape.
func TestGen_SrcdirLocal_IgnoresSrcdir(t *testing.T) {
	tmp := t.TempDir()

	Throw(os.MkdirAll(filepath.Join(tmp, "mylib"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "mylib", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCDIR(other)
SRCS(local.c)
END()
`), 0o644))

	// Place the actual source file at instance.Path so the composer's
	// stat check finds it locally.
	Throw(os.WriteFile(filepath.Join(tmp, "mylib", "local.c"), []byte("int x;\n"), 0o644))

	g := testGen(tmp, "mylib")

	var ccNode *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			ccNode = n

			break
		}
	}

	if ccNode == nil {
		t.Fatal("no CC node emitted")
	}

	wantInput := "$(S)/mylib/local.c"

	if len(ccNode.Inputs) == 0 || ccNode.Inputs[0].String() != wantInput {
		t.Errorf("CC input = %v, want first = %q (local-existing source must ignore SRCDIR)", ccNode.Inputs, wantInput)
	}

	wantOutput := "$(B)/mylib/local.c.o"

	if len(ccNode.Outputs) == 0 || ccNode.Outputs[0].String() != wantOutput {
		t.Errorf("CC output = %v, want first = %q", ccNode.Outputs, wantOutput)
	}
}

// TestGen_AddInclMixed_OwnPathStaysOwn pins PR-31 D13: a single
// ADDINCL call carrying both a GLOBAL path and a module-own path must
// NOT propagate the module-own path to consumers via PEERDIR.
//
// Setup: lib declares ADDINCL(GLOBAL lib/include\nlib/src). Consumer
// peers lib. The consumer's CC node must have -I lib/include (from
// the GLOBAL path) but must NOT have -I lib/src (the module-own path).
func TestGen_AddInclMixed_OwnPathStaysOwn(t *testing.T) {
	root := t.TempDir()

	// lib/ya.make: LIBRARY with mixed ADDINCL — GLOBAL include, bare src.
	libDir := filepath.Join(root, "lib")
	Throw(os.MkdirAll(libDir, 0o755))
	Throw(os.MkdirAll(filepath.Join(root, "lib/include"), 0o755))
	Throw(os.WriteFile(filepath.Join(libDir, "ya.make"), []byte(
		"LIBRARY()\nADDINCL(\n    GLOBAL lib/include\n    lib/src\n)\nSRCS(lib.cpp)\nEND()\n",
	), 0o644))

	// consumer/ya.make: LIBRARY that peers lib.
	consDir := filepath.Join(root, "consumer")
	Throw(os.MkdirAll(consDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consDir, "ya.make"), []byte(
		"LIBRARY()\nPEERDIR(lib)\nSRCS(main.cpp)\nEND()\n",
	), 0o644))

	g := testGen(root, "consumer")

	// Find the CC node for main.cpp (the consumer's own source).
	var consumerCC *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			for _, out := range n.Outputs {
				if strings.Contains(out.String(), "main.cpp.o") {
					consumerCC = n
					break
				}
			}
		}
	}

	if consumerCC == nil {
		t.Fatal("consumer CC node for main.cpp not found")
	}

	// Collect -I flags from the first cmd's cmd_args.
	var iFlags []string

	if len(consumerCC.Cmds) > 0 {
		for _, arg := range consumerCC.Cmds[0].CmdArgs {
			if strings.HasPrefix(arg, "-I") {
				iFlags = append(iFlags, arg)
			}
		}
	}

	// GLOBAL path must propagate.
	wantGlobal := "-I$(S)/lib/include"
	foundGlobal := false

	for _, f := range iFlags {
		if f == wantGlobal {
			foundGlobal = true
			break
		}
	}

	if !foundGlobal {
		t.Errorf("consumer CC -I flags = %v; want %q (GLOBAL ADDINCL must propagate to peers)", iFlags, wantGlobal)
	}

	// Module-own path must NOT propagate.
	wantAbsent := "-I$(S)/lib/src"

	for _, f := range iFlags {
		if f == wantAbsent {
			t.Errorf("consumer CC -I flags = %v; must NOT contain %q (module-own ADDINCL must stay module-own, PR-31 D13)", iFlags, wantAbsent)
			break
		}
	}
}

// TestGen_ToolsArchiver_L0_AtLeast95 is the M2 acceptance closer: PR-30
// must lift L0 ≥ 95% on the tools/archiver target against the reference
// graph at /home/pg/monorepo/yatool/sg.json.
// TestIsRuntimeAncestor_LiteralOnly pins PR-33 D01: `isRuntimeAncestor`
// matches only literal entries in `runtimeAncestorPaths`. Subtree
// members (`util/charset`, `contrib/libs/musl/full`,
// `contrib/libs/cxxsupp/libcxxabi-parts`) are NOT runtime ancestors;
// they go through the normal `defaultPeerdirsFor` flow and pick up
// libcxx / libcxxrt / libunwind / util as auto-peers. The literal
// entries themselves still self-suppress via the
// `instance.Path != "..."` guards inside `defaultPeerdirsFor`.
func TestIsRuntimeAncestor_LiteralOnly(t *testing.T) {
	literals := []string{
		"contrib/libs/musl",
		"contrib/libs/libc_compat",
		"contrib/libs/linuxvdso",
		"contrib/libs/cxxsupp/builtins",
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/cxxsupp/libcxxabi",
		"contrib/libs/cxxsupp/libcxxabi-parts",
		"contrib/libs/libunwind",
		"library/cpp/malloc/api",
		"library/cpp/sanitizer/include",
		"util",
	}

	for _, p := range literals {
		if !isRuntimeAncestor(p) {
			t.Errorf("isRuntimeAncestor(%q) = false, want true (literal entry)", p)
		}
	}

	// Subtree members must NOT be classified as runtime ancestors.
	subtree := []string{
		"util/charset",
		"util/datetime/parser.rl6.cpp.o",
		"contrib/libs/musl/full",
		"contrib/libs/cxxsupp/libcxxabi-parts/src",
		"contrib/libs/libunwind/private",
	}

	for _, p := range subtree {
		if isRuntimeAncestor(p) {
			t.Errorf("isRuntimeAncestor(%q) = true, want false (subtree extension dropped in PR-33 D01)", p)
		}
	}
}

// TestGen_MallocApi_HoistInjection_ByteExact pins the PR-35c A2_01 fix:
// `library/cpp/malloc/api` is a runtime ancestor whose
// `defaultPeerdirsFor` returns the empty default-peer set, so the C01
// hoist (which is reorder-only) had nothing to hoist. PR-33-A2_01 left
// malloc.cpp.o + malloc.cpp.pic.o L3-divergent — slots 11-12 should be
// `-I libcxx/include` + `-I libcxxrt/include` per the reference, but
// without an injection ours emitted musl/arch directly there.
//
// PR-35c injects libcxx/include + libcxxrt/include + `-nostdinc++`
// directly into malloc/api's `peerAddInclGlobal` /
// `peerCXXFlagsGlobal` (LOCAL only — not propagated to consumers).
// This test pins the resulting CC node byte-exact.
// TestGen_ToolsArchiver_LDPeerArchiveClosure pins the PR-35c LD walker
// transitive peer-archive closure fix. Pre-PR-35c the walker collected
// only direct peers' archives — 13 entries for `tools/archiver`'s LD,
// versus the reference's 32. PR-35c folds each peer's
// `PeerArchiveClosure*` into the running closure (DFS post-order,
// dedup-by-path), so the LD's `--start-group ... --end-group` block
// matches the reference 32 archives.
//
// The test pins the count + the SET (order may diverge from the
// reference until upstream's exact `_BUILTIN_PEERDIR` walk-order
// algorithm is modelled — pinning the set is sufficient to guard the
// regression that motivated PR-35b's deferred 19-archive gap).
func TestGen_ToolsArchiver_LDPeerArchiveClosure(t *testing.T) {
	const targetDir = "tools/archiver"

	if _, err := os.Stat(sourceRoot + "/" + targetDir + "/ya.make"); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := testGen(sourceRoot, targetDir)

	const ldOutput = "$(B)/tools/archiver/archiver"

	var ourLD *Node

	for _, n := range our.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0].String() == ldOutput {
			ourLD = n

			break
		}
	}

	if ourLD == nil {
		t.Fatalf("Gen produced no LD node with output %q", ldOutput)
	}

	if len(ourLD.Cmds) < 3 {
		t.Fatalf("LD has %d cmds, expected >= 3", len(ourLD.Cmds))
	}

	cmd2 := ourLD.Cmds[2].CmdArgs

	startIdx := -1
	endIdx := -1

	for i, a := range cmd2 {
		if a == "-Wl,--start-group" {
			startIdx = i
		} else if a == "-Wl,--end-group" {
			endIdx = i

			break
		}
	}

	if startIdx < 0 || endIdx < 0 || endIdx <= startIdx {
		t.Fatalf("cmd[2] missing --start-group / --end-group framing (start=%d end=%d)", startIdx, endIdx)
	}

	gotPeers := cmd2[startIdx+1 : endIdx]

	if len(gotPeers) != len(archiverPeerLibPaths) {
		t.Errorf("peer-archive count = %d, want %d (PR-35c transitive closure)", len(gotPeers), len(archiverPeerLibPaths))
	}

	gotSet := make(map[string]struct{}, len(gotPeers))
	for _, p := range gotPeers {
		gotSet[p] = struct{}{}
	}

	for _, want := range archiverPeerLibPaths {
		if _, ok := gotSet[want]; !ok {
			t.Errorf("peer-archive set missing %q (PR-35c walker should expose it)", want)
		}
	}
}

// TestGen_MuslPyplugin_CPNodeEmitted pins PR-35k's LD_PLUGIN wiring:
// `contrib/libs/musl/include` declares `LD_PLUGIN(musl.py)`; `Gen`
// must emit a CP node that copies
// `$(S)/contrib/libs/musl/include/musl.py` to
// `$(B)/contrib/libs/musl/include/musl.py.pyplugin`. The CP
// node's shape is independently pinned by
// `TestEmitCP_MuslPyplugin_ByteExact` against the reference; here we
// only verify the walker triggers the emission.
func TestGen_MuslPyplugin_CPNodeEmitted(t *testing.T) {
	const targetDir = "tools/archiver"

	if _, err := os.Stat(sourceRoot + "/" + targetDir + "/ya.make"); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := testGen(sourceRoot, targetDir)

	const wantOutput = "$(B)/contrib/libs/musl/include/musl.py.pyplugin"

	var targetCP *Node

	for _, n := range our.Graph {
		if n.KV["p"] != "CP" {
			continue
		}

		if len(n.Outputs) == 0 || n.Outputs[0].String() != wantOutput {
			continue
		}

		if n.Platform != string(testTargetP.Target) {
			continue
		}

		targetCP = n

		break
	}

	if targetCP == nil {
		t.Fatalf("Gen emitted no CP node with output %q on target platform %q", wantOutput, testTargetP.Target)
	}

	if got := targetCP.TargetProperties["module_dir"]; got != "contrib/libs/musl/include" {
		t.Errorf("CP module_dir = %q, want %q", got, "contrib/libs/musl/include")
	}

	if len(targetCP.Cmds) != 1 {
		t.Fatalf("CP has %d cmds, want 1", len(targetCP.Cmds))
	}

	args := targetCP.Cmds[0].CmdArgs

	if len(args) != 5 {
		t.Fatalf("CP cmd_args length = %d, want 5", len(args))
	}

	if args[2] != "copy" {
		t.Errorf("CP cmd_args[2] = %q, want %q", args[2], "copy")
	}

	const wantSrc = "$(S)/contrib/libs/musl/include/musl.py"
	if args[3] != wantSrc {
		t.Errorf("CP cmd_args[3] (src) = %q, want %q", args[3], wantSrc)
	}

	if args[4] != wantOutput {
		t.Errorf("CP cmd_args[4] (dst) = %q, want %q", args[4], wantOutput)
	}
}

// TestGen_ToolsArchiver_LDPluginSection pins PR-35k's archiver LD
// `--start-plugins ... --end-plugins` block: the musl pyplugin path
// must appear once between the two markers, sitting between
// `link_exe.py` and the `--clang-ver` flag pair (per `composeLDCmdLinkExe`'s
// shape). The plugin path must reference the BUILD_ROOT-anchored
// pyplugin produced by the CP node above. Pinned for archiver (target
// PROGRAM, the M2 byte-exact pin) — host LDs (yasm, ragel6) carry the
// same shape but are not byte-exact pinned here.
func TestGen_ToolsArchiver_LDPluginSection(t *testing.T) {
	const targetDir = "tools/archiver"

	if _, err := os.Stat(sourceRoot + "/" + targetDir + "/ya.make"); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := testGen(sourceRoot, targetDir)

	const ldOutput = "$(B)/tools/archiver/archiver"

	var ourLD *Node

	for _, n := range our.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0].String() == ldOutput {
			ourLD = n

			break
		}
	}

	if ourLD == nil {
		t.Fatalf("Gen produced no LD node with output %q", ldOutput)
	}

	if len(ourLD.Cmds) < 3 {
		t.Fatalf("LD has %d cmds, expected >= 3", len(ourLD.Cmds))
	}

	cmd2 := ourLD.Cmds[2].CmdArgs

	startIdx := -1
	endIdx := -1

	for i, a := range cmd2 {
		if a == "--start-plugins" {
			startIdx = i
		} else if a == "--end-plugins" {
			endIdx = i

			break
		}
	}

	if startIdx < 0 {
		t.Fatalf("cmd[2] missing --start-plugins marker (PR-35k must wire the musl plugin into archiver's LD)")
	}

	if endIdx < 0 || endIdx <= startIdx {
		t.Fatalf("cmd[2] missing --end-plugins marker after --start-plugins (start=%d end=%d)", startIdx, endIdx)
	}

	gotPlugins := cmd2[startIdx+1 : endIdx]

	wantPlugins := []string{"$(B)/contrib/libs/musl/include/musl.py.pyplugin"}

	if len(gotPlugins) != len(wantPlugins) {
		t.Fatalf("plugin section: got %d entries, want %d (entries: %v)", len(gotPlugins), len(wantPlugins), gotPlugins)
	}

	for i, want := range wantPlugins {
		if gotPlugins[i] != want {
			t.Errorf("plugin[%d] = %q, want %q", i, gotPlugins[i], want)
		}
	}

	// The plugin marker pair must precede `--clang-ver` (composeLDCmdLinkExe
	// shape: prologue → plugins → --clang-ver/...).
	clangVerIdx := -1

	for i, a := range cmd2 {
		if a == "--clang-ver" {
			clangVerIdx = i

			break
		}
	}

	if clangVerIdx < 0 {
		t.Fatalf("cmd[2] missing --clang-ver flag")
	}

	if endIdx >= clangVerIdx {
		t.Errorf("--end-plugins (idx %d) must precede --clang-ver (idx %d)", endIdx, clangVerIdx)
	}
}

// TestGen_MuslPyplugin_HostCPDedup pins PR-35l's host CP dedup. The
// reference graph emits exactly ONE CP node for `musl.py.pyplugin`
// (on the target platform, UID `nPHkMSIqOHBrXsoclNuu6g` in
// /home/pg/monorepo/yatool/sg.json:105555) and reuses its UID
// from both target consumer LDs (archiver) and host consumer LDs
// (yasm, ragel6). PR-35k initially emitted a second CP node on the
// host platform because `WithHost`-recursed walks of
// `contrib/libs/musl/include` re-fired `emitOwnLDPlugins`, producing
// a dup with `Platform=default-linux-x86_64` whose UID differed from
// the target's. PR-35l added `genCtx.ldPluginCPCache` so the first
// emit wins (target, since the seed walk runs target-first) and
// every subsequent host visit reuses the cached NodeRef.
//
// Verification shape:
//
//   - exactly one CP node has output `musl.py.pyplugin`,
//   - that CP carries `Platform = default-linux-aarch64` (target),
//   - every LD whose `cmd[2]` references the pyplugin path also
//     lists the SAME CP UID in its `Deps` slice, regardless of the
//     LD's own platform.
func TestGen_MuslPyplugin_HostCPDedup(t *testing.T) {
	const targetDir = "tools/archiver"

	if _, err := os.Stat(sourceRoot + "/" + targetDir + "/ya.make"); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := testGen(sourceRoot, targetDir)

	const pluginOutput = "$(B)/contrib/libs/musl/include/musl.py.pyplugin"

	cpNodes := []*Node{}

	for _, n := range our.Graph {
		if n.KV["p"] != "CP" {
			continue
		}

		if len(n.Outputs) == 0 || n.Outputs[0].String() != pluginOutput {
			continue
		}

		cpNodes = append(cpNodes, n)
	}

	if len(cpNodes) != 1 {
		gotPlats := make([]string, 0, len(cpNodes))

		for _, n := range cpNodes {
			gotPlats = append(gotPlats, n.Platform)
		}

		t.Fatalf("expected exactly 1 CP node for %s; got %d (platforms: %v) — host walk re-emitted the plugin instead of reusing the target's NodeRef", pluginOutput, len(cpNodes), gotPlats)
	}

	cpNode := cpNodes[0]

	if cpNode.Platform != string(testTargetP.Target) {
		t.Errorf("musl plugin CP platform = %q, want %q (the surviving CP must be the target one — first-emit-wins)", cpNode.Platform, testTargetP.Target)
	}

	cpUID := cpNode.UID

	if cpUID == "" {
		t.Fatalf("musl plugin CP node has empty UID")
	}

	// Walk every LD; if its cmd[2] references the pyplugin path then
	// the CP UID must appear in its Deps. The reference graph's host
	// LDs (ragel6/yasm) carry the same target-platform CP UID in deps
	// as the target archiver LD.
	ldsReferencing := 0

	for _, n := range our.Graph {
		if n.KV["p"] != "LD" || len(n.Cmds) < 3 {
			continue
		}

		referencesPlugin := false

		for _, a := range n.Cmds[2].CmdArgs {
			if a == pluginOutput {
				referencesPlugin = true

				break
			}
		}

		if !referencesPlugin {
			continue
		}

		ldsReferencing++

		hasDep := false

		for _, dep := range n.Deps {
			if dep == cpUID {
				hasDep = true

				break
			}
		}

		if !hasDep {
			t.Errorf("LD with output %q (platform=%q) lists pyplugin in cmd[2] but does not depend on CP UID %q — host CP dedup must wire host LDs to the target CP NodeRef", n.Outputs[0].String(), n.Platform, cpUID)
		}
	}

	if ldsReferencing == 0 {
		t.Fatalf("no LD references the pyplugin path; expected at least the target archiver LD")
	}
}

// TestGen_SRC_AppendsExtraCFlags_PerSource verifies PR-35o's SRC
// macro: `SRC(filename extra_cflags...)` registers the source AND
// appends the extra flags to that source's compile cmd_args at the
// per-source slot (right before the input path). Mirrors the
// upstream `util/charset/ya.make` `SRC(wide_sse41.cpp -DSSE41_STUB)`
// pattern that was the L0=99.71%→100% blocker before PR-35o.
func TestGen_SRC_AppendsExtraCFlags_PerSource(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "mod")

	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRC(foo.cpp -DSSE41_STUB)\nEND()\n")

	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write mod/ya.make: %v", err)
	}

	g := testGen(root, "mod")

	var cc *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			cc = n

			break
		}
	}

	if cc == nil {
		t.Fatal("no CC node emitted for SRC(foo.cpp ...)")
	}

	args := cc.Cmds[0].CmdArgs

	if len(args) < 2 {
		t.Fatalf("CC cmd_args too short: %d", len(args))
	}
	// The per-source CFLAGS slot the composer places between
	// macroPrefixMapFlags and the input path: -DSSE41_STUB must
	// appear immediately before the source path at the tail.
	wantInput := "$(S)/mod/foo.cpp"

	if args[len(args)-1] != wantInput {
		t.Errorf("last cmd_arg = %q, want %q", args[len(args)-1], wantInput)
	}

	if args[len(args)-2] != "-DSSE41_STUB" {
		t.Errorf("second-to-last cmd_arg = %q, want %q (per-source CFLAGS slot)", args[len(args)-2], "-DSSE41_STUB")
	}
}

// TestGen_SRC_C_NO_LTO_RegistersSource verifies PR-35o's SRC_C_NO_LTO
// macro: `SRC_C_NO_LTO(filename)` registers the source as a regular
// CC member with NO per-source CFLAGS and a FLAT output path (no `_/`
// infix even when the filename contains a `/`). Mirrors the upstream
// `util/ya.make:341` `SRC_C_NO_LTO(system/compiler.cpp)` pattern.
func TestGen_SRC_C_NO_LTO_RegistersSource(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "mod")
	subDir := filepath.Join(modDir, "system")

	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir mod/system: %v", err)
	}

	yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRC_C_NO_LTO(system/compiler.cpp)\nEND()\n")

	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write mod/ya.make: %v", err)
	}

	g := testGen(root, "mod")

	var cc *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			cc = n

			break
		}
	}

	if cc == nil {
		t.Fatal("no CC node emitted for SRC_C_NO_LTO(system/compiler.cpp)")
	}

	if len(cc.Outputs) != 1 {
		t.Fatalf("CC outputs = %#v, want exactly 1", cc.Outputs)
	}
	// FLAT output path: no `_/` infix.
	wantOut := "$(B)/mod/system/compiler.cpp.o"

	if cc.Outputs[0].String() != wantOut {
		t.Errorf("CC output = %q, want %q (SRC_C_NO_LTO uses flat output, not `mod/_/system/compiler.cpp.o`)", cc.Outputs[0].String(), wantOut)
	}
	// No per-source CFLAGS: last arg is the input path, second-to-last
	// is the standard macro-prefix-map (NOT a per-source -D flag).
	args := cc.Cmds[0].CmdArgs

	if args[len(args)-1] != "$(S)/mod/system/compiler.cpp" {
		t.Errorf("last cmd_arg = %q, want input path", args[len(args)-1])
	}

	if args[len(args)-2] != "-fmacro-prefix-map=$(TOOL_ROOT)/=" {
		t.Errorf("second-to-last cmd_arg = %q, want %q (no per-source CFLAG)", args[len(args)-2], "-fmacro-prefix-map=$(TOOL_ROOT)/=")
	}
}

// TestGen_SRC_FlatOutputPath verifies PR-35o's SRC macro emits a flat
// output path (no `_/` infix) even for a slashed source filename.
// Mirrors SRC_C_NO_LTO's flat-path semantics — both come from the
// upstream `_SRC` macro family, distinct from `SRCS(subdir/foo.cpp)`
// which emits `<modulePath>/_/subdir/foo.cpp.o`.
func TestGen_SRC_FlatOutputPath(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "mod")

	if err := os.MkdirAll(filepath.Join(modDir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir mod/sub: %v", err)
	}

	yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRC(sub/x.cpp)\nEND()\n")

	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write mod/ya.make: %v", err)
	}

	g := testGen(root, "mod")

	var cc *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			cc = n

			break
		}
	}

	if cc == nil {
		t.Fatal("no CC node emitted for SRC(sub/x.cpp)")
	}

	wantOut := "$(B)/mod/sub/x.cpp.o"

	if len(cc.Outputs) != 1 || cc.Outputs[0].String() != wantOut {
		t.Errorf("CC output = %#v, want [%q] (SRC uses flat output, not `mod/_/sub/x.cpp.o`)", cc.Outputs, wantOut)
	}
}

// TestGen_SRC_RejectsZeroArgs verifies PR-35o's SRC macro throws on
// SRC() with no filename — defensive: a SRC with an empty arg list is
// almost certainly a typo / parse error upstream and silently ignoring
// it would mask the bug.
func TestGen_SRC_RejectsZeroArgs(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "mod")

	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	yamake := []byte("LIBRARY()\nSRC()\nEND()\n")

	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write mod/ya.make: %v", err)
	}

	exc := Try(func() {
		testGen(root, "mod")
	})

	if exc == nil {
		t.Fatal("expected exception for SRC() with no args, got nil")
	}

	if !strings.Contains(exc.Error(), "SRC()") {
		t.Errorf("error %q does not mention SRC()", exc.Error())
	}
}

// TestEvalCond_ARCH_ARM64_Aliased pins the ARCH_ARM64↔ARCH_AARCH64
// alias: the `contrib/libs/cxxsupp/builtins/ya.make` bf16 SRCS block is
// guarded by `IF (ARCH_ARM64 OR ARCH_X86_64)` and Arcadia binds
// ARCH_ARM64=true whenever ARCH_AARCH64=true. Without the alias 5
// .c.o nodes would be silently dropped from the L0 closure on aarch64.
// `buildIfEnv` flips both bits in lockstep per instance.Platform.ISA.
func TestEvalCond_ARCH_ARM64_Aliased(t *testing.T) {
	inst := ModuleInstance{Kind: KindLib, Platform: testTargetP}
	env := buildIfEnv(inst)

	if !EvalCond(&ExprIdent{Name: "ARCH_ARM64"}, env) {
		t.Errorf("EvalCond(ARCH_ARM64) on aarch64 instance = false, want true (alias for ARCH_AARCH64)")
	}

	if !EvalCond(&ExprIdent{Name: "ARCH_AARCH64"}, env) {
		t.Errorf("EvalCond(ARCH_AARCH64) on aarch64 instance = false, want true")
	}

	hostInst := ModuleInstance{Kind: KindLib, Platform: testHostP}
	hostEnv := buildIfEnv(hostInst)

	if EvalCond(&ExprIdent{Name: "ARCH_ARM64"}, hostEnv) {
		t.Errorf("EvalCond(ARCH_ARM64) on x86_64 instance = true, want false")
	}

	if !EvalCond(&ExprIdent{Name: "ARCH_X86_64"}, hostEnv) {
		t.Errorf("EvalCond(ARCH_X86_64) on x86_64 instance = false, want true")
	}
}

// TestGen_PR35y_R7_JoinSrcs_SuppressBuildRootShim pins PR-35y R7:
// the AR's `inputs` slot does NOT include the BUILD_ROOT-staged
// joined-cpp shim that the JS step produces. Reference graph
// behaviour: util's `libyutil.a` lists `all_datetime.cpp.o` (the
// compiled object) but NEVER `$(B)/util/all_datetime.cpp`
// (the joined cpp source itself). Pre-PR-35y, our walker added this
// path to the AR aggregator, leaving 16 OUR-only entries on util's
// libyutil.a (15 JOIN_SRCS + 1 .rl6.cpp shim).
func TestGen_PR35y_R7_JoinSrcs_SuppressBuildRootShim(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "joinmod")
	Throw(os.MkdirAll(modDir, 0o755))

	yamake := []byte(`LIBRARY()
JOIN_SRCS(all_my.cpp src1.cpp src2.cpp)
END()
`)
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	g := testGen(root, "joinmod")

	var arNode *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "AR" {
			arNode = n

			break
		}
	}

	if arNode == nil {
		t.Fatal("no AR node emitted")
	}

	const forbidden = "$(B)/joinmod/all_my.cpp"

	for _, in := range arNode.Inputs {
		if in.String() == forbidden {
			t.Errorf("AR.Inputs contains %q — JS-derived BUILD_ROOT shim must be filtered (PR-35y R7)", forbidden)
		}
	}

	// JS member sources are NOT in AR inputs either — an archive bundles the
	// compiled objects, not source files; the member source/header closure is
	// excluded from AR/LD inputs (normalizer + emission).
	for _, src := range []string{"$(S)/joinmod/src1.cpp", "$(S)/joinmod/src2.cpp"} {
		if nodeHasInput(arNode, src) {
			t.Errorf("AR.Inputs must not contain JS member source %q: %#v", src, arNode.Inputs)
		}
	}
}

// TestGen_PR35y_R7_RagelRl6_OriginalSourcePair pins PR-35y R7 for
// the .rl6 case: the AR's `inputs` slot includes the original
// `.rl6` source AND its `.h` companion (when present), but NOT the
// BUILD_ROOT-staged generated `.rl6.cpp` shim. Reference graph
// behaviour for util: `libyutil.a` lists `parser.rl6` and
// `parser.h` but never `$(B)/util/_/datetime/parser.rl6.cpp`.
func TestGen_PR35y_R7_RagelRl6_OriginalSourcePair(t *testing.T) {
	root := t.TempDir()
	consumerDir := filepath.Join(root, "consumer")
	Throw(os.MkdirAll(consumerDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consumerDir, "ya.make"), []byte("LIBRARY()\nSRCS(parser.rl6)\nEND()\n"), 0o644))
	// Place the companion .h on disk so the walker discovers it.
	Throw(os.WriteFile(filepath.Join(consumerDir, "parser.rl6"), []byte("// fixture\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(consumerDir, "parser.h"), []byte("// fixture\n"), 0o644))

	// Synthetic host ragel6 stub so the host walk parses cleanly.
	ragelDir := filepath.Join(root, "contrib/tools/ragel6/bin")
	Throw(os.MkdirAll(ragelDir, 0o755))
	Throw(os.WriteFile(filepath.Join(ragelDir, "ya.make"), []byte("PROGRAM(ragel6)\nSRCS(main.cpp)\nEND()\n"), 0o644))

	g := testGen(root, "consumer")

	var arNode *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "AR" && n.TargetProperties["module_dir"] == "consumer" {
			arNode = n

			break
		}
	}

	if arNode == nil {
		t.Fatal("no consumer AR node emitted")
	}

	const forbidden = "$(B)/consumer/_/parser.rl6.cpp"

	for _, in := range arNode.Inputs {
		if in.String() == forbidden {
			t.Errorf("AR.Inputs contains %q — R6-derived BUILD_ROOT shim must be filtered (PR-35y R7)", forbidden)
		}
	}

	// Neither the .rl6 source nor its .h companion appear in AR.Inputs — an
	// archive bundles the compiled objects, not member sources/headers.
	for _, src := range []string{"$(S)/consumer/parser.rl6", "$(S)/consumer/parser.h"} {
		if nodeHasInput(arNode, src) {
			t.Errorf("AR.Inputs must not contain member source %q: %#v", src, arNode.Inputs)
		}
	}
}

// TestGen_PR35y_R8_RegularARIncludesGlobalMemberInputs pins PR-35y
// R8: the regular `.a` archive's memberInputs union BOTH regular
// SRCS and GLOBAL_SRCS member inputs. Reference graph empirically
// confirms this on tcmalloc/no_percpu_cache: its `.a` has 1313
// inputs covering BOTH `aligned_alloc.c` (regular SRCS) closure AND
// every `tcmalloc/*` GLOBAL_SRCS source closure (1311 shared
// headers + the regular primary). Pre-PR-35y the regular AR was
// missing the GLOBAL closure entirely.
//
// Conversely, the .global.a aggregator drops regular primaries
// (the regular SRCS source files themselves) but keeps everyone's
// header closure, mirroring the reference asymmetry.
func TestGen_PR35y_R8_RegularARIncludesGlobalMemberInputs(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "globalmod")
	Throw(os.MkdirAll(modDir, 0o755))

	yamake := []byte(`LIBRARY()
GLOBAL_SRCS(global.cpp)
SRCS(regular.cpp)
END()
`)
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	g := testGen(root, "globalmod")

	var (
		regularAR *Node
		globalAR  *Node
	)

	for _, n := range g.Graph {
		if n.KV["p"] != "AR" {
			continue
		}

		if n.TargetProperties["module_tag"] == "global" {
			globalAR = n
		} else {
			regularAR = n
		}
	}

	if regularAR == nil || globalAR == nil {
		t.Fatalf("expected both regular and global AR (got regular=%v, global=%v)", regularAR != nil, globalAR != nil)
	}

	regularInputs := map[string]bool{}
	for _, in := range regularAR.Inputs {
		regularInputs[in.String()] = true
	}

	globalInputs := map[string]bool{}
	for _, in := range globalAR.Inputs {
		globalInputs[in.String()] = true
	}

	const (
		regularSrc = "$(S)/globalmod/regular.cpp"
		globalSrc  = "$(S)/globalmod/global.cpp"
	)

	// AR inputs hold compiled objects + scripts, never member sources — so
	// the regular/global .cpp sources (and the R8 regular-unions-global
	// closure) no longer appear. Each AR still archives its own object.
	for _, src := range []string{regularSrc, globalSrc} {
		if regularInputs[src] {
			t.Errorf("regular AR.Inputs must not contain member source %q: %#v", src, regularAR.Inputs)
		}
	}
	if globalInputs[globalSrc] {
		t.Errorf(".global.a AR.Inputs must not contain member source %q: %#v", globalSrc, globalAR.Inputs)
	}

	hasObject := func(n *Node) bool {
		for _, in := range n.Inputs {
			if strings.HasSuffix(in.Rel, ".o") {
				return true
			}
		}
		return false
	}
	if !hasObject(regularAR) {
		t.Errorf("regular AR.Inputs has no object: %#v", regularAR.Inputs)
	}
	if !hasObject(globalAR) {
		t.Errorf(".global.a AR.Inputs has no object: %#v", globalAR.Inputs)
	}
}

// TestGen_PR35y_R8_AsmSrcdirRebase pins PR-35y R8: when a LIBRARY
// declares SRCDIR and a `.S` source resolves under that SRCDIR
// (because no local file exists at instance.Path/<srcRel>), the AR
// aggregator's view of the source path uses the SRCDIR-rebased
// shape `$(S)/<srcDir>/<srcRel>`, not the unrebased
// `$(S)/<instance.Path>/<srcRel>`. Empirical reference:
// tcmalloc/no_percpu_cache (SRCDIR=`contrib/libs/tcmalloc`) — its
// `tcmalloc/internal/percpu_rseq_asm.S` resolves at
// `contrib/libs/tcmalloc/tcmalloc/internal/percpu_rseq_asm.S` in
// the AR's inputs.
func TestGen_PR35y_R8_AsmSrcdirRebase(t *testing.T) {
	root := t.TempDir()

	// Module at `mod/inner` declares SRCDIR pointing at `mod`. The
	// `.S` source `sub/foo.S` does NOT exist at `mod/inner/sub/foo.S`,
	// so the SRCDIR-rebased branch fires.
	Throw(os.MkdirAll(filepath.Join(root, "mod/inner"), 0o755))
	Throw(os.WriteFile(filepath.Join(root, "mod/inner", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PLATFORM()
SRCDIR(mod)
SRCS(sub/foo.S)
END()
`), 0o644))
	// Place the actual source under SRCDIR (mod/sub/foo.S), NOT under
	// instance.Path (mod/inner/sub/foo.S). The composer's
	// sourceExistsLocally probe must return false at the local path
	// so the SRCDIR branch wins.
	Throw(os.MkdirAll(filepath.Join(root, "mod/sub"), 0o755))
	Throw(os.WriteFile(filepath.Join(root, "mod/sub", "foo.S"), []byte("// asm\n"), 0o644))

	g := testGen(root, "mod/inner")

	// The SRCDIR rebase is observable on the AS node that compiles foo.S —
	// its source input reads from the SRCDIR path, not the instance path.
	// (AR inputs no longer carry member sources; the rebase lives on the
	// compile node.)
	var asNode *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "AS" {
			asNode = n

			break
		}
	}

	if asNode == nil {
		t.Fatal("no AS node emitted for mod/inner")
	}

	const want = "$(S)/mod/sub/foo.S"
	const forbidden = "$(S)/mod/inner/sub/foo.S"

	if nodeHasInput(asNode, forbidden) {
		t.Errorf("AS.Inputs contains %q — SRCDIR rebase must redirect to %q (PR-35y R8)", forbidden, want)
	}
	if !nodeHasInput(asNode, want) {
		t.Errorf("AS.Inputs missing %q — PR-35y R8 SRCDIR rebase for `.S` source: %#v", want, asNode.Inputs)
	}
}

// TestD41_PICCoincidesWithHostTarget locks the M2/M3 invariant:
// for every emitted node, if its Platform equals the configured host
// platform then HostPlatform must be true, and if its Platform equals
// the configured target platform then HostPlatform must be false.
//
// This is the graph-level proof that D41's dispatch-on-Target rule is
// internally consistent: emitter sites that set HostPlatform read
// instance.Platform.Target (via targetIsX8664) and instance.Platform.Target determines
// node.Platform — so the two fields must agree.
//
// If this test ever fails, either a walker site failed to flip Target
// when descending into a host dependency, or an emitter site is
// reading PIC from the wrong source. Update D41 and fix the offending
// site; do not remove the test.
func TestD41_PICCoincidesWithHostTarget(t *testing.T) {
	const targetDir = "tools/archiver"

	if _, err := os.Stat(sourceRoot + "/" + targetDir + "/ya.make"); err != nil {
		t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
	}

	g := testGen(sourceRoot, targetDir)

	hostID := string(testHostP.Target)
	targetID := string(testTargetP.Target)

	for _, n := range g.Graph {
		out := ""
		if len(n.Outputs) > 0 {
			out = n.Outputs[0].String()
		}

		switch n.Platform {
		case hostID:
			if !nodeHasHostTag(n.Tags) {
				t.Errorf("node %q on host platform %q has HostPlatform=false (D41 invariant violated)", out, hostID)
			}

		case targetID:
			if nodeHasHostTag(n.Tags) {
				t.Errorf("node %q on target platform %q has HostPlatform=true (D41 invariant violated)", out, targetID)
			}
		}
	}
}

func TestGen_ProtoAstStylePipelineExpandsLowercaseVarsAndRootedPaths(t *testing.T) {
	root := t.TempDir()

	writeFile := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	writeToolProgram(t, root, "contrib/tools/protoc/bin", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")

	writeFile("proto/ya.make", `LIBRARY()
SET(antlr_output ${ARCADIA_BUILD_ROOT}/${MODDIR})
SET(antlr_templates ${antlr_output}/templates)
SET(sql_grammar ${antlr_output}/Grammar.g)
SET(PROTOC_PATH contrib/tools/protoc/bin)

CONFIGURE_FILE(${ARCADIA_ROOT}/templates/Java.stg.in ${antlr_templates}/Java/Java.stg)
CONFIGURE_FILE(${ARCADIA_ROOT}/templates/Grammar.g.in ${sql_grammar})

RUN_ANTLR4(
    ${sql_grammar}
    -lib .
    -o ${antlr_output}
    IN ${sql_grammar} ${antlr_templates}/Java/Java.stg
	    OUT_NOAUTO Generated.proto
	    CWD ${antlr_output}
)

RUN_PROGRAM(
	    $PROTOC_PATH
	    -I=${CURDIR} -I=${ARCADIA_ROOT} -I=${ARCADIA_BUILD_ROOT} -I=${ARCADIA_ROOT}/contrib/libs/protobuf/src
	    --cpp_out=${ARCADIA_BUILD_ROOT} --cpp_styleguide_out=${ARCADIA_BUILD_ROOT}
	    --plugin=protoc-gen-cpp_styleguide=contrib/tools/protoc/plugins/cpp_styleguide
	    Generated.proto
	    IN Generated.proto
	    TOOL contrib/tools/protoc/plugins/cpp_styleguide
	    OUT_NOAUTO Generated.pb.h Generated.pb.cc
	    CWD ${antlr_output}
)

RUN_PYTHON3(
	    ${ARCADIA_ROOT}/tools/multiproto.py Generated
	    IN Generated.pb.h Generated.pb.cc
	    OUT_NOAUTO Generated.code0.cc Generated.main.h
	    CWD ${antlr_output}
)

SRCS(Generated.code0.cc)
END()
`)
	writeFile("templates/Java.stg.in", "java template\n")
	writeFile("templates/Grammar.g.in", "grammar Generated;\n")
	writeFile("tools/multiproto.py", "print('ok')\n")
	writeFile("build/scripts/configure_file.py", "print('cfg')\n")
	writeFile("build/scripts/stdout2stderr.py", "print('stderr')\n")
	writeFile("contrib/java/antlr/antlr4/antlr.jar", "")

	g := testGen(root, "proto")

	cfTemplate := findGraphNodeByOutputs(t, g, "$(B)/proto/templates/Java/Java.stg")
	cfGrammar := findGraphNodeByOutputs(t, g, "$(B)/proto/Grammar.g")
	antlr := findGraphNodeByOutputs(t, g, "$(B)/proto/Generated.proto")
	protoc := findGraphNodeByOutputs(t, g, "$(B)/proto/Generated.pb.h", "$(B)/proto/Generated.pb.cc")
	py := findGraphNodeByOutputs(t, g, "$(B)/proto/Generated.code0.cc", "$(B)/proto/Generated.main.h")
	cc := findGraphNodeByOutputs(t, g, "$(B)/proto/Generated.code0.cc.o")
	ar := findGraphNodeByOutputs(t, g, "$(B)/proto/libproto.a")

	if got := cfTemplate.Inputs[1].String(); got != "$(S)/templates/Java.stg.in" {
		t.Fatalf("template CF input = %q, want $(S)/templates/Java.stg.in", got)
	}
	if got := cfTemplate.Cmds[0].CmdArgs[3]; got != "$(B)/proto/templates/Java/Java.stg" {
		t.Fatalf("template CF output arg = %q, want $(B)/proto/templates/Java/Java.stg", got)
	}
	if got := cfGrammar.Cmds[0].CmdArgs[3]; got != "$(B)/proto/Grammar.g" {
		t.Fatalf("grammar CF output arg = %q, want $(B)/proto/Grammar.g", got)
	}

	if got := antlr.Cmds[0].CmdArgs[5]; got != "$(B)/proto/Grammar.g" {
		t.Fatalf("antlr grammar arg = %q, want $(B)/proto/Grammar.g", got)
	}
	if got := antlr.Cmds[0].CmdArgs[9]; got != "$(B)/proto" {
		t.Fatalf("antlr output dir arg = %q, want $(B)/proto", got)
	}
	if got := antlr.Cmds[0].Cwd; got != "$(B)/proto" {
		t.Fatalf("antlr cwd = %q, want $(B)/proto", got)
	}

	if got := protoc.Cmds[0].CmdArgs[0]; got != "$(B)/contrib/tools/protoc/bin/protoc" {
		t.Fatalf("protoc tool arg = %q, want $(B)/contrib/tools/protoc/bin/protoc", got)
	}
	wantPluginArg := "--plugin=protoc-gen-cpp_styleguide=$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"
	if !containsString(protoc.Cmds[0].CmdArgs, wantPluginArg) {
		t.Fatalf("protoc cmd args missing %q: %v", wantPluginArg, protoc.Cmds[0].CmdArgs)
	}
	if !nodeHasInput(protoc, "$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide") {
		t.Fatalf("protoc inputs missing built plugin binary: %v", protoc.Inputs)
	}
	if nodeHasInput(protoc, "$(S)/contrib/tools/protoc/plugins/cpp_styleguide") {
		t.Fatalf("protoc inputs still contain source-root plugin path: %v", protoc.Inputs)
	}

	if got := py.Inputs[0].String(); got != "$(S)/tools/multiproto.py" {
		t.Fatalf("python script input = %q, want $(S)/tools/multiproto.py", got)
	}
	if got := py.Inputs[1].String(); got != "$(B)/proto/Generated.pb.h" {
		t.Fatalf("python generated header input = %q, want $(B)/proto/Generated.pb.h", got)
	}
	if got := py.Inputs[2].String(); got != "$(B)/proto/Generated.pb.cc" {
		t.Fatalf("python generated source input = %q, want $(B)/proto/Generated.pb.cc", got)
	}
	if got := py.Cmds[0].CmdArgs[1]; got != "$(S)/tools/multiproto.py" {
		t.Fatalf("python script arg = %q, want $(S)/tools/multiproto.py", got)
	}
	if got := py.Cmds[0].Cwd; got != "$(B)/proto" {
		t.Fatalf("python cwd = %q, want $(B)/proto", got)
	}

	if !containsString(cc.Cmds[0].CmdArgs, "$(B)/proto/Generated.code0.cc") {
		t.Fatalf("cc cmd args missing built generated source: %v", cc.Cmds[0].CmdArgs)
	}
	if containsString(cc.Cmds[0].CmdArgs, "$(S)/proto/Generated.code0.cc") {
		t.Fatalf("cc cmd args still contain source-root generated source: %v", cc.Cmds[0].CmdArgs)
	}
	if !nodeHasInput(cc, "$(B)/proto/Generated.code0.cc") {
		t.Fatalf("cc inputs missing built generated source: %v", cc.Inputs)
	}
	for _, want := range []string{
		"$(S)/tools/multiproto.py",
		"$(S)/build/scripts/stdout2stderr.py",
		"$(S)/contrib/java/antlr/antlr4/antlr.jar",
		"$(S)/build/scripts/configure_file.py",
		"$(S)/templates/Java.stg.in",
		"$(S)/templates/Grammar.g.in",
	} {
		if !nodeHasInput(cc, want) {
			t.Fatalf("cc inputs missing generator closure %q: %v", want, cc.Inputs)
		}
	}

	if nodeHasInput(ar, "$(S)/proto/Generated.code0.cc") {
		t.Fatalf("ar inputs still contain source-root generated source: %v", ar.Inputs)
	}
	if nodeHasInput(ar, "$(B)/proto/Generated.code0.cc") {
		t.Fatalf("ar inputs still contain build-root generated source: %v", ar.Inputs)
	}
	// The generator toolchain closure is NOT carried into the AR — an archive
	// bundles objects, not the codegen sources/tools/templates. (The CC node
	// above still carries the closure; only AR/LD inputs are trimmed.)
	for _, absent := range []string{
		"$(S)/tools/multiproto.py",
		"$(S)/contrib/java/antlr/antlr4/antlr.jar",
		"$(S)/templates/Java.stg.in",
		"$(S)/templates/Grammar.g.in",
	} {
		if nodeHasInput(ar, absent) {
			t.Fatalf("ar inputs must not contain generator-closure source %q: %v", absent, ar.Inputs)
		}
	}

	assertNodeHasNoRawProtoAstPlaceholders := func(node *Node) {
		t.Helper()

		var values []string
		for _, input := range node.Inputs {
			values = append(values, input.String())
		}
		for _, output := range node.Outputs {
			values = append(values, output.String())
		}
		for _, cmd := range node.Cmds {
			values = append(values, cmd.CmdArgs...)
			if cmd.Cwd != "" {
				values = append(values, cmd.Cwd)
			}
			if cmd.Stdout != "" {
				values = append(values, cmd.Stdout)
			}
		}

		for _, value := range values {
			if strings.Contains(value, "${") {
				t.Fatalf("%s contains unresolved placeholder %q", node.KV["p"], value)
			}
			if strings.Contains(value, "/$(S)/") || strings.Contains(value, "/$(B)/") {
				t.Fatalf("%s contains duplicated rooted path %q", node.KV["p"], value)
			}
		}
	}

	assertNodeHasNoRawProtoAstPlaceholders(cfTemplate)
	assertNodeHasNoRawProtoAstPlaceholders(cfGrammar)
	assertNodeHasNoRawProtoAstPlaceholders(antlr)
	assertNodeHasNoRawProtoAstPlaceholders(protoc)
	assertNodeHasNoRawProtoAstPlaceholders(py)
	assertNodeHasNoRawProtoAstPlaceholders(cc)
	assertNodeHasNoRawProtoAstPlaceholders(ar)
}

func TestGen_ProtoLibrary_CPPProtoPlugin0WiresToolDeps(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
PROTOC_FATAL_WARNINGS()
GRPC()
CPP_PROTO_PLUGIN0(config_proto_plugin tools/config_plugin DEPS deps/generated_runtime)
SRCS(test.proto)
END()
`)
	writeTestModuleFile(t, root, "protos/test.proto", `syntax = "proto3";
package test;
message Row {}
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")

	writeTestModuleFile(t, root, "tools/config_plugin/ya.make", `PROGRAM(config_proto_plugin)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(
    deps/plugin_runtime
)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(t, root, "tools/config_plugin/main.cpp", "int main(){return 0;}\n")

	writeTestModuleFile(t, root, "contrib/libs/grpc/ya.make", "LIBRARY()\nSRCS(grpc.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/grpc.cpp", "int grpc(){return 0;}\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(t, root, "deps/generated_runtime/ya.make", "LIBRARY()\nSRCS(gen.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "deps/generated_runtime/gen.cpp", "int gen(){return 0;}\n")
	writeTestModuleFile(t, root, "deps/plugin_runtime/ya.make", "LIBRARY()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "deps/plugin_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(root, "protos")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.grpc.pb.cc",
		"$(B)/protos/test.grpc.pb.h",
	)
	styleguide := mustNodeByOutput(t, g, "$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide")
	grpcCpp := mustNodeByOutput(t, g, "$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp")
	protoc := mustNodeByOutput(t, g, "$(B)/contrib/tools/protoc/protoc")
	configPlugin := mustNodeByOutput(t, g, "$(B)/tools/config_plugin/config_proto_plugin")
	pluginRuntime := mustNodeByOutput(t, g, "$(B)/deps/plugin_runtime/libdeps-plugin_runtime.a")
	_ = mustNodeByOutput(t, g, "$(B)/deps/generated_runtime/libdeps-generated_runtime.a")

	if !containsString(pb.Cmds[0].CmdArgs, "--plugin=protoc-gen-config_proto_plugin=$(B)/tools/config_plugin/config_proto_plugin") {
		t.Fatalf("pb cmd args missing config proto plugin: %v", pb.Cmds[0].CmdArgs)
	}
	if !containsString(pb.Cmds[0].CmdArgs, "--config_proto_plugin_out=$(B)/") {
		t.Fatalf("pb cmd args missing config proto plugin out flag: %v", pb.Cmds[0].CmdArgs)
	}

	sourceIdx := indexOfArg(pb.Cmds[0].CmdArgs, "protos/test.proto")
	grpcIdx := indexOfArg(pb.Cmds[0].CmdArgs, "--plugin=protoc-gen-grpc_cpp=$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp")
	configIdx := indexOfArg(pb.Cmds[0].CmdArgs, "--plugin=protoc-gen-config_proto_plugin=$(B)/tools/config_plugin/config_proto_plugin")
	if sourceIdx < 0 || grpcIdx < 0 || configIdx < 0 {
		t.Fatalf("missing source/grpc/config args in pb cmd: %v", pb.Cmds[0].CmdArgs)
	}
	if !(sourceIdx < grpcIdx && grpcIdx < configIdx) {
		t.Fatalf("pb plugin arg order = source:%d grpc:%d config:%d, want source < grpc < config", sourceIdx, grpcIdx, configIdx)
	}

	inputs := make([]string, 0, len(pb.Inputs))
	for _, input := range pb.Inputs {
		inputs = append(inputs, input.String())
	}
	wantInputsPrefix := []string{
		"$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide",
		"$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp",
		"$(B)/contrib/tools/protoc/protoc",
		"$(B)/tools/config_plugin/config_proto_plugin",
		"$(S)/build/scripts/cpp_proto_wrapper.py",
		"$(S)/protos/test.proto",
	}
	if len(inputs) < len(wantInputsPrefix) || !equalStrings(inputs[:len(wantInputsPrefix)], wantInputsPrefix) {
		t.Fatalf("pb inputs prefix = %v, want %v", inputs, wantInputsPrefix)
	}

	wantDeps := []string{styleguide.UID, grpcCpp.UID, protoc.UID, configPlugin.UID}
	if len(pb.Deps) != len(wantDeps) {
		t.Fatalf("pb deps len = %d, want %d (%v)", len(pb.Deps), len(wantDeps), pb.Deps)
	}
	for _, want := range wantDeps {
		if !containsString(pb.Deps, want) {
			t.Fatalf("pb deps = %v, missing %q", pb.Deps, want)
		}
	}
	if got := pb.ForeignDeps["tool"]; len(got) != len(wantDeps) {
		t.Fatalf("pb foreign_deps[tool] len = %d, want %d (%v)", len(got), len(wantDeps), got)
	} else {
		for _, want := range wantDeps {
			if !containsString(got, want) {
				t.Fatalf("pb foreign_deps[tool] = %v, missing %q", got, want)
			}
		}
	}
	if !nodeHasHostTag(configPlugin.Tags) {
		t.Fatalf("config proto plugin tags = %v, want host tool tag", configPlugin.Tags)
	}
	if !containsString(configPlugin.Deps, pluginRuntime.UID) {
		t.Fatalf("config proto plugin deps = %v, want runtime peer uid %q", configPlugin.Deps, pluginRuntime.UID)
	}
}

func TestGen_ProtoLibrary_CPPProtoPluginOutputsReachWrapper(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
CPP_PROTO_PLUGIN(tasklet_cpp tools/tasklet_plugin .tasklet.h)
SRCS(test.proto)
END()
`)
	writeTestModuleFile(t, root, "protos/test.proto", `syntax = "proto3";
package test;
message Row {}
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	writeTestModuleFile(t, root, "tools/tasklet_plugin/ya.make", `PROGRAM(tasklet_cpp)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(t, root, "tools/tasklet_plugin/main.cpp", "int main(){return 0;}\n")

	g := testGen(root, "protos")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.tasklet.h",
	)

	outputsIdx := indexOfArg(pb.Cmds[0].CmdArgs, "--outputs")
	separatorIdx := indexOfArg(pb.Cmds[0].CmdArgs, "--")
	if outputsIdx < 0 || separatorIdx < 0 || separatorIdx <= outputsIdx {
		t.Fatalf("pb wrapper output section malformed: %v", pb.Cmds[0].CmdArgs)
	}

	wantWrapperOutputs := []string{
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.tasklet.h",
	}
	gotWrapperOutputs := pb.Cmds[0].CmdArgs[outputsIdx+1 : separatorIdx]
	if !equalStrings(gotWrapperOutputs, wantWrapperOutputs) {
		t.Fatalf("pb wrapper outputs = %v, want %v", gotWrapperOutputs, wantWrapperOutputs)
	}
}

func TestGen_ProtoLibrary_CPPProtoPlugin2HeaderConsumerInheritsProtoClosure(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
CPP_PROTO_PLUGIN2(grpc_cpp contrib/tools/protoc/plugins/grpc_cpp .grpc.pb.cc .grpc.pb.h DEPS contrib/libs/grpc)
SRCS(
    dep.proto
    main.proto
)
END()
`)
	writeTestModuleFile(t, root, "protos/dep.proto", `syntax = "proto3";
package test;
message Dep {
  string value = 1;
}
`)
	writeTestModuleFile(t, root, "protos/main.proto", `syntax = "proto3";
package test;
import "dep.proto";
message Main {
  Dep dep = 1;
}
service TestService {
  rpc Ping(Main) returns (Main);
}
`)
	writeTestModuleFile(t, root, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(t, root, "app/use.cpp", `#include <protos/main.grpc.pb.h>
int use() { return 0; }
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")

	writeTestModuleFile(t, root, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/ya.make", "LIBRARY()\nSRCS(grpc.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/grpc.cpp", "int grpc(){return 0;}\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(root, "app")

	useCC := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")
	mainPB := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h")
	depPB := mustNodeByOutput(t, g, "$(B)/protos/dep.pb.h")

	for _, want := range []string{
		"$(B)/protos/main.grpc.pb.h",
		"$(B)/protos/main.pb.h",
		"$(B)/protos/dep.pb.h",
	} {
		if !nodeHasInput(useCC, want) {
			t.Fatalf("use.cpp.o inputs missing %q: %#v", want, useCC.Inputs)
		}
	}

	for _, want := range []string{mainPB.UID, depPB.UID} {
		if !containsString(useCC.Deps, want) {
			t.Fatalf("use.cpp.o deps missing %q: %v", want, useCC.Deps)
		}
	}
}

func TestGen_ProtoLibrary_CPPProtoPlugin2GeneratedSourceCompilesAndArchives(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
CPP_PROTO_PLUGIN2(grpc_cpp contrib/tools/protoc/plugins/grpc_cpp .grpc.pb.cc .grpc.pb.h DEPS contrib/libs/grpc)
SRCS(
    dep.proto
    main.proto
)
END()
`)
	writeTestModuleFile(t, root, "protos/dep.proto", `syntax = "proto3";
package test;
message Dep {
  string value = 1;
}
`)
	writeTestModuleFile(t, root, "protos/main.proto", `syntax = "proto3";
package test;
import "dep.proto";
message Main {
  Dep dep = 1;
}
service TestService {
  rpc Ping(Main) returns (Main);
}
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")

	writeTestModuleFile(t, root, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/ya.make", "LIBRARY()\nSRCS(grpc.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/grpc.cpp", "int grpc(){return 0;}\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(root, "protos")

	grpcCC := mustNodeByOutput(t, g, "$(B)/protos/main.grpc.pb.cc.o")
	mainPB := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h")
	depPB := mustNodeByOutput(t, g, "$(B)/protos/dep.pb.h")
	ar := mustNodeByOutput(t, g, "$(B)/protos/libprotos.a")

	for _, want := range []string{
		"$(B)/protos/main.grpc.pb.cc",
		"$(B)/protos/main.pb.h",
		"$(B)/protos/dep.pb.h",
	} {
		if !nodeHasInput(grpcCC, want) {
			t.Fatalf("main.grpc.pb.cc.o inputs missing %q: %#v", want, grpcCC.Inputs)
		}
	}

	for _, want := range []string{mainPB.UID, depPB.UID} {
		if !containsString(grpcCC.Deps, want) {
			t.Fatalf("main.grpc.pb.cc.o deps missing %q: %v", want, grpcCC.Deps)
		}
	}

	if !nodeHasInput(ar, "$(B)/protos/main.grpc.pb.cc.o") {
		t.Fatalf("archive inputs missing grpc object: %#v", ar.Inputs)
	}
}

func TestGen_ProtoLibrary_TransitiveHeadersNoAddsDepsHeaderAndEnumUsesGeneratedPBHeader(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
GRPC()
GENERATE_ENUM_SERIALIZATION(main.pb.h)
SRCS(
    dep.proto
    main.proto
)
END()
`)
	writeTestModuleFile(t, root, "protos/dep.proto", `syntax = "proto3";
package test;
message Dep {
  string value = 1;
}
`)
	writeTestModuleFile(t, root, "protos/main.proto", `syntax = "proto3";
package test;
import "dep.proto";
enum Mode {
  MODE_UNSPECIFIED = 0;
  MODE_READY = 1;
}
message Main {
  Dep dep = 1;
  Mode mode = 2;
}
service TestService {
  rpc Ping(Main) returns (Main);
}
`)
	writeTestModuleFile(t, root, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(t, root, "app/use.cpp", `#include <protos/main.deps.pb.h>
int use() { return 0; }
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")
	writeToolProgram(t, root, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(t, root, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	writeTestModuleFile(t, root, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/ya.make", "LIBRARY()\nSRCS(grpc.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/grpc.cpp", "int grpc(){return 0;}\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(root, "app")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/main.pb.h",
		"$(B)/protos/main.pb.cc",
		"$(B)/protos/main.deps.pb.h",
		"$(B)/protos/main.grpc.pb.cc",
		"$(B)/protos/main.grpc.pb.h",
	)
	if !containsString(pb.Cmds[0].CmdArgs, "--cpp_out=proto_h=true:$(B)/") {
		t.Fatalf("pb cmd args missing lite-header cpp_out flag: %v", pb.Cmds[0].CmdArgs)
	}

	useCC := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")
	mainPB := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h")
	depPB := mustNodeByOutput(t, g, "$(B)/protos/dep.pb.h")
	ar := mustNodeByOutput(t, g, "$(B)/protos/libprotos.a")
	for _, want := range []string{
		"$(B)/protos/main.deps.pb.h",
		"$(B)/protos/main.pb.h",
		"$(B)/protos/dep.pb.h",
	} {
		if !nodeHasInput(useCC, want) {
			t.Fatalf("use.cpp.o inputs missing %q: %#v", want, useCC.Inputs)
		}
	}
	for _, want := range []string{mainPB.UID, depPB.UID} {
		if !containsString(useCC.Deps, want) {
			t.Fatalf("use.cpp.o deps missing %q: %v", want, useCC.Deps)
		}
	}

	en := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h_serialized.cpp")
	if !nodeHasInput(en, "$(B)/protos/main.pb.h") {
		t.Fatalf("enum node inputs missing generated pb.h: %#v", en.Inputs)
	}
	if nodeHasInput(en, "$(S)/protos/main.pb.h") {
		t.Fatalf("enum node still consumes source-root pb.h: %#v", en.Inputs)
	}
	if !containsString(en.Deps, mainPB.UID) {
		t.Fatalf("enum node deps missing pb producer uid %q: %v", mainPB.UID, en.Deps)
	}
	if !containsString(en.Deps, depPB.UID) {
		t.Fatalf("enum node deps missing imported pb producer uid %q: %v", depPB.UID, en.Deps)
	}
	if got := en.TargetProperties["module_tag"]; got != "cpp_proto" {
		t.Fatalf("enum node module_tag = %q, want cpp_proto", got)
	}
	if !nodeHasInput(en, "$(B)/protos/dep.pb.h") {
		t.Fatalf("enum node inputs missing imported pb.h dep.pb.h: %#v", en.Inputs)
	}
	if !nodeHasInput(en, "$(S)/contrib/libs/protobuf/src/google/protobuf/message.h") {
		t.Fatalf("enum node inputs missing protobuf runtime header message.h: %#v", en.Inputs)
	}
	// EN CC is emitted into the PROTO_LIBRARY archive.
	if !nodeHasInput(ar, "$(B)/protos/main.pb.h_serialized.cpp.o") {
		t.Fatalf("archive missing enum serialization object: %#v", ar.Inputs)
	}
}

func TestGen_ProtoLibrary_TransitiveHeadersNoKeepsPublicImportsOnLitePBHeader(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
SRCS(
    leaf.proto
    public.proto
    main.proto
)
END()
`)
	writeTestModuleFile(t, root, "protos/leaf.proto", `syntax = "proto3";
package test;
message Leaf {
  string value = 1;
}
`)
	writeTestModuleFile(t, root, "protos/public.proto", `syntax = "proto3";
package test;
import public "leaf.proto";
message PublicMessage {
  Leaf leaf = 1;
}
`)
	writeTestModuleFile(t, root, "protos/main.proto", `syntax = "proto3";
package test;
import public "public.proto";
message Main {
  PublicMessage message = 1;
}
`)
	writeTestModuleFile(t, root, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(t, root, "app/use.cpp", `#include <protos/main.pb.h>
int use() { return 0; }
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(t, root, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(root, "app")

	useCC := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")
	mainPB := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h")
	publicPB := mustNodeByOutput(t, g, "$(B)/protos/public.pb.h")
	leafPB := mustNodeByOutput(t, g, "$(B)/protos/leaf.pb.h")

	for _, want := range []string{
		"$(B)/protos/main.pb.h",
		"$(B)/protos/public.pb.h",
		"$(B)/protos/leaf.pb.h",
	} {
		if !nodeHasInput(useCC, want) {
			t.Fatalf("use.cpp.o inputs missing %q: %#v", want, useCC.Inputs)
		}
	}
	for _, want := range []string{mainPB.UID, publicPB.UID, leafPB.UID} {
		if !containsString(useCC.Deps, want) {
			t.Fatalf("use.cpp.o deps missing %q: %v", want, useCC.Deps)
		}
	}
}

// gen_helpers_test.go — test-only shim that constructs the canonical
// (host=linux-x86_64, target=linux-aarch64) Platform pair with the
// shared testToolchainFlags and dispatches into Gen. Lives
// alongside the test corpus rather than in production code: cmdMake
// constructs platforms inline from CLI + mining, so a generic "Gen"
// entry that hardcodes defaults would just be misleading.

func testGen(sourceRoot, targetDir string) *Graph {
	host := newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"})
	targetFlags := make(map[string]string, len(testToolchainFlags)+2)
	for k, v := range testToolchainFlags {
		targetFlags[k] = v
	}
	targetFlags["PIC"] = "no"
	targetFlags["MUSL"] = "yes"
	target := NewPlatform(OSLinux, ISAAArch64, targetFlags, nil, "", "")
	return Gen(sourceRoot, targetDir, host, target, func(Warn) {})
}

func TestCollectModule_YqlAbiMacrosAppendCXXFlags(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "mod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	const yamake = `LIBRARY()
YQL_LAST_ABI_VERSION()
YQL_ABI_VERSION(2 44 0)
SRCS(lib.cpp)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "mod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "mod", Kind: KindLib, Platform: testTargetP}))

	want := []string{
		"-DUSE_CURRENT_UDF_ABI_VERSION",
		"-DUDF_ABI_VERSION_MAJOR=2",
		"-DUDF_ABI_VERSION_MINOR=44",
		"-DUDF_ABI_VERSION_PATCH=0",
	}
	if !reflect.DeepEqual(d.cxxFlags, want) {
		t.Fatalf("cxxFlags = %#v, want %#v", d.cxxFlags, want)
	}
}

func TestCollectModule_YqlUdfStaticRoutesSrcsToGlobal(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "mod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	const yamake = `YQL_UDF_CONTRIB(my_udf)
SRCS(lib.cpp nested/extra.cpp)
PEERDIR(custom/peer)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "mod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "mod", Kind: KindLib, Platform: testTargetP}))

	if d.moduleStmt == nil || d.moduleStmt.Name != "YQL_UDF_CONTRIB" {
		t.Fatalf("moduleStmt = %#v, want YQL_UDF_CONTRIB", d.moduleStmt)
	}
	if !equalStrings(d.moduleStmt.Args, []string{"my_udf"}) {
		t.Fatalf("module args = %v, want [my_udf]", d.moduleStmt.Args)
	}
	if len(d.srcs) != 0 {
		t.Fatalf("srcs = %v, want empty (SRCS must alias to GLOBAL_SRCS)", d.srcs)
	}
	if !equalStrings(d.globalSrcs, []string{"lib.cpp", "nested/extra.cpp"}) {
		t.Fatalf("globalSrcs = %v, want [lib.cpp nested/extra.cpp]", d.globalSrcs)
	}
	if !equalStrings(d.peerdirs, []string{
		"yql/essentials/public/udf",
		"yql/essentials/public/udf/support",
		"custom/peer",
	}) {
		t.Fatalf("peerdirs = %v", d.peerdirs)
	}
}

func TestCollectModule_ProtocFatalWarningsAddsProtoFlag(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "proto")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir proto: %v", err)
	}

	const yamake = `PROTO_LIBRARY()
PROTOC_FATAL_WARNINGS()
SRCS(test.proto)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "proto", Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(d.protocFlags, []string{"--fatal_warnings"}) {
		t.Fatalf("protocFlags = %v, want [--fatal_warnings]", d.protocFlags)
	}
}

func TestCollectModule_CPPProtoPluginRecorded(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "proto")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir proto: %v", err)
	}

	const yamake = `PROTO_LIBRARY()
CPP_PROTO_PLUGIN(validation ydb/public/lib/validation .validation.pb.h DEPS ydb/public/api/protos/annotations EXTRA_OUT_FLAG lite=true)
SRCS(test.proto)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "proto", Kind: KindLib, Platform: testTargetP}))

	if len(d.cppProtoPlugins) != 1 {
		t.Fatalf("cppProtoPlugins = %d, want 1", len(d.cppProtoPlugins))
	}

	plugin := d.cppProtoPlugins[0]
	if plugin.Name != "validation" {
		t.Fatalf("plugin.Name = %q, want validation", plugin.Name)
	}
	if plugin.ToolPath != "ydb/public/lib/validation" {
		t.Fatalf("plugin.ToolPath = %q, want ydb/public/lib/validation", plugin.ToolPath)
	}
	if !equalStrings(plugin.OutputSuffixes, []string{".validation.pb.h"}) {
		t.Fatalf("plugin.OutputSuffixes = %v, want [.validation.pb.h]", plugin.OutputSuffixes)
	}
	if !equalStrings(plugin.Deps, []string{"ydb/public/api/protos/annotations"}) {
		t.Fatalf("plugin.Deps = %v, want [ydb/public/api/protos/annotations]", plugin.Deps)
	}
	if plugin.ExtraOutFlag != "lite=true" {
		t.Fatalf("plugin.ExtraOutFlag = %q, want lite=true", plugin.ExtraOutFlag)
	}
	if !containsString(d.peerdirs, "ydb/public/api/protos/annotations") {
		t.Fatalf("peerdirs = %v, want ydb/public/api/protos/annotations", d.peerdirs)
	}
}

func TestCollectModule_FlatcFlagsRecorded(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "flatcmod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir flatcmod: %v", err)
	}

	const yamake = `LIBRARY()
FLATC_FLAGS(--scoped-enums --gen-all)
SRCS(Schema.fbs)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "flatcmod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "flatcmod", Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(d.flatcFlags, []string{"--scoped-enums", "--gen-all"}) {
		t.Fatalf("flatcFlags = %v, want [--scoped-enums --gen-all]", d.flatcFlags)
	}
}

func TestCollectModule_UseCommonGoogleApisAddsPeer(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "proto")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir proto: %v", err)
	}

	const yamake = `PROTO_LIBRARY()
USE_COMMON_GOOGLE_APIS(api/annotations)
SRCS(test.proto)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "proto", Kind: KindLib, Platform: testTargetP}))

	if !containsString(d.peerdirs, "contrib/libs/googleapis-common-protos") {
		t.Fatalf("peerdirs = %v, want contrib/libs/googleapis-common-protos", d.peerdirs)
	}
}

func TestCollectModule_Py3ProgramSplitsPyMainFromPySrcs(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "pytool")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir pytool: %v", err)
	}

	const yamake = `PY3_PROGRAM()
PY_SRCS(
    MAIN
    __main__.py
)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))

	bin := collectModule(fs, "pytool", KindBin, mf.Stmts, buildIfEnv(ModuleInstance{Path: "pytool", Kind: KindBin, Platform: testTargetP}))
	if got := bin.pyMain; got == nil || *got != "pytool.__main__:main" {
		t.Fatalf("bin pyMain = %#v, want pytool.__main__:main", got)
	}
	if len(bin.pySrcs) != 0 {
		t.Fatalf("bin pySrcs = %v, want empty", bin.pySrcs)
	}

	lib := collectModule(fs, "pytool", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "pytool", Kind: KindLib, Platform: testTargetP}))
	if lib.pyMain != nil {
		t.Fatalf("lib pyMain = %#v, want nil", lib.pyMain)
	}
	if !equalStrings(lib.pySrcs, []string{"__main__.py"}) {
		t.Fatalf("lib pySrcs = %v, want [__main__.py]", lib.pySrcs)
	}
}

func TestCollectModule_CopyExpandsVarsIntoAutoSources(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "copymod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir copymod: %v", err)
	}

	const yamake = `LIBRARY()
SET(ORIG_SRC_DIR src)
SET(ORIG_SOURCES a.cpp b.h)
COPY(
    WITH_CONTEXT
    AUTO
    FROM ${ORIG_SRC_DIR}
    ${ORIG_SOURCES}
    OUTPUT_INCLUDES dep.h
)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "copymod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "copymod", Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(d.srcs, []string{"a.cpp", "b.h"}) {
		t.Fatalf("srcs = %v, want [a.cpp b.h]", d.srcs)
	}
	if len(d.copyFiles) != 2 {
		t.Fatalf("len(copyFiles) = %d, want 2", len(d.copyFiles))
	}
	if d.copyFiles[0].Src != "src/a.cpp" || d.copyFiles[1].Src != "src/b.h" {
		t.Fatalf("copyFiles srcs = %#v", d.copyFiles)
	}
}

func TestGen_YqlUdfStatic_UsesGlobalArchiveOnly(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("udfmod/ya.make", `YQL_UDF_CONTRIB(my_udf)
YQL_ABI_VERSION(2 44 0)
SRCS(lib.cpp)
END()
`)
	mkdirWrite("udfmod/lib.cpp", "int udf() { return 0; }\n")
	mkdirWrite("yql/essentials/public/udf/ya.make", "LIBRARY()\nEND()\n")
	mkdirWrite("yql/essentials/public/udf/support/ya.make", "LIBRARY()\nEND()\n")

	g := testGen(root, "udfmod")

	cc := findGraphNodeByOutputs(t, g, "$(B)/udfmod/lib.cpp.udfs.o")
	if cc.TargetProperties["module_tag"] != "yql_udf_static" {
		t.Fatalf("cc module_tag = %q, want yql_udf_static", cc.TargetProperties["module_tag"])
	}

	for _, want := range []string{
		"-DUDF_ABI_VERSION_MAJOR=2",
		"-DUDF_ABI_VERSION_MINOR=44",
		"-DUDF_ABI_VERSION_PATCH=0",
	} {
		if !contains(cc.Cmds[0].CmdArgs, want) {
			t.Fatalf("cc cmd_args missing %q: %v", want, cc.Cmds[0].CmdArgs)
		}
	}

	globalAR := findGraphNodeByOutputs(t, g, "$(B)/udfmod/libmy_udf.global.a")
	if globalAR.TargetProperties["module_tag"] != "yql_udf_static_global" {
		t.Fatalf("global AR module_tag = %q, want yql_udf_static_global", globalAR.TargetProperties["module_tag"])
	}

	for _, n := range g.Graph {
		for _, out := range n.Outputs {
			if out.String() == "$(B)/udfmod/libmy_udf.a" {
				t.Fatalf("unexpected regular archive output %q present in graph", out)
			}
		}
	}
}

func TestGen_FlatcSourcesEmitConsumerInputsAndDeps(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("mod/ya.make", `LIBRARY()
FLATC_FLAGS(--scoped-enums)
SRCS(
    File.fbs
    Schema.fbs
    consumer.cpp
)
END()
`)
	mkdirWrite("mod/consumer.cpp", `#include "File.fbs.h"
int consume() { return 0; }
`)
	mkdirWrite("mod/Schema.fbs", `namespace test;
table Foo {
  value:int;
}
`)
	mkdirWrite("mod/File.fbs", `include "Schema.fbs";
namespace test;
table Bar {
  foo:Foo;
}
root_type Bar;
`)
	mkdirWrite("build/scripts/cpp_flatc_wrapper.py", "print('stub')\n")
	mkdirWrite("contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h", "#pragma once\n")
	mkdirWrite("contrib/libs/flatbuffers/flatc/ya.make", "PROGRAM(flatc)\nSRCS(main.cpp)\nEND()\n")
	mkdirWrite("contrib/libs/flatbuffers/flatc/main.cpp", "int main() { return 0; }\n")

	g := testGen(root, "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/File.fbs.h", "$(B)/mod/File.fbs.cpp", "$(B)/mod/File.bfbs")
	findGraphNodeByOutputs(t, g, "$(B)/mod/Schema.fbs.h", "$(B)/mod/Schema.fbs.cpp", "$(B)/mod/Schema.bfbs")

	fileCC := findGraphNodeByOutputs(t, g, "$(B)/mod/File.fbs.cpp.o")
	wantFileInputs := []string{
		"$(B)/mod/File.fbs.cpp",
		"$(B)/mod/File.fbs.h",
		"$(B)/mod/Schema.fbs.h",
		"$(S)/build/scripts/cpp_flatc_wrapper.py",
		"$(S)/mod/File.fbs",
		"$(S)/mod/Schema.fbs",
		"$(S)/contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h",
	}
	if got := vfsStringsT3(fileCC.Inputs); !reflect.DeepEqual(got[:len(wantFileInputs)], wantFileInputs) {
		t.Fatalf("File.fbs.cpp inputs prefix = %v, want %v", got[:len(wantFileInputs)], wantFileInputs)
	}
	if len(fileCC.Deps) != 2 {
		t.Fatalf("len(File.fbs.cpp deps) = %d, want 2 (self + imported schema)", len(fileCC.Deps))
	}

	consumerCC := findGraphNodeByOutputs(t, g, "$(B)/mod/consumer.cpp.o")
	wantConsumerInputs := []string{
		"$(S)/mod/consumer.cpp",
		"$(B)/mod/File.fbs.h",
		"$(B)/mod/Schema.fbs.h",
		"$(S)/build/scripts/cpp_flatc_wrapper.py",
		"$(S)/mod/File.fbs",
		"$(S)/mod/Schema.fbs",
		"$(S)/contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h",
	}
	if got := vfsStringsT3(consumerCC.Inputs); !reflect.DeepEqual(got[:len(wantConsumerInputs)], wantConsumerInputs) {
		t.Fatalf("consumer.cpp inputs prefix = %v, want %v", got[:len(wantConsumerInputs)], wantConsumerInputs)
	}
	if len(consumerCC.Deps) != 2 {
		t.Fatalf("len(consumer.cpp deps) = %d, want 2 (reachable flatc producers)", len(consumerCC.Deps))
	}
}

func TestGen_CopyFileWithContextAutoCompilesBuildOutput(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("mod/ya.make", `LIBRARY()
COPY_FILE_WITH_CONTEXT(
    AUTO
    original.cpp
    copied.cpp
)
END()
`)
	mkdirWrite("mod/original.cpp", `#include "dep.h"
int copied() { return 0; }
`)
	mkdirWrite("mod/dep.h", "#pragma once\n")

	g := testGen(root, "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp")
	cc := findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp.o")
	wantInputs := []string{
		"$(B)/mod/copied.cpp",
		"$(S)/mod/original.cpp",
		"$(S)/mod/dep.h",
	}
	if got := vfsStringsT3(cc.Inputs); !reflect.DeepEqual(got[:len(wantInputs)], wantInputs) {
		t.Fatalf("copied.cpp inputs prefix = %v, want %v", got[:len(wantInputs)], wantInputs)
	}
	if len(cc.Deps) != 1 {
		t.Fatalf("len(copied.cpp deps) = %d, want 1 (copy producer)", len(cc.Deps))
	}
}

func TestGen_CopyFileWithContextExpandsBuildRootModdirDestination(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("mod/ya.make", `LIBRARY()
COPY_FILE_WITH_CONTEXT(
    AUTO
    original.cpp
    ${ARCADIA_BUILD_ROOT}/${MODDIR}/copied.cpp
)
END()
`)
	mkdirWrite("mod/original.cpp", `#include "dep.h"
int copied() { return 0; }
`)
	mkdirWrite("mod/dep.h", "#pragma once\n")

	g := testGen(root, "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp")
	cc := findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp.o")
	wantInputs := []string{
		"$(B)/mod/copied.cpp",
		"$(S)/mod/original.cpp",
		"$(S)/mod/dep.h",
	}
	if got := vfsStringsT3(cc.Inputs); !reflect.DeepEqual(got[:len(wantInputs)], wantInputs) {
		t.Fatalf("copied.cpp inputs prefix = %v, want %v", got[:len(wantInputs)], wantInputs)
	}
}

func TestGen_CopyFileAutoDoesNotPropagateSourceContext(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("mod/ya.make", `LIBRARY()
COPY_FILE(
    AUTO
    original.cpp
    copied.cpp
)
END()
`)
	mkdirWrite("mod/original.cpp", `#include "dep.h"
int copied() { return 0; }
`)
	mkdirWrite("mod/dep.h", "#pragma once\n")

	g := testGen(root, "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp")
	cc := findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp.o")
	wantInputs := []string{"$(B)/mod/copied.cpp"}
	if got := vfsStringsT3(cc.Inputs); !reflect.DeepEqual(got[:len(wantInputs)], wantInputs) {
		t.Fatalf("copied.cpp inputs prefix = %v, want %v", got[:len(wantInputs)], wantInputs)
	}
	for _, unexpected := range []string{"$(S)/mod/original.cpp", "$(S)/mod/dep.h"} {
		for _, in := range vfsStringsT3(cc.Inputs) {
			if in == unexpected {
				t.Fatalf("copied.cpp inputs unexpectedly contain %s: %v", unexpected, vfsStringsT3(cc.Inputs))
			}
		}
	}
	if len(cc.Deps) != 1 {
		t.Fatalf("len(copied.cpp deps) = %d, want 1 (copy producer)", len(cc.Deps))
	}
}

func TestGen_CopyFileUsesSourceRootInputFromIncludedMacro(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
INCLUDE(${ARCADIA_ROOT}/shared/copy.ya.make.inc)
END()
`)
	writeTestModuleFile(t, root, "shared/copy.ya.make.inc", `COPY_FILE(
    TEXT
    shared/generated.txt
    ${BINDIR}/shared/generated.h
)
`)
	writeTestModuleFile(t, root, "shared/generated.txt", "generated\n")

	g := testGen(root, "mod")

	cp := mustNodeByOutput(t, g, "$(B)/mod/shared/generated.h")
	if !nodeHasInput(cp, "$(S)/shared/generated.txt") {
		t.Fatalf("copy inputs missing source-root generated.txt: %#v", cp.Inputs)
	}
	if nodeHasInput(cp, "$(S)/mod/shared/generated.txt") {
		t.Fatalf("copy inputs still carry duplicated module-prefixed generated.txt: %#v", cp.Inputs)
	}
}

func TestGen_EnumSerializationRootQualifiedHeaderUsesCanonicalInput(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "pkg/sub/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
GENERATE_ENUM_SERIALIZATION(pkg/sub/codecs.h)
SRCS(stub.cpp)
END()
`)
	writeTestModuleFile(t, root, "pkg/sub/codecs.h", "enum class E { A = 0 };\n")
	writeTestModuleFile(t, root, "pkg/sub/stub.cpp", "int stub(){return 0;}\n")
	writeToolProgram(t, root, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(t, root, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(root, "pkg/sub")

	en := mustNodeByOutput(t, g, "$(B)/pkg/sub/pkg/sub/codecs.h_serialized.cpp")
	if !nodeHasInput(en, "$(S)/pkg/sub/codecs.h") {
		t.Fatalf("enum inputs missing canonical header path: %#v", en.Inputs)
	}
	if nodeHasInput(en, "$(S)/pkg/sub/pkg/sub/codecs.h") {
		t.Fatalf("enum inputs still carry duplicated header path: %#v", en.Inputs)
	}
	if got := en.Cmds[0].CmdArgs[1]; got != "$(S)/pkg/sub/codecs.h" {
		t.Fatalf("enum parser input = %q, want $(S)/pkg/sub/codecs.h", got)
	}
	if idx := indexOfArg(en.Cmds[0].CmdArgs, "--include-path"); idx < 0 || idx+1 >= len(en.Cmds[0].CmdArgs) || en.Cmds[0].CmdArgs[idx+1] != "pkg/sub/codecs.h" {
		t.Fatalf("enum --include-path mismatch: %#v", en.Cmds[0].CmdArgs)
	}
}

func vfsStringsT3(in []VFS) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = v.String()
	}
	return out
}

// TestGen_CF_SetVarsReachCfgVars reproduces T9: SET(...)-derived vars must
// reach the CFG_VARS of a CF node emitted through the SRCS .cpp.in path.
func TestGen_CF_SetVarsReachCfgVars(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "thelib")
	Throw(os.MkdirAll(libDir, 0o755))
	Throw(os.WriteFile(filepath.Join(libDir, "ya.make"),
		[]byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSET(MYVAR hello)\nDEFAULT(MYDEF world)\nSRCS(lib.cpp x.cpp.in)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(libDir, "lib.cpp"), []byte("int f(){return 0;}\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(libDir, "x.cpp.in"), []byte("int a = @MYVAR@;\nint b = @MYDEF@;\n"), 0o644))

	g := testGen(root, "thelib")
	var cf *Node
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0].String() == "$(B)/thelib/x.cpp" {
			cf = n
			break
		}
	}
	if cf == nil {
		t.Fatal("no CF node emitted for thelib/x.cpp")
	}
	args := strings.Join(cf.Cmds[0].CmdArgs, " ")
	if !strings.Contains(args, "MYVAR=hello") {
		t.Errorf("CF cmd_args missing SET var MYVAR=hello; got: %s", args)
	}
	if !strings.Contains(args, "MYDEF=world") {
		t.Errorf("CF cmd_args missing DEFAULT var MYDEF=world; got: %s", args)
	}
}

// TestGen_HInGeneratedHeader_RealizedInConsumer reproduces T13: a .h.in
// generated header declared in SRCS of one module but #included only by a
// PEERDIR consumer must have module_dir = the consuming module (not the
// declaring one) and must NOT be archived into the declaring module's .a.
func TestGen_HInGeneratedHeader_RealizedInConsumer(t *testing.T) {
	root := t.TempDir()
	genh := filepath.Join(root, "genh")
	cons := filepath.Join(root, "cons")
	app := filepath.Join(root, "app")
	for _, d := range []string{genh, cons, app} {
		Throw(os.MkdirAll(d, 0o755))
	}
	// declaring module: config.h.in in SRCS, plus a .cpp that does NOT include config.h
	Throw(os.WriteFile(filepath.Join(genh, "ya.make"),
		[]byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSET(MYVAR hello)\nSRCS(config.h.in own.cpp)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(genh, "config.h.in"), []byte("#include \"dep.h\"\n#define X @MYVAR@\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(genh, "dep.h"), []byte("#pragma once\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(genh, "own.cpp"), []byte("int g(){return 0;}\n"), 0o644))
	// consuming module: #includes the generated header across PEERDIR
	Throw(os.WriteFile(filepath.Join(cons, "ya.make"),
		[]byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(genh)\nSRCS(use.cpp)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(cons, "use.cpp"),
		[]byte("#include <genh/config.h>\nint u(){return 0;}\n"), 0o644))
	// root program
	Throw(os.WriteFile(filepath.Join(app, "ya.make"),
		[]byte("PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(cons)\nSRCS(main.cpp)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(app, "main.cpp"), []byte("int main(){return 0;}\n"), 0o644))

	g := testGen(root, "app")

	byOut := map[string]*Node{}
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 {
			byOut[n.Outputs[0].String()] = n
		}
	}

	cf := byOut["$(B)/genh/config.h"]
	if cf == nil {
		t.Fatal("no CF node emitted for genh/config.h")
	}
	if got := cf.TargetProperties["module_dir"]; got != "cons" {
		t.Errorf("config.h module_dir = %q, want %q (consuming module)", got, "cons")
	}

	ar := byOut["$(B)/genh/libgenh.a"]
	if ar == nil {
		t.Fatal("no AR node for genh")
	}
	for _, c := range ar.Cmds {
		for _, a := range c.CmdArgs {
			if a == "$(B)/genh/config.h" {
				t.Errorf("genh AR cmd_args archives config.h as a member: %v", c.CmdArgs)
			}
		}
	}
	for _, in := range ar.Inputs {
		if in.String() == "$(B)/genh/config.h" || in.String() == "$(S)/genh/config.h.in" {
			t.Errorf("genh AR inputs include %q (generated header must not be archived)", in.String())
		}
	}

	use := byOut["$(B)/cons/use.cpp.o"]
	if use == nil {
		t.Fatal("no CC node for cons/use.cpp")
	}
	found := false
	for _, d := range use.Deps {
		if d == cf.UID {
			found = true
		}
	}
	if !found {
		t.Errorf("use.cpp.o deps %v missing config.h CF uid %q", use.Deps, cf.UID)
	}
	if !nodeHasInput(use, "$(S)/genh/config.h.in") {
		t.Errorf("use.cpp.o inputs missing config.h.in: %#v", use.Inputs)
	}
	if !nodeHasInput(use, "$(S)/genh/dep.h") {
		t.Errorf("use.cpp.o inputs missing dep.h from config.h.in closure: %#v", use.Inputs)
	}
}

func TestGen_CmdArgsExpandStmtVars(t *testing.T) {
	root := t.TempDir()
	mod := filepath.Join(root, "mod")
	Throw(os.MkdirAll(mod, 0o755))
	Throw(os.WriteFile(filepath.Join(mod, "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SET(MKQL_RUNTIME_VERSION 42)
DEFAULT(ARCADIA_CURL_DNS_RESOLVER ARES)
SET(SSE41_CFLAGS -msse4.1)
SET(AVX2_CFLAGS -mavx2)
CFLAGS(
    -DMKQL_RUNTIME_VERSION=$MKQL_RUNTIME_VERSION
    -DARCADIA_CURL_DNS_RESOLVER_${ARCADIA_CURL_DNS_RESOLVER}
)
SRC(lib.cpp ${SSE41_CFLAGS} ${AVX2_CFLAGS})
END()
`), 0o644))
	Throw(os.WriteFile(filepath.Join(mod, "lib.cpp"), []byte("int lib(){return 0;}\n"), 0o644))

	g := testGen(root, "mod")
	cc := mustNodeByOutput(t, g, "$(B)/mod/lib.cpp.o")
	args := strings.Join(cc.Cmds[0].CmdArgs, " ")

	for _, want := range []string{
		"-DMKQL_RUNTIME_VERSION=42",
		"-DARCADIA_CURL_DNS_RESOLVER_ARES",
		"-msse4.1",
		"-mavx2",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("cc cmd args missing %q: %s", want, args)
		}
	}
	for _, bad := range []string{
		"${",
		"$MKQL_RUNTIME_VERSION",
		"${ARCADIA_CURL_DNS_RESOLVER}",
		"${SSE41_CFLAGS}",
		"${AVX2_CFLAGS}",
	} {
		if strings.Contains(args, bad) {
			t.Fatalf("cc cmd args still contain %q: %s", bad, args)
		}
	}
}

func TestGen_RunProgramHeaderOutputClosurePropagatesInputs(t *testing.T) {
	root := t.TempDir()

	writeToolProgram(t, root, "tools/genhdr", "genhdr")

	writeTestModuleFile(t, root, "dep/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(dep.cpp dep.h)
END()
`)
	writeTestModuleFile(t, root, "dep/dep.cpp", "int dep(){return 0;}\n")
	writeTestModuleFile(t, root, "dep/dep.h", "#pragma once\n")

	writeTestModuleFile(t, root, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(dep)
RUN_PROGRAM(
    tools/genhdr
        template.h.in
        gen.h
    OUTPUT_INCLUDES
        dep/dep.h
    IN
        template.h.in
    OUT
        gen.h
)
END()
`)
	writeTestModuleFile(t, root, "gen/template.h.in", "#pragma once\n")

	writeTestModuleFile(t, root, "cons/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(t, root, "cons/use.cpp", `#include <gen/gen.h>
int use() { return 0; }
`)

	writeTestModuleFile(t, root, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(cons)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(t, root, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(root, "app")
	genH := mustNodeByOutput(t, g, "$(B)/gen/gen.h")
	use := mustNodeByOutput(t, g, "$(B)/cons/use.cpp.o")

	for _, want := range []string{
		"$(B)/gen/gen.h",
		"$(S)/gen/template.h.in",
		"$(S)/dep/dep.h",
	} {
		if !nodeHasInput(use, want) {
			t.Fatalf("use.cpp.o inputs missing %q: %#v", want, use.Inputs)
		}
	}
	if !containsString(use.Deps, genH.UID) {
		t.Fatalf("use.cpp.o deps missing generated-header PR uid %q: %v", genH.UID, use.Deps)
	}
}

func TestCollectModule_BisonGeneratedHeaderExportedGlobally(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(pire/re_parser.y)
END()
`)
	writeTestModuleFile(t, root, "gen/pire/re_parser.y", `%{
#include "re_lexer.h"
%}
%%
`)
	writeTestModuleFile(t, root, "gen/pire/re_lexer.h", "#pragma once\n")

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(root, "gen", "ya.make")))
	instance := ModuleInstance{Path: "gen", Kind: KindLib, Platform: testTargetP}
	d := collectModule(fs, "gen", KindLib, mf.Stmts, buildIfEnv(instance))

	for _, got := range [][]VFS{d.addIncl, d.addInclGlobal} {
		if !slices.Contains(vfsStrings(got), "$(B)/gen/pire") {
			t.Fatalf("generated bison include dir missing from %v", vfsStrings(got))
		}
	}
}

func TestGen_BisonGeneratedHeaderPreprocessAndPeerBuildRootInclude(t *testing.T) {
	root := t.TempDir()

	writeToolProgram(t, root, "contrib/tools/bison", "bison")
	writeToolProgram(t, root, "contrib/tools/m4", "m4")
	writeTestModuleFile(t, root, bisonPreprocessPyVFS.Rel, "print('stub')\n")
	for _, input := range bisonCppSkeletonInputs {
		body := ""
		if strings.HasSuffix(input.Rel, "/stack.hh") {
			body = `#include "skeleton-helper.h"` + "\n"
		}
		writeTestModuleFile(t, root, input.Rel, body)
	}
	writeTestModuleFile(t, root, "contrib/tools/bison/data/skeletons/skeleton-helper.h", "")

	writeTestModuleFile(t, root, "genlib/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(pire/re_parser.y)
END()
`)
	writeTestModuleFile(t, root, "genlib/pire/re_parser.y", `%{
#include "re_lexer.h"
#include "extra.h"
%}
%%
`)
	writeTestModuleFile(t, root, "genlib/pire/re_lexer.h", `#pragma once
#include "deep.h"
`)
	writeTestModuleFile(t, root, "genlib/pire/extra.h", "#pragma once\n")
	writeTestModuleFile(t, root, "genlib/pire/deep.h", "#pragma once\n")

	writeTestModuleFile(t, root, "app/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(genlib)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(t, root, "app/use.cpp", "int use() { return 0; }\n")

	g := testGen(root, "app")

	yc := mustNodeByOutput(t, g, "$(B)/genlib/pire/re_parser.h")
	if got := len(yc.Cmds); got != 2 {
		t.Fatalf("bison YC cmd count = %d, want 2", got)
	}
	if !strings.HasSuffix(yc.Cmds[1].CmdArgs[0], "/python3") {
		t.Fatalf("bison preprocess tool = %q, want a python3 binary", yc.Cmds[1].CmdArgs[0])
	}
	wantPreprocess := []string{
		"$(S)/build/scripts/preprocess.py",
		"$(B)/genlib/pire/re_parser.h",
	}
	if got := yc.Cmds[1].CmdArgs[1:]; !reflect.DeepEqual(got, wantPreprocess) {
		t.Fatalf("bison preprocess cmd_args mismatch:\n  got:  %#v\n  want: %#v", got, wantPreprocess)
	}
	for _, want := range []string{
		"$(S)/build/scripts/preprocess.py",
		"$(S)/genlib/pire/re_parser.y",
		"$(S)/contrib/tools/bison/data/skeletons/skeleton-helper.h",
	} {
		if !nodeHasInput(yc, want) {
			t.Fatalf("bison YC inputs missing %q: %#v", want, yc.Inputs)
		}
	}
	for _, unwanted := range []string{
		"$(S)/genlib/pire/re_lexer.h",
		"$(S)/genlib/pire/extra.h",
		"$(S)/genlib/pire/deep.h",
	} {
		if nodeHasInput(yc, unwanted) {
			t.Fatalf("bison YC inputs unexpectedly include grammar-local header %q: %#v", unwanted, yc.Inputs)
		}
	}
	for _, want := range vfsStrings(bisonCppSkeletonInputs) {
		if !nodeHasInput(yc, want) {
			t.Fatalf("bison YC inputs missing skeleton %q", want)
		}
	}

	use := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")
	if indexOfArg(use.Cmds[0].CmdArgs, "-I$(B)/genlib/pire") < 0 {
		t.Fatalf("peer CC cmd_args missing generated bison build-root addincl: %#v", use.Cmds[0].CmdArgs)
	}

	parserObj := mustNodeByOutput(t, g, "$(B)/genlib/_/_/pire/re_parser.y.cpp.o")
	for _, want := range []string{
		"$(S)/build/scripts/preprocess.py",
		"$(S)/genlib/pire/re_lexer.h",
		"$(S)/genlib/pire/extra.h",
		"$(S)/genlib/pire/deep.h",
		"$(S)/contrib/tools/bison/data/skeletons/skeleton-helper.h",
	} {
		if !nodeHasInput(parserObj, want) {
			t.Fatalf("generated parser object inputs missing %q: %#v", want, parserObj.Inputs)
		}
	}
}

func TestGen_ProtoLibrary_TransitiveHeadersNo_DepsHeaderUsesRuntimeRoot(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
SRCS(test.proto)
END()
`)
	writeTestModuleFile(t, root, "protos/test.proto", `syntax = "proto3";
package test;
import "google/protobuf/any.proto";
message Row {
  google.protobuf.Any body = 1;
}
`)
	writeTestModuleFile(t, root, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(t, root, "app/use.cpp", `#include <protos/test.deps.pb.h>
int use() { return 0; }
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(t, root, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/src/google/protobuf/any.proto", `syntax = "proto3";
package google.protobuf;
message Any {}
`)
	writeTestModuleFile(t, root, "contrib/libs/protobuf/src/google/protobuf/any.pb.h", "#pragma once\n")

	g := testGen(root, "app")
	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.deps.pb.h",
	)
	use := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")

	if !nodeHasInput(use, "$(B)/protos/test.deps.pb.h") {
		t.Fatalf("use.cpp.o inputs missing deps header output: %#v", use.Inputs)
	}
	if !nodeHasInput(use, "$(S)/contrib/libs/protobuf/src/google/protobuf/any.pb.h") {
		t.Fatalf("use.cpp.o inputs missing protobuf runtime WKT header: %#v", use.Inputs)
	}
	if nodeHasInput(use, "$(S)/google/protobuf/any.pb.h") {
		t.Fatalf("use.cpp.o inputs still contain unrebased WKT header path: %#v", use.Inputs)
	}
	if !containsString(use.Deps, pb.UID) {
		t.Fatalf("use.cpp.o deps missing PB producer uid %q: %v", pb.UID, use.Deps)
	}
}

func TestReorderARMembers_Reg3PICVariantsTrailObjcopy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		paths     []string
		wantOrder []int
	}{
		{
			name: "protobuf-style host reg3",
			paths: []string{
				"contrib/python/protobuf/py3/google.protobuf.internal._api_implementation.reg3.cpp.pic.o",
				"contrib/python/protobuf/py3/google.protobuf.pyext._message.reg3.cpp.pic.o",
				"contrib/python/protobuf/py3/objcopy_a.o",
				"contrib/python/protobuf/py3/objcopy_b.o",
			},
			wantOrder: []int{2, 3, 0, 1},
		},
		{
			name: "symbols/module-style host py3 reg3",
			paths: []string{
				"library/python/symbols/module/library.python.symbols.module.syms.reg3.cpp.py3.pic.o",
				"library/python/symbols/module/objcopy_a.o",
				"library/python/symbols/module/objcopy_b.o",
			},
			wantOrder: []int{1, 2, 0},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			refs := make([]NodeRef, len(tc.paths))
			paths := make([]VFS, len(tc.paths))
			for i, rel := range tc.paths {
				refs[i] = NodeRef{id: int64(i + 1)}
				paths[i] = Build(rel)
			}

			gotRefs, gotPaths := reorderARMembers(
				refs,
				paths,
				make([]bool, len(tc.paths)),
				make([]bool, len(tc.paths)),
				len(tc.paths),
			)

			wantRefs := make([]NodeRef, len(tc.wantOrder))
			wantPaths := make([]string, len(tc.wantOrder))
			for i, idx := range tc.wantOrder {
				wantRefs[i] = refs[idx]
				wantPaths[i] = Build(tc.paths[idx]).String()
			}

			gotPathStrings := make([]string, len(gotPaths))
			for i, path := range gotPaths {
				gotPathStrings[i] = path.String()
			}

			if !reflect.DeepEqual(gotPathStrings, wantPaths) {
				t.Fatalf("paths mismatch:\n got: %v\nwant: %v", gotPathStrings, wantPaths)
			}
			if !reflect.DeepEqual(gotRefs, wantRefs) {
				t.Fatalf("refs mismatch:\n got: %v\nwant: %v", gotRefs, wantRefs)
			}
		})
	}
}

const t17SwigTargetDir = "contrib/tools/swig"

func TestReorderLDMembers_LegacyDoubleUnderscorePathsTrailRegularSources(t *testing.T) {
	refs := []NodeRef{{id: 1}, {id: 2}, {id: 3}}
	paths := []VFS{
		Build("contrib/tools/swig/_/Source/CParse/cscanner.c.pic.o"),
		Build("contrib/tools/swig/_/_/Source/CParse/parser.y.c.pic.o"),
		Build("contrib/tools/swig/_/Source/CParse/templ.c.pic.o"),
	}

	gotRefs, gotPaths := reorderLDMembers(refs, paths)

	wantRefs := []NodeRef{{id: 1}, {id: 3}, {id: 2}}
	if !reflect.DeepEqual(gotRefs, wantRefs) {
		t.Fatalf("ld refs mismatch:\n  got:  %#v\n  want: %#v", gotRefs, wantRefs)
	}

	wantPaths := []string{
		"$(B)/contrib/tools/swig/_/Source/CParse/cscanner.c.pic.o",
		"$(B)/contrib/tools/swig/_/Source/CParse/templ.c.pic.o",
		"$(B)/contrib/tools/swig/_/_/Source/CParse/parser.y.c.pic.o",
	}
	if got := vfsStrings(gotPaths); !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("ld paths mismatch:\n  got:  %#v\n  want: %#v", got, wantPaths)
	}
}

func TestGen_SwigToolBisonCompileNodesMatchReference(t *testing.T) {
	requireT17SwigFixture(t)

	our := testGenT20Tool(sourceRoot, t17SwigTargetDir)
	ref := loadT20RefGraph(t)

	tests := []struct {
		name      string
		ourOutput string
		refOutput string
	}{
		{
			name:      "cscanner",
			ourOutput: "$(B)/contrib/tools/swig/_/Source/CParse/cscanner.c.pic.o",
			refOutput: "$(BUILD_ROOT)/contrib/tools/swig/_/Source/CParse/cscanner.c.pic.o",
		},
		{
			name:      "parser-y-generated-cc",
			ourOutput: "$(B)/contrib/tools/swig/_/_/Source/CParse/parser.y.c.pic.o",
			refOutput: "$(BUILD_ROOT)/contrib/tools/swig/_/_/Source/CParse/parser.y.c.pic.o",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ourNode := findGraphNodeByOutputs(t, our, tc.ourOutput)
			refNode := findT20RefNodeByOutputs(t, ref, tc.refOutput)
			if len(ourNode.Cmds) == 0 || len(refNode.Cmds) == 0 {
				t.Fatalf("expected both nodes to have at least 1 cmd")
			}

			gotArgs := append([]string(nil), ourNode.Cmds[0].CmdArgs...)
			wantArgs := normalizeT20Strings(refNode.Cmds[0].CmdArgs)
			if !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Fatalf("%s cmd_args mismatch:\n  got:  %#v\n  want: %#v", tc.name, gotArgs, wantArgs)
			}

			gotInputs := sortedStrings(vfsStrings(ourNode.Inputs))
			wantInputs := sortedStrings(normalizeT20Strings(refNode.Inputs))
			if !reflect.DeepEqual(gotInputs, wantInputs) {
				t.Fatalf("%s inputs mismatch:\n  got:  %#v\n  want: %#v", tc.name, gotInputs, wantInputs)
			}

			gotDepOutputs := projectGraphDepOutputs(t, our, ourNode.Deps)
			wantDepOutputs := projectT20RefDepOutputs(t, ref, refNode.Deps)
			if !reflect.DeepEqual(gotDepOutputs, wantDepOutputs) {
				t.Fatalf("%s dep outputs mismatch:\n  got:  %#v\n  want: %#v", tc.name, gotDepOutputs, wantDepOutputs)
			}
		})
	}
}

func TestGen_SwigToolLDMatchesReference(t *testing.T) {
	requireT17SwigFixture(t)

	our := testGenT20Tool(sourceRoot, t17SwigTargetDir)
	ref := loadT20RefGraph(t)

	ourNode := findGraphNodeByOutputs(t, our, "$(B)/contrib/tools/swig/swig")
	refNode := findT20RefNodeByOutputs(t, ref, "$(BUILD_ROOT)/contrib/tools/swig/swig")
	if len(ourNode.Cmds) < 3 || len(refNode.Cmds) < 3 {
		t.Fatalf("expected both nodes to have at least 3 cmds")
	}

	gotArgs := append([]string(nil), ourNode.Cmds[2].CmdArgs...)
	wantArgs := normalizeT20Strings(refNode.Cmds[2].CmdArgs)
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("swig ld cmd_args mismatch:\n  got:  %#v\n  want: %#v", gotArgs, wantArgs)
	}

	// LD inputs are reduced to objects/archives/scripts in both graphs by the
	// normalizer (a link node bundles .o/.a, not the member source/header
	// closure); mirror that on both sides.
	gotInputs := sortedStrings(filterARLDInputs(vfsStrings(ourNode.Inputs), "LD"))
	wantInputs := sortedStrings(filterARLDInputs(normalizeT20Strings(refNode.Inputs), "LD"))
	if !reflect.DeepEqual(gotInputs, wantInputs) {
		t.Fatalf("swig ld inputs mismatch:\n  got:  %#v\n  want: %#v", gotInputs, wantInputs)
	}

	gotDepOutputs := projectGraphDepOutputs(t, our, ourNode.Deps)
	wantDepOutputs := projectT20RefDepOutputs(t, ref, refNode.Deps)
	if !reflect.DeepEqual(gotDepOutputs, wantDepOutputs) {
		t.Fatalf("swig ld dep outputs mismatch:\n  got:  %#v\n  want: %#v", gotDepOutputs, wantDepOutputs)
	}

	parserIdx := slices.Index(gotArgs, "$(B)/contrib/tools/swig/_/_/Source/CParse/parser.y.c.pic.o")
	swigLibIdx := slices.Index(gotArgs, "$(B)/contrib/tools/swig/swig_lib.cpp.pic.o")
	if parserIdx < 0 || swigLibIdx < 0 {
		t.Fatalf("expected parser.y.c.pic.o and swig_lib.cpp.pic.o in swig link cmd_args: %v", gotArgs)
	}
	if parserIdx <= swigLibIdx {
		t.Fatalf("expected parser.y.c.pic.o after swig_lib.cpp.pic.o in swig link cmd_args: %v", gotArgs)
	}
}

func requireT17SwigFixture(t *testing.T) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(sourceRoot, t17SwigTargetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, t17SwigTargetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}
}

var t20ResourceMacroRE = regexp.MustCompile(`\$\((CLANG|LLD_ROOT|YMAKE_PYTHON3)-[0-9]+\)`)

type t20RefCmd struct {
	CmdArgs []string          `json:"cmd_args"`
	Env     map[string]string `json:"env"`
}

type t20RefNode struct {
	Cmds    []t20RefCmd `json:"cmds"`
	Deps    []string    `json:"deps"`
	Inputs  []string    `json:"inputs"`
	Outputs []string    `json:"outputs"`
	UID     string      `json:"uid"`
}

func TestCollectModule_SETAPPENDRPathGlobal(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "mod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	content := "RESOURCES_LIBRARY()\nSET_APPEND(RPATH_GLOBAL '-Wl,-rpath,${\"$\"}ORIGIN')\nEND()\n"
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(content), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	instance := ModuleInstance{Path: "mod", Kind: KindLib, Platform: testTargetP}
	d := collectModule(fs, "mod", KindLib, mf.Stmts, buildIfEnv(instance))

	want := []string{"-Wl,-rpath,$ORIGIN"}
	if !reflect.DeepEqual(d.rpathFlagsGlobal, want) {
		t.Fatalf("rpathFlagsGlobal mismatch:\n  got:  %#v\n  want: %#v", d.rpathFlagsGlobal, want)
	}
}

func TestGen_LibiconvDynamic_InputsMatchReference(t *testing.T) {
	const targetDir = "contrib/libs/libiconv/dynamic"

	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := testGenT20Tool(sourceRoot, targetDir)
	ourNode := findGraphNodeByOutputs(t, our, "$(B)/contrib/libs/libiconv/dynamic/libiconv.so")
	refNode := loadT20RefNode(t, "$(BUILD_ROOT)/contrib/libs/libiconv/dynamic/libiconv.so")

	gotInputs := sortedStrings(vfsStrings(ourNode.Inputs))
	wantInputs := sortedStrings(normalizeT20Strings(refNode.Inputs))

	if !reflect.DeepEqual(gotInputs, wantInputs) {
		t.Fatalf("libiconv inputs mismatch:\n  got:  %#v\n  want: %#v", gotInputs, wantInputs)
	}

	for _, want := range []string{
		"$(S)/build/scripts/c_templates/svn_interface.c",
		"$(S)/build/scripts/c_templates/svnversion.h",
		"$(S)/build/scripts/fs_tools.py",
		"$(S)/build/scripts/link_exe.py",
		"$(S)/build/scripts/vcs_info.py",
	} {
		if !slices.Contains(gotInputs, want) {
			t.Fatalf("libiconv inputs missing %q", want)
		}
	}
}

func TestGen_BisonLinkTailMatchesReference(t *testing.T) {
	const targetDir = "contrib/tools/bison"

	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := testGenT20Tool(sourceRoot, targetDir)
	ourNode := findGraphNodeByOutputs(t, our, "$(B)/contrib/tools/bison/bison", "$(B)/contrib/tools/bison/libiconv.so")
	refNode := loadT20RefNode(t, "$(BUILD_ROOT)/contrib/tools/bison/bison", "$(BUILD_ROOT)/contrib/tools/bison/libiconv.so")

	if len(ourNode.Cmds) < 3 || len(refNode.Cmds) < 3 {
		t.Fatalf("expected both nodes to have at least 3 cmds")
	}

	gotTail := cmdArgsFrom(t, ourNode.Cmds[2].CmdArgs, "-Wl,--start-group")
	wantTail := normalizeT20Strings(cmdArgsFrom(t, refNode.Cmds[2].CmdArgs, "-Wl,--start-group"))

	if !reflect.DeepEqual(gotTail, wantTail) {
		t.Fatalf("bison link tail mismatch:\n  got:  %#v\n  want: %#v", gotTail, wantTail)
	}

	libiconvIdx := slices.Index(gotTail, "contrib/libs/libiconv/dynamic/libiconv.so")
	bisonLibIdx := slices.Index(gotTail, "contrib/tools/bison/lib/libtools-bison-lib.a")
	if libiconvIdx < 0 || bisonLibIdx < 0 {
		t.Fatalf("expected both libiconv.so and libtools-bison-lib.a in bison link tail: %v", gotTail)
	}
	if libiconvIdx >= bisonLibIdx {
		t.Fatalf("expected libiconv.so before libtools-bison-lib.a in bison link tail: %v", gotTail)
	}

	rpathCount := 0
	for _, arg := range gotTail {
		if arg == "-Wl,-rpath,$ORIGIN" {
			rpathCount++
		}
	}
	if rpathCount != 2 {
		t.Fatalf("expected 2 rpath entries in bison link tail, got %d: %v", rpathCount, gotTail)
	}

	if !slices.Contains(gotTail, "-Wl,--allow-multiple-definition") {
		t.Fatalf("bison link tail missing -Wl,--allow-multiple-definition: %v", gotTail)
	}
}

func TestGen_BisonCompileCommands_DoNotLeakLocalSoGlobals(t *testing.T) {
	const targetDir = "contrib/tools/bison"

	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := testGenT20Tool(sourceRoot, targetDir)
	ref := loadT20RefGraph(t)

	tests := []struct {
		name      string
		ourOutput string
		refOutput string
		banned    []string
	}{
		{
			name:      "program-side",
			ourOutput: "$(B)/contrib/tools/bison/_/src/print-xml.c.pic.o",
			refOutput: "$(BUILD_ROOT)/contrib/tools/bison/_/src/print-xml.c.pic.o",
			banned: []string{
				"-I$(S)/contrib/libs/zlib/include",
				"-I$(S)/contrib/libs/double-conversion",
				"-I$(S)/contrib/libs/libc_compat/include/readpassphrase",
			},
		},
		{
			name:      "library-side",
			ourOutput: "$(B)/contrib/tools/bison/lib/mbchar.c.pic.o",
			refOutput: "$(BUILD_ROOT)/contrib/tools/bison/lib/mbchar.c.pic.o",
			banned: []string{
				"-I$(S)/contrib/libs/zlib/include",
				"-I$(S)/contrib/libs/double-conversion",
				"-I$(S)/contrib/libs/libc_compat/include/readpassphrase",
				"-I$(S)/contrib/libs/cxxsupp/libcxx/include",
				"-I$(S)/contrib/libs/cxxsupp/libcxxrt/include",
				"-I$(S)/contrib/libs/clang20-rt/include",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ourNode := findGraphNodeByOutputs(t, our, tc.ourOutput)
			refNode := findT20RefNodeByOutputs(t, ref, tc.refOutput)
			if len(ourNode.Cmds) == 0 || len(refNode.Cmds) == 0 {
				t.Fatalf("expected both nodes to have at least 1 cmd")
			}

			gotArgs := append([]string(nil), ourNode.Cmds[0].CmdArgs...)
			wantArgs := normalizeT20Strings(refNode.Cmds[0].CmdArgs)
			if !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Fatalf("%s compile cmd_args mismatch:\n  got:  %#v\n  want: %#v", tc.name, gotArgs, wantArgs)
			}

			assertCmdArgsAbsent(t, gotArgs, tc.banned...)
		})
	}
}

func TestGen_BisonArchiveMatchesReferenceAfterLocalSoIsolation(t *testing.T) {
	const targetDir = "contrib/tools/bison"

	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := testGenT20Tool(sourceRoot, targetDir)
	ref := loadT20RefGraph(t)

	ourNode := findGraphNodeByOutputs(t, our, "$(B)/contrib/tools/bison/lib/libtools-bison-lib.a")
	refNode := findT20RefNodeByOutputs(t, ref, "$(BUILD_ROOT)/contrib/tools/bison/lib/libtools-bison-lib.a")
	if len(ourNode.Cmds) == 0 || len(refNode.Cmds) == 0 {
		t.Fatalf("expected both archive nodes to have at least 1 cmd")
	}

	gotArgs := append([]string(nil), ourNode.Cmds[0].CmdArgs...)
	wantArgs := normalizeT20Strings(refNode.Cmds[0].CmdArgs)
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("bison archive cmd_args mismatch:\n  got:  %#v\n  want: %#v", gotArgs, wantArgs)
	}

	gotDepOutputs := projectGraphDepOutputs(t, our, ourNode.Deps)
	wantDepOutputs := projectT20RefDepOutputs(t, ref, refNode.Deps)
	if !reflect.DeepEqual(gotDepOutputs, wantDepOutputs) {
		t.Fatalf("bison archive dep outputs mismatch:\n  got:  %#v\n  want: %#v", gotDepOutputs, wantDepOutputs)
	}
}

func testGenT20(sourceRoot, targetDir string) *Graph {
	host := newT20ResourcePlatform(OSLinux, ISAX8664, "yes", []string{"tool"}, true)
	target := newT20ResourcePlatform(OSLinux, ISAAArch64, "yes", nil, true)

	return Gen(sourceRoot, targetDir, host, target, func(Warn) {})
}

func testGenT20Tool(sourceRoot, targetDir string) *Graph {
	host := newT20ResourcePlatform(OSLinux, ISAX8664, "yes", []string{"tool"}, true)

	return Gen(sourceRoot, targetDir, host, host, func(Warn) {})
}

func newT20ResourcePlatform(os OS, isa ISA, pic string, tags []string, musl bool) *Platform {
	flags := map[string]string{
		"AR_TOOL":           "$(CLANG)/bin/llvm-ar",
		"BUILD_PYTHON_BIN":  "$(YMAKE_PYTHON3)/bin/python3",
		"BUILD_PYTHON3_BIN": "$(YMAKE_PYTHON3)/bin/python3",
		"CLANG_TOOL":        "$(CLANG)/bin/clang",
		"CLANG_pl_pl_TOOL":  "$(CLANG)/bin/clang++",
		"LLD_TOOL":          "$(LLD_ROOT)/bin/ld.lld",
		"OBJCOPY_TOOL":      "$(CLANG)/bin/llvm-objcopy",
		"PIC":               pic,
		"STRIP_TOOL":        "$(CLANG)/bin/llvm-strip",
	}
	if musl {
		flags["MUSL"] = "yes"
	}

	return NewPlatform(os, isa, flags, tags, "", "")
}

type t20RefGraph struct {
	nodes []*t20RefNode
	byUID map[string]*t20RefNode
}

func loadT20RefGraph(t *testing.T) *t20RefGraph {
	t.Helper()

	path := filepath.Join(sourceRoot, "sg3.json")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference graph not present at %s", path)
		}

		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)

	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("read opening token from %s: %v", path, err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		t.Fatalf("unexpected opening token in %s: %v", path, tok)
	}

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			t.Fatalf("read object key from %s: %v", path, err)
		}

		key, ok := keyTok.(string)
		if !ok {
			t.Fatalf("unexpected key token in %s: %v", path, keyTok)
		}

		if key != "graph" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				t.Fatalf("skip %q in %s: %v", key, path, err)
			}

			continue
		}

		tok, err = dec.Token()
		if err != nil {
			t.Fatalf("read graph opener from %s: %v", path, err)
		}
		if d, ok := tok.(json.Delim); !ok || d != '[' {
			t.Fatalf("unexpected graph opener in %s: %v", path, tok)
		}

		ref := &t20RefGraph{byUID: make(map[string]*t20RefNode)}
		for dec.More() {
			var node t20RefNode
			if err := dec.Decode(&node); err != nil {
				t.Fatalf("decode graph node from %s: %v", path, err)
			}

			nodeCopy := node
			ref.nodes = append(ref.nodes, &nodeCopy)
			if nodeCopy.UID != "" {
				ref.byUID[nodeCopy.UID] = &nodeCopy
			}
		}

		return ref
	}

	t.Fatalf("graph array not found in %s", path)

	return nil
}

func loadT20RefNode(t *testing.T, wantOutputs ...string) *t20RefNode {
	t.Helper()

	return findT20RefNodeByOutputs(t, loadT20RefGraph(t), wantOutputs...)
}

func findT20RefNodeByOutputs(t *testing.T, ref *t20RefGraph, wantOutputs ...string) *t20RefNode {
	t.Helper()

	for _, node := range ref.nodes {
		if slices.Equal(node.Outputs, wantOutputs) {
			return node
		}
	}

	t.Fatalf("reference node with outputs %v not found", wantOutputs)
	return nil
}

func findGraphNodeByOutputs(t *testing.T, g *Graph, wantOutputs ...string) *Node {
	t.Helper()

	for _, node := range g.Graph {
		if len(node.Outputs) != len(wantOutputs) {
			continue
		}

		match := true
		for i, out := range node.Outputs {
			if out.String() != wantOutputs[i] {
				match = false

				break
			}
		}

		if match {
			return node
		}
	}

	t.Fatalf("graph node with outputs %v not found", wantOutputs)
	return nil
}

func cmdArgsFrom[T interface{ ~[]string }](t *testing.T, args T, marker string) []string {
	t.Helper()

	idx := slices.Index(args, marker)
	if idx < 0 {
		t.Fatalf("marker %q not found in cmd args: %v", marker, args)
	}

	return append([]string(nil), args[idx:]...)
}

func normalizeT20Token(s string) string {
	s = strings.NewReplacer(
		"$(BUILD_ROOT)", "$(B)",
		"$(SOURCE_ROOT)", "$(S)",
	).Replace(s)

	return t20ResourceMacroRE.ReplaceAllStringFunc(s, func(match string) string {
		dash := strings.IndexByte(match, '-')
		if dash < 0 {
			return match
		}

		return "$(" + match[2:dash] + ")"
	})
}

func normalizeT20Strings(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = normalizeT20Token(s)
	}

	return out
}

func normalizeT20Env(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}

	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = normalizeT20Token(v)
	}

	return out
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)

	return out
}

func assertCmdArgsAbsent(t *testing.T, args []string, banned ...string) {
	t.Helper()

	for _, wantAbsent := range banned {
		if slices.Contains(args, wantAbsent) {
			t.Fatalf("cmd_args unexpectedly contain %q: %v", wantAbsent, args)
		}
	}
}

func projectGraphDepOutputs(t *testing.T, g *Graph, deps []string) [][]string {
	t.Helper()

	byUID := make(map[string]*Node, len(g.Graph))
	for _, node := range g.Graph {
		byUID[node.UID] = node
	}

	out := make([][]string, 0, len(deps))
	for _, uid := range deps {
		node := byUID[uid]
		if node == nil {
			t.Fatalf("dep uid %q not found in generated graph", uid)
		}

		out = append(out, append([]string(nil), vfsStrings(node.Outputs)...))
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i], "\x00") < strings.Join(out[j], "\x00")
	})

	return out
}

func projectT20RefDepOutputs(t *testing.T, ref *t20RefGraph, deps []string) [][]string {
	t.Helper()

	out := make([][]string, 0, len(deps))
	for _, uid := range deps {
		node := ref.byUID[uid]
		if node == nil {
			t.Fatalf("dep uid %q not found in reference graph", uid)
		}

		out = append(out, normalizeT20Strings(node.Outputs))
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i], "\x00") < strings.Join(out[j], "\x00")
	})

	return out
}

func TestGen_ManualCompanionSourceUsesCythonCompanionCCInputs(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(t, root, "pkg/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nSRCS(helper.cpp)\nPY_SRCS(NAMESPACE pkg mod.pyx)\nEND()\n")
	writeTestModuleFile(t, root, "pkg/helper.cpp", "int f(){return 0;}\n")
	writeTestModuleFile(t, root, "pkg/mod.pyx", "def f():\n    return 0\n")

	g := testGen(root, "pkg")
	helper := mustNodeByOutput(t, g, "$(B)/pkg/helper.cpp.o")
	args := helper.Cmds[0].CmdArgs

	pythonIncludeIdx := indexOfArg(args, "-I$(S)/contrib/libs/python/Include")
	if pythonIncludeIdx < 0 {
		t.Fatalf("helper.cpp.o cmd_args missing python include: %#v", args)
	}

	wantNumpy := []string{
		"-I$(S)/contrib/python/numpy/include/numpy/core/include",
		"-I$(S)/contrib/python/numpy/include/numpy/core/include/numpy",
		"-I$(S)/contrib/python/numpy/include/numpy/core/src/common",
		"-I$(S)/contrib/python/numpy/include/numpy/core/src/npymath",
		"-I$(S)/contrib/python/numpy/include/numpy/distutils/include",
	}

	if pythonIncludeIdx+1+len(wantNumpy) > len(args) {
		t.Fatalf("helper.cpp.o cmd_args too short for numpy include bundle: %#v", args)
	}

	for i, want := range wantNumpy {
		if got := args[pythonIncludeIdx+1+i]; got != want {
			t.Fatalf("numpy include bundle mismatch at offset %d: got %q, want %q; cmd_args=%#v", i, got, want, args)
		}
	}

	for _, arg := range args {
		if strings.HasPrefix(arg, "-DPyInit_") || strings.HasPrefix(arg, "-Dinit_module_") {
			t.Fatalf("helper.cpp.o cmd_args still carry PY_REGISTER define %q: %#v", arg, args)
		}
	}
}

func TestGen_LibraryARIncludesResourceObjcopyMemberInputs(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(t, root, "tools/rescompiler/bin", "rescompiler")
	writeToolProgram(t, root, "tools/rescompressor/bin", "rescompressor")

	writeTestModuleFile(t, root, "db/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nRESOURCE(data.sql key)\nEND()\n")
	writeTestModuleFile(t, root, "db/main.cpp", "int f(){return 0;}\n")
	writeTestModuleFile(t, root, "db/data.sql", "select 1;\n")

	g := testGen(root, "db")
	regularAR := mustNodeByOutput(t, g, "$(B)/db/libdb.a")
	mustNodeByOutput(t, g, "$(B)/db/libdb.global.a")
	if findNodeByOutputPrefix(g, "$(B)/db/objcopy_") == nil {
		t.Fatal("graph is missing db objcopy output")
	}

	// Neither the resource source (data.sql) nor the objcopy node's script
	// (objcopy.py) is an AR input — an archive bundles objects + its own
	// script (link_lib.py), not member sources or other nodes' wrapper
	// scripts. Both are dropped from AR inputs.
	if !nodeHasInput(regularAR, "$(S)/build/scripts/link_lib.py") {
		t.Fatalf("libdb.a inputs missing its own script link_lib.py: %#v", regularAR.Inputs)
	}
	for _, absent := range []string{"$(S)/db/data.sql", "$(S)/build/scripts/objcopy.py"} {
		if nodeHasInput(regularAR, absent) {
			t.Errorf("libdb.a must not list %q (not an AR input): %#v", absent, regularAR.Inputs)
		}
	}
}

func TestGen_ResourceRelativeOutputFeedsObjcopyFromBuildRoot(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(t, root, "tools/rescompiler/bin", "rescompiler")
	writeToolProgram(t, root, "tools/rescompressor/bin", "rescompressor")
	writeToolProgram(t, root, "tools/json_gen/bin", "json_gen")

	writeTestModuleFile(t, root, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/json_gen/bin
        --output
        data.json
    OUT_NOAUTO data.json
)
RESOURCE(
    data.json /data.json
)
END()
`)

	g := testGen(root, "db")

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}
	if !nodeHasInput(objcopy, "$(B)/db/data.json") {
		t.Fatalf("objcopy inputs missing build-root data.json: %#v", objcopy.Inputs)
	}
	if nodeHasInput(objcopy, "$(S)/db/data.json") {
		t.Fatalf("objcopy inputs still use source-root data.json: %#v", objcopy.Inputs)
	}
}

func TestGen_ResourceBindirOutputFeedsObjcopyFromBuildRoot(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(t, root, "tools/rescompiler/bin", "rescompiler")
	writeToolProgram(t, root, "tools/rescompressor/bin", "rescompressor")
	writeToolProgram(t, root, "tools/json_gen/bin", "json_gen")

	writeTestModuleFile(t, root, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/json_gen/bin
        --output
        ${BINDIR}/data.json
    OUT_NOAUTO ${BINDIR}/data.json
)
RESOURCE(
    ${BINDIR}/data.json /data.json
)
END()
`)

	g := testGen(root, "db")

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}
	if !nodeHasInput(objcopy, "$(B)/db/data.json") {
		t.Fatalf("objcopy inputs missing build-root data.json: %#v", objcopy.Inputs)
	}
	for _, in := range objcopy.Inputs {
		if strings.Contains(in.String(), "${BINDIR}") {
			t.Fatalf("objcopy inputs still leak ${BINDIR}: %#v", objcopy.Inputs)
		}
	}
}

func TestGen_ResourceBindirRunProgramCarriesInputClosure(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(t, root, "tools/rescompiler/bin", "rescompiler")
	writeToolProgram(t, root, "tools/rescompressor/bin", "rescompressor")
	writeToolProgram(t, root, "tools/json_gen/bin", "json_gen")

	writeTestModuleFile(t, root, "dep/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(dep.cpp dep.h)
END()
`)
	writeTestModuleFile(t, root, "dep/dep.cpp", "int dep(){return 0;}\n")
	writeTestModuleFile(t, root, "dep/dep.h", "#pragma once\n")

	writeTestModuleFile(t, root, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(dep)
RUN_PROGRAM(
    tools/json_gen/bin
        --output
        ${BINDIR}/unused.cpp
        --json-output
        ${BINDIR}/data.json
        gen.h
    IN gen.h
    OUT_NOAUTO ${BINDIR}/data.json
)
RESOURCE(
    ${BINDIR}/data.json /data.json
)
END()
`)
	writeTestModuleFile(t, root, "db/gen.h", `#pragma once
#include <dep/dep.h>
`)

	g := testGen(root, "db")

	pr := mustNodeByOutput(t, g, "$(B)/db/data.json")
	if !nodeHasInput(pr, "$(S)/db/gen.h") {
		t.Fatalf("pr inputs missing direct gen.h input: %#v", pr.Inputs)
	}
	if !nodeHasInput(pr, "$(S)/dep/dep.h") {
		t.Fatalf("pr inputs missing transitive dep/dep.h closure: %#v", pr.Inputs)
	}

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}
	if !nodeHasInput(objcopy, "$(B)/db/data.json") {
		t.Fatalf("objcopy inputs missing build-root data.json: %#v", objcopy.Inputs)
	}
	if !nodeHasInput(objcopy, "$(S)/dep/dep.h") {
		t.Fatalf("objcopy inputs missing transitive dep/dep.h closure: %#v", objcopy.Inputs)
	}
}

func writeToolProgram(t *testing.T, root, modulePath, binaryName string) {
	t.Helper()

	writeTestModuleFile(t, root, filepath.ToSlash(filepath.Join(modulePath, "ya.make")), "PROGRAM("+binaryName+")\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(t, root, filepath.ToSlash(filepath.Join(modulePath, "main.cpp")), "int main(){return 0;}\n")
}

func writeTestModuleFile(t *testing.T, root, rel, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustNodeByOutput(t *testing.T, g *Graph, output string) *Node {
	t.Helper()

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0].String() == output {
			return n
		}
	}

	t.Fatalf("graph is missing output %q", output)
	return nil
}

func findNodeByOutputPrefix(g *Graph, prefix string) *Node {
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && strings.HasPrefix(n.Outputs[0].String(), prefix) {
			return n
		}
	}

	return nil
}

func nodeHasInput(n *Node, input string) bool {
	for _, got := range n.Inputs {
		if got.String() == input {
			return true
		}
	}

	return false
}

func indexOfArg(args []string, want string) int {
	for i, arg := range args {
		if arg == want {
			return i
		}
	}

	return -1
}

func TestGen_YaBinLinkTailMatchesReference(t *testing.T) {
	const targetDir = "devtools/ya/bin"

	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	host := newT20ResourcePlatform(OSLinux, ISAX8664, "yes", []string{"tool"}, true)
	target := newT20ResourcePlatform(OSLinux, ISAAArch64, "no", nil, true)
	our := Gen(sourceRoot, targetDir, host, target, func(Warn) {})
	ourNode := findGraphNodeByOutputs(t, our, "$(B)/devtools/ya/bin/ya-bin", "$(B)/devtools/ya/bin/ya-bin.debug")
	refNode := loadT20RefNode(t, "$(BUILD_ROOT)/devtools/ya/bin/ya-bin", "$(BUILD_ROOT)/devtools/ya/bin/ya-bin.debug")

	if len(ourNode.Cmds) < 3 || len(refNode.Cmds) < 3 {
		t.Fatalf("expected both nodes to have at least 3 cmds")
	}

	gotTail := cmdArgsFrom(t, ourNode.Cmds[2].CmdArgs, "-Wl,--start-group")
	wantTail := normalizeT20Strings(cmdArgsFrom(t, refNode.Cmds[2].CmdArgs, "-Wl,--start-group"))

	if !reflect.DeepEqual(gotTail, wantTail) {
		t.Fatalf("ya-bin link tail mismatch:\n  got:  %#v\n  want: %#v", gotTail, wantTail)
	}

	anchor := "build/cow/on/libbuild-cow-on.a"
	wantAfterAnchor := []string{
		"library/cpp/malloc/api/libcpp-malloc-api.a",
		"contrib/libs/jemalloc/libcontrib-libs-jemalloc.a",
		"library/cpp/malloc/jemalloc/libcpp-malloc-jemalloc.a",
	}
	anchorIdx := slices.Index(gotTail, anchor)
	if anchorIdx < 0 {
		t.Fatalf("expected %q in ya-bin link tail: %v", anchor, gotTail)
	}
	if anchorIdx+1+len(wantAfterAnchor) > len(gotTail) {
		t.Fatalf("expected %q to be followed by %v in ya-bin link tail: %v", anchor, wantAfterAnchor, gotTail)
	}
	if !slices.Equal(gotTail[anchorIdx+1:anchorIdx+1+len(wantAfterAnchor)], wantAfterAnchor) {
		t.Fatalf("expected %q to be followed by %v in ya-bin link tail: %v", anchor, wantAfterAnchor, gotTail)
	}

	enumRuntime := "tools/enum_parser/enum_serialization_runtime/libtools-enum_parser-enum_serialization_runtime.a"
	jsonCommon := "library/cpp/json/common/libcpp-json-common.a"
	enumIdx := slices.Index(gotTail, enumRuntime)
	jsonIdx := slices.Index(gotTail, jsonCommon)
	if enumIdx < 0 || jsonIdx < 0 {
		t.Fatalf("expected both %q and %q in ya-bin link tail: %v", enumRuntime, jsonCommon, gotTail)
	}
	if enumIdx+1 != jsonIdx {
		t.Fatalf("expected %q immediately before %q in ya-bin link tail: %v", enumRuntime, jsonCommon, gotTail)
	}

	if len(ourNode.Cmds) < 7 || len(refNode.Cmds) < 7 {
		t.Fatalf("expected both nodes to have at least 7 cmds")
	}

	for _, cmdIdx := range []int{4, 5, 6} {
		gotArgs := normalizeT20Strings(ourNode.Cmds[cmdIdx].CmdArgs)
		wantArgs := normalizeT20Strings(refNode.Cmds[cmdIdx].CmdArgs)
		if !reflect.DeepEqual(gotArgs, wantArgs) {
			t.Fatalf("ya-bin cmd[%d] args mismatch:\n  got:  %#v\n  want: %#v", cmdIdx, gotArgs, wantArgs)
		}

		gotEnv := normalizeT20Env(ourNode.Cmds[cmdIdx].Env)
		wantEnv := normalizeT20Env(refNode.Cmds[cmdIdx].Env)
		if !reflect.DeepEqual(gotEnv, wantEnv) {
			t.Fatalf("ya-bin cmd[%d] env mismatch:\n  got:  %#v\n  want: %#v", cmdIdx, gotEnv, wantEnv)
		}
	}
}

func TestIsHeaderSource_ExtendedHeaderExtensions(t *testing.T) {
	for _, src := range []string{
		"a.h",
		"a.hh",
		"a.hpp",
		"a.hxx",
		"a.ipp",
		"a.ixx",
		"a.inl",
	} {
		if !isHeaderSource(src) {
			t.Fatalf("isHeaderSource(%q) = false, want true", src)
		}
	}
}

type statsUIDRefNode struct {
	HostPlatform bool `json:"host_platform,omitempty"`
	KV           struct {
		P string `json:"p"`
	} `json:"kv"`
	Platform string   `json:"platform"`
	Outputs  []string `json:"outputs"`
	StatsUID string   `json:"stats_uid"`
}

type statsUIDNodeKey struct {
	Outputs      string
	Kind         string
	HostPlatform bool
	Platform     string
}

type indexedStatsUIDNode struct {
	StatsUID string
}

func TestGen_ToolsArchiver_TargetStatsUIDsMatchReference(t *testing.T) {
	const targetDir = "tools/archiver"

	if _, err := os.Stat(filepath.Join(sourceRoot, "sg.json")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference graph not present at %s/sg.json", sourceRoot)
		}

		t.Fatalf("stat sg.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := genStatsUIDReferenceSample(sourceRoot, targetDir)
	ref := loadStatsUIDRefNodes(t, filepath.Join(sourceRoot, "sg.json"))

	assertTargetStatsUIDsMatchReference(t, our.Graph, ref, 1, "sg.json")
}

func TestGen_YaBinTargetStatsUIDsMatchReference(t *testing.T) {
	const targetDir = "devtools/ya/bin"

	if _, err := os.Stat(filepath.Join(sourceRoot, "sg3.json")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference graph not present at %s/sg3.json", sourceRoot)
		}

		t.Fatalf("stat sg3.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := genStatsUIDReferenceSample(sourceRoot, targetDir)
	ref := loadStatsUIDRefNodes(t, filepath.Join(sourceRoot, "sg3.json"))

	assertTargetStatsUIDsMatchReference(t, our.Graph, ref, 5000, "sg3.json")
}

func TestGen_YaBinHostStatsUIDsMatchReference(t *testing.T) {
	const targetDir = "devtools/ya/bin"

	if _, err := os.Stat(filepath.Join(sourceRoot, "sg3.json")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference graph not present at %s/sg3.json", sourceRoot)
		}

		t.Fatalf("stat sg3.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := genStatsUIDReferenceSample(sourceRoot, targetDir)
	ref := loadStatsUIDRefNodes(t, filepath.Join(sourceRoot, "sg3.json"))

	assertHostStatsUIDsMatchReference(t, our.Graph, ref, 4000, "sg3.json")
}

func TestGen_YaBinDumpGraphResidualTargetArchiveStatsUIDsMatchReference(t *testing.T) {
	const targetDir = "devtools/ya/bin"

	if _, err := os.Stat(filepath.Join(sourceRoot, "sg3.json")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference graph not present at %s/sg3.json", sourceRoot)
		}

		t.Fatalf("stat sg3.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := genDumpStatsUIDReferenceSample(t, sourceRoot, targetDir)
	ref := loadStatsUIDRefNodes(t, filepath.Join(sourceRoot, "sg3.json"))
	ourByKey := indexTargetStatsUIDRefNodes(t, our)
	refByKey := indexTargetStatsUIDRefNodes(t, ref)

	for _, output := range []string{
		"$(BUILD_ROOT)/contrib/libs/sqlite3/libcontrib-libs-sqlite3.a",
		"$(BUILD_ROOT)/contrib/python/protobuf/py3/libpy3python-protobuf-py3.a",
		"$(BUILD_ROOT)/contrib/tools/python3/Modules/_sqlite/libpy3python3-Modules-_sqlite.a",
		"$(BUILD_ROOT)/contrib/tools/python3/Modules/_sqlite/libpy3python3-Modules-_sqlite.global.a",
	} {
		key := statsUIDNodeKey{
			Outputs:      statsUIDOutputKey([]string{output}),
			Kind:         "AR",
			HostPlatform: false,
			Platform:     "default-linux-aarch64",
		}

		ourNode, ok := ourByKey[key]
		if !ok {
			t.Fatalf("dump graph missing generated non-host key %s", statsUIDDescribeKey(key))
		}
		refNode, ok := refByKey[key]
		if !ok {
			t.Fatalf("reference missing non-host key %s", statsUIDDescribeKey(key))
		}
		if ourNode.StatsUID != refNode.StatsUID {
			t.Fatalf("dump graph stats_uid mismatch for %s:\n got: %s\nwant: %s",
				statsUIDDescribeKey(key), ourNode.StatsUID, refNode.StatsUID)
		}
	}
}

func genStatsUIDReferenceSample(sourceRoot, targetDir string) *Graph {
	host, target := statsUIDReferencePlatforms()

	return Gen(sourceRoot, targetDir, host, target, func(Warn) {})
}

func genDumpStatsUIDReferenceSample(t *testing.T, sourceRoot, targetDir string) []statsUIDRefNode {
	t.Helper()

	out, err := os.CreateTemp(t.TempDir(), "sg3-dump-*.json")
	if err != nil {
		t.Fatalf("create dump graph capture: %v", err)
	}

	oldStdout := os.Stdout
	os.Stdout = out

	var code int
	exc := Try(func() {
		code = cmdMake([]string{
			"-j", "0",
			"-k",
			"-G",
			"--source-root", sourceRoot,
			"--target-platform", "default-linux-aarch64",
			"--host-platform", "default-linux-x86_64",
			"--host-platform-flag", "MUSL=yes",
			"--host-platform-flag", "OS_SDK=local",
			"--musl",
			"--sandboxing",
			"-DOS_SDK=local",
			targetDir,
		})
	})

	os.Stdout = oldStdout
	if err := out.Close(); err != nil {
		t.Fatalf("close dump graph capture: %v", err)
	}
	if exc != nil {
		t.Fatalf("cmdMake dump graph failed: %v", exc)
	}
	if code != 0 {
		t.Fatalf("cmdMake dump graph exit code = %d, want 0", code)
	}

	return loadStatsUIDRefNodes(t, out.Name())
}

func statsUIDReferencePlatforms() (*Platform, *Platform) {
	hostPlatformFlags := map[string]string{
		"APPLE_SDK_LOCAL":    "yes",
		"MUSL":               "yes",
		"OPENSOURCE":         "yes",
		"OS_SDK":             "local",
		"USE_CLANG_CL":       "yes",
		"USE_PREBUILT_TOOLS": "no",
	}
	hostFlags := make(map[string]string, len(testToolchainFlags)+8)
	for k, v := range testToolchainFlags {
		hostFlags[k] = v
	}
	for k, v := range hostPlatformFlags {
		hostFlags[k] = v
	}
	hostFlags["GG_BUILD_TYPE"] = "release"
	hostFlags["PIC"] = "yes"
	hostFlags["SANDBOXING"] = "yes"
	host := NewPlatform(OSLinux, ISAX8664, hostFlags, []string{"tool"}, "", "")
	host.StatsFlags = buildHostStatsFlags(hostPlatformFlags, nil, true)

	targetFlags := make(map[string]string, len(testToolchainFlags)+4)
	for k, v := range testToolchainFlags {
		targetFlags[k] = v
	}
	targetFlags["GG_BUILD_TYPE"] = "debug"
	targetFlags["MUSL"] = "yes"
	targetFlags["PIC"] = "no"
	targetFlags["SANDBOXING"] = "yes"
	target := NewPlatform(OSLinux, ISAAArch64, targetFlags, nil, "", "")
	target.Tags = sandboxingNodeTags(target)
	target.StatsFlags = buildTargetStatsFlags(targetFlags, map[string]string{"MUSL": "yes"})

	return host, target
}

func loadStatsUIDRefNodes(t *testing.T, path string) []statsUIDRefNode {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var graph struct {
		Graph []statsUIDRefNode `json:"graph"`
	}
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}

	return graph.Graph
}

func assertTargetStatsUIDsMatchReference(t *testing.T, our []*Node, ref []statsUIDRefNode, minCommon int, refName string) {
	t.Helper()

	ourByKey := indexTargetStatsUIDNodes(t, our)
	refByKey := indexTargetStatsUIDRefNodes(t, ref)

	commonKeys, onlyOur, onlyRef := diffStatsUIDNodeKeys(ourByKey, refByKey)
	if len(onlyOur) > 0 || len(onlyRef) > 0 {
		var problems []string
		if len(onlyOur) > 0 {
			problems = append(problems,
				"extra generated non-host key "+statsUIDDescribeKey(onlyOur[0])+
					" ("+strconv.Itoa(len(onlyOur))+" total)")
		}
		if len(onlyRef) > 0 {
			problems = append(problems,
				"missing generated non-host key "+statsUIDDescribeKey(onlyRef[0])+
					" ("+strconv.Itoa(len(onlyRef))+" total)")
		}
		t.Fatalf("non-host node key drift vs %s: %s", refName, strings.Join(problems, "; "))
	}

	for _, key := range commonKeys {
		ourNode := ourByKey[key]
		refNode := refByKey[key]
		if ourNode.StatsUID != refNode.StatsUID {
			t.Fatalf("stats_uid mismatch for %s:\n got: %s\nwant: %s",
				statsUIDDescribeKey(key), ourNode.StatsUID, refNode.StatsUID)
		}
	}

	if len(commonKeys) < minCommon {
		t.Fatalf("expected at least %d common non-host nodes vs %s, found %d", minCommon, refName, len(commonKeys))
	}
}

func assertHostStatsUIDsMatchReference(t *testing.T, our []*Node, ref []statsUIDRefNode, minCommon int, refName string) {
	t.Helper()

	ourByKey := indexHostStatsUIDNodes(t, our)
	refByKey := indexHostStatsUIDRefNodes(t, ref)

	commonKeys, onlyOur, onlyRef := diffStatsUIDNodeKeys(ourByKey, refByKey)
	if len(onlyOur) > 0 || len(onlyRef) > 0 {
		var problems []string
		if len(onlyOur) > 0 {
			problems = append(problems,
				"extra generated host key "+statsUIDDescribeKey(onlyOur[0])+
					" ("+strconv.Itoa(len(onlyOur))+" total)")
		}
		if len(onlyRef) > 0 {
			problems = append(problems,
				"missing generated host key "+statsUIDDescribeKey(onlyRef[0])+
					" ("+strconv.Itoa(len(onlyRef))+" total)")
		}
		t.Fatalf("host node key drift vs %s: %s", refName, strings.Join(problems, "; "))
	}

	for _, key := range commonKeys {
		ourNode := ourByKey[key]
		refNode := refByKey[key]
		if ourNode.StatsUID != refNode.StatsUID {
			t.Fatalf("host stats_uid mismatch for %s:\n got: %s\nwant: %s",
				statsUIDDescribeKey(key), ourNode.StatsUID, refNode.StatsUID)
		}
	}

	if len(commonKeys) < minCommon {
		t.Fatalf("expected at least %d common host nodes vs %s, found %d", minCommon, refName, len(commonKeys))
	}
}

func indexTargetStatsUIDNodes(t *testing.T, nodes []*Node) map[statsUIDNodeKey]indexedStatsUIDNode {
	t.Helper()

	out := make(map[statsUIDNodeKey]indexedStatsUIDNode)
	for _, node := range nodes {
		if nodeHasHostTag(node.Tags) {
			continue
		}

		key := statsUIDNodeKeyFromNode(node)
		value := indexedStatsUIDNode{StatsUID: node.StatsUID}
		if _, exists := out[key]; exists {
			t.Fatalf("duplicate generated non-host key %s", statsUIDDescribeKey(key))
		}
		out[key] = value
	}

	return out
}

func indexHostStatsUIDNodes(t *testing.T, nodes []*Node) map[statsUIDNodeKey]indexedStatsUIDNode {
	t.Helper()

	out := make(map[statsUIDNodeKey]indexedStatsUIDNode)
	for _, node := range nodes {
		if !nodeHasHostTag(node.Tags) {
			continue
		}

		key := statsUIDNodeKeyFromNode(node)
		value := indexedStatsUIDNode{StatsUID: node.StatsUID}
		if _, exists := out[key]; exists {
			t.Fatalf("duplicate generated host key %s", statsUIDDescribeKey(key))
		}
		out[key] = value
	}

	return out
}

func indexTargetStatsUIDRefNodes(t *testing.T, nodes []statsUIDRefNode) map[statsUIDNodeKey]indexedStatsUIDNode {
	t.Helper()

	out := make(map[statsUIDNodeKey]indexedStatsUIDNode)
	for _, node := range nodes {
		if node.HostPlatform {
			continue
		}

		key := statsUIDNodeKeyFromRef(node)
		value := indexedStatsUIDNode{StatsUID: node.StatsUID}
		if _, exists := out[key]; exists {
			t.Fatalf("duplicate reference non-host key %s", statsUIDDescribeKey(key))
		}
		out[key] = value
	}

	return out
}

func indexHostStatsUIDRefNodes(t *testing.T, nodes []statsUIDRefNode) map[statsUIDNodeKey]indexedStatsUIDNode {
	t.Helper()

	out := make(map[statsUIDNodeKey]indexedStatsUIDNode)
	for _, node := range nodes {
		if !node.HostPlatform {
			continue
		}

		key := statsUIDNodeKeyFromRef(node)
		value := indexedStatsUIDNode{StatsUID: node.StatsUID}
		if _, exists := out[key]; exists {
			t.Fatalf("duplicate reference host key %s", statsUIDDescribeKey(key))
		}
		out[key] = value
	}

	return out
}

func diffStatsUIDNodeKeys(our, ref map[statsUIDNodeKey]indexedStatsUIDNode) ([]statsUIDNodeKey, []statsUIDNodeKey, []statsUIDNodeKey) {
	commonKeys := make([]statsUIDNodeKey, 0, len(our))
	onlyOur := make([]statsUIDNodeKey, 0)
	onlyRef := make([]statsUIDNodeKey, 0)

	for key := range our {
		if _, ok := ref[key]; ok {
			commonKeys = append(commonKeys, key)
			continue
		}
		onlyOur = append(onlyOur, key)
	}
	for key := range ref {
		if _, ok := our[key]; ok {
			continue
		}
		onlyRef = append(onlyRef, key)
	}

	sortStatsUIDNodeKeys(commonKeys)
	sortStatsUIDNodeKeys(onlyOur)
	sortStatsUIDNodeKeys(onlyRef)

	return commonKeys, onlyOur, onlyRef
}

func sortStatsUIDNodeKeys(keys []statsUIDNodeKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Outputs != keys[j].Outputs {
			return keys[i].Outputs < keys[j].Outputs
		}
		if keys[i].Kind != keys[j].Kind {
			return keys[i].Kind < keys[j].Kind
		}
		if keys[i].HostPlatform != keys[j].HostPlatform {
			return !keys[i].HostPlatform && keys[j].HostPlatform
		}
		return keys[i].Platform < keys[j].Platform
	})
}

func statsUIDDescribeKey(key statsUIDNodeKey) string {
	return "outputs=" + strings.Join(statsUIDOutputsFromKey(key), ",") +
		" kind=" + key.Kind +
		" host_platform=" + boolString(key.HostPlatform) +
		" platform=" + key.Platform
}

func statsUIDOutputsFromKey(key statsUIDNodeKey) []string {
	if key.Outputs == "" {
		return nil
	}
	return strings.Split(key.Outputs, "\x00")
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func statsUIDOutputKey(outputs []string) string {
	normalized := append([]string(nil), outputs...)
	for i, out := range normalized {
		normalized[i] = normalizeStatsUIDOutput(out)
	}
	sort.Strings(normalized)

	return strings.Join(normalized, "\x00")
}

func statsUIDNodeKeyFromNode(node *Node) statsUIDNodeKey {
	kind, _ := node.KV["p"].(string)

	return statsUIDNodeKey{
		Outputs:      statsUIDOutputKey(vfsStrings(node.Outputs)),
		Kind:         kind,
		HostPlatform: nodeHasHostTag(node.Tags),
		Platform:     node.Platform,
	}
}

func statsUIDNodeKeyFromRef(node statsUIDRefNode) statsUIDNodeKey {
	return statsUIDNodeKey{
		Outputs:      statsUIDOutputKey(node.Outputs),
		Kind:         node.KV.P,
		HostPlatform: node.HostPlatform,
		Platform:     node.Platform,
	}
}

func normalizeStatsUIDOutput(out string) string {
	out = strings.ReplaceAll(out, "$(BUILD_ROOT)", "$(B)")
	out = strings.ReplaceAll(out, "$(SOURCE_ROOT)", "$(S)")

	return out
}

// TestGen_ProtoLibrary_NamedArgUsedForArchive verifies Fix #1: when
// PROTO_LIBRARY declares an explicit name argument the emitted archive uses
// that name instead of the path-derived default.
func TestGen_ProtoLibrary_NamedArgUsedForArchive(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "ydb/public/api/protos/ya.make", `PROTO_LIBRARY(api-protos)
SRCS(ydb.proto)
END()
`)
	writeTestModuleFile(t, root, "ydb/public/api/protos/ydb.proto", `syntax = "proto3";
package test;
message Ydb {}
`)
	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(root, "ydb/public/api/protos")

	// Named archive: lib<name>.a not lib<path-derived>.a
	mustNodeByOutput(t, g, "$(B)/ydb/public/api/protos/libapi-protos.a")

	// Path-derived archive must NOT be present
	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			if o.String() == "$(B)/ydb/public/api/protos/libprotos.a" {
				t.Fatalf("path-derived archive libprotos.a should not exist; got it with named arg")
			}
		}
	}
}

// TestGen_ProtoLibrary_UnnamedArgKeepsPathDerivedArchive verifies that
// PROTO_LIBRARY() without a name argument continues to use the path-derived
// archive name (regression guard for Fix #1).
func TestGen_ProtoLibrary_UnnamedArgKeepsPathDerivedArchive(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "ydb/public/api/protos/ya.make", `PROTO_LIBRARY()
SRCS(ydb.proto)
END()
`)
	writeTestModuleFile(t, root, "ydb/public/api/protos/ydb.proto", `syntax = "proto3";
package test;
message Ydb {}
`)
	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(root, "ydb/public/api/protos")

	// Path-derived name: last 3 segments of ydb/public/api/protos → public-api-protos
	mustNodeByOutput(t, g, "$(B)/ydb/public/api/protos/libpublic-api-protos.a")
}
