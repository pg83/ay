package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func build3NodeDAG() (*BufferedEmitter, NodeRef, NodeRef, NodeRef) {
	e := NewBufferedEmitter()
	c := e.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: []string{"build", "C"}, Env: map[string]string{}}},
		Env:              map[string]string{},
		Inputs:           ToVFSSlice([]string{"c.in"}),
		KV:               KV{Name: "C"},
		Outputs:          ToVFSSlice([]string{"c.out"}),
		Platform:         "linux",
		Requirements:     Requirements{},
		Tags:             []string{},
		TargetProperties: TargetProperties{},
	})
	b := e.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: []string{"build", "B"}, Env: map[string]string{}}},
		Env:              map[string]string{},
		Inputs:           ToVFSSlice([]string{"b.in"}),
		KV:               KV{Name: "B"},
		Outputs:          ToVFSSlice([]string{"b.out"}),
		Platform:         "linux",
		Requirements:     Requirements{},
		Tags:             []string{},
		TargetProperties: TargetProperties{},
		DepRefs:          []NodeRef{c},
	})
	a := e.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: []string{"build", "A"}, Env: map[string]string{}}},
		Env:              map[string]string{},
		Inputs:           ToVFSSlice([]string{"a.in"}),
		KV:               KV{Name: "A"},
		Outputs:          ToVFSSlice([]string{"a.out"}),
		Platform:         "linux",
		Requirements:     Requirements{},
		Tags:             []string{},
		TargetProperties: TargetProperties{},
		DepRefs:          []NodeRef{b, c},
	})
	e.Result(a)

	return e, a, b, c
}

// graphDeps / graphForeignDeps resolve a node's refs to dep uids via the graph's
// uid vector — deps are no longer materialized on the node.
func graphDeps(g *Graph, n *Node) []UID {
	out := make([]UID, len(n.DepRefs))
	for i, r := range n.DepRefs {
		out[i] = g.uids.get(r)
	}

	return out
}

func graphForeignDeps(g *Graph, n *Node) []UID {
	out := make([]UID, len(n.ForeignDepRefs))
	for i, r := range n.ForeignDepRefs {
		out[i] = g.uids.get(r)
	}

	return out
}

func nodeNameByKV(g *Graph, idx int) string {
	name := g.Graph[idx].KV.Name

	return name
}

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

	if nodeNameByKV(g, 0) != "A" {
		t.Errorf("graph[0] = %q, want A (DFS root)", nodeNameByKV(g, 0))
	}

	remaining := map[string]bool{nodeNameByKV(g, 1): true, nodeNameByKV(g, 2): true}
	if !remaining["B"] || !remaining["C"] {
		t.Errorf("graph[1..2] = [%q, %q], want {B, C} in some order",
			nodeNameByKV(g, 1), nodeNameByKV(g, 2))
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

	for i, n := range g1.Graph {
		if len(n.UID.String()) != 22 {
			t.Errorf("graph[%d].UID len = %d, want 22 (got %q)", i, len(n.UID.String()), n.UID)
		}

		if n.SelfUID != n.UID {
			t.Logf("PR-02 placeholder: SelfUID currently equals UID; future PR will compute distinct value. graph[%d].SelfUID=%q UID=%q", i, n.SelfUID, n.UID)
		}

		if len(n.StatsUID) != 32 {
			t.Errorf("graph[%d].StatsUID len = %d, want 32 (got %q)", i, len(n.StatsUID), n.StatsUID)
		}
		for _, ch := range n.StatsUID {
			if !strings.ContainsRune("0123456789abcdef", ch) {
				t.Errorf("graph[%d].StatsUID = %q, want lowercase hex", i, n.StatsUID)
				break
			}
		}
	}

	if len(g1.Result) != 1 {
		t.Fatalf("expected 1 result, got %d (%v)", len(g1.Result), g1.Result)
	}

	if g1.Result[0] != g1.Graph[0].UID {
		t.Errorf("result[0] = %q, want graph[0].UID %q", g1.Result[0], g1.Graph[0].UID)
	}
}

func TestFinalize_DepsPreserveInsertionOrder(t *testing.T) {

	e := NewBufferedEmitter()
	mkLeaf := func(name string) NodeRef {
		return e.Emit(&Node{
			Cmds:   []Cmd{{CmdArgs: []string{name}, Env: map[string]string{}}},
			Env:    map[string]string{},
			Inputs: ToVFSSlice([]string{}), KV: KV{Name: name},
			Outputs:      ToVFSSlice([]string{}),
			Requirements: Requirements{}, Tags: []string{},
			TargetProperties: TargetProperties{},
		})
	}
	x := mkLeaf("X")
	y := mkLeaf("Y")
	z := mkLeaf("Z")

	a := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}),
		KV: KV{Name: "A"}, Outputs: ToVFSSlice([]string{}),
		Requirements:     Requirements{},
		Tags:             []string{},
		TargetProperties: TargetProperties{},
		DepRefs:          []NodeRef{z, x, y},
	})
	e.Result(a)
	g := Finalize(e)

	var aNode *Node
	for _, n := range g.Graph {
		if n.KV.Name == "A" {
			aNode = n

			break
		}
	}

	if aNode == nil {
		t.Fatalf("A not found in graph")
	}

	byName := map[string]UID{}
	for _, n := range g.Graph {
		if nm := n.KV.Name; nm != "" {
			byName[nm] = n.UID
		}
	}

	// Deps are the DepRefs resolved to uids in insertion order — no sort, no
	// dedup (the dump-sort normalization owns ordering; the gate is the oracle).
	want := []UID{byName["Z"], byName["X"], byName["Y"]}
	if !slices.Equal(graphDeps(g, aNode), want) {
		t.Errorf("A.Deps = %v, want insertion order %v", graphDeps(g, aNode), want)
	}
}

