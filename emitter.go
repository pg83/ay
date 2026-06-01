package main

import (
	"container/heap"
)

type intHeap []int

func (h intHeap) Len() int            { return len(h) }
func (h intHeap) Less(i, j int) bool  { return h[i] < h[j] }
func (h intHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *intHeap) Push(x interface{}) { *h = append(*h, x.(int)) }
func (h *intHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]

	return x
}

type NodeRef struct {
	id int64
}

type Emitter interface {
	Emit(n *Node) NodeRef
	Result(NodeRef)
	OnReady(NodeRef) <-chan struct{}
}

type BufferedEmitter struct {
	nodes     []*Node
	results   []int64
	finalized bool

	// generatedFirstClaim is populated by runGen after gen completes, merging
	// the per-scanner generatedFirstClaim maps. finalizeDumpGraph reads it to
	// override producer-node target_properties["module_dir"] with the first
	// scan-time consumer's module path, mirroring upstream ymake's Node2Module
	// rule (see scanner.go: generatedFirstClaim doc).
	generatedFirstClaim map[VFS]string

	// fs, set by runGen, lets finalizeNodesInOrder mix $(S) input content hashes
	// into node uids (see canonBuf.fs).
	fs      FS
	readyCh chan struct{}
}

func NewBufferedEmitter() *BufferedEmitter {
	return &BufferedEmitter{
		readyCh: make(chan struct{}),
	}
}
func (e *BufferedEmitter) OnReady(_ NodeRef) <-chan struct{} {
	return e.readyCh
}

func (e *BufferedEmitter) Emit(n *Node) NodeRef {
	if e.finalized {
		panic("BufferedEmitter.Emit called after Finalize")
	}

	id := int64(len(e.nodes))
	e.nodes = append(e.nodes, n)
	return NodeRef{id: id}
}

func (e *BufferedEmitter) Result(r NodeRef) {
	if e.finalized {
		panic("BufferedEmitter.Result called after Finalize")
	}

	e.results = append(e.results, r.id)
}

type Graph struct {
	Conf   map[string]interface{} `json:"conf"`
	Graph  []*Node                `json:"graph"`
	Inputs map[string]interface{} `json:"inputs"`
	Result []UID                  `json:"result"`

	// uids resolves each node's DepRefs/ForeignDepRefs (by id) to dep uids at
	// JSON-write time; deps are never materialized onto the node.
	uids *uidVec `json:"-"`
}

func FinalizeStream(e *BufferedEmitter, yield func(*Node)) []UID {
	uids := finalizeNodes(e, yield)

	results := make([]UID, 0, len(e.results))
	seen := map[UID]struct{}{}

	for _, rid := range e.results {
		u := uids.get(rid)

		if _, ok := seen[u]; ok {
			continue
		}

		seen[u] = struct{}{}
		results = append(results, u)
	}

	return results
}

func finalizeNodesInOrder(e *BufferedEmitter, order []int, yield func(*Node)) *uidVec {
	if e.finalized {
		ThrowFmt("finalize: emitter already finalized")
	}

	n := len(e.nodes)

	if len(order) != n {
		ThrowFmt("finalize: order length %d does not match buffer size %d", len(order), n)
	}

	uids := &uidVec{}
	uidScratch := canonBuf{fs: e.fs, uids: uids}

	for _, i := range order {
		node := e.nodes[i]
		uids.set(int64(i), resolveAndUID(node, uids, &uidScratch))

		if yield != nil {
			yield(node)
		}
	}

	e.finalized = true

	if e.readyCh != nil {
		close(e.readyCh)
	}

	return uids
}

func finalizeOrder(e *BufferedEmitter) []int {
	if e.finalized {
		ThrowFmt("finalize: emitter already finalized")
	}

	n := len(e.nodes)

	checkRef := func(owner int, r NodeRef) {
		if r.id < 0 || r.id >= int64(n) {
			ThrowFmt("node %d references out-of-range NodeRef id=%d (buffer size %d)", owner, r.id, n)
		}
	}

	for i, node := range e.nodes {
		for _, r := range node.DepRefs {
			checkRef(i, r)
		}

		for _, r := range node.ForeignDepRefs {
			checkRef(i, r)
		}
	}

	for i, rid := range e.results {
		if rid < 0 || rid >= int64(n) {
			ThrowFmt("result %d references out-of-range NodeRef id=%d (buffer size %d)", i, rid, n)
		}
	}

	indeg := make([]int, n)

	children := make([][]int, n)
	addEdge := func(child, parent int) {
		children[child] = append(children[child], parent)
		indeg[parent]++
	}

	for i, node := range e.nodes {
		seen := make(map[int64]struct{})

		for _, r := range node.DepRefs {
			if _, ok := seen[r.id]; ok {
				continue
			}

			seen[r.id] = struct{}{}
			addEdge(int(r.id), i)
		}

		for _, r := range node.ForeignDepRefs {
			if _, ok := seen[r.id]; ok {
				continue
			}

			seen[r.id] = struct{}{}
			addEdge(int(r.id), i)
		}
	}

	queue := make(intHeap, 0, n)

	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}

	heap.Init(&queue)

	order := make([]int, 0, n)

	for queue.Len() > 0 {
		i := heap.Pop(&queue).(int)
		order = append(order, i)

		for _, c := range children[i] {
			indeg[c]--

			if indeg[c] == 0 {
				heap.Push(&queue, c)
			}
		}
	}

	if len(order) != n {
		for i, d := range indeg {
			if d > 0 {
				ThrowFmt("cycle detected involving node %d", i)
			}
		}

		ThrowFmt("cycle detected (could not order all %d nodes; ordered %d)", n, len(order))
	}

	return order
}

