package main

import (
	"container/heap"
)

type NodeRef uint32

type Emitter interface {
	emit(n *Node) NodeRef

	reserve() NodeRef

	emitReserved(n *Node, id NodeRef)
	result(NodeRef)
	onReady(NodeRef) <-chan struct{}

	nodeArenas() *NodeArenas
}

type BufferedEmitter struct {
	nodes     []*Node
	results   []NodeRef
	finalized bool

	fs FS

	fetchRefs *DenseMap[STR, NodeRef]
	readyCh   chan struct{}
	na        *NodeArenas

	reserved int
}

func newBufferedEmitter() *BufferedEmitter {
	return &BufferedEmitter{
		readyCh:   make(chan struct{}),
		na:        newNodeArenas(),
		fetchRefs: &DenseMap[STR, NodeRef]{},
	}
}

func (e *BufferedEmitter) nodeArenas() *NodeArenas {
	return e.na
}

func (e *BufferedEmitter) reserve() NodeRef {
	if e.finalized {
		panic("BufferedEmitter.reserve called after Finalize")
	}

	id := NodeRef(len(e.nodes))
	e.nodes = append(e.nodes, nil)
	e.reserved++

	return id
}

func (e *BufferedEmitter) emitReserved(n *Node, id NodeRef) {
	if e.finalized {
		panic("BufferedEmitter.emitReserved called after Finalize")
	}

	if e.nodes[id] != nil {
		throwFmt("emitReserved: slot %d already filled", id)
	}

	e.nodes[id] = n
	e.reserved--
}

func (e *BufferedEmitter) onReady(_ NodeRef) <-chan struct{} {
	return e.readyCh
}

func (e *BufferedEmitter) emit(n *Node) NodeRef {
	if e.finalized {
		panic("BufferedEmitter.Emit called after Finalize")
	}

	id := NodeRef(len(e.nodes))
	e.nodes = append(e.nodes, n)

	return id
}

func (e *BufferedEmitter) result(r NodeRef) {
	if e.finalized {
		panic("BufferedEmitter.Result called after Finalize")
	}

	e.results = append(e.results, r)
}

type Graph struct {
	Graph  []*Node                `json:"graph"`
	Inputs map[string]interface{} `json:"inputs"`
	Result []UID                  `json:"result"`

	uids *UidVec `json:"-"`

	fetchRefs *DenseMap[STR, NodeRef] `json:"-"`
}

func finalizeNodesInOrder(e *BufferedEmitter, order []int, yield func(*Node)) *UidVec {
	if e.finalized {
		throwFmt("finalize: emitter already finalized")
	}

	n := len(e.nodes)

	if len(order) != n {
		throwFmt("finalize: order length %d does not match buffer size %d", len(order), n)
	}

	uids := &UidVec{}
	uidScratch := CanonBuf{fs: e.fs, uids: uids, fetchRefs: e.fetchRefs}

	for _, i := range order {
		node := e.nodes[i]
		uids.set(NodeRef(i), resolveAndUID(node, uids, &uidScratch))

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
		throwFmt("finalize: emitter already finalized")
	}

	if e.reserved != 0 {
		throwFmt("finalize: %d reserved node slot(s) left unfilled", e.reserved)
	}

	n := len(e.nodes)

	checkRef := func(owner int, r NodeRef) {
		if int(r) >= n {
			throwFmt("node %d references out-of-range NodeRef id=%d (buffer size %d)", owner, r, n)
		}
	}

	for i, node := range e.nodes {
		for r := range node.buildDeps(e.fetchRefs) {
			checkRef(i, r)
		}
	}

	for i, rid := range e.results {
		if int(rid) >= n {
			throwFmt("result %d references out-of-range NodeRef id=%d (buffer size %d)", i, rid, n)
		}
	}

	indeg := make([]int, n)

	children := make([][]int, n)
	addEdge := func(child, parent int) {
		children[child] = append(children[child], parent)
		indeg[parent]++
	}

	for i, node := range e.nodes {
		seen := make(map[NodeRef]struct{})

		for r := range node.buildDeps(e.fetchRefs) {
			if _, ok := seen[r]; ok {
				continue
			}

			seen[r] = struct{}{}
			addEdge(int(r), i)
		}
	}

	queue := make(IntHeap, 0, n)

	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}

	heap.Init(&queue)

	order := make([]int, 0, n)

	for queue.len() > 0 {
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
				throwFmt("cycle detected involving node %d", i)
			}
		}

		throwFmt("cycle detected (could not order all %d nodes; ordered %d)", n, len(order))
	}

	return order
}

func finalizeNodes(e *BufferedEmitter, yield func(*Node)) *UidVec {
	return finalizeNodesInOrder(e, finalizeOrder(e), yield)
}

func resolveAndUID(node *Node, uids *UidVec, uidScratch *CanonBuf) UID {
	node.Sandboxing = true

	if node.UID != (UID{}) {
		node.SelfUID = node.UID

		return node.UID
	}

	uidScratch.uids = uids

	u := nodeUIDWithBuf(node, uidScratch)
	node.UID = u
	node.SelfUID = u

	return u
}

