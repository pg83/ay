package main

import (
	"fmt"
	"os"
)

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
	nodes      Vec[*Node]
	resolved   BitSet
	pendingIdx []NodeRef
	pendingSet map[NodeRef]bool
	results    []NodeRef
	onNode     func(*Node, *DenseMap[STR, NodeRef])
	finalized  bool
	na         *NodeArenas
	fetchRefs  *DenseMap[STR, NodeRef]
}

func newStreamingEmitter(onNode func(*Node, *DenseMap[STR, NodeRef])) *StreamingEmitter {
	return &StreamingEmitter{
		pendingSet: map[NodeRef]bool{},
		onNode:     onNode,
		na:         newStrictNodeArenas(),
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

	id := NodeRef(e.nodes.len())

	e.nodes.pushBack(nil)

	return id
}

func (e *StreamingEmitter) emitReserved(n *Node, id NodeRef) {
	if e.finalized {
		panic("StreamingEmitter.emitReserved called after Finish")
	}

	if e.nodes.s[id] != nil {
		throwFmt("emitReserved: slot %d already filled", id)
	}

	n.Ref = id
	e.nodes.s[id] = n
	e.resolveOrPend(n, id)
}

func (e *StreamingEmitter) newNode() *Node {
	return e.na.nodes.one()
}

func (e *StreamingEmitter) emitNode(n Node) NodeRef {
	ownershipCheckNode(&n)

	p := e.na.nodes.one()

	*p = n

	return e.emit(p)
}

func (e *StreamingEmitter) emitReservedNode(n Node, id NodeRef) {
	ownershipCheckNode(&n)

	p := e.na.nodes.one()

	*p = n

	e.emitReserved(p, id)
}

func (e *StreamingEmitter) emit(n *Node) NodeRef {
	if e.finalized {
		panic("StreamingEmitter.Emit called after Finish")
	}

	id := NodeRef(e.nodes.len())

	n.Ref = id
	e.nodes.pushBack(n)
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

	pending := e.pendingIdx

	for len(pending) > 0 {
		next := pending[:0]

		for _, id := range pending {
			n := e.nodes.s[id]

			if e.hasUnresolvedDeps(n) {
				next = append(next, id)

				continue
			}

			e.resolved.add(uint32(id))

			if e.onNode != nil {
				e.onNode(n, e.fetchRefs)
			}
		}

		if len(next) == len(pending) {
			if os.Getenv("AY_DEBUG_PENDING") != "" {
				for _, id := range next {
					n := e.nodes.s[id]

					outs := ""

					if len(n.Outputs) > 0 {
						outs = n.Outputs[0].string()
					}

					unres := ""

					for _, d := range n.DepRefs {
						if !e.resolved.has(uint32(d)) {
							unres += " " + fmt.Sprint(uint32(d))

							if e.nodes.s[d] == nil {
								unres += "(nil-slot)"
							}
						}
					}

					fmt.Fprintf(os.Stderr, "pending node %d out=%s unresolved deps:%s\n", uint32(id), outs, unres)
				}
			}

			throwFmt("finish: %d pending node(s) form a dependency cycle", len(next))
		}

		pending = next
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
		Graph:     e.nodes.s,
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