func TestFinalize_KeepsDuplicateDeps(t *testing.T) {
	e := NewBufferedEmitter()
	c := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"C"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}),
		KV: KV{Name: "C"}, Outputs: ToVFSSlice([]string{}),
		Requirements:     Requirements{},
		Tags:             []string{},
		TargetProperties: TargetProperties{},
	})
	a := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}),
		KV: KV{Name: "A"}, Outputs: ToVFSSlice([]string{}),
		Requirements:     Requirements{},
		Tags:             []string{},
		TargetProperties: TargetProperties{},
		DepRefs:          []NodeRef{c, c, c},
	})
	e.Result(a)
	g := Finalize(e)

	var aNode *Node
	for _, n := range g.Graph {
		if n.KV.Name == "A" {
			aNode = n
		}
	}

	if aNode == nil {
		t.Fatalf("A not found")
	}

	// resolveAndUID no longer dedups — duplicate DepRefs surface as duplicate
	// Deps. Generators must not emit duplicate refs (see EmitLD whole-archive).
	if len(graphDeps(g, aNode)) != 3 {
		t.Errorf("expected duplicates preserved (len 3); A.Deps = %v", graphDeps(g, aNode))
	}
}

func TestFinalize_CycleReturnsError(t *testing.T) {

	e := NewBufferedEmitter()
	aNode := &Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}),
		KV: KV{Name: "A"}, Outputs: ToVFSSlice([]string{}),
		Requirements:     Requirements{},
		Tags:             []string{},
		TargetProperties: TargetProperties{},
	}
	bNode := &Node{
		Cmds: []Cmd{{CmdArgs: []string{"B"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}),
		KV: KV{Name: "B"}, Outputs: ToVFSSlice([]string{}),
		Requirements:     Requirements{},
		Tags:             []string{},
		TargetProperties: TargetProperties{},
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
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}),
		KV: KV{Name: "A"}, Outputs: ToVFSSlice([]string{}),
		Requirements:     Requirements{},
		Tags:             []string{},
		TargetProperties: TargetProperties{},
		DepRefs:          []NodeRef{999},
	})
	e.Result(a)

	_, exc := finalizeExc(e)
	if exc == nil {
		t.Errorf("Finalize with bogus ref returned no exception")
	}
}

func TestFinalize_GraphTopLevelKeyOrder(t *testing.T) {

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

func TestFinalize_DedupesIdenticalEmits(t *testing.T) {

	e := NewBufferedEmitter()
	mk := func() NodeRef {
		return e.Emit(&Node{
			Cmds: []Cmd{{CmdArgs: []string{"identical"}, Env: map[string]string{}}},
			Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}),
			KV: KV{Name: "L"}, Outputs: ToVFSSlice([]string{}),
			Requirements:     Requirements{},
			Tags:             []string{},
			TargetProperties: TargetProperties{},
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

	e := NewBufferedEmitter()
	a := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}),
		KV: KV{Name: "A"}, Outputs: ToVFSSlice([]string{}),
		Requirements:     Requirements{},
		Tags:             []string{},
		TargetProperties: TargetProperties{},
		ForeignDepRefs:   []NodeRef{},
	})
	e.Result(a)
	g := Finalize(e)

	var aNode *Node
	for _, n := range g.Graph {
		if n.KV.Name == "A" {
			aNode = n
		}
	}

	if aNode == nil {
		t.Fatalf("A not found")
	}

	if len(aNode.ForeignDepRefs) != 0 {
		t.Errorf("expected empty ForeignDepRefs; got %v", aNode.ForeignDepRefs)
	}

	var buf bytes.Buffer
	writeGraphCompact(&buf, g)
	if bytes.Contains(buf.Bytes(), []byte(`"foreign_deps"`)) {
		t.Errorf("foreign_deps key leaked into JSON for empty ForeignDepRefs: %s", buf.Bytes())
	}
}

