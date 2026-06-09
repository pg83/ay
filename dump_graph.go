package main

// nodeRefDropped marks an old node id that was pruned (no kept remapping).
const nodeRefDropped = ^NodeRef(0)

func finalizeDumpGraph(e *BufferedEmitter) *Graph {
	if e == nil {
		return &Graph{
			Inputs: map[string]interface{}{},
			Graph:  []*Node{},
			Result: []UID{},
		}
	}

	if len(e.nodes) == 0 {
		return Finalize(e)
	}

	overrideGeneratedModuleDir(e)

	order := finalizeOrder(e)

	incoming := dumpGraphIncomingRefs(e.nodes)
	results := dumpGraphResultRefs(e.results)
	drop := dumpGraphDropNodeRefs(e.nodes, incoming, results)

	if len(drop) == 0 {
		return finalizeGraphInOrder(e, order)
	}

	return finalizeGraphInOrder(e, pruneDumpGraphEmitterInPlace(e, drop, order))
}

func dumpGraphIncomingRefs(nodes []*Node) []int {
	incoming := make([]int, len(nodes))

	for _, node := range nodes {
		for _, dep := range node.DepRefs {
			incoming[int(dep)]++
		}

		for _, dep := range node.ForeignDepRefs {
			incoming[int(dep)]++
		}
	}

	return incoming
}

func dumpGraphResultRefs(results []NodeRef) map[NodeRef]struct{} {
	if len(results) == 0 {
		return nil
	}

	out := make(map[NodeRef]struct{}, len(results))

	for _, refID := range results {
		out[refID] = struct{}{}
	}

	return out
}

func dumpGraphDropNodeRefs(nodes []*Node, incoming []int, results map[NodeRef]struct{}) map[NodeRef]struct{} {
	drop := make(map[NodeRef]struct{})
	queue := make([]int, 0, len(nodes))

	maybeDrop := func(nodeID int) {
		refID := NodeRef(nodeID)

		if _, ok := drop[refID]; ok {
			return
		}

		if _, ok := results[refID]; ok {
			return
		}

		switch {
		// Resource FETCH nodes (CLANG, …) are NOT dropped here: -G must emit the
		// same graph that gets executed. They are folded out later, only in
		// `dump normalize`, for the byte-exact comparison against upstream.
		case isDumpGraphStandaloneLLVMPRNode(nodes[nodeID], nodeID, incoming):
			drop[refID] = struct{}{}
			queue = append(queue, nodeID)
		}
	}

	for nodeID := range nodes {
		maybeDrop(nodeID)
	}

	for head := 0; head < len(queue); head++ {
		node := nodes[queue[head]]
		dumpGraphDropNodeDeps(node.DepRefs, drop, incoming, maybeDrop)
		dumpGraphDropNodeDeps(node.ForeignDepRefs, drop, incoming, maybeDrop)
	}

	return drop
}

func isDumpGraphStandaloneLLVMPRNode(node *Node, nodeID int, incoming []int) bool {
	if dumpGraphNodeKind(node) != "PR" {
		return false
	}

	if incoming[nodeID] != 0 {
		return false
	}

	if node.TargetProperties.ModuleDir != "contrib/libs/llvm16/include" {
		return false
	}

	if len(node.Outputs) == 0 {
		return false
	}

	for _, out := range node.Outputs {
		if isCCSourceExt(out.Rel()) {
			return false
		}
	}

	return true
}

func dumpGraphDropNodeDeps(refs []NodeRef, drop map[NodeRef]struct{}, incoming []int, maybeDrop func(int)) {
	for _, ref := range refs {
		if _, ok := drop[ref]; ok {
			continue
		}

		refID := int(ref)

		if incoming[refID] == 0 {
			ThrowFmt("finalizeDumpGraph: incoming ref count underflow at id=%d", ref)
		}

		incoming[refID]--
		maybeDrop(refID)
	}
}

func dumpGraphNodeKind(node *Node) string {
	if node == nil {
		return ""
	}

	return node.KV.P.String()
}

func pruneDumpGraphEmitterInPlace(e *BufferedEmitter, drop map[NodeRef]struct{}, order []int) []int {
	origNodes := e.nodes
	keptNodes := make([]*Node, 0, len(origNodes)-len(drop))
	newIDs := make([]NodeRef, len(origNodes))

	for i := range newIDs {
		newIDs[i] = nodeRefDropped
	}

	for oldID, node := range origNodes {
		if _, ok := drop[NodeRef(oldID)]; ok {
			continue
		}

		newIDs[oldID] = NodeRef(len(keptNodes))
		keptNodes = append(keptNodes, node)
	}

	for oldID, node := range origNodes {
		if _, ok := drop[NodeRef(oldID)]; ok {
			continue
		}

		node.UID = UID{}
		node.SelfUID = UID{}
		node.DepRefs = trimDumpGraphNodeRefList(node.DepRefs, drop, newIDs)
		node.ForeignDepRefs = trimDumpGraphNodeRefList(node.ForeignDepRefs, drop, newIDs)
	}

	e.nodes = keptNodes
	e.results = trimDumpGraphResultRefs(e.results, newIDs)

	return remapDumpGraphOrder(order, drop, newIDs)
}

func trimDumpGraphNodeRefList(in []NodeRef, drop map[NodeRef]struct{}, newIDs []NodeRef) []NodeRef {
	if len(in) == 0 {
		return nil
	}

	out := make([]NodeRef, 0, len(in))

	for _, ref := range in {
		if _, ok := drop[ref]; ok {
			continue
		}

		newID := newIDs[int(ref)]

		if newID == nodeRefDropped {
			ThrowFmt("finalizeDumpGraph: kept ref id=%d missing after prune", ref)
		}

		out = append(out, newID)
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func trimDumpGraphResultRefs(in []NodeRef, newIDs []NodeRef) []NodeRef {
	if len(in) == 0 {
		return nil
	}

	out := make([]NodeRef, 0, len(in))

	for _, refID := range in {
		newID := newIDs[int(refID)]

		if newID == nodeRefDropped {
			ThrowFmt("finalizeDumpGraph: result ref id=%d missing after prune", refID)
		}

		out = append(out, newID)
	}

	return out
}

func remapDumpGraphOrder(order []int, drop map[NodeRef]struct{}, newIDs []NodeRef) []int {
	out := make([]int, 0, len(order)-len(drop))

	for _, oldID := range order {
		if _, ok := drop[NodeRef(oldID)]; ok {
			continue
		}

		newID := newIDs[oldID]

		if newID == nodeRefDropped {
			ThrowFmt("finalizeDumpGraph: kept order id=%d missing after prune", oldID)
		}

		out = append(out, int(newID))
	}

	return out
}
