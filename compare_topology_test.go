package main

import (
	"os"
	"strings"
	"testing"
)

// compare_topology_test.go — exercises the L0 fingerprint comparator.
//
// Each test builds a tiny DAG via BufferedEmitter+Finalize (the only
// way to get a real graph with deterministic UIDs), then runs Compare
// and asserts on the report. The "real graph self-match" test is the
// load-bearing acceptance signal: comparing the on-disk reference
// graph to itself MUST report L0=1.0 exactly.

// build3NodeDAGGraph wraps build3NodeDAG (defined in emitter_test.go)
// and finalises the emitter. Returns the *Graph for direct use by
// Compare.
func build3NodeDAGGraph() *Graph {
	e, _, _, _ := build3NodeDAG()

	return Finalize(e)
}

func TestCompareL0_IdentityIsPerfect(t *testing.T) {
	g := build3NodeDAGGraph()
	r := Compare(g, g, 0)

	if r.L0 != 1.0 {
		t.Errorf("Compare(g, g).L0 = %v, want 1.0", r.L0)
	}

	if !strings.Contains(r.L0Note, "3 of 3") {
		t.Errorf("Compare(g, g).L0Note = %q, want substring %q", r.L0Note, "3 of 3")
	}

	if len(r.Skipped) != 0 {
		t.Errorf("Compare(g, g, 0).Skipped = %v, want empty", r.Skipped)
	}
}

func TestCompareL0_RenumberedUIDsStillMatch(t *testing.T) {
	// The core property of the comparator: rename every UID in `got`
	// to a different (deterministic) string and L0 must stay 1.0.
	// This proves the fingerprint algorithm depends on shape + kv.p,
	// not on the raw UID strings.
	want := build3NodeDAGGraph()
	got := build3NodeDAGGraph()

	rename := func(uid string) string {
		// Stable invertible renumbering: prepend a tag. As long as it
		// is bijective, every dep reference stays consistent.
		return "X-" + uid
	}

	for _, n := range got.Graph {
		n.UID = rename(n.UID)
		n.SelfUID = rename(n.SelfUID)

		for i, d := range n.Deps {
			n.Deps[i] = rename(d)
		}

		for k, vals := range n.ForeignDeps {

			for i, d := range vals {
				vals[i] = rename(d)
			}
			n.ForeignDeps[k] = vals
		}
	}

	for i, u := range got.Result {
		got.Result[i] = rename(u)
	}

	if want.Graph[0].UID == got.Graph[0].UID {
		t.Fatalf("test setup broken: rename did not change UIDs (want.Graph[0].UID = %q)", want.Graph[0].UID)
	}

	if !strings.HasPrefix(got.Graph[0].UID, "X-") {
		t.Fatalf("test setup broken: rename did not produce X- prefix (got %q)", got.Graph[0].UID)
	}

	r := Compare(want, got, 0)
	if r.L0 != 1.0 {
		t.Errorf("renumbered-UID L0 = %v, want 1.0 (note: %q)", r.L0, r.L0Note)
	}
}

func TestCompareL0_AddedNodeDropsBelow1(t *testing.T) {
	want := build3NodeDAGGraph()

	// Build a 4-node graph: same as the 3-node base plus a fresh leaf
	// hanging off A. The new leaf's fingerprint will not match any in
	// `want`, and A's fingerprint changes too (its child set changed),
	// so the multiset diverges by at least 2 entries on the larger
	// side.
	e, _, _, c := build3NodeDAG()
	d := e.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: []string{"build", "D"}, Env: map[string]string{}}},
		Env:              map[string]string{},
		Inputs:           []string{"d.in"},
		KV:               map[string]string{"name": "D", "p": "CC"},
		Outputs:          []string{"d.out"},
		Platform:         "linux",
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
		DepRefs:          []NodeRef{c},
	})
	_ = d
	got := Finalize(e)

	if len(got.Graph) <= len(want.Graph) {
		t.Fatalf("test setup: got graph not larger than want (got=%d want=%d)", len(got.Graph), len(want.Graph))
	}

	r := Compare(want, got, 0)
	if r.L0 >= 1.0 {
		t.Errorf("added-node L0 = %v, want < 1.0 (note: %q)", r.L0, r.L0Note)
	}

	denom := len(got.Graph)

	expectedNote := "of " + itoa(denom) + " fingerprints matched"
	if !strings.Contains(r.L0Note, expectedNote) {
		t.Errorf("L0Note = %q, want substring %q", r.L0Note, expectedNote)
	}
}

