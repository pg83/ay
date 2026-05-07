package main

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// compare_props_test.go — exercises the L1 + L2 per-pair comparator.
//
// Pairing helper, then one test per "this property differs → this
// level drops" case, then the load-bearing real-graph self-match.
//
// Test graphs are built by hand (not via the BufferedEmitter) so we
// can pin Outputs / Platform / KV / TargetProperties / Inputs / Tags /
// Requirements precisely without going through Finalize's Merkle
// hashing pass — these L1/L2 tests are about per-field equality, not
// UID computation.

// mkNode is a small helper that fills every required field with sane
// zero values. The caller overrides whatever the test wants to vary.
// Outputs default to a single-element slice so pairing succeeds; the
// caller passing []string{} disables pairing for that node.
func mkNode(uid, output, platform, kvP string) *Node {
	outputs := []string{}
	if output != "" {
		outputs = []string{output}
	}

	return &Node{
		UID:              uid,
		SelfUID:          uid,
		Cmds:             []Cmd{},
		Deps:             []string{},
		Env:              map[string]string{},
		Inputs:           []string{},
		KV:               map[string]string{"p": kvP},
		Outputs:          outputs,
		Platform:         platform,
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
	}
}

// mk2NodeGraph constructs a graph with two leaf nodes A and B (no
// edges between them — L1/L2 do not depend on topology so we keep
// the fixtures minimal). Returns the graph and the two nodes so the
// test can mutate fields on them in-place before calling Compare.
func mk2NodeGraph() (*Graph, *Node, *Node) {
	a := mkNode("uid-A", "out/a.o", "linux", "CC")
	b := mkNode("uid-B", "out/b.o", "linux", "CC")

	g := &Graph{
		Conf:   map[string]interface{}{},
		Inputs: map[string]interface{}{},
		Graph:  []*Node{a, b},
		Result: []string{a.UID},
	}

	return g, a, b
}

// cloneGraph deep-copies enough of g so the caller can mutate one
// side without affecting the other. We share Conf/Inputs (top-level
// opaque maps the comparator does not look at) but copy every node
// and every per-node slice/map.
func cloneGraph(g *Graph) *Graph {
	out := &Graph{
		Conf:   g.Conf,
		Inputs: g.Inputs,
		Graph:  make([]*Node, 0, len(g.Graph)),
		Result: append([]string{}, g.Result...),
	}

	for _, n := range g.Graph {
		out.Graph = append(out.Graph, cloneNode(n))
	}

	return out
}

func cloneNode(n *Node) *Node {
	cp := *n

	cp.Cmds = append([]Cmd{}, n.Cmds...)
	cp.Deps = append([]string{}, n.Deps...)
	cp.Inputs = append([]string{}, n.Inputs...)
	cp.Outputs = append([]string{}, n.Outputs...)
	cp.Tags = append([]string{}, n.Tags...)

	cp.Env = copyStringMap(n.Env)
	cp.KV = copyStringMap(n.KV)
	cp.TargetProperties = copyStringMap(n.TargetProperties)

	cp.Requirements = make(map[string]interface{}, len(n.Requirements))
	for k, v := range n.Requirements {
		cp.Requirements[k] = v
	}

	return &cp
}

func copyStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}

	return out
}

func TestCompareL1L2_IdentityIsPerfect(t *testing.T) {
	g, _, _ := mk2NodeGraph()
	r := Compare(g, g, 2)

	if r.L0 != 1.0 {
		t.Errorf("identity L0 = %v, want 1.0 (note: %q)", r.L0, r.L0Note)
	}

	if r.L1 != 1.0 {
		t.Errorf("identity L1 = %v, want 1.0 (note: %q)", r.L1, r.L1Note)
	}

	if r.L2 != 1.0 {
		t.Errorf("identity L2 = %v, want 1.0 (note: %q)", r.L2, r.L2Note)
	}

	if !strings.Contains(r.L1Note, "matched") {
		t.Errorf("L1Note missing 'matched': %q", r.L1Note)
	}

	if !strings.Contains(r.L2Note, "matched") {
		t.Errorf("L2Note missing 'matched': %q", r.L2Note)
	}

	if len(r.Skipped) != 0 {
		t.Errorf("Compare(g, g, 2).Skipped = %v, want empty", r.Skipped)
	}
}

func TestCompareL1_DifferentOutputsBreaksPairing(t *testing.T) {
	// L0 looks at the fingerprint, which encodes (kv.p, child fingerprints).
	// Outputs are NOT in the fingerprint. So flipping Outputs[0] on the
	// `got` side keeps L0 == 1.0 (the multiset of fingerprints is
	// unchanged) but breaks the (outputs[0], platform) pairing for that
	// node, dropping L1 below 1.0.
	want, _, _ := mk2NodeGraph()
	got := cloneGraph(want)

	got.Graph[1].Outputs[0] = "out/b-renamed.o"

	r := Compare(want, got, 2)

	if r.L0 != 1.0 {
		t.Errorf("flipped-outputs L0 = %v, want 1.0 (outputs are not in the fingerprint; note: %q)", r.L0, r.L0Note)
	}

	if r.L1 >= 1.0 {
		t.Errorf("flipped-outputs L1 = %v, want < 1.0 (note: %q)", r.L1, r.L1Note)
	}
}

