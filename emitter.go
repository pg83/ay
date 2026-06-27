package main

type NodeRef uint32

type Graph struct {
	Graph     []*Node                 `json:"graph"`
	Inputs    map[string]interface{}  `json:"inputs"`
	Result    []UID                   `json:"result"`
	uids      *UidVec                 `json:"-"`
	fetchRefs *DenseMap[STR, NodeRef] `json:"-"`
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
	fs         FS
	uidBuf     []byte
	na         *NodeArenas
	fetchRefs  *DenseMap[STR, NodeRef]
	reserved   int
}

func newStreamingEmitter(fs FS, onNode func(*Node, *UidVec, *DenseMap[STR, NodeRef])) *StreamingEmitter {
	return &StreamingEmitter{
		uids:       &UidVec{},
		pendingSet: map[NodeRef]bool{},
		onNode:     onNode,
		readyCh:    make(chan struct{}),
		na:         newNodeArenas(),
		fetchRefs:  &DenseMap[STR, NodeRef]{},
		fs:         fs,
	}
}

func (e *StreamingEmitter) resolveAndUID(node *Node) UID {
	if node.UID != (UID{}) {
		node.SelfUID = node.UID

		return node.UID
	}

	u := CanonBuf{fs: e.fs, uids: e.uids, fetchRefs: e.fetchRefs, bufStore: &e.uidBuf}.calcUID(node)
	node.UID = u
	node.SelfUID = u

	return u
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

	e.uids.set(id, e.resolveAndUID(n))
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
		e.uids.set(id, e.resolveAndUID(n))
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

func graphFromEmitter(e *StreamingEmitter) *Graph {
	n := len(e.nodes)

	out := &Graph{
		Inputs:    map[string]interface{}{},
		Graph:     make([]*Node, 0, n),
		Result:    make([]UID, 0, len(e.results)),
		uids:      e.uids,
		fetchRefs: e.fetchRefs,
	}

	seenResult := map[UID]struct{}{}

	for _, rid := range e.results {
		u := e.uids.get(rid)

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
		u := e.uids.get(id)

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

func finalize(e *StreamingEmitter) *Graph {
	if e.finalized {
		throwFmt("finalize: emitter already finalized")
	}

	e.finish()

	return graphFromEmitter(e)
}

func finalizeDumpGraph(e *StreamingEmitter) *Graph {
	return finalize(e)
}
