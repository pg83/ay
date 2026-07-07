package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func build3NodeDAG() (*StreamingEmitter, NodeRef, NodeRef, NodeRef) {
	e := newStreamingEmitter(nil)
	c := e.emitNode(Node{
		Cmds:         []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"build", "C"}))}, Env: nil}},
		Env:          nil,
		Inputs:       InputChunks{ToVFSSlice([]string{"c.in"})},
		KV:           &KV{Name: "C"},
		Outputs:      ToVFSSlice([]string{"c.out"}),
		Platform:     &Platform{Target: "linux"},
		Requirements: Requirements{},
	})
	b := e.emitNode(Node{
		Cmds:         []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"build", "B"}))}, Env: nil}},
		Env:          nil,
		Inputs:       InputChunks{ToVFSSlice([]string{"b.in"})},
		KV:           &KV{Name: "B"},
		Outputs:      ToVFSSlice([]string{"b.out"}),
		Platform:     &Platform{Target: "linux"},
		Requirements: Requirements{},
		DepRefs:      []NodeRef{c},
	})
	a := e.emitNode(Node{
		Cmds:         []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"build", "A"}))}, Env: nil}},
		Env:          nil,
		Inputs:       InputChunks{ToVFSSlice([]string{"a.in"})},
		KV:           &KV{Name: "A"},
		Outputs:      ToVFSSlice([]string{"a.out"}),
		Platform:     &Platform{Target: "linux"},
		Requirements: Requirements{},
		DepRefs:      []NodeRef{b, c},
	})
	e.result(a)

	return e, a, b, c
}

func graphDeps(g *Graph, n *Node) []NodeRef {
	var out []NodeRef

	for r := range n.buildDeps(g.fetchRefs) {
		out = append(out, r)
	}

	return out
}

func graphForeignDeps(g *Graph, n *Node) []NodeRef {
	out := make([]NodeRef, len(n.ForeignDepRefs))

	for i, r := range n.ForeignDepRefs {
		out[i] = r
	}

	return out
}

func nodeNameByKV(g *Graph, idx int) string {
	name := g.Graph[idx].KV.Name

	return name
}

func finalizeExc(e *StreamingEmitter) (g *Graph, exc *Exception) {
	exc = try(func() {
		g = finalize(e)
	})

	return g, exc
}

func TestFinalize_TopoOrder_LeavesFirst(t *testing.T) {
	e, _, _, _ := build3NodeDAG()
	g := finalize(e)

	if len(g.Graph) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(g.Graph))
	}

	present := map[string]bool{nodeNameByKV(g, 0): true, nodeNameByKV(g, 1): true, nodeNameByKV(g, 2): true}

	if !present["A"] || !present["B"] || !present["C"] {
		t.Errorf("graph = [%q, %q, %q], want {A, B, C}",
			nodeNameByKV(g, 0), nodeNameByKV(g, 1), nodeNameByKV(g, 2))
	}
}

func TestFinalize_UIDsStableAcrossRuns(t *testing.T) {
	e1, _, _, _ := build3NodeDAG()
	g1 := finalize(e1)
	raw1 := throw2(json.Marshal(g1))

	e2, _, _, _ := build3NodeDAG()
	g2 := finalize(e2)
	raw2 := throw2(json.Marshal(g2))

	if !bytes.Equal(raw1, raw2) {
		t.Errorf("Finalize output not byte-stable across runs.\nrun1: %s\nrun2: %s", raw1, raw2)
	}

	for i, n := range g1.Graph {
		if n.Ref != NodeRef(i) {
			t.Errorf("graph[%d].Ref = %d, want %d", i, n.Ref, i)
		}
	}

	if len(g1.Result) != 1 {
		t.Fatalf("expected 1 result, got %d (%v)", len(g1.Result), g1.Result)
	}
}