func TestCompareL0_DifferentKVPDropsBelow1(t *testing.T) {
	// Same 3-node shape but flip one node's kv.p — a single fingerprint
	// (and any of its ancestors') should diverge, dropping L0 below 1.
	mk := func(pVal string) *Graph {
		e := NewBufferedEmitter()
		c := e.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: []string{"build", "C"}, Env: map[string]string{}}},
			Env:              map[string]string{},
			Inputs:           []string{"c.in"},
			KV:               map[string]string{"name": "C", "p": pVal},
			Outputs:          []string{"c.out"},
			Platform:         "linux",
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
		})
		b := e.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: []string{"build", "B"}, Env: map[string]string{}}},
			Env:              map[string]string{},
			Inputs:           []string{"b.in"},
			KV:               map[string]string{"name": "B", "p": "AR"},
			Outputs:          []string{"b.out"},
			Platform:         "linux",
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
			DepRefs:          []NodeRef{c},
		})
		a := e.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: []string{"build", "A"}, Env: map[string]string{}}},
			Env:              map[string]string{},
			Inputs:           []string{"a.in"},
			KV:               map[string]string{"name": "A", "p": "LD"},
			Outputs:          []string{"a.out"},
			Platform:         "linux",
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
			DepRefs:          []NodeRef{b, c},
		})
		e.Result(a)

		return Finalize(e)
	}

	want := mk("CC")
	got := mk("AR")

	r := Compare(want, got, 0)
	if r.L0 >= 1.0 {
		t.Errorf("flipped-kvp L0 = %v, want < 1.0 (note: %q)", r.L0, r.L0Note)
	}
}

func TestCompareL0_RealGraphSelfMatch(t *testing.T) {
	// The load-bearing acceptance: real graph compared to itself MUST
	// be L0=100%. If this ever drops below 1.0, the comparator is
	// broken regardless of how clever it looks on synthetic inputs.
	if _, err := os.Stat(referenceGraphPath); err != nil {
		t.Skipf("reference graph %s not present: %v", referenceGraphPath, err)
	}

	g := LoadReference(referenceGraphPath)
	r := Compare(g, g, 0)

	if r.L0 != 1.0 {
		t.Errorf("real-graph self-match L0 = %v, want 1.0 exactly (note: %q)", r.L0, r.L0Note)
	}
}

func TestCompareL0_CycleInInputThrows(t *testing.T) {
	// Synthetic cyclic graph — built by hand because Finalize rejects
	// cycles and we need the Compare path to surface the error itself.
	// Two nodes that depend on each other.
	g := &Graph{
		Conf:   map[string]interface{}{},
		Inputs: map[string]interface{}{},
		Graph: []*Node{
			{
				UID:              "A",
				SelfUID:          "A",
				Cmds:             []Cmd{},
				Deps:             []string{"B"},
				Env:              map[string]string{},
				Inputs:           []string{},
				KV:               map[string]string{"p": "CC"},
				Outputs:          []string{},
				Requirements:     map[string]interface{}{},
				Tags:             []string{},
				TargetProperties: map[string]string{},
			},
			{
				UID:              "B",
				SelfUID:          "B",
				Cmds:             []Cmd{},
				Deps:             []string{"A"},
				Env:              map[string]string{},
				Inputs:           []string{},
				KV:               map[string]string{"p": "CC"},
				Outputs:          []string{},
				Requirements:     map[string]interface{}{},
				Tags:             []string{},
				TargetProperties: map[string]string{},
			},
		},
		Result: []string{"A"},
	}

	exc := Try(func() {
		Compare(g, g, 0)
	})

	if exc == nil {
		t.Fatal("Compare on cyclic input returned no exception")
	}

	if !strings.Contains(exc.Error(), "cycle") {
		t.Errorf("exception %q does not mention 'cycle'", exc.Error())
	}
}

// itoa is a tiny local helper so the test file doesn't pull strconv
// just for one call site (and so the assertion stays inline-readable).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	neg := n < 0
	if neg {
		n = -n
	}

	var buf [20]byte
	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	if neg {
		i--
		buf[i] = '-'
	}

	return string(buf[i:])
}