func TestCompareL1_DifferentTargetPropertiesDropsBelow1(t *testing.T) {
	// Same shape, same kv.p, same outputs (so pairing works), but
	// target_properties differs on one node — L0 stays 1.0, L1 drops.
	want, _, _ := mk2NodeGraph()
	got := cloneGraph(want)

	got.Graph[0].TargetProperties["module_dir"] = "different/dir"

	r := Compare(want, got, 2)

	if r.L0 != 1.0 {
		t.Errorf("flipped-tp L0 = %v, want 1.0 (target_properties is not in the fingerprint; note: %q)", r.L0, r.L0Note)
	}

	if r.L1 >= 1.0 {
		t.Errorf("flipped-tp L1 = %v, want < 1.0 (note: %q)", r.L1, r.L1Note)
	}

	if r.L2 != 1.0 {
		t.Errorf("flipped-tp L2 = %v, want 1.0 (target_properties is L1-only; note: %q)", r.L2, r.L2Note)
	}
}

func TestCompareL2_DifferentInputsDropsBelow1(t *testing.T) {
	// Same outputs (pairing works), same kv.p / target_properties (L1
	// matches), but inputs differ — only L2 drops.
	want, _, _ := mk2NodeGraph()
	got := cloneGraph(want)

	got.Graph[0].Inputs = []string{"new-input.h"}

	r := Compare(want, got, 2)

	if r.L0 != 1.0 {
		t.Errorf("flipped-inputs L0 = %v, want 1.0 (inputs is not in the fingerprint; note: %q)", r.L0, r.L0Note)
	}

	if r.L1 != 1.0 {
		t.Errorf("flipped-inputs L1 = %v, want 1.0 (inputs is L2-only; note: %q)", r.L1, r.L1Note)
	}

	if r.L2 >= 1.0 {
		t.Errorf("flipped-inputs L2 = %v, want < 1.0 (note: %q)", r.L2, r.L2Note)
	}
}

func TestCompareL2_DifferentTagsDropsBelow1(t *testing.T) {
	want, _, _ := mk2NodeGraph()
	got := cloneGraph(want)

	got.Graph[0].Tags = []string{"new-tag"}

	r := Compare(want, got, 2)

	if r.L0 != 1.0 {
		t.Errorf("flipped-tags L0 = %v, want 1.0 (tags is not in the fingerprint; note: %q)", r.L0, r.L0Note)
	}

	if r.L1 != 1.0 {
		t.Errorf("flipped-tags L1 = %v, want 1.0 (tags is L2-only; note: %q)", r.L1, r.L1Note)
	}

	if r.L2 >= 1.0 {
		t.Errorf("flipped-tags L2 = %v, want < 1.0 (note: %q)", r.L2, r.L2Note)
	}
}

func TestPairByOutput_UnmatchedReportedSeparately(t *testing.T) {
	// Graph A has nodes producing x and y; graph B has y and z. Pairs
	// should contain just the y-y entry; wantOnly should report the x
	// node's UID; gotOnly should report the z node's UID.
	wantA := mkNode("uid-x", "x", "linux", "CC")
	wantB := mkNode("uid-y", "y", "linux", "CC")
	want := &Graph{
		Conf:   map[string]interface{}{},
		Inputs: map[string]interface{}{},
		Graph:  []*Node{wantA, wantB},
		Result: []string{"uid-x"},
	}

	gotA := mkNode("uid-y2", "y", "linux", "CC")
	gotB := mkNode("uid-z", "z", "linux", "CC")
	got := &Graph{
		Conf:   map[string]interface{}{},
		Inputs: map[string]interface{}{},
		Graph:  []*Node{gotA, gotB},
		Result: []string{"uid-y2"},
	}

	pairs, wantOnly, gotOnly := pairByOutput(want, got)

	if len(pairs) != 1 {
		t.Fatalf("pairs has %d entries, want 1: %v", len(pairs), pairs)
	}

	gotPairUID, ok := pairs["uid-y"]
	if !ok {
		t.Fatalf("expected pairs[%q], got %v", "uid-y", pairs)
	}

	if gotPairUID != "uid-y2" {
		t.Errorf("pairs[%q] = %q, want %q", "uid-y", gotPairUID, "uid-y2")
	}

	sort.Strings(wantOnly)
	if len(wantOnly) != 1 || wantOnly[0] != "uid-x" {
		t.Errorf("wantOnly = %v, want [uid-x]", wantOnly)
	}

	sort.Strings(gotOnly)
	if len(gotOnly) != 1 || gotOnly[0] != "uid-z" {
		t.Errorf("gotOnly = %v, want [uid-z]", gotOnly)
	}
}

