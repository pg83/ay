package main

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// emitter_test.go — exercises BufferedEmitter + Finalize end-to-end.

// build3NodeDAG constructs A -> {B, C}, B -> C. C is a leaf, B depends
// on C, A depends on both B and C. Returns the emitter and the refs in
// (A, B, C) order so callers can assert against any of them.
func build3NodeDAG() (*BufferedEmitter, NodeRef, NodeRef, NodeRef) {
	e := NewBufferedEmitter()
	c := e.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: []string{"build", "C"}, Env: map[string]string{}}},
		Env:              map[string]string{},
		Inputs:           []string{"c.in"},
		KV:               map[string]string{"name": "C"},
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
		KV:               map[string]string{"name": "B"},
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
		KV:               map[string]string{"name": "A"},
		Outputs:          []string{"a.out"},
		Platform:         "linux",
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
		DepRefs:          []NodeRef{b, c},
	})
	e.Result(a)

	return e, a, b, c
}

func nodeNameByKV(g *Graph, idx int) string {
	return g.Graph[idx].KV["name"]
}

// finalizeExc is a small helper that runs Finalize inside Try and
// returns (graph, exception). Used by tests that need to inspect a
// finalize-time error message; tests that expect success can call
// Finalize directly.
//
// Success-path tests should call Finalize(e) directly so an unexpected
// panic surfaces as a test failure rather than being silently captured by Try.
func finalizeExc(e *BufferedEmitter) (g *Graph, exc *Exception) {
	exc = Try(func() {
		g = Finalize(e)
	})

	return g, exc
}

func TestFinalize_TopoOrder_LeavesFirst(t *testing.T) {
	e, _, _, _ := build3NodeDAG()
	g := Finalize(e)

	if len(g.Graph) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(g.Graph))
	}

	want := []string{"C", "B", "A"}
	for i, w := range want {
		got := nodeNameByKV(g, i)
		if got != w {
			t.Errorf("topo[%d] = %q, want %q (full order: %v)", i, got, w,
				[]string{nodeNameByKV(g, 0), nodeNameByKV(g, 1), nodeNameByKV(g, 2)})
		}
	}
}

func TestFinalize_UIDsStableAcrossRuns(t *testing.T) {
	e1, _, _, _ := build3NodeDAG()
	g1 := Finalize(e1)
	raw1 := Throw2(json.Marshal(g1))

	e2, _, _, _ := build3NodeDAG()
	g2 := Finalize(e2)
	raw2 := Throw2(json.Marshal(g2))

	if !bytes.Equal(raw1, raw2) {
		t.Errorf("Finalize output not byte-stable across runs.\nrun1: %s\nrun2: %s", raw1, raw2)
	}

	// And every UID is content-derived (length 22).
	for i, n := range g1.Graph {
		if len(n.UID) != 22 {
			t.Errorf("graph[%d].UID len = %d, want 22 (got %q)", i, len(n.UID), n.UID)
		}

		if n.SelfUID != n.UID {
			t.Logf("PR-02 placeholder: SelfUID currently equals UID; future PR will compute distinct value. graph[%d].SelfUID=%q UID=%q", i, n.SelfUID, n.UID)
		}

		if n.StatsUID != "" {
			t.Errorf("graph[%d].StatsUID = %q, want \"\" (PR-02 placeholder)", i, n.StatsUID)
		}
	}

	// Result is a single UID matching A (the last topo entry).
	if len(g1.Result) != 1 {
		t.Fatalf("expected 1 result, got %d (%v)", len(g1.Result), g1.Result)
	}

	if g1.Result[0] != g1.Graph[2].UID {
		t.Errorf("result[0] = %q, want graph[2].UID %q", g1.Result[0], g1.Graph[2].UID)
	}
}

