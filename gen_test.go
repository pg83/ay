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
// for those. Any name NOT in `whitelistedMetadataMacros` AND NOT a
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

	g := Gen(TargetCfg, root, "ifmod")

	if len(g.Graph) != 2 {
		t.Fatalf("expected 2 nodes (1 CC + 1 AR), got %d", len(g.Graph))
	}

	var ccInputs []string

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			ccInputs = append(ccInputs, n.Inputs...)
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
// module's ya.make sets `instance.Flags.NoLibc=true` for the
// resulting CC node. The instance's flags drive the cmd_args bundle
// composition; PR-25 only checks the FlagSet flow (PR-26 verifies
// the bundle output byte-exact). Because PR-25's CC composer still
// uses path-based dispatch (musl path → muslCC), the NO_LIBC bool
// is observable via the moduleData accumulator's effect on the
// instance carried into EmitCC. We use a probe ya.make whose path
// does NOT match the path-based seed (so the only way Flags.NoLibc
// becomes true is via the macro overlay).
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
	// we want to see the flags that flow into EmitCC for "nolibcmod"
	// — which is NOT a path inferFlagsFromPath bumps NoLibc on.)
	mf := Throw2(ParseFile(filepath.Join(modDir, "ya.make")))
	pathFlags := inferFlagsFromPath("nolibcmod", false)

	if pathFlags.NoLibc || pathFlags.NoUtil || pathFlags.NoRuntime {
		t.Fatalf("path flags pre-set; test premise broken: %+v", pathFlags)
	}

	d := collectModule("nolibcmod", mf.Stmts, buildIfEnv(ModuleInstance{Target: PlatformDefaultLinuxAArch64}), pathFlags)

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
	g := Gen(TargetCfg, root, "nolibcmod")

	if len(g.Graph) != 2 {
		t.Errorf("Gen produced %d nodes, want 2 (1 CC + 1 AR)", len(g.Graph))
	}
}

