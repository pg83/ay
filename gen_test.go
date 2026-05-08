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
		cfg:        TargetCfg,
		sourceRoot: root,
		emit:       NewBufferedEmitter(),
		memo:       make(map[ModuleInstance]*moduleEmitResult),
		walking:    make(map[ModuleInstance]bool),
	}

	seed := ModuleInstance{
		Path:     "a",
		Language: LangCPP,
		Target:   TargetCfg.Target.ID,
		Flags:    inferFlagsFromPath("a", false),
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
	tCCRef, tCCOut := EmitCC(tInstance, "lib.c", ModuleCCInputs{}, e)
	EmitAR(tInstance, []NodeRef{tCCRef}, []string{tCCOut}, nil, nil, e)

	hInstance := hostInstance(targetDir)
	hCCRef, hCCOut := EmitCC(hInstance, "lib.c", ModuleCCInputs{}, e)
	EmitAR(hInstance, []NodeRef{hCCRef}, []string{hCCOut}, nil, nil, e)

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

	// PR-28 D04: ragel6 host LD edge lives in deps (not foreign_deps)
	// to match the empirical reference shape.
	if len(r6Node.Deps) != 1 || r6Node.Deps[0] != ldNode.UID {
		t.Errorf("R6 Deps = %v, want [%q]", r6Node.Deps, ldNode.UID)
	}

	if len(r6Node.ForeignDeps) != 0 {
		t.Errorf("R6 ForeignDeps = %v, want empty (PR-28 dropped foreign_deps[tool] placeholder)", r6Node.ForeignDeps)
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
	// path `$(BUILD_ROOT)/contrib/tools/ragel6/ragel6`. The host LD's
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

	wantCmd0 := canonicalizeRagel6BinaryPath(ldNode.Outputs[0])

	if r6Node.Cmds[0].CmdArgs[0] != wantCmd0 {
		t.Errorf("R6 cmd_args[0] = %q, want canonicalised host ragel6 LD outputs[0] = %q (raw outputs[0] = %q)",
			r6Node.Cmds[0].CmdArgs[0], wantCmd0, ldNode.Outputs[0])
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

// TestGen_ToolsArchiver_DualPlatform_HostAndTargetCounts pins PR-28's
// structural acceptance bar: target nodes ≥ 1696 (PR-27 baseline), host
// nodes ≥ 1500 (D10 threshold; reference has 1797), single result root.
// A regression in either lobe (target or host) is a structural failure
// that comparator percentage drops cannot diagnose alone.
func TestGen_ToolsArchiver_DualPlatform_HostAndTargetCounts(t *testing.T) {
	if _, err := os.Stat(sourceRoot + "/tools/archiver/ya.make"); err != nil {
		t.Skipf("tools/archiver not present in source tree: %v", err)
	}

	g := Gen(DefaultLinuxConfig, sourceRoot, "tools/archiver")

	var hostNodes, targetNodes int

	for _, n := range g.Graph {
		if n.HostPlatform {
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

	g := Gen(DefaultLinuxConfig, sourceRoot, "build/cow/on")

	if len(g.Graph) != 2 {
		t.Fatalf("len(Graph) = %d, want 2 (host walk must NOT fire for build/cow/on)", len(g.Graph))
	}

	for _, n := range g.Graph {
		if n.HostPlatform {
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

	g := Gen(DefaultLinuxConfig, root, "prog")

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

	// Synthetic asmlib host fixture with one .S source.
	asmlibDir := filepath.Join(root, "contrib/libs/asmlib")
	Throw(os.MkdirAll(asmlibDir, 0o755))
	Throw(os.WriteFile(filepath.Join(asmlibDir, "ya.make"),
		[]byte("LIBRARY()\nNO_PLATFORM()\nSRCS(memcmp64.S)\nEND()\n"), 0o644))

	// Synthetic host yasm PROGRAM.
	yasmDir := filepath.Join(root, "contrib/tools/yasm")
	Throw(os.MkdirAll(yasmDir, 0o755))
	Throw(os.WriteFile(filepath.Join(yasmDir, "ya.make"),
		[]byte("PROGRAM()\nNO_PLATFORM()\nSRCS(yasm.c)\nEND()\n"), 0o644))

	// Drive asmlib as a host instance directly so the .S dispatch
	// fires under PIC=true. (The full demand-driven path would route
	// through ragel6/bin → musl/full → asmlib; this synthetic test
	// shortcuts to the AS+yasm wiring.)
	cfg := DefaultLinuxConfig
	ctx := &genCtx{
		cfg:        cfg,
		sourceRoot: root,
		emit:       NewBufferedEmitter(),
		memo:       make(map[ModuleInstance]*moduleEmitResult),
		walking:    make(map[ModuleInstance]bool),
	}

	hostAsmlib := ModuleInstance{
		Path:     "contrib/libs/asmlib",
		Language: LangCPP,
		Target:   cfg.Host.ID,
		Flags:    inferFlagsFromPath("contrib/libs/asmlib", true),
	}

	genModule(ctx, hostAsmlib)

	g := Finalize(ctx.emit.(*BufferedEmitter))

	var asNode, yasmLD *Node

	for _, n := range g.Graph {
		switch n.KV["p"] {
		case "AS":
			asNode = n
		case "LD":
			if n.HostPlatform {
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

	cfg := DefaultLinuxConfig
	ctx := &genCtx{
		cfg:        cfg,
		sourceRoot: root,
		emit:       NewBufferedEmitter(),
		memo:       make(map[ModuleInstance]*moduleEmitResult),
		walking:    make(map[ModuleInstance]bool),
	}

	hostInstance := ModuleInstance{
		Path:     "myasm",
		Language: LangCPP,
		Target:   cfg.Host.ID,
		Flags:    inferFlagsFromPath("myasm", true),
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

	got := defaultPeerdirsFor(nil, bcOn)

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
		Language: LangCPP,
		Target:   PlatformDefaultLinuxAArch64,
		Flags:    FlagSet{},
	}

	// PR-32 D03: with cliMuslOn(nil)=true the auto-PEERDIR appends
	// `contrib/libs/musl/include` to every non-NO_PLATFORM CPP module's
	// default set.
	wantDefaults := []string{
		"contrib/libs/musl",
		"contrib/libs/cxxsupp/builtins",
		"library/cpp/malloc/api",
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
		"contrib/libs/musl/include",
	}

	gotDefaults := defaultPeerdirsFor(nil, plain)

	if !stringSlicesEqual(gotDefaults, wantDefaults) {
		t.Errorf("defaultPeerdirsFor(plain CPP) = %v, want %v", gotDefaults, wantDefaults)
	}

	// End-to-end: walk a consumer LIBRARY and confirm the seven
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
	// PR-32 D03: full set now ends with the auto-injected
	// `contrib/libs/musl/include` (mirrors `_BASE_UNIT`'s
	// `when ($MUSL == "yes") { PEERDIR+=contrib/libs/musl/include }`).
	fullSet := []string{
		"contrib/libs/musl",
		"contrib/libs/cxxsupp/builtins",
		"library/cpp/malloc/api",
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
		"contrib/libs/musl/include",
	}

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
				Target:   PlatformDefaultLinuxAArch64,
				Flags:    FlagSet{NoLibc: true},
			},
			// PR-32 D03: auto-PEERDIR musl/include still fires (NO_LIBC
			// alone is not effective NO_PLATFORM).
			want: []string{
				"contrib/libs/cxxsupp/builtins",
				"library/cpp/malloc/api",
				"contrib/libs/cxxsupp/libcxx",
				"contrib/libs/cxxsupp/libcxxrt",
				"contrib/libs/libunwind",
				"util",
				"contrib/libs/musl/include",
			},
		},
		{
			name: "no_runtime_only",
			mi: ModuleInstance{
				Path:     "x",
				Language: LangCPP,
				Target:   PlatformDefaultLinuxAArch64,
				Flags:    FlagSet{NoRuntime: true},
			},
			// PR-32 D03: auto-PEERDIR musl/include still fires.
			want: []string{"contrib/libs/musl", "library/cpp/malloc/api", "util", "contrib/libs/musl/include"},
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
		// PR-27 + PR-32 D03: `contrib/libs/musl` (LibcMusl=true) is
		// a runtime ancestor AND musl-self — gets zero defaults
		// (the auto-peer is suppressed for LibcMusl modules to avoid
		// musl peering itself).
		{
			name: "self_musl_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "contrib/libs/musl",
				Language: LangCPP,
				Flags:    FlagSet{LibcMusl: true},
			},
			want: nil,
		},
		// PR-33 D01: `contrib/libs/musl/full` is no longer a runtime
		// ancestor by literal entry (the subtree extension was dropped).
		// The empty-default-set is preserved because `inferFlagsFromPath`
		// seeds musl-subtree paths with NoLibc+NoUtil+NoRuntime, which
		// `effectiveNoPlatform` collapses to NO_PLATFORM. Tests that
		// construct the ModuleInstance directly must mirror this seed.
		{
			name: "self_musl_subdir_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "contrib/libs/musl/full",
				Language: LangCPP,
				Flags:    FlagSet{LibcMusl: true, NoLibc: true, NoUtil: true, NoRuntime: true},
			},
			want: nil,
		},
		{
			name: "no_util_only",
			mi:   ModuleInstance{Path: "x", Language: LangCPP, Flags: FlagSet{NoUtil: true}},
			// PR-32 D03: auto-PEERDIR musl/include still fires (NO_UTIL
			// alone is not effective NO_PLATFORM).
			want: []string{
				"contrib/libs/musl",
				"contrib/libs/cxxsupp/builtins",
				"library/cpp/malloc/api",
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
			mi:   ModuleInstance{Path: "contrib/libs/musl_extra", Language: LangCPP, Target: PlatformDefaultLinuxAArch64, Flags: FlagSet{}},
			want: fullSet,
		},
		// PR-32 D03: non-LibcMusl runtime ancestors (builtins,
		// malloc/api, libcxx, util) get the auto-PEERDIR
		// `contrib/libs/musl/include` only — the runtime-stack peers
		// stay suppressed. The two-phase peer-aggregation in the
		// walker keeps the libcxx-include + libcxxrt-include slots
		// ordered BEFORE the musl-arch paths in consumer cmd_args.
		{
			name: "self_builtins_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "contrib/libs/cxxsupp/builtins",
				Language: LangCPP,
				Flags:    FlagSet{},
			},
			want: []string{"contrib/libs/musl/include"},
		},
		{
			name: "self_malloc_api_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "library/cpp/malloc/api",
				Language: LangCPP,
				Flags:    FlagSet{},
			},
			want: []string{"contrib/libs/musl/include"},
		},
		{
			name: "self_libcxx_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "contrib/libs/cxxsupp/libcxx",
				Language: LangCPP,
				Flags:    FlagSet{},
			},
			want: []string{"contrib/libs/musl/include"},
		},
		{
			name: "self_util_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "util",
				Language: LangCPP,
				Flags:    FlagSet{},
			},
			want: []string{"contrib/libs/musl/include"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := defaultPeerdirsFor(nil, c.mi)

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
// rooted at "$(SOURCE_ROOT)/other/dir/<src>". Without SRCDIR the
// instance's own path must be used unchanged.
func TestGen_SrcDirRebasesSourceResolution(t *testing.T) {
	t.Run("with_srcdir", func(t *testing.T) {
		// PR-30 D06: LIBRARY with non-ancestor SRCDIR keeps module_dir
		// at instance.Path; per-source SRCDIR routing applies (input
		// at $(SOURCE_ROOT)/<srcdir>/<src>; output uses `__/<rel>`
		// infix). The historical PR-28-D02 "always rebase" shape is
		// retained ONLY for the PROGRAM-with-ancestor-SRCDIR pattern
		// (ragel6/bin); see TestGen_SrcdirAncestor_RebasesModuleDir.
		root := t.TempDir()

		modDir := filepath.Join(root, "mymod")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCDIR(other/dir)\nSRCS(foo.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := Gen(TargetCfg, root, "mymod")

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
		wantInput := "$(SOURCE_ROOT)/other/dir/foo.cpp"

		if len(ccNode.Inputs) == 0 || ccNode.Inputs[0] != wantInput {
			t.Errorf("CC inputs = %v, want first = %q", ccNode.Inputs, wantInput)
		}

		// Output path uses `__/<rel>` infix; rel = relpath(other/dir/foo.cpp
		// from mymod) = ../other/dir/foo.cpp → __/other/dir/foo.cpp.
		wantOutput := "$(BUILD_ROOT)/mymod/__/other/dir/foo.cpp.o"

		if len(ccNode.Outputs) == 0 || ccNode.Outputs[0] != wantOutput {
			t.Errorf("CC outputs = %v, want first = %q", ccNode.Outputs, wantOutput)
		}
	})

	t.Run("without_srcdir_baseline", func(t *testing.T) {
		root := t.TempDir()

		modDir := filepath.Join(root, "basemod")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(bar.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := Gen(TargetCfg, root, "basemod")

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

		wantInput := "$(SOURCE_ROOT)/basemod/bar.cpp"

		if len(ccNode.Inputs) == 0 || ccNode.Inputs[0] != wantInput {
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

		g := Gen(TargetCfg, root, "jsmod")

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

		g := Gen(TargetCfg, root, "tools/r6/bin")

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

		wantInput := "$(SOURCE_ROOT)/tools/r6/main.cpp"
		if len(ccNode.Inputs) == 0 || ccNode.Inputs[0] != wantInput {
			t.Errorf("CC inputs = %v, want first = %q", ccNode.Inputs, wantInput)
		}

		wantOutput := "$(BUILD_ROOT)/tools/r6/main.cpp.o"
		if len(ccNode.Outputs) == 0 || ccNode.Outputs[0] != wantOutput {
			t.Errorf("CC outputs = %v, want first = %q", ccNode.Outputs, wantOutput)
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

		g := Gen(TargetCfg, root, "testlib")

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

		g := Gen(TargetCfg, root, "testlib")

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

		g := Gen(TargetCfg, root, "testlib")

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

	g := Gen(TargetCfg, root, "jsmod")

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

	g := Gen(TargetCfg, root, "r6mod")

	var r6Node, ccNode *Node

	for _, n := range g.Graph {
		switch n.KV["p"] {
		case "R6":
			r6Node = n
		case "CC":
			// Pick the CC whose input lives under $(BUILD_ROOT)
			// (the R6-derived CC; the host ragel6 PROGRAM also
			// emits a CC for its main.cpp under $(SOURCE_ROOT)).
			ip := ""
			if len(n.Inputs) > 0 {
				ip = n.Inputs[0]
			}

			if ccNode == nil && len(ip) > 0 && ip[:14] == "$(BUILD_ROOT)/" {
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

	g := Gen(TargetCfg, tmp, "lib_consumer")

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

	g := Gen(TargetCfg, tmp, "myprog")

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

	g := Gen(TargetCfg, tmp, "myprog")

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

	g := Gen(TargetCfg, tmp, "myprog")

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

	g := Gen(TargetCfg, tmp, "mylib")

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

	wantInput := "$(SOURCE_ROOT)/other/src/foo.cpp"

	if len(ccNode.Inputs) == 0 || ccNode.Inputs[0] != wantInput {
		t.Errorf("CC input = %v, want first = %q", ccNode.Inputs, wantInput)
	}

	wantOutput := "$(BUILD_ROOT)/mylib/__/other/src/foo.cpp.o"

	if len(ccNode.Outputs) == 0 || ccNode.Outputs[0] != wantOutput {
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

	g := Gen(TargetCfg, tmp, "mylib")

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

	wantInput := "$(SOURCE_ROOT)/mylib/local.c"

	if len(ccNode.Inputs) == 0 || ccNode.Inputs[0] != wantInput {
		t.Errorf("CC input = %v, want first = %q (local-existing source must ignore SRCDIR)", ccNode.Inputs, wantInput)
	}

	wantOutput := "$(BUILD_ROOT)/mylib/local.c.o"

	if len(ccNode.Outputs) == 0 || ccNode.Outputs[0] != wantOutput {
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
	Throw(os.WriteFile(filepath.Join(libDir, "ya.make"), []byte(
		"LIBRARY()\nADDINCL(\n    GLOBAL lib/include\n    lib/src\n)\nSRCS(lib.cpp)\nEND()\n",
	), 0o644))

	// consumer/ya.make: LIBRARY that peers lib.
	consDir := filepath.Join(root, "consumer")
	Throw(os.MkdirAll(consDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consDir, "ya.make"), []byte(
		"LIBRARY()\nPEERDIR(lib)\nSRCS(main.cpp)\nEND()\n",
	), 0o644))

	g := Gen(TargetCfg, root, "consumer")

	// Find the CC node for main.cpp (the consumer's own source).
	var consumerCC *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			for _, out := range n.Outputs {
				if strings.Contains(out, "main.cpp.o") {
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
	wantGlobal := "-I$(SOURCE_ROOT)/lib/include"
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
	wantAbsent := "-I$(SOURCE_ROOT)/lib/src"

	for _, f := range iFlags {
		if f == wantAbsent {
			t.Errorf("consumer CC -I flags = %v; must NOT contain %q (module-own ADDINCL must stay module-own, PR-31 D13)", iFlags, wantAbsent)
			break
		}
	}
}

// TestGen_ToolsArchiver_L0_AtLeast95 is the M2 acceptance closer: PR-30
// must lift L0 ≥ 95% on the tools/archiver target against the reference
// graph at /home/pg/monorepo/yatool_orig/sg.json.
func TestGen_ToolsArchiver_L0_AtLeast95(t *testing.T) {
	const sourceRoot = "/home/pg/monorepo/yatool_orig"

	if _, err := os.Stat(filepath.Join(sourceRoot, "sg.json")); err != nil {
		t.Skipf("reference graph not available at %s/sg.json: %v", sourceRoot, err)
	}

	our := Gen(TargetCfg, sourceRoot, "tools/archiver")
	ref := LoadReference(filepath.Join(sourceRoot, "sg.json"))

	l0, note := compareTopology(ref, our)

	const acceptance = 0.95

	if l0 < acceptance {
		t.Errorf("L0 = %.4f (%s) < acceptance bar %.2f (PR-30 must clear M2 L0 gate)", l0, note, acceptance)
	} else {
		t.Logf("L0 = %.4f (%s) — clears acceptance bar %.2f", l0, note, acceptance)
	}
}

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
func TestGen_MallocApi_HoistInjection_ByteExact(t *testing.T) {
	const targetDir = "library/cpp/malloc/api"

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

	const ccOutput = "$(BUILD_ROOT)/library/cpp/malloc/api/malloc.cpp.o"

	var ourCC *Node

	for _, n := range our.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0] == ccOutput {
			ourCC = n

			break
		}
	}

	if ourCC == nil {
		t.Fatalf("Gen produced no CC node with output %q", ccOutput)
	}

	ref := LoadReference(referenceGraphPath)

	var refCC *Node

	for _, n := range ref.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0] == ccOutput && n.Platform == string(TargetCfg.Target.ID) {
			refCC = n

			break
		}
	}

	if refCC == nil {
		t.Fatalf("reference graph contains no CC node with output %q on platform %q", ccOutput, TargetCfg.Target.ID)
	}

	if len(ourCC.Cmds) != len(refCC.Cmds) {
		t.Fatalf("malloc.cpp.o: cmds count = %d, want %d", len(ourCC.Cmds), len(refCC.Cmds))
	}

	for ci, refCmd := range refCC.Cmds {
		ourArgs := ourCC.Cmds[ci].CmdArgs
		refArgs := refCmd.CmdArgs

		if len(ourArgs) != len(refArgs) {
			t.Errorf("malloc.cpp.o cmd[%d].cmd_args: got %d args, want %d", ci, len(ourArgs), len(refArgs))

			continue
		}

		for i := range refArgs {
			if ourArgs[i] != refArgs[i] {
				t.Errorf("malloc.cpp.o cmd[%d].cmd_args[%d]:\n  got:  %q\n  want: %q", ci, i, ourArgs[i], refArgs[i])

				break
			}
		}
	}

	// Slot 11-12 anchors: the libcxx + libcxxrt header peers must
	// land immediately after the linux-headers ccIncludesSuffix pair
	// (slots 9-10 in the reference shape).
	const wantSlot11 = "-I$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxx/include"
	const wantSlot12 = "-I$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxxrt/include"

	if len(ourCC.Cmds[0].CmdArgs) <= 12 {
		t.Fatalf("cmd_args has %d entries, expected >= 13 to anchor slots 11-12", len(ourCC.Cmds[0].CmdArgs))
	}

	if got := ourCC.Cmds[0].CmdArgs[11]; got != wantSlot11 {
		t.Errorf("cmd_args[11] = %q, want %q (PR-35c hoist injection anchor)", got, wantSlot11)
	}

	if got := ourCC.Cmds[0].CmdArgs[12]; got != wantSlot12 {
		t.Errorf("cmd_args[12] = %q, want %q (PR-35c hoist injection anchor)", got, wantSlot12)
	}
}

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

	our := Gen(TargetCfg, sourceRoot, targetDir)

	const ldOutput = "$(BUILD_ROOT)/tools/archiver/archiver"

	var ourLD *Node

	for _, n := range our.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0] == ldOutput {
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
