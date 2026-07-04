package main

type NodeRef uint32

func (r NodeRef) strID() uint32 {
	return uint32(r)
}

type Graph struct {
	Graph     []*Node                 `json:"graph"`
	Inputs    map[string]interface{}  `json:"inputs"`
	Result    []NodeRef               `json:"result"`
	fetchRefs *DenseMap[STR, NodeRef] `json:"-"`
}

type StreamingEmitter struct {
	nodes      []*Node
	resolved   BitSet
	pendingIdx []NodeRef
	pendingSet map[NodeRef]bool
	results    []NodeRef
	onNode     func(*Node, *DenseMap[STR, NodeRef])
	finalized  bool
	na         *NodeArenas
	fetchRefs  *DenseMap[STR, NodeRef]
	reserved   int
}

func newStreamingEmitter(onNode func(*Node, *DenseMap[STR, NodeRef])) *StreamingEmitter {
	return &StreamingEmitter{
		pendingSet: map[NodeRef]bool{},
		onNode:     onNode,
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

	n.Ref = id
	e.nodes[id] = n
	e.reserved--
	e.resolveOrPend(n, id)
}

func (e *StreamingEmitter) newNode() *Node {
	return e.na.nodes.one()
}

func (e *StreamingEmitter) emitNode(n Node) NodeRef {
	p := e.na.nodes.one()

	*p = n

	return e.emit(p)
}

func (e *StreamingEmitter) emitReservedNode(n Node, id NodeRef) {
	p := e.na.nodes.one()

	*p = n

	e.emitReserved(p, id)
}

func (e *StreamingEmitter) emit(n *Node) NodeRef {
	if e.finalized {
		panic("StreamingEmitter.Emit called after Finish")
	}

	id := NodeRef(len(e.nodes))

	n.Ref = id
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

	e.resolved.add(uint32(id))

	if e.onNode != nil {
		e.onNode(n, e.fetchRefs)
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

func (e *StreamingEmitter) finish() []NodeRef {
	if e.finalized {
		panic("StreamingEmitter.Finish called twice")
	}

	if e.reserved != 0 {
		throwFmt("finish: %d reserved node slot(s) left unfilled", e.reserved)
	}

	for _, id := range e.pendingIdx {
		n := e.nodes[id]

		e.resolved.add(uint32(id))

		if e.onNode != nil {
			e.onNode(n, e.fetchRefs)
		}
	}

	e.finalized = true

	return dedupResultRefs(e.results)
}

func dedupResultRefs(results []NodeRef) []NodeRef {
	out := make([]NodeRef, 0, len(results))
	seen := map[NodeRef]struct{}{}

	for _, r := range results {
		if _, ok := seen[r]; ok {
			continue
		}

		seen[r] = struct{}{}
		out = append(out, r)
	}

	return out
}

func graphFromEmitter(e *StreamingEmitter) *Graph {
	return &Graph{
		Inputs:    map[string]interface{}{},
		Graph:     e.nodes,
		Result:    dedupResultRefs(e.results),
		fetchRefs: e.fetchRefs,
	}
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