func finalizeNodes(e *BufferedEmitter, yield func(*Node)) *uidVec {
	return finalizeNodesInOrder(e, finalizeOrder(e), yield)
}

// resolveAndUID computes a node's uid and stamps Sandboxing/SelfUID/StatsUID. It
// does NOT materialize Deps/ForeignDeps: the uid preimage resolves the node's
// DepRefs/ForeignDepRefs through uids (via uidScratch), and downstream consumers
// (the JSON writer and the executor) do the same direct id->uid lookup. DepRefs
// are kept on the node for that purpose. All of a node's deps are resolved before
// it reaches here, so uids.get(dep) is valid.
func resolveAndUID(node *Node, uids *uidVec, uidScratch *canonBuf) UID {
	uidScratch.uids = uids
	node.Sandboxing = true

	u := nodeUIDWithBuf(node, uidScratch)
	node.UID = u
	node.SelfUID = u
	node.StatsUID = nodeStatsUID(node, uidScratch)

	return u
}

type StreamingEmitter struct {
	nodes      []*Node
	uids       *uidVec
	resolved   []bool // resolved[id]: uids has the computed uid for id (gen-goroutine only)
	pendingIdx []int64
	pendingSet map[int64]bool
	results    []int64
	onNode     func(*Node, *uidVec)
	finalized  bool
	readyCh    chan struct{}
	uidScratch canonBuf
}

func NewStreamingEmitter(onNode func(*Node, *uidVec)) *StreamingEmitter {
	return &StreamingEmitter{
		uids:       &uidVec{},
		pendingSet: map[int64]bool{},
		onNode:     onNode,
		readyCh:    make(chan struct{}),
	}
}

func (e *StreamingEmitter) Emit(n *Node) NodeRef {
	if e.finalized {
		panic("StreamingEmitter.Emit called after Finish")
	}

	id := int64(len(e.nodes))
	e.nodes = append(e.nodes, n)
	e.resolved = append(e.resolved, false)

	if e.hasUnresolvedDeps(n) {
		e.pendingSet[id] = true
		e.pendingIdx = append(e.pendingIdx, id)
		return NodeRef{id: id}
	}

	e.uids.set(id, resolveAndUID(n, e.uids, &e.uidScratch))
	e.resolved[id] = true

	if e.onNode != nil {
		e.onNode(n, e.uids)
	}

	return NodeRef{id: id}
}

func (e *StreamingEmitter) hasUnresolvedDeps(n *Node) bool {
	for _, r := range n.DepRefs {
		if !e.resolved[r.id] {
			return true
		}
	}

	for _, r := range n.ForeignDepRefs {
		if !e.resolved[r.id] {
			return true
		}
	}

	return false
}

func (e *StreamingEmitter) Result(r NodeRef) {
	if e.finalized {
		panic("StreamingEmitter.Result called after Finish")
	}

	e.results = append(e.results, r.id)
}
func (e *StreamingEmitter) OnReady(_ NodeRef) <-chan struct{} {
	return e.readyCh
}

func (e *StreamingEmitter) Finish() []UID {
	if e.finalized {
		panic("StreamingEmitter.Finish called twice")
	}

	for _, id := range e.pendingIdx {
		n := e.nodes[id]
		e.uids.set(id, resolveAndUID(n, e.uids, &e.uidScratch))
		e.resolved[id] = true

		if e.onNode != nil {
			e.onNode(n, e.uids)
		}
	}

	e.finalized = true
	close(e.readyCh)

	results := make([]UID, 0, len(e.results))
	seen := map[UID]struct{}{}

	for _, rid := range e.results {
		u := e.uids.get(rid)

		if _, ok := seen[u]; ok {
			continue
		}

		seen[u] = struct{}{}
		results = append(results, u)
	}

	return results
}

func graphFromFinalizedEmitter(e *BufferedEmitter, uids *uidVec) *Graph {
	n := len(e.nodes)

	out := &Graph{
		Conf:   map[string]interface{}{},
		Inputs: map[string]interface{}{},
		Graph:  make([]*Node, 0, n),
		Result: make([]UID, 0, len(e.results)),
		uids:   uids,
	}

	seenResult := map[UID]struct{}{}

	for _, rid := range e.results {
		u := uids.get(rid)

		if _, ok := seenResult[u]; ok {
			continue
		}

		seenResult[u] = struct{}{}
		out.Result = append(out.Result, u)
	}

	// DFS the dep DAG by node id (following DepRefs directly), deduping by uid so
	// each distinct content-address appears once. Graph order is irrelevant —
	// downstream `ay dump sort` re-sorts.
	seenNode := make(map[UID]struct{}, n)
	var dfsVisit func(id int64)
	dfsVisit = func(id int64) {
		node := e.nodes[id]
		u := uids.get(id)

		if _, ok := seenNode[u]; ok {
			return
		}

		seenNode[u] = struct{}{}
		out.Graph = append(out.Graph, node)

		for _, r := range node.DepRefs {
			dfsVisit(r.id)
		}
	}

	for _, rid := range e.results {
		dfsVisit(rid)
	}

	for i := range e.nodes {
		dfsVisit(int64(i))
	}

	return out
}

func finalizeGraphInOrder(e *BufferedEmitter, order []int) *Graph {
	return graphFromFinalizedEmitter(e, finalizeNodesInOrder(e, order, nil))
}

func Finalize(e *BufferedEmitter) *Graph {
	return graphFromFinalizedEmitter(e, finalizeNodes(e, nil))
}

func applyGraphConf(g *Graph, conf *graphConf) {
	if conf == nil || len(conf.Resources) == 0 {
		return
	}

	g.Conf = map[string]interface{}{"resources": conf.Resources}
}
