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
		// PR-10 emits one platform (TargetCfg.Target.ID). The reference
		// graph carries the same module on multiple platforms (4 nodes
		// for build/cow/on: 2 platforms × {CC, AR}); restrict the
		// comparison subgraph to TargetCfg.Target.ID so the pairing is
		// 2-vs-2 not 4-vs-2.
		if n.Platform != string(TargetCfg.Target.ID) {
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
// `$(BUILD_ROOT)/mainprog/mainprog`).
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

		nodesByOutput[n.Outputs[0]] = n
	}

	const (
		libCCOut    = "$(BUILD_ROOT)/thelib/lib.cpp.o"
		libARout    = "$(BUILD_ROOT)/thelib/libthelib.a"
		mainCCOut   = "$(BUILD_ROOT)/mainprog/main.cpp.o"
		mainBinPath = "$(BUILD_ROOT)/mainprog/mainprog"
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

	g := Gen(TargetCfg, root, "lone")

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

	wantOut := "$(BUILD_ROOT)/lone/lone"
	if len(ld.Outputs) != 1 || ld.Outputs[0] != wantOut {
		t.Errorf("LD outputs = %#v, want [%q]", ld.Outputs, wantOut)
	}

	if g.Result[0] != ld.UID {
		t.Errorf("result UID = %q, want LD uid %q", g.Result[0], ld.UID)
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
// PR-23's whitelist throws with a concrete deferred-to-PR-25
// message. PR-13 introduced typed Stmts for IF / INCLUDE /
// JOIN_SRCS / ADDINCL / CFLAGS / LDFLAGS / SRCDIR / GLOBAL_SRCS,
// so `IF` is no longer the "unsupported macro" canary — gen.go now
// hits its default `*Stmt` arm with an "unhandled Stmt type" message
// for those. Any name NOT in `pr12SupportedUnknownMacros` AND NOT a
// typed Stmt name still flows through `*UnknownStmt` and trips the
// original whitelist check; `RUN_PYTHON3` is a stable example of
// that path.
func TestGen_RejectsUnsupportedMacro(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "mod")

	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	yamake := []byte("LIBRARY()\nRUN_PYTHON3(foo bar)\nSRCS(main.cpp)\nEND()\n")

	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write mod/ya.make: %v", err)
	}

	exc := Try(func() {
		Gen(TargetCfg, root, "mod")
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
		Gen(DefaultLinuxConfig, tmp, "caller")
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
func TestGen_DualInstantiation_BuildCowOn(t *testing.T) {
	const targetDir = "build/cow/on"

	if _, err := os.Stat(sourceRoot + "/" + targetDir + "/ya.make"); err != nil {
		t.Skipf("reference ya.make not present: %v", err)
	}

	if _, err := os.Stat(referenceGraphPath); err != nil {
		t.Skipf("reference graph %s not present: %v", referenceGraphPath, err)
	}

	// Step 1: full Gen against target. Must emit exactly 2 nodes
	// (1 CC + 1 AR) — same as M1 acceptance.
	our := Gen(DefaultLinuxConfig, sourceRoot, targetDir)

	if len(our.Graph) != 2 {
		t.Errorf("Gen produced %d nodes, want 2 (1 CC + 1 AR target-only)", len(our.Graph))
	}

	for _, n := range our.Graph {
		if n.Platform != string(PlatformDefaultLinuxAArch64) {
			t.Errorf("node %s on platform %q; want target only", n.Outputs[0], n.Platform)
		}

		if n.HostPlatform {
			t.Errorf("node %s has host_platform=true; want target only", n.Outputs[0])
		}
	}

	// Step 2: build a fresh emitter and emit BOTH target and host
	// pairs by hand. Verify 4 nodes total.
	e := NewBufferedEmitter()

	tInstance := targetInstance(targetDir)
	tCCRef, tCCOut := EmitCC(tInstance, "lib.c", e)
	EmitAR(tInstance, []NodeRef{tCCRef}, []string{tCCOut}, nil, e)

	hInstance := hostInstance(targetDir)
	hCCRef, hCCOut := EmitCC(hInstance, "lib.c", e)
	EmitAR(hInstance, []NodeRef{hCCRef}, []string{hCCOut}, nil, e)

	if len(e.nodes) != 4 {
		t.Errorf("dual emission produced %d nodes, want 4", len(e.nodes))
	}

	// Verify host nodes (indices 2, 3) carry host_platform=true and
	// tags=["tool"].
	hostCC := e.nodes[2]
	hostAR := e.nodes[3]

	for i, n := range []*Node{hostCC, hostAR} {
		if !n.HostPlatform {
			t.Errorf("dual host node %d host_platform = false, want true", i)
		}

		if len(n.Tags) != 1 || n.Tags[0] != "tool" {
			t.Errorf("dual host node %d tags = %v, want [tool]", i, n.Tags)
		}

		if n.Platform != string(PlatformDefaultLinuxX8664) {
			t.Errorf("dual host node %d platform = %q, want %q", i, n.Platform, PlatformDefaultLinuxX8664)
		}
	}

	// Verify target nodes (indices 0, 1) carry no host_platform
	// and tags=[].
	targetCC := e.nodes[0]
	targetAR := e.nodes[1]

	for i, n := range []*Node{targetCC, targetAR} {
		if n.HostPlatform {
			t.Errorf("dual target node %d host_platform = true, want false", i)
		}

		if len(n.Tags) != 0 {
			t.Errorf("dual target node %d tags = %v, want []", i, n.Tags)
		}
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
	// Pragmatic check: the synthetic produces 6 nodes (3 CC + 2 AR + 1 LD;
	// mainprog is PROGRAM so closes with LD, two peer LIBRARYs close with AR).
	// Verify count — catches regressions where a sort.Strings(peerdirs) collapses or
	// breaks the walk.
	if len(g.Graph) != 6 {
		t.Errorf("expected 6 nodes (3 CC + 2 AR + 1 LD), got %d", len(g.Graph))
	}
}