func TestFinalize_DepsPreserveInsertionOrder(t *testing.T) {
	e := newStreamingEmitter(nil)
	mkLeaf := func(name string) NodeRef {
		return e.emitNode(Node{Platform: &Platform{},
			Cmds:   []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{name}))}, Env: nil}},
			Env:    nil,
			Inputs: InputChunks{ToVFSSlice([]string{})}, KV: &KV{Name: name},
			Outputs:      ToVFSSlice([]string{}),
			Requirements: Requirements{},
		})
	}
	x := mkLeaf("X")
	y := mkLeaf("Y")
	z := mkLeaf("Z")

	a := e.emitNode(Node{Platform: &Platform{},
		Cmds: []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"A"}))}, Env: nil}},
		Env:  nil, Inputs: InputChunks{ToVFSSlice([]string{})},
		KV: &KV{Name: "A"}, Outputs: ToVFSSlice([]string{}),
		Requirements: Requirements{},
		DepRefs:      []NodeRef{z, x, y},
	})
	e.result(a)
	g := finalize(e)

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

	byName := map[string]NodeRef{}

	for _, n := range g.Graph {
		if nm := n.KV.Name; nm != "" {
			byName[nm] = n.Ref
		}
	}

	want := []NodeRef{byName["Z"], byName["X"], byName["Y"]}

	if !slices.Equal(graphDeps(g, aNode), want) {
		t.Errorf("A.Deps = %v, want insertion order %v", graphDeps(g, aNode), want)
	}
}

func TestFinalize_KeepsDuplicateDeps(t *testing.T) {
	e := newStreamingEmitter(nil)
	c := e.emitNode(Node{Platform: &Platform{},
		Cmds: []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"C"}))}, Env: nil}},
		Env:  nil, Inputs: InputChunks{ToVFSSlice([]string{})},
		KV: &KV{Name: "C"}, Outputs: ToVFSSlice([]string{}),
		Requirements: Requirements{},
	})
	a := e.emitNode(Node{Platform: &Platform{},
		Cmds: []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"A"}))}, Env: nil}},
		Env:  nil, Inputs: InputChunks{ToVFSSlice([]string{})},
		KV: &KV{Name: "A"}, Outputs: ToVFSSlice([]string{}),
		Requirements: Requirements{},
		DepRefs:      []NodeRef{c, c, c},
	})
	e.result(a)
	g := finalize(e)

	var aNode *Node

	for _, n := range g.Graph {
		if n.KV.Name == "A" {
			aNode = n
		}
	}

	if aNode == nil {
		t.Fatalf("A not found")
	}

	if len(graphDeps(g, aNode)) != 3 {
		t.Errorf("expected duplicates preserved (len 3); A.Deps = %v", graphDeps(g, aNode))
	}
}

func TestFinalize_GraphTopLevelKeyOrder(t *testing.T) {
	e, _, _, _ := build3NodeDAG()
	g := finalize(e)
	raw := throw2(json.Marshal(g))
	keys := extractKeyOrder(t, raw)
	want := []string{"graph", "inputs", "result"}

	if len(keys) != len(want) {
		t.Fatalf("graph keys = %v, want %v", keys, want)
	}

	for i, w := range want {
		if keys[i] != w {
			t.Errorf("graph key[%d] = %q, want %q", i, keys[i], w)
		}
	}
}

func TestFinalize_KeepsIdenticalEmits(t *testing.T) {
	// Construction no longer dedups identical nodes; each emit is a distinct
	// NodeRef. Content-addressed dedup happens later in `dev dump normalize`.
	e := newStreamingEmitter(nil)
	mk := func() NodeRef {
		return e.emitNode(Node{Platform: &Platform{},
			Cmds: []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"identical"}))}, Env: nil}},
			Env:  nil, Inputs: InputChunks{ToVFSSlice([]string{})},
			KV: &KV{Name: "L"}, Outputs: ToVFSSlice([]string{}),
			Requirements: Requirements{},
		})
	}
	r1 := mk()
	r2 := mk()
	e.result(r1)
	e.result(r2)
	g := finalize(e)

	if len(g.Graph) != 2 {
		t.Errorf("expected 2 nodes in Graph (no construction dedup), got %d (%+v)", len(g.Graph), g.Graph)
	}
}

func TestFinalize_SecondCallErrors(t *testing.T) {
	e, _, _, _ := build3NodeDAG()
	finalize(e)

	_, exc := finalizeExc(e)

	if exc == nil {
		t.Fatalf("second Finalize returned nil exception; want already-finalized error")
	}

	if !strings.Contains(exc.Error(), "already finalized") {
		t.Errorf("error message %q does not mention already-finalized state", exc.Error())
	}
}