func TestFinalize_DepsAreSortedAlphabetically(t *testing.T) {
	// Build A with two children whose UIDs we don't know up-front.
	// Whatever the UIDs are, A.Deps must come out sorted.
	e := NewBufferedEmitter()
	mkLeaf := func(name string) NodeRef {
		return e.Emit(&Node{
			Cmds:   []Cmd{{CmdArgs: []string{name}, Env: map[string]string{}}},
			Env:    map[string]string{},
			Inputs: []string{}, KV: map[string]string{"name": name},
			Outputs:      []string{},
			Requirements: map[string]interface{}{}, Tags: []string{},
			TargetProperties: map[string]string{},
		})
	}
	x := mkLeaf("X")
	y := mkLeaf("Y")
	z := mkLeaf("Z")

	// Emit child refs in a deliberately unsorted-by-UID order. After
	// Finalize the published Deps slice must be alphabetical.
	a := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{},
		KV: map[string]string{"name": "A"}, Outputs: []string{},
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
		DepRefs:          []NodeRef{z, x, y},
	})
	e.Result(a)
	g := Finalize(e)

	// Find A in the graph.
	var aNode *Node
	for _, n := range g.Graph {
		if n.KV["name"] == "A" {
			aNode = n

			break
		}
	}

	if aNode == nil {
		t.Fatalf("A not found in graph")
	}

	if len(aNode.Deps) != 3 {
		t.Fatalf("A.Deps len = %d, want 3 (%v)", len(aNode.Deps), aNode.Deps)
	}

	if !sort.StringsAreSorted(aNode.Deps) {
		t.Errorf("A.Deps not sorted: %v", aNode.Deps)
	}
}

func TestFinalize_DedupesDuplicateDeps(t *testing.T) {
	e := NewBufferedEmitter()
	c := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"C"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{},
		KV: map[string]string{"name": "C"}, Outputs: []string{},
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
	})
	a := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{},
		KV: map[string]string{"name": "A"}, Outputs: []string{},
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
		DepRefs:          []NodeRef{c, c, c}, // intentional duplicates
	})
	e.Result(a)
	g := Finalize(e)

	var aNode *Node
	for _, n := range g.Graph {
		if n.KV["name"] == "A" {
			aNode = n
		}
	}

	if aNode == nil {
		t.Fatalf("A not found")
	}

	if len(aNode.Deps) != 1 {
		t.Errorf("expected duplicates collapsed; A.Deps = %v", aNode.Deps)
	}
}

func TestFinalize_CycleReturnsError(t *testing.T) {
	// A <-> B cycle. We need to mutate after Emit to install the
	// back-edge, because Emit returns the ref by value.
	e := NewBufferedEmitter()
	aNode := &Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{},
		KV: map[string]string{"name": "A"}, Outputs: []string{},
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
	}
	bNode := &Node{
		Cmds: []Cmd{{CmdArgs: []string{"B"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{},
		KV: map[string]string{"name": "B"}, Outputs: []string{},
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
	}
	a := e.Emit(aNode)
	b := e.Emit(bNode)
	aNode.DepRefs = []NodeRef{b}
	bNode.DepRefs = []NodeRef{a}
	e.Result(a)

	_, exc := finalizeExc(e)
	if exc == nil {
		t.Errorf("Finalize on cyclic graph returned no exception")
	}
}

func TestFinalize_OutOfRangeRefReturnsError(t *testing.T) {
	e := NewBufferedEmitter()
	a := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{},
		KV: map[string]string{"name": "A"}, Outputs: []string{},
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
		DepRefs:          []NodeRef{{id: 999}}, // bogus
	})
	e.Result(a)

	_, exc := finalizeExc(e)
	if exc == nil {
		t.Errorf("Finalize with bogus ref returned no exception")
	}
}

func TestFinalize_GraphTopLevelKeyOrder(t *testing.T) {
	// The Graph wrapper must serialise as { conf, graph, inputs, result }.
	e, _, _, _ := build3NodeDAG()
	g := Finalize(e)
	raw := Throw2(json.Marshal(g))
	keys := extractKeyOrder(t, raw)
	want := []string{"conf", "graph", "inputs", "result"}

	if len(keys) != len(want) {
		t.Fatalf("graph keys = %v, want %v", keys, want)
	}

	for i, w := range want {
		if keys[i] != w {
			t.Errorf("graph key[%d] = %q, want %q", i, keys[i], w)
		}
	}
}