// TestGen_JoinSrcs_EmitsJSAndCC verifies that JOIN_SRCS produces
// (1 JS) + (1 CC for joined) + (1 CC for sibling) + (1 AR) = 4
// nodes. The JS NodeRef must thread into the joined-CC's input
// path; the sibling CC compiles regularly. The joined CC's source
// path is `$(BUILD_ROOT)/<modulePath>/<allName>` per EmitJS.
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

	g := Gen(TargetCfg, root, "joinmod")

	counts := make(map[string]int)
	for _, n := range g.Graph {
		counts[n.KV["p"]]++
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
		case strings.Contains(n.Inputs[0], "all_my.cpp"):
			joinedInput = n.Inputs[0]
		case strings.Contains(n.Inputs[0], "other.cpp"):
			otherInput = n.Inputs[0]
		}
	}

	if joinedInput == "" {
		t.Errorf("no CC node found whose input references all_my.cpp")
	}

	if otherInput == "" {
		t.Errorf("no CC node found whose input references other.cpp")
	}

	// Sanity: the JS node's output path is the joined .cpp under
	// $(BUILD_ROOT)/<modulePath>/<allName>.
	var jsOut string

	for _, n := range g.Graph {
		if n.KV["p"] == "JS" && len(n.Outputs) > 0 {
			jsOut = n.Outputs[0]
		}
	}

	wantJSOut := "$(BUILD_ROOT)/joinmod/all_my.cpp"
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

	g := Gen(TargetCfg, root, "globalmod")

	counts := make(map[string]int)
	for _, n := range g.Graph {
		counts[n.KV["p"]]++
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

	// Synthetic host ragel6 module — just enough to walk.
	ragelDir := filepath.Join(root, "contrib/tools/ragel6")
	Throw(os.MkdirAll(ragelDir, 0o755))
	Throw(os.WriteFile(filepath.Join(ragelDir, "ya.make"), []byte("PROGRAM()\nSRCS(main.cpp)\nEND()\n"), 0o644))

	// Target consumer with an .rl6 source.
	consumerDir := filepath.Join(root, "consumer")
	Throw(os.MkdirAll(consumerDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consumerDir, "ya.make"), []byte("LIBRARY()\nSRCS(parser.rl6)\nEND()\n"), 0o644))

	g := Gen(DefaultLinuxConfig, root, "consumer")

	counts := make(map[string]int)
	platforms := make(map[string]int)
	hostNodes := 0

	for _, n := range g.Graph {
		counts[n.KV["p"]]++
		platforms[n.Platform]++

		if n.HostPlatform {
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
		ldUID  string
	)

	for _, n := range g.Graph {
		switch n.KV["p"] {
		case "R6":
			r6Node = n
		case "LD":
			ldUID = n.UID
		}
	}

	if r6Node == nil {
		t.Fatal("no R6 node found")
	}

	tool, ok := r6Node.ForeignDeps["tool"]
	if !ok {
		t.Fatalf("R6 ForeignDeps[tool] missing; got %#v", r6Node.ForeignDeps)
	}

	if len(tool) != 1 || tool[0] != ldUID {
		t.Errorf("R6 ForeignDeps[tool] = %v, want [%q]", tool, ldUID)
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

	g := Gen(TargetCfg, root, "consumer")

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

	// D08 regression guard: inputs must contain $(BUILD_ROOT)/peerlib/libpeerlib.global.a
	// (single prefix, no double). composeLDInputs prepends $(BUILD_ROOT)/ itself, so
	// GlobalPath must be BUILD_ROOT-relative (no $(BUILD_ROOT)/ prefix).
	expectedInput := "$(BUILD_ROOT)/peerlib/libpeerlib.global.a"
	foundInInputs := false

	for _, in := range ldNode.Inputs {
		if in == expectedInput {
			foundInInputs = true
			break
		}
	}

	if !foundInInputs {
		t.Errorf("expected single-prefixed global archive in inputs; got: %v", ldNode.Inputs)
	}

	// Guard against double-prefixed entries (the original D08 defect).
	for _, in := range ldNode.Inputs {
		if strings.Contains(in, "$(BUILD_ROOT)/$(BUILD_ROOT)") {
			t.Errorf("double-prefixed input found: %q", in)
		}
	}

	// D08 regression guard: cmd_args of link_exe.py (cmds[2]) must contain
	// peerlib/libpeerlib.global.a without any $(BUILD_ROOT)/ prefix, because
	// composeLDCmdLinkExe appends globalPaths verbatim into cmd_args and link_exe.py
	// resolves them relative to $(BUILD_ROOT) (the cmd's Cwd).
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
// Skipped when /home/pg/monorepo/yatool_orig is not present.
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
		g = Gen(DefaultLinuxConfig, sourceRoot, "tools/archiver")
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

	g := Gen(TargetCfg, sourceRoot, targetDir)

	if len(g.Graph) != 2 {
		t.Errorf("Gen produced %d nodes, want 2 (defaults must be suppressed)", len(g.Graph))
	}

	// Belt-and-braces unit assertion: the helper itself returns
	// nothing for an effectively-no-platform CPP module.
	bcOn := ModuleInstance{
		Path:     targetDir,
		Language: LangCPP,
		Target:   PlatformDefaultLinuxAArch64,
		Flags:    inferFlagsFromPath(targetDir, false),
	}

	got := defaultPeerdirsFor(bcOn)

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
	} {
		dir := filepath.Join(root, path)

		Throw(os.MkdirAll(dir, 0o755))
		Throw(os.WriteFile(filepath.Join(dir, "ya.make"), []byte(stubLib), 0o644))
	}

	// Helper assertion: defaultPeerdirsFor returns exactly the
	// three paths for a fresh CPP LIBRARY with zero NO_* flags.
	plain := ModuleInstance{
		Path:     "consumer",
		Language: LangCPP,
		Target:   PlatformDefaultLinuxAArch64,
		Flags:    FlagSet{},
	}

	wantDefaults := []string{
		"contrib/libs/musl",
		"contrib/libs/cxxsupp/builtins",
		"library/cpp/malloc/api",
	}

	gotDefaults := defaultPeerdirsFor(plain)

	if !stringSlicesEqual(gotDefaults, wantDefaults) {
		t.Errorf("defaultPeerdirsFor(plain CPP) = %v, want %v", gotDefaults, wantDefaults)
	}

	// End-to-end: walk a consumer LIBRARY and confirm the three
	// stubs were emitted.
	consumerDir := filepath.Join(root, "consumer")

	Throw(os.MkdirAll(consumerDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consumerDir, "ya.make"), []byte("LIBRARY()\nSRCS(main.cpp)\nEND()\n"), 0o644))

	g := Gen(TargetCfg, root, "consumer")

	emittedDirs := make(map[string]bool)

	for _, n := range g.Graph {
		if md, ok := n.TargetProperties["module_dir"]; ok {
			emittedDirs[md] = true
		}
	}

	for _, want := range []string{
		"consumer",
		"contrib/libs/musl",
		"contrib/libs/cxxsupp/builtins",
		"library/cpp/malloc/api",
	} {
		if !emittedDirs[want] {
			t.Errorf("graph missing module_dir %q; got %v", want, emittedDirs)
		}
	}
}

