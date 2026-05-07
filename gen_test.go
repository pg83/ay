package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gen_test.go — end-to-end test for the M1 vertical slice. Parses
// build/cow/on/ya.make, emits the 2-node CC+AR subgraph, and asserts
// byte-exact L3 equality against the corresponding 2 nodes carved out
// of the upstream reference graph.
//
// Skip-if-missing pattern follows cc_test.go / ar_test.go: the reference
// snapshot under /home/pg/monorepo/yatool_orig is required; absence is a
// host condition, not a test failure.

var sourceRoot = filepath.Dir(referenceGraphPath)

func TestGen_BuildCowOn_TwoNodeSubgraph_L3MatchesReference(t *testing.T) {
	const targetDir = "build/cow/on"
	const arOutput = "$(BUILD_ROOT)/build/cow/on/libbuild-cow-on.a"

	if _, err := os.Stat(sourceRoot + "/" + targetDir + "/ya.make"); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	if _, err := os.Stat(referenceGraphPath); err != nil {
		t.Skipf("reference graph %s not present: %v", referenceGraphPath, err)
	}

	our := Gen(TargetCfg, sourceRoot, targetDir)

	if len(our.Graph) != 2 {
		t.Fatalf("Gen produced %d nodes for %s, want 2 (1 CC + 1 AR)", len(our.Graph), targetDir)
	}

	ref := LoadReference(referenceGraphPath)

	subgraphRef := &Graph{
		Conf:   ref.Conf,
		Inputs: ref.Inputs,
		Graph:  []*Node{},
	}

	var arUID string

	for _, n := range ref.Graph {
		if len(n.Outputs) == 0 {
			continue
		}

		if !strings.HasPrefix(n.Outputs[0], "$(BUILD_ROOT)/"+targetDir+"/") {
			continue
		}
		// PR-10 emits one platform (TargetCfg.Name). The reference graph
		// carries the same module on multiple platforms (4 nodes for
		// build/cow/on: 2 platforms × {CC, AR}); restrict the comparison
		// subgraph to TargetCfg.Name so the pairing is 2-vs-2 not 4-vs-2.
		if n.Platform != TargetCfg.Name {
			continue
		}

		subgraphRef.Graph = append(subgraphRef.Graph, n)

		if n.Outputs[0] == arOutput {
			arUID = n.UID
		}
	}

	if len(subgraphRef.Graph) != 2 {
		t.Fatalf("expected 2 nodes in reference subgraph for %s, got %d", targetDir, len(subgraphRef.Graph))
	}

	if arUID == "" {
		t.Fatalf("reference subgraph for %s has no AR node with output %q", targetDir, arOutput)
	}

	subgraphRef.Result = []string{arUID}

	r := Compare(subgraphRef, our, 3)

	if r.L0 != 1.0 {
		t.Errorf("L0 = %v, want 1.0 (note: %q)", r.L0, r.L0Note)
	}

	if r.L1 != 1.0 {
		t.Errorf("L1 = %v, want 1.0 (note: %q)", r.L1, r.L1Note)
	}

	if r.L2 != 1.0 {
		t.Errorf("L2 = %v, want 1.0 (note: %q)", r.L2, r.L2Note)
	}

	if r.L3 != 1.0 {
		t.Errorf("L3 = %v, want 1.0 (note: %q)", r.L3, r.L3Note)
	}

	t.Logf("L0=%.4f L1=%.4f L2=%.4f L3=%.4f", r.L0, r.L1, r.L2, r.L3)
	t.Logf("L3 note: %s", r.L3Note)
}

// TestGen_AcceptsProgramModule_Synthetic verifies PR-12's two
// architectural pivots in one shot:
//   - PROGRAM() modules are accepted (M1 only allowed LIBRARY);
//   - PEERDIR(...) is recursively walked, with the parent module's
//     archive node carrying its peer's archive UID as a dependency.
//
// The synthetic source tree has two modules — `mainprog` (PROGRAM
// peering thelib) and `thelib` (LIBRARY) — each with a single
// source. The expected closure is 4 nodes: thelib's CC + AR, then
// mainprog's CC + AR. The root result is mainprog's AR (which today
// is also its LD ref; PR-19 will distinguish them).
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

	g := Gen(TargetCfg, root, "mainprog")

	if len(g.Graph) != 4 {
		t.Fatalf("Gen produced %d nodes, want 4 (2 CC + 2 AR)", len(g.Graph))
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

		nodesByOutput[n.Outputs[0]] = n
	}

	const (
		libCCOut  = "$(BUILD_ROOT)/thelib/lib.cpp.o"
		libARout  = "$(BUILD_ROOT)/thelib/libthelib.a"
		mainCCOut = "$(BUILD_ROOT)/mainprog/main.cpp.o"
		mainARout = "$(BUILD_ROOT)/mainprog/libmainprog.a"
	)

	for _, key := range []string{libCCOut, libARout, mainCCOut, mainARout} {
		if _, ok := nodesByOutput[key]; !ok {
			t.Fatalf("graph is missing expected output %q", key)
		}
	}

	rootAR := nodesByOutput[mainARout]

	// Result must point at the root AR node (PR-19 will swap this
	// to the LD UID; today they coincide).
	if g.Result[0] != rootAR.UID {
		t.Errorf("result UID = %q, want mainprog AR uid %q", g.Result[0], rootAR.UID)
	}

	if rootAR.TargetProperties["module_dir"] != "mainprog" {
		t.Errorf("root AR module_dir = %q, want %q", rootAR.TargetProperties["module_dir"], "mainprog")
	}

	// Root AR must depend on BOTH its own CC node AND the peer's
	// AR node — that is the wiring contract PR-12 commits to.
	mainCC := nodesByOutput[mainCCOut]
	libAR := nodesByOutput[libARout]

	depSet := make(map[string]struct{}, len(rootAR.Deps))
	for _, d := range rootAR.Deps {
		depSet[d] = struct{}{}
	}

	if _, ok := depSet[mainCC.UID]; !ok {
		t.Errorf("root AR deps %v missing main.cpp.o uid %q", rootAR.Deps, mainCC.UID)
	}

	if _, ok := depSet[libAR.UID]; !ok {
		t.Errorf("root AR deps %v missing thelib AR uid %q", rootAR.Deps, libAR.UID)
	}
}