func TestFinalize_ForeignDepsResolvedAndSorted(t *testing.T) {
	e := NewBufferedEmitter()
	mk := func(name string) NodeRef {
		return e.Emit(&Node{
			Cmds: []Cmd{{CmdArgs: []string{name}, Env: map[string]string{}}},
			Env:  map[string]string{}, Inputs: []string{},
			KV: map[string]string{"name": name}, Outputs: []string{},
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
		})
	}
	t1 := mk("T1")
	t2 := mk("T2")
	a := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{},
		KV: map[string]string{"name": "A"}, Outputs: []string{},
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
		ForeignDepRefs: map[string][]NodeRef{
			"tool": {t2, t1, t2}, // unsorted + duplicate
		},
	})
	e.Result(a)
	g := Finalize(e)

	var aNode *Node
	for _, n := range g.Graph {
		if n.KV["name"] == "A" {
			aNode = n
		}
	}

	if aNode == nil {
		t.Fatalf("A not found")
	}

	got := aNode.ForeignDeps["tool"]
	if len(got) != 2 {
		t.Errorf("ForeignDeps[tool] not deduped: %v", got)
	}

	if !sort.StringsAreSorted(got) {
		t.Errorf("ForeignDeps[tool] not sorted: %v", got)
	}
}

func TestFinalize_RejectsPreSetDeps(t *testing.T) {
	// Rules must express dependencies via DepRefs; a pre-populated Deps
	// slice would corrupt the Merkle hash (Finalize would either
	// overwrite it for nodes with refs or canonicalise the stale value
	// for nodes without). Finalize must reject this up-front.
	e := NewBufferedEmitter()
	e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{},
		KV: map[string]string{"name": "A"}, Outputs: []string{},
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
		Deps:             []string{"FAKE"},
	})

	_, exc := finalizeExc(e)
	if exc == nil {
		t.Fatalf("Finalize accepted pre-populated Deps; want exception")
	}

	if !strings.Contains(exc.Error(), "pre-populated Deps") {
		t.Errorf("error message %q does not mention pre-populated Deps", exc.Error())
	}
}

func TestFinalize_RejectsPreSetForeignDeps(t *testing.T) {
	// Symmetric to TestFinalize_RejectsPreSetDeps but for ForeignDeps.
	e := NewBufferedEmitter()
	e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{},
		KV: map[string]string{"name": "A"}, Outputs: []string{},
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
		ForeignDeps:      map[string][]string{"tool": {"FAKE"}},
	})

	_, exc := finalizeExc(e)
	if exc == nil {
		t.Fatalf("Finalize accepted pre-populated ForeignDeps; want exception")
	}

	if !strings.Contains(exc.Error(), "pre-populated ForeignDeps") {
		t.Errorf("error message %q does not mention pre-populated ForeignDeps", exc.Error())
	}
}

func TestFinalize_DedupesIdenticalEmits(t *testing.T) {
	// Two leaf nodes with identical content hash to the same UID and
	// must collapse into a single Graph entry — graph[uid] is a set,
	// not a multiset.
	e := NewBufferedEmitter()
	mk := func() NodeRef {
		return e.Emit(&Node{
			Cmds: []Cmd{{CmdArgs: []string{"identical"}, Env: map[string]string{}}},
			Env:  map[string]string{}, Inputs: []string{},
			KV: map[string]string{"name": "L"}, Outputs: []string{},
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
		})
	}
	r1 := mk()
	r2 := mk()
	e.Result(r1)
	e.Result(r2)
	g := Finalize(e)

	if len(g.Graph) != 1 {
		t.Errorf("expected 1 deduped node in Graph, got %d (%+v)", len(g.Graph), g.Graph)
	}
}

func TestFinalize_SecondCallErrors(t *testing.T) {
	// The emitter is single-shot. A second Finalize must error out so
	// callers don't silently get a stale graph after the *Refs slices
	// have been cleared.
	e, _, _, _ := build3NodeDAG()
	Finalize(e)

	_, exc := finalizeExc(e)
	if exc == nil {
		t.Fatalf("second Finalize returned nil exception; want already-finalized error")
	}

	if !strings.Contains(exc.Error(), "already finalized") {
		t.Errorf("error message %q does not mention already-finalized state", exc.Error())
	}
}