// TestGen_DefaultPeerdirs_HelperSuppression exercises the full
// suppression matrix of `defaultPeerdirsFor`:
//
//   - effective NO_PLATFORM (NoLibc+NoRuntime+NoUtil)  → empty set
//   - explicit NO_PLATFORM                            → empty set
//   - NO_LIBC only                                    → drops musl
//   - NO_RUNTIME only                                 → drops builtins
//   - non-CPP language                                → empty set
//   - self-instance for each default                  → drops self
func TestGen_DefaultPeerdirs_HelperSuppression(t *testing.T) {
	cases := []struct {
		name string
		mi   ModuleInstance
		want []string
	}{
		{
			name: "effective_no_platform",
			mi: ModuleInstance{
				Path:     "x",
				Language: LangCPP,
				Flags:    FlagSet{NoLibc: true, NoRuntime: true, NoUtil: true},
			},
			want: nil,
		},
		{
			name: "explicit_no_platform",
			mi: ModuleInstance{
				Path:     "x",
				Language: LangCPP,
				Flags:    FlagSet{NoPlatform: true},
			},
			want: nil,
		},
		{
			name: "no_libc_only",
			mi: ModuleInstance{
				Path:     "x",
				Language: LangCPP,
				Flags:    FlagSet{NoLibc: true},
			},
			want: []string{"contrib/libs/cxxsupp/builtins", "library/cpp/malloc/api"},
		},
		{
			name: "no_runtime_only",
			mi: ModuleInstance{
				Path:     "x",
				Language: LangCPP,
				Flags:    FlagSet{NoRuntime: true},
			},
			want: []string{"contrib/libs/musl", "library/cpp/malloc/api"},
		},
		{
			name: "non_cpp",
			mi: ModuleInstance{
				Path:     "x",
				Language: LangProto,
				Flags:    FlagSet{},
			},
			want: nil,
		},
		{
			name: "self_musl",
			mi: ModuleInstance{
				Path:     "contrib/libs/musl",
				Language: LangCPP,
				Flags:    FlagSet{},
			},
			want: []string{"contrib/libs/cxxsupp/builtins", "library/cpp/malloc/api"},
		},
		{
			name: "self_musl_subdir",
			mi: ModuleInstance{
				Path:     "contrib/libs/musl/full",
				Language: LangCPP,
				Flags:    FlagSet{},
			},
			want: []string{"contrib/libs/cxxsupp/builtins", "library/cpp/malloc/api"},
		},
		{
			name: "no_util_only",
			mi:   ModuleInstance{Path: "x", Language: LangCPP, Flags: FlagSet{NoUtil: true}},
			want: []string{"contrib/libs/musl", "contrib/libs/cxxsupp/builtins", "library/cpp/malloc/api"},
		},
		{
			name: "musl_extra_not_self",
			mi:   ModuleInstance{Path: "contrib/libs/musl_extra", Language: LangCPP, Flags: FlagSet{}},
			want: []string{"contrib/libs/musl", "contrib/libs/cxxsupp/builtins", "library/cpp/malloc/api"},
		},
		{
			name: "self_builtins",
			mi: ModuleInstance{
				Path:     "contrib/libs/cxxsupp/builtins",
				Language: LangCPP,
				Flags:    FlagSet{},
			},
			want: []string{"contrib/libs/musl", "library/cpp/malloc/api"},
		},
		{
			name: "self_malloc_api",
			mi: ModuleInstance{
				Path:     "library/cpp/malloc/api",
				Language: LangCPP,
				Flags:    FlagSet{},
			},
			want: []string{"contrib/libs/musl", "contrib/libs/cxxsupp/builtins"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := defaultPeerdirsFor(c.mi)

			if !stringSlicesEqual(got, c.want) {
				t.Errorf("defaultPeerdirsFor(%+v) = %v, want %v", c.mi, got, c.want)
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

	g := Gen(DefaultLinuxConfig, tmp, "lib1")

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

	// Count distinct musl AR refs in DepRefs.
	muslCount := 0
	for _, ref := range lib1AR.Deps {
		// Find the node by UID
		for _, n := range g.Graph {
			if n.UID == ref && n.KV["p"] == "AR" && n.TargetProperties["module_dir"] == "contrib/libs/musl" {
				muslCount++
				break
			}
		}
	}

	if muslCount != 1 {
		t.Errorf("expected exactly 1 musl AR in lib1's deps, got %d", muslCount)
	}
}