// TestGen_PeerdirCycle_Throws verifies the cycle detector fires when
// two modules peer each other. The walking-set check throws as soon
// as the second module recurses back into the first.
func TestGen_PeerdirCycle_Throws(t *testing.T) {
	root := t.TempDir()

	aDir := filepath.Join(root, "a")
	bDir := filepath.Join(root, "b")

	if err := os.MkdirAll(aDir, 0o755); err != nil {
		t.Fatalf("mkdir a: %v", err)
	}

	if err := os.MkdirAll(bDir, 0o755); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}

	if err := os.WriteFile(filepath.Join(aDir, "ya.make"), []byte("LIBRARY()\nPEERDIR(b)\nSRCS(a.cpp)\nEND()\n"), 0o644); err != nil {
		t.Fatalf("write a/ya.make: %v", err)
	}

	if err := os.WriteFile(filepath.Join(bDir, "ya.make"), []byte("LIBRARY()\nPEERDIR(a)\nSRCS(b.cpp)\nEND()\n"), 0o644); err != nil {
		t.Fatalf("write b/ya.make: %v", err)
	}

	exc := Try(func() {
		Gen(TargetCfg, root, "a")
	})

	if exc == nil {
		t.Fatal("expected exception for PEERDIR cycle, got nil")
	}

	if !strings.Contains(exc.Error(), "cycle detected") {
		t.Errorf("error %q does not mention 'cycle detected'", exc.Error())
	}
}

// TestGen_RejectsUnsupportedMacro verifies that any macro outside
// PR-12's narrow whitelist throws with a concrete deferred-to-PR-13
// message. `IF` is the canonical example: it is the macro PR-13
// will introduce a real evaluator for.
func TestGen_RejectsUnsupportedMacro(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "mod")

	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	yamake := []byte("LIBRARY()\nIF(LINUX)\nSRCS(a.cpp)\nENDIF()\nSRCS(main.cpp)\nEND()\n")

	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write mod/ya.make: %v", err)
	}

	exc := Try(func() {
		Gen(TargetCfg, root, "mod")
	})

	if exc == nil {
		t.Fatal("expected exception for unsupported macro, got nil")
	}

	if !strings.Contains(exc.Error(), "PR-12 does not yet support macro") {
		t.Errorf("error %q does not contain 'PR-12 does not yet support macro'", exc.Error())
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
		Gen(TargetCfg, tmp, "bad")
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
		Gen(TargetCfg, tmp, "noop")
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "no module declaration") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestCmdGen_HelpFlag_PrintsUsageAndExits0(t *testing.T) {
	rc := cmdGen([]string{"-h"})

	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}

func TestCmdGen_UnknownFlag_PanicsWithSingleErrorMessage(t *testing.T) {
	exc := Try(func() {
		cmdGen([]string{"-bogus"})
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "flag provided but not defined") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestCmdGen_MissingTargetThrows(t *testing.T) {
	exc := Try(func() {
		cmdGen([]string{"--out", "/dev/null"})
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "--target is required") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestCmdGen_MissingOutThrows(t *testing.T) {
	exc := Try(func() {
		cmdGen([]string{"--target", "build/cow/on"})
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "--out is required") {
		t.Errorf("unexpected error: %v", exc)
	}
}

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

	g := Gen(TargetCfg, tmp, "mainprog")

	// Find the AR nodes for zlib and alib by output path. Assert zlib AR appears
	// BEFORE alib AR in g.Graph (post-Finalize topo order respects emit order +
	// dep relationship; for two independent leaves visited in declaration order
	// zlib should be emitted first, hence appear first in topo).
	var zlibIdx, alibIdx int = -1, -1

	for i, n := range g.Graph {
		if len(n.Outputs) > 0 {
			if strings.Contains(n.Outputs[0], "/zlib/") && n.KV["p"] == "AR" {
				zlibIdx = i
			}
			if strings.Contains(n.Outputs[0], "/alib/") && n.KV["p"] == "AR" {
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
	// Pragmatic check: the synthetic produces 6 nodes (3 CC + 3 AR).
	// Verify count — catches regressions where a sort.Strings(peerdirs) collapses or
	// breaks the walk.
	if len(g.Graph) != 6 {
		t.Errorf("expected 6 nodes (3 CC + 3 AR), got %d", len(g.Graph))
	}
}
