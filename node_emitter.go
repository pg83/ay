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
	addModulesToResults      bool
	stripPackagesFromResults bool
	nodes                    Vec[*Node]
	modules                  BitSet
	resolved                 BitSet
	results                  []NodeRef
	deferPublication         int
	publicationRoots         []NodeRef
	exportEligible           BitSet
	onNode                   func(*Node, InputChunks, *DenseMap[STR, NodeRef])
	na                       *NodeArenas
	fetchRefs                *DenseMap[STR, NodeRef]
}

func newStreamingEmitter(onNode func(*Node, InputChunks, *DenseMap[STR, NodeRef])) *StreamingEmitter {
	return &StreamingEmitter{
		onNode:    onNode,
		na:        newStrictNodeArenas(),
		fetchRefs: &DenseMap[STR, NodeRef]{},
	}
}

func (e *StreamingEmitter) nodeArenas() *NodeArenas {
	return e.na
}

func (e *StreamingEmitter) reserve() NodeRef {
	id := NodeRef(e.nodes.len())

	e.nodes.pushBack(nil)

	return id
}

func (e *StreamingEmitter) emitReserved(n *Node, id NodeRef) {
	n.Ref = id
	e.nodes.s[id] = n
	e.resolveOrPend(n, id)
}

func (e *StreamingEmitter) emitNode(n NodeSpec) NodeRef {
	return e.emitNodePtr(&n.Node)
}

func (e *StreamingEmitter) emitNodePtr(n *Node) NodeRef {
	ownershipCheckNode(n)

	p := e.na.nodes.one()

	*p = *n

	return e.emit(p)
}

func (e *StreamingEmitter) emitReservedNode(n NodeSpec, id NodeRef) {
	e.emitReservedNodePtr(&n.Node, id)
}

func (e *StreamingEmitter) emitReservedNodePtr(n *Node, id NodeRef) {
	ownershipCheckNode(n)

	p := e.nodes.s[id]

	if p == nil {
		p = e.na.nodes.one()
	}

	*p = *n

	e.emitReserved(p, id)
}

func (e *StreamingEmitter) emit(n *Node) NodeRef {
	id := NodeRef(e.nodes.len())

	n.Ref = id
	e.nodes.pushBack(n)
	e.resolveOrPend(n, id)

	return id
}

func (e *StreamingEmitter) resolveOrPend(n *Node, id NodeRef) {
	assert(len(n.Outputs) > 0, "emitter: node without outputs")
	e.commandResources(n)

	if e.deferPublication == 0 {
		e.publicationRoots = append(e.publicationRoots, id)
	}
}

func (e *StreamingEmitter) emitResolved(n *Node, id NodeRef) {
	e.resolved.add(uint32(id))

	if e.onNode != nil {
		e.onNode(n, n.Inputs, e.fetchRefs)
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
	e.results = append(e.results, r)
}

func (ctx *GenCtx) activateSelectedInputs(selected BitSet, stack []NodeRef) BitSet {
	e := ctx.emit

	selectProducer := func(ref NodeRef) {
		stack = append(stack, ref)
	}

	for len(stack) > 0 {
		ref := stack[len(stack)-1]

		stack = stack[:len(stack)-1]

		node := e.nodes.s[ref]

		if node == nil {
			continue
		}

		if ctx.hasPeerInputFixes(node) {
			ctx.activateNodeInputs(node, selectProducer, true)
		}

		if selected.has(uint32(ref)) {
			continue
		}

		selected.add(uint32(ref))
		ctx.activateNodeInputs(node, selectProducer, false)
		stack = append(stack, node.DepRefs...)

		for _, dependency := range node.ForeignDepRefs {
			if target := ctx.foreignToolOf(dependency); target != nil && target.ForeignTool.host != nil {
				dependency = target.ForeignTool.host.LDRef
			}

			selectProducer(dependency)
		}

		for _, resource := range node.Resources {
			if dependency, ok := node.resourceRef(resource, e.fetchRefs); ok {
				selectProducer(dependency)
			}
		}
	}

	return selected
}

func (e *StreamingEmitter) finish() []NodeRef {
	if e.stripPackagesFromResults {
		e.stripPackageResults()
	}

	e.exportEligible = e.reachableNodes(e.publicationFrontier())

	pending := make([]NodeRef, 0, e.nodes.len())

	for _, node := range e.nodes.s {
		if node != nil && e.exportEligible.has(uint32(node.Ref)) {
			pending = append(pending, node.Ref)
		}
	}

	for len(pending) > 0 {
		next := pending[:0]

		for _, id := range pending {
			n := e.nodes.s[id]

			if e.hasUnresolvedDeps(n) {
				next = append(next, id)

				continue
			}

			e.emitResolved(n, id)
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

	if e.addModulesToResults {
		for _, n := range e.nodes.s {
			if n != nil && e.exportEligible.has(uint32(n.Ref)) && (n.Module || e.modules.has(uint32(n.Ref))) {
				e.result(n.Ref)
			}
		}
	}

	return dedupResultRefs(e.results)
}

func (e *StreamingEmitter) stripPackageResults() {
	results := e.results[:0]

	for _, ref := range e.results {
		node := e.nodes.s[ref]

		if node != nil && (node.KV == nil || node.KV.P != pkPK && node.KV.P != pkUN) {
			results = append(results, ref)
		}
	}

	e.results = results

	stack := append([]NodeRef(nil), results...)

	var live BitSet

	for len(stack) > 0 {
		ref := stack[len(stack)-1]

		stack = stack[:len(stack)-1]

		if live.has(uint32(ref)) {
			continue
		}

		live.add(uint32(ref))

		for dependency := range e.nodes.s[ref].buildDeps(e.fetchRefs) {
			stack = append(stack, dependency)
		}
	}

	for i := range e.nodes.s {
		if !live.has(uint32(i)) {
			e.nodes.s[i] = nil
		}
	}
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

func (e *StreamingEmitter) pendingPublication() *StreamingEmitter {
	if e.deferPublication > 0 {
		return e
	}

	return nil
}

func (e *StreamingEmitter) publicationFrontier() []NodeRef {
	roots := append([]NodeRef(nil), e.publicationRoots...)

	return append(roots, e.results...)
}

func graphFromEmitter(e *StreamingEmitter) *Graph {
	nodes := make([]*Node, 0, e.nodes.len())

	for _, node := range e.nodes.s {
		if node != nil && e.exportEligible.has(uint32(node.Ref)) {
			nodes = append(nodes, node)
		}
	}

	return &Graph{
		Inputs:    map[string]interface{}{},
		Graph:     nodes,
		Result:    dedupResultRefs(e.results),
		fetchRefs: e.fetchRefs,
	}
}

func finalize(e *StreamingEmitter) *Graph {
	e.finish()

	return graphFromEmitter(e)
}