func TestFinalize_DedupesDuplicateResultCalls(t *testing.T) {

	e := NewBufferedEmitter()
	a := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}),
		KV: KV{Name: "A"}, Outputs: ToVFSSlice([]string{}),
		Requirements:     Requirements{},
		Tags:             []string{},
		TargetProperties: TargetProperties{},
	})
	e.Result(a)
	e.Result(a)
	g := Finalize(e)

	if len(g.Result) != 1 {
		t.Errorf("expected 1 deduped result, got %d (%v)", len(g.Result), g.Result)
	}
}

func TestEmitter_OnReady_BufferedNoOp(t *testing.T) {
	e := NewBufferedEmitter()
	r := e.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"X"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}),
		KV: KV{Name: "X"}, Outputs: ToVFSSlice([]string{}),
		Requirements: Requirements{},
		Tags:         []string{}, TargetProperties: TargetProperties{},
	})
	e.Result(r)

	ch := e.OnReady(r)

	select {
	case <-ch:
		t.Fatalf("OnReady channel closed pre-Finalize (BufferedEmitter contract)")
	default:

	}

	Finalize(e)

	select {
	case <-ch:

	default:
		t.Fatalf("OnReady channel not closed post-Finalize")
	}
}

func TestEmitter_PostFinalizeEmitPanics(t *testing.T) {
	e := NewBufferedEmitter()
	e.Emit(&Node{KV: KV{P: "TEST"}})
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

	e.Emit(&Node{KV: KV{P: "TEST2"}})
}

func TestEmitter_PostFinalizeResultPanics(t *testing.T) {
	e := NewBufferedEmitter()
	ref := e.Emit(&Node{KV: KV{P: "TEST"}})
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

	e1 := NewBufferedEmitter()
	c1 := e1.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"C", "v1"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}), KV: KV{},
		Outputs: ToVFSSlice([]string{}), Requirements: Requirements{},
		Tags: []string{}, TargetProperties: TargetProperties{},
	})
	a1 := e1.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}), KV: KV{},
		Outputs: ToVFSSlice([]string{}), Requirements: Requirements{},
		Tags: []string{}, TargetProperties: TargetProperties{},
		DepRefs: []NodeRef{c1},
	})
	e1.Result(a1)
	g1 := Finalize(e1)

	e2 := NewBufferedEmitter()
	c2 := e2.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"C", "v2"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}), KV: KV{},
		Outputs: ToVFSSlice([]string{}), Requirements: Requirements{},
		Tags: []string{}, TargetProperties: TargetProperties{},
	})
	a2 := e2.Emit(&Node{
		Cmds: []Cmd{{CmdArgs: []string{"A"}, Env: map[string]string{}}},
		Env:  map[string]string{}, Inputs: ToVFSSlice([]string{}), KV: KV{},
		Outputs: ToVFSSlice([]string{}), Requirements: Requirements{},
		Tags: []string{}, TargetProperties: TargetProperties{},
		DepRefs: []NodeRef{c2},
	})
	e2.Result(a2)
	g2 := Finalize(e2)

	a1uid := g1.Graph[0].UID
	a2uid := g2.Graph[0].UID

	if a1uid == a2uid {
		t.Errorf("Merkle property violated: parent UID stayed %q after leaf change", a1uid)
	}
}

func TestFinalize_HeapTopo_Determinism(t *testing.T) {
	e := NewBufferedEmitter()
	mk := func(name string, deps ...NodeRef) NodeRef {
		return e.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: []string{name}, Env: map[string]string{}}},
			Env:              map[string]string{},
			Inputs:           ToVFSSlice([]string{}),
			KV:               KV{Name: name},
			Outputs:          ToVFSSlice([]string{}),
			Requirements:     Requirements{},
			Tags:             []string{},
			TargetProperties: TargetProperties{},
			DepRefs:          deps,
		})
	}
	l0 := mk("L0")
	l1 := mk("L1")
	l2 := mk("L2")
	m3 := mk("M3", l0)
	m4 := mk("M4", l1)
	t6 := mk("T", l2, m3, m4)
	e.Result(t6)
	g := Finalize(e)

	if len(g.Graph) != 6 {
		t.Fatalf("graph len = %d, want 6", len(g.Graph))
	}

	if g.Graph[0].KV.Name != "T" {
		t.Errorf("graph[0] = %q, want T (DFS root)", g.Graph[0].KV.Name)
	}

	pos := make(map[string]int, 6)
	for i, n := range g.Graph {
		name := n.KV.Name
		pos[name] = i
	}

	edges := map[string][]string{
		"T":  {"L2", "M3", "M4"},
		"M3": {"L0"},
		"M4": {"L1"},
	}
	for parent, children := range edges {
		for _, child := range children {
			if pos[parent] > pos[child] {
				t.Errorf("DFS invariant violated: %s (pos %d) must appear before %s (pos %d)",
					parent, pos[parent], child, pos[child])
			}
		}
	}

	for _, name := range []string{"T", "L0", "L1", "L2", "M3", "M4"} {
		if _, ok := pos[name]; !ok {
			t.Errorf("node %q missing from graph", name)
		}
	}
}