func TestPairByOutput_EmptyOutputsLandsInOnlySlice(t *testing.T) {
	// A node with empty Outputs cannot be paired — confirm it shows
	// up in wantOnly / gotOnly rather than disappearing or panicking.
	wantA := mkNode("uid-A", "out/a.o", "linux", "CC")
	wantB := mkNode("uid-no-output", "", "linux", "CC")
	want := &Graph{
		Conf:   map[string]interface{}{},
		Inputs: map[string]interface{}{},
		Graph:  []*Node{wantA, wantB},
		Result: []string{"uid-A"},
	}

	gotA := mkNode("uid-A2", "out/a.o", "linux", "CC")
	got := &Graph{
		Conf:   map[string]interface{}{},
		Inputs: map[string]interface{}{},
		Graph:  []*Node{gotA},
		Result: []string{"uid-A2"},
	}

	pairs, wantOnly, _ := pairByOutput(want, got)

	if len(pairs) != 1 || pairs["uid-A"] != "uid-A2" {
		t.Errorf("pairs = %v, want {uid-A: uid-A2}", pairs)
	}

	if len(wantOnly) != 1 || wantOnly[0] != "uid-no-output" {
		t.Errorf("wantOnly = %v, want [uid-no-output]", wantOnly)
	}
}

func TestPairByOutput_DuplicateKeyThrows(t *testing.T) {
	// Two nodes with identical (outputs[0], platform) on the same side
	// is an internal-error condition — the comparator cannot decide
	// which one to pair against. We surface a throw rather than
	// silently keeping the last one.
	a := mkNode("uid-A", "out/x.o", "linux", "CC")
	b := mkNode("uid-B", "out/x.o", "linux", "AR")
	g := &Graph{
		Conf:   map[string]interface{}{},
		Inputs: map[string]interface{}{},
		Graph:  []*Node{a, b},
		Result: []string{"uid-A"},
	}

	exc := Try(func() {
		pairByOutput(g, g)
	})

	if exc == nil {
		t.Fatal("pairByOutput on duplicate-key input returned no exception")
	}

	if !strings.Contains(exc.Error(), "duplicate") {
		t.Errorf("exception %q does not mention 'duplicate'", exc.Error())
	}
}

func TestCompareL1L2_RealGraphSelfMatch(t *testing.T) {
	// Load-bearing acceptance: comparing the real reference graph to
	// itself MUST report L0 == L1 == L2 == 1.0 exactly. If any of these
	// drops below 1.0 the comparator is broken for the only "trusted"
	// input we have.
	if _, err := os.Stat(referenceGraphPath); err != nil {
		t.Skipf("reference graph %s not present: %v", referenceGraphPath, err)
	}

	g := LoadReference(referenceGraphPath)
	r := Compare(g, g, 2)

	if r.L0 != 1.0 {
		t.Errorf("real-graph self-match L0 = %v, want 1.0 (note: %q)", r.L0, r.L0Note)
	}

	if r.L1 != 1.0 {
		t.Errorf("real-graph self-match L1 = %v, want 1.0 (note: %q)", r.L1, r.L1Note)
	}

	if r.L2 != 1.0 {
		t.Errorf("real-graph self-match L2 = %v, want 1.0 (note: %q)", r.L2, r.L2Note)
	}
}

func TestCompareL1L2_LevelGating(t *testing.T) {
	// --level=0 should leave L1/L2 untouched (zero values, empty notes)
	// and list 1+2 in Skipped. --level=1 should compute L1, leave L2
	// untouched, list only L2 in... wait, L2 is implemented now — so
	// --level=1 should compute L0+L1 only and Skipped should be empty
	// (no UNimplemented levels were requested). The "skipped" slice
	// only tracks levels above highestImplementedLevel.
	g, _, _ := mk2NodeGraph()

	r0 := Compare(g, g, 0)
	if r0.L1 != 0 || r0.L1Note != "" {
		t.Errorf("Compare(_,_,0) populated L1 (=%v, note=%q); should be zero/empty", r0.L1, r0.L1Note)
	}

	if r0.L2 != 0 || r0.L2Note != "" {
		t.Errorf("Compare(_,_,0) populated L2 (=%v, note=%q); should be zero/empty", r0.L2, r0.L2Note)
	}

	r1 := Compare(g, g, 1)
	if r1.L1 != 1.0 {
		t.Errorf("Compare(_,_,1) L1 = %v, want 1.0", r1.L1)
	}

	if r1.L2 != 0 || r1.L2Note != "" {
		t.Errorf("Compare(_,_,1) populated L2 (=%v, note=%q); should be zero/empty", r1.L2, r1.L2Note)
	}

	// --level=3 requests one unimplemented level: 3.
	r3 := Compare(g, g, 3)
	if len(r3.Skipped) != 1 || r3.Skipped[0] != 3 {
		t.Errorf("Compare(_,_,3).Skipped = %v, want [3]", r3.Skipped)
	}
}
