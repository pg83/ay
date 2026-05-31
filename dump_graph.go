package main

import "strings"

func finalizeDumpGraph(e *BufferedEmitter) *Graph {
	if e == nil {
		return &Graph{
			Conf:   map[string]interface{}{},
			Inputs: map[string]interface{}{},
			Graph:  []*Node{},
			Result: []string{},
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
			incoming[int(dep.id)]++
		}

		for _, deps := range node.ForeignDepRefs {
			for _, dep := range deps {
				incoming[int(dep.id)]++
			}
		}
	}

	return incoming
}
func dumpGraphResultRefs(results []int64) map[int64]struct{} {
	if len(results) == 0 {
		return nil
	}

	out := make(map[int64]struct{}, len(results))

	for _, refID := range results {
		out[refID] = struct{}{}
	}

	return out
}
func dumpGraphDropNodeRefs(nodes []*Node, incoming []int, results map[int64]struct{}) map[int64]struct{} {
	drop := make(map[int64]struct{})
	queue := make([]int, 0, len(nodes))

	maybeDrop := func(nodeID int) {
		refID := int64(nodeID)

		if _, ok := drop[refID]; ok {
			return
		}

		if _, ok := results[refID]; ok {
			return
		}

		switch {
		case isDumpGraphResourceFetchNode(nodes[nodeID]):
			drop[refID] = struct{}{}
			queue = append(queue, nodeID)
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

		for _, deps := range node.ForeignDepRefs {
			dumpGraphDropNodeDeps(deps, drop, incoming, maybeDrop)
		}
	}

	return drop
}

func isDumpGraphResourceFetchNode(node *Node) bool {
	if dumpGraphNodeKind(node) != "FETCH" || len(node.Outputs) == 0 {
		return false
	}

	for _, out := range node.Outputs {
		if !out.IsBuild() || !strings.HasPrefix(out.Rel(), "resources/") {
			return false
		}
	}

	return true
}

func isDumpGraphStandaloneLLVMPRNode(node *Node, nodeID int, incoming []int) bool {
	if dumpGraphNodeKind(node) != "PR" {
		return false
	}

	if incoming[nodeID] != 0 {
		return false
	}

	if node.TargetProperties["module_dir"] != "contrib/libs/llvm16/include" {
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
func dumpGraphDropNodeDeps(refs []NodeRef, drop map[int64]struct{}, incoming []int, maybeDrop func(int)) {
	for _, ref := range refs {
		if _, ok := drop[ref.id]; ok {
			continue
		}

		refID := int(ref.id)

		if incoming[refID] == 0 {
			ThrowFmt("finalizeDumpGraph: incoming ref count underflow at id=%d", ref.id)
		}

		incoming[refID]--
		maybeDrop(refID)
	}
}

func dumpGraphNodeKind(node *Node) string {
	if node == nil {
		return ""
	}

	kind, _ := node.KV["p"].(string)
	return kind
}
func pruneDumpGraphEmitterInPlace(e *BufferedEmitter, drop map[int64]struct{}, order []int) []int {
	origNodes := e.nodes
	keptNodes := make([]*Node, 0, len(origNodes)-len(drop))
	newIDs := make([]int64, len(origNodes))

	for i := range newIDs {
		newIDs[i] = -1
	}

	for oldID, node := range origNodes {
		if _, ok := drop[int64(oldID)]; ok {
			continue
		}

		newIDs[oldID] = int64(len(keptNodes))
		keptNodes = append(keptNodes, node)
	}

	for oldID, node := range origNodes {
		if _, ok := drop[int64(oldID)]; ok {
			continue
		}

		node.Deps = nil
		node.ForeignDeps = nil
		node.UID = ""
		node.SelfUID = ""
		node.StatsUID = ""
		node.DepRefs = trimDumpGraphNodeRefList(node.DepRefs, drop, newIDs)
		node.ForeignDepRefs = trimDumpGraphForeignDepRefs(node.ForeignDepRefs, drop, newIDs)
	}

	e.nodes = keptNodes
	e.results = trimDumpGraphResultRefs(e.results, newIDs)

	return remapDumpGraphOrder(order, drop, newIDs)
}
func trimDumpGraphNodeRefList(in []NodeRef, drop map[int64]struct{}, newIDs []int64) []NodeRef {
	if len(in) == 0 {
		return nil
	}

	out := make([]NodeRef, 0, len(in))

	for _, ref := range in {
		if _, ok := drop[ref.id]; ok {
			continue
		}

		newID := newIDs[int(ref.id)]

		if newID < 0 {
			ThrowFmt("finalizeDumpGraph: kept ref id=%d missing after prune", ref.id)
		}

		out = append(out, NodeRef{id: newID})
	}

	if len(out) == 0 {
		return nil
	}

	return out
}
func trimDumpGraphForeignDepRefs(in map[string][]NodeRef, drop map[int64]struct{}, newIDs []int64) map[string][]NodeRef {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string][]NodeRef, len(in))

	for key, deps := range in {
		trimmed := trimDumpGraphNodeRefList(deps, drop, newIDs)

		if len(trimmed) == 0 {
			continue
		}

		out[key] = trimmed
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func trimDumpGraphResultRefs(in []int64, newIDs []int64) []int64 {
	if len(in) == 0 {
		return nil
	}

	out := make([]int64, 0, len(in))

	for _, refID := range in {
		newID := newIDs[int(refID)]

		if newID < 0 {
			ThrowFmt("finalizeDumpGraph: result ref id=%d missing after prune", refID)
		}

		out = append(out, newID)
	}

	return out
}
func remapDumpGraphOrder(order []int, drop map[int64]struct{}, newIDs []int64) []int {
	out := make([]int, 0, len(order)-len(drop))

	for _, oldID := range order {
		if _, ok := drop[int64(oldID)]; ok {
			continue
		}

		newID := newIDs[oldID]

		if newID < 0 {
			ThrowFmt("finalizeDumpGraph: kept order id=%d missing after prune", oldID)
		}

		out = append(out, int(newID))
	}

	return out
}