type StreamingEmitter struct {
	nodes      []*Node
	uids       *UidVec
	resolved   BitSet
	pendingIdx []NodeRef
	pendingSet map[NodeRef]bool
	results    []NodeRef
	onNode     func(*Node, *UidVec, *DenseMap[STR, NodeRef])
	finalized  bool
	readyCh    chan struct{}
	uidScratch CanonBuf
	na         *NodeArenas

	fetchRefs *DenseMap[STR, NodeRef]

	reserved int
}

func newStreamingEmitter(onNode func(*Node, *UidVec, *DenseMap[STR, NodeRef])) *StreamingEmitter {
	return &StreamingEmitter{
		uids:       &UidVec{},
		pendingSet: map[NodeRef]bool{},
		onNode:     onNode,
		readyCh:    make(chan struct{}),
		na:         newNodeArenas(),
		fetchRefs:  &DenseMap[STR, NodeRef]{},
	}
}

func (e *StreamingEmitter) nodeArenas() *NodeArenas {
	return e.na
}

func (e *StreamingEmitter) reserve() NodeRef {
	if e.finalized {
		panic("StreamingEmitter.reserve called after Finish")
	}

	id := NodeRef(len(e.nodes))
	e.nodes = append(e.nodes, nil)
	e.reserved++

	return id
}

func (e *StreamingEmitter) emitReserved(n *Node, id NodeRef) {
	if e.finalized {
		panic("StreamingEmitter.emitReserved called after Finish")
	}

	if e.nodes[id] != nil {
		throwFmt("emitReserved: slot %d already filled", id)
	}

	e.nodes[id] = n
	e.reserved--
	e.resolveOrPend(n, id)
}

func (e *StreamingEmitter) emit(n *Node) NodeRef {
	if e.finalized {
		panic("StreamingEmitter.Emit called after Finish")
	}

	id := NodeRef(len(e.nodes))
	e.nodes = append(e.nodes, n)
	e.resolveOrPend(n, id)

	return id
}

func (e *StreamingEmitter) resolveOrPend(n *Node, id NodeRef) {
	if e.hasUnresolvedDeps(n) {
		e.pendingSet[id] = true
		e.pendingIdx = append(e.pendingIdx, id)

		return
	}

	e.uids.set(id, resolveAndUID(n, e.uids, &e.uidScratch))
	e.resolved.add(uint32(id))

	if e.onNode != nil {
		e.onNode(n, e.uids, e.fetchRefs)
	}
}

func (e *StreamingEmitter) hasUnresolvedDeps(n *Node) bool {
	for r := range n.buildDeps(e.fetchRefs) {
		if !e.resolved.has(uint32(r)) {
			return true
		}
	}

	return false
}

func (e *StreamingEmitter) result(r NodeRef) {
	if e.finalized {
		panic("StreamingEmitter.Result called after Finish")
	}

	e.results = append(e.results, r)
}

func (e *StreamingEmitter) onReady(_ NodeRef) <-chan struct{} {
	return e.readyCh
}

func (e *StreamingEmitter) finish() []UID {
	if e.finalized {
		panic("StreamingEmitter.Finish called twice")
	}

	if e.reserved != 0 {
		throwFmt("finish: %d reserved node slot(s) left unfilled", e.reserved)
	}

	for _, id := range e.pendingIdx {
		n := e.nodes[id]
		e.uids.set(id, resolveAndUID(n, e.uids, &e.uidScratch))
		e.resolved.add(uint32(id))

		if e.onNode != nil {
			e.onNode(n, e.uids, e.fetchRefs)
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

func graphFromFinalizedEmitter(e *BufferedEmitter, uids *UidVec) *Graph {
	n := len(e.nodes)

	out := &Graph{
		Inputs:    map[string]interface{}{},
		Graph:     make([]*Node, 0, n),
		Result:    make([]UID, 0, len(e.results)),
		uids:      uids,
		fetchRefs: e.fetchRefs,
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

	seenNode := make(map[UID]struct{}, n)
	var dfsVisit func(id NodeRef)
	dfsVisit = func(id NodeRef) {
		node := e.nodes[id]
		u := uids.get(id)

		if _, ok := seenNode[u]; ok {
			return
		}

		seenNode[u] = struct{}{}
		out.Graph = append(out.Graph, node)

		for _, r := range node.DepRefs {
			dfsVisit(r)
		}
	}

	for _, rid := range e.results {
		dfsVisit(rid)
	}

	for i := range e.nodes {
		dfsVisit(NodeRef(i))
	}

	return out
}

func finalizeGraphInOrder(e *BufferedEmitter, order []int) *Graph {
	return graphFromFinalizedEmitter(e, finalizeNodesInOrder(e, order, nil))
}

func finalize(e *BufferedEmitter) *Graph {
	return graphFromFinalizedEmitter(e, finalizeNodes(e, nil))
}