func TestFinalize_DropsEmptyForeignDepsKey(t *testing.T) {
	// A ForeignDepRefs key with an empty slice must NOT serialize as
	// `foreign_deps:{key:[]}`; the whole foreign_deps field must be
	// omitted (omitempty on a nil map). Pin via the resolved field
	// being nil — equivalent to "the key never made it through".
	e := NewBufferedEmitter()
	a := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{},
		KV: map[string]string{"name": "A"}, Outputs: []string{},
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
		ForeignDepRefs:   map[string][]NodeRef{"tool": {}}, // empty slice
	})
	e.Result(a)
	g := Finalize(e)

	var aNode *Node
	for _, n := range g.Graph {
		if n.KV["name"] == "A" {
			aNode = n
		}
	}

	if aNode == nil {
		t.Fatalf("A not found")
	}

	if aNode.ForeignDeps != nil {
		t.Errorf("expected ForeignDeps==nil (omitempty drops the field); got %v", aNode.ForeignDeps)
	}

	raw := Throw2(json.Marshal(aNode))
	if bytes.Contains(raw, []byte(`"foreign_deps"`)) {
		t.Errorf("foreign_deps key leaked into JSON for empty-only ForeignDepRefs: %s", raw)
	}
}

func TestFinalize_DedupesDuplicateResultCalls(t *testing.T) {
	// Result(ref) called twice on the same ref must produce exactly
	// one entry in Graph.Result, preserving first-seen order.
	e := NewBufferedEmitter()
	a := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{},
		KV: map[string]string{"name": "A"}, Outputs: []string{},
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
	})
	e.Result(a)
	e.Result(a) // duplicate, must collapse
	g := Finalize(e)

	if len(g.Result) != 1 {
		t.Errorf("expected 1 deduped result, got %d (%v)", len(g.Result), g.Result)
	}
}

func TestEmitter_PostFinalizeEmitPanics(t *testing.T) {
	e := NewBufferedEmitter()
	e.Emit(&Node{KV: map[string]string{"p": "TEST"}})
	Finalize(e)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on post-finalize Emit")
		}

		msg, ok := r.(string)
		if !ok {
			t.Fatalf("recover returned %T, want string", r)
		}

		if !strings.Contains(msg, "after Finalize") {
			t.Errorf("unexpected panic message: %q", msg)
		}
	}()

	e.Emit(&Node{KV: map[string]string{"p": "TEST2"}})
}

func TestEmitter_PostFinalizeResultPanics(t *testing.T) {
	e := NewBufferedEmitter()
	ref := e.Emit(&Node{KV: map[string]string{"p": "TEST"}})
	Finalize(e)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on post-finalize Result")
		}

		msg, ok := r.(string)
		if !ok {
			t.Fatalf("recover returned %T, want string", r)
		}

		if !strings.Contains(msg, "after Finalize") {
			t.Errorf("unexpected panic message: %q", msg)
		}
	}()

	e.Result(ref)
}

func TestFinalize_ChildContentChangeChangesParentUID(t *testing.T) {
	// Merkle property: changing a leaf must change its parent's UID
	// (because the parent's canonical bytes include the leaf's UID).
	e1 := NewBufferedEmitter()
	c1 := e1.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"C", "v1"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{}, KV: map[string]string{},
		Outputs: []string{}, Requirements: map[string]interface{}{},
		Tags: []string{}, TargetProperties: map[string]string{},
	})
	a1 := e1.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: []string{}, KV: map[string]string{},
		Outputs: []string{}, Requirements: map[string]interface{}{},
		Tags: []string{}, TargetProperties: map[string]string{},
		DepRefs: []NodeRef{c1},
	})
	e1.Result(a1)
	g1 := Finalize(e1)

	e2 := NewBufferedEmitter()
	c2 := e2.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"C", "v2"}, Env: map[string]string{}}}, // changed
		Env:  map[string]string{}, Inputs: []string{}, KV: map[string]string{},
		Outputs: []string{}, Requirements: map[string]interface{}{},
		Tags: []string{}, TargetProperties: map[string]string{},
	})
	a2 := e2.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}}, // unchanged
		Env:  map[string]string{}, Inputs: []string{}, KV: map[string]string{},
		Outputs: []string{}, Requirements: map[string]interface{}{},
		Tags: []string{}, TargetProperties: map[string]string{},
		DepRefs: []NodeRef{c2},
	})
	e2.Result(a2)
	g2 := Finalize(e2)

	// Find A in each graph (it's the topo-last entry).
	a1uid := g1.Graph[len(g1.Graph)-1].UID
	a2uid := g2.Graph[len(g2.Graph)-1].UID

	if a1uid == a2uid {
		t.Errorf("Merkle property violated: parent UID stayed %q after leaf change", a1uid)
	}
}