func TestFinalize_DropsEmptyForeignDepsKey(t *testing.T) {
	e := newStreamingEmitter(nil)
	a := e.emitNode(Node{Platform: &Platform{},
		Cmds: []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"A"}))}, Env: nil}},
		Env:  nil, Inputs: InputChunks{ToVFSSlice([]string{})},
		KV: &KV{Name: "A"}, Outputs: ToVFSSlice([]string{}),
		Requirements:   Requirements{},
		ForeignDepRefs: []NodeRef{},
	})
	e.result(a)
	g := finalize(e)

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
	writeGraphCompact(&buf, g, false)

	if bytes.Contains(buf.Bytes(), []byte(`"foreign_deps"`)) {
		t.Errorf("foreign_deps key leaked into JSON for empty ForeignDepRefs: %s", buf.Bytes())
	}
}

func TestFinalize_DedupesDuplicateResultCalls(t *testing.T) {
	e := newStreamingEmitter(nil)
	a := e.emitNode(Node{Platform: &Platform{},
		Cmds: []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"A"}))}, Env: nil}},
		Env:  nil, Inputs: InputChunks{ToVFSSlice([]string{})},
		KV: &KV{Name: "A"}, Outputs: ToVFSSlice([]string{}),
		Requirements: Requirements{},
	})
	e.result(a)
	e.result(a)
	g := finalize(e)

	if len(g.Result) != 1 {
		t.Errorf("expected 1 deduped result, got %d (%v)", len(g.Result), g.Result)
	}
}

func TestEmitter_PostFinalizeEmitPanics(t *testing.T) {
	e := newStreamingEmitter(nil)
	e.emitNode(Node{Platform: &Platform{}, KV: &KV{P: pkTEST}})
	finalize(e)

	defer func() {
		r := recover()

		if r == nil {
			t.Fatal("expected panic on post-finalize Emit")
		}

		msg, ok := r.(string)

		if !ok {
			t.Fatalf("recover returned %T, want string", r)
		}

		if !strings.Contains(msg, "after Finish") {
			t.Errorf("unexpected panic message: %q", msg)
		}
	}()

	e.emitNode(Node{Platform: &Platform{}, KV: &KV{P: pkTEST2}})
}

func TestEmitter_PostFinalizeResultPanics(t *testing.T) {
	e := newStreamingEmitter(nil)
	ref := e.emitNode(Node{Platform: &Platform{}, KV: &KV{P: pkTEST}})
	finalize(e)

	defer func() {
		r := recover()

		if r == nil {
			t.Fatal("expected panic on post-finalize Result")
		}

		msg, ok := r.(string)

		if !ok {
			t.Fatalf("recover returned %T, want string", r)
		}

		if !strings.Contains(msg, "after Finish") {
			t.Errorf("unexpected panic message: %q", msg)
		}
	}()

	e.result(ref)
}

func TestFinish_PendingDeliveredAfterTheirDeps(t *testing.T) {
	delivered := map[NodeRef]bool{}

	var order []NodeRef

	var e *StreamingEmitter

	e = newStreamingEmitter(func(n *Node, fetchRefs *DenseMap[STR, NodeRef]) {
		for r := range n.buildDeps(fetchRefs) {
			if !delivered[r] {
				t.Errorf("node %d delivered before its dep %d", n.Ref, r)
			}
		}

		delivered[n.Ref] = true
		order = append(order, n.Ref)
	})

	_ = e

	r1 := e.reserve()
	r2 := e.reserve()

	a := e.emitNode(Node{
		KV:       &KV{Name: "A"},
		Platform: &Platform{Target: "linux"},
		DepRefs:  []NodeRef{r1},
	})

	e.emitReservedNode(Node{
		KV:       &KV{Name: "R1"},
		Platform: &Platform{Target: "linux"},
		DepRefs:  []NodeRef{r2},
	}, r1)

	e.emitReservedNode(Node{
		KV:       &KV{Name: "R2"},
		Platform: &Platform{Target: "linux"},
	}, r2)

	e.finish()

	if len(order) != 3 {
		t.Fatalf("delivered %d nodes, want 3", len(order))
	}

	if !delivered[a] || !delivered[r1] || !delivered[r2] {
		t.Fatalf("not all nodes delivered: %v", order)
	}
}
