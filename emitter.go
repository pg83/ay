package main

import (
	"container/heap"
	"sort"
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
	fs FS

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
	Result []string               `json:"result"`
}

func FinalizeStream(e *BufferedEmitter, yield func(*Node)) []string {
	uids := finalizeNodes(e, yield)

	results := make([]string, 0, len(e.results))
	seen := map[string]struct{}{}

	for _, rid := range e.results {
		u := uids[rid]
		if _, ok := seen[u]; ok {
			continue
		}

		seen[u] = struct{}{}
		results = append(results, u)
	}

	return results
}

func finalizeNodesInOrder(e *BufferedEmitter, order []int, yield func(*Node)) []string {
	if e.finalized {
		ThrowFmt("finalize: emitter already finalized")
	}

	n := len(e.nodes)
	if len(order) != n {
		ThrowFmt("finalize: order length %d does not match buffer size %d", len(order), n)
	}

	uids := make([]string, n)
	uidScratch := canonBuf{fs: e.fs}

	for _, i := range order {
		node := e.nodes[i]
		uids[i] = resolveAndUID(node, uids, &uidScratch)

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

	for id, node := range e.nodes {
		if len(node.Deps) > 0 {
			ThrowFmt("finalize: node %d has pre-populated Deps; rules must use DepRefs only", id)
		}

		if len(node.ForeignDeps) > 0 {
			ThrowFmt("finalize: node %d has pre-populated ForeignDeps; rules must use ForeignDepRefs only", id)
		}
	}

	checkRef := func(owner int, r NodeRef) {
		if r.id < 0 || r.id >= int64(n) {
			ThrowFmt("node %d references out-of-range NodeRef id=%d (buffer size %d)", owner, r.id, n)
		}
	}

	for i, node := range e.nodes {
		for _, r := range node.DepRefs {
			checkRef(i, r)
		}

		fkeys := make([]string, 0, len(node.ForeignDepRefs))
		for k := range node.ForeignDepRefs {
			fkeys = append(fkeys, k)
		}
		sort.Strings(fkeys)

		for _, k := range fkeys {
			for _, r := range node.ForeignDepRefs[k] {
				checkRef(i, r)
			}
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

		fkeys := make([]string, 0, len(node.ForeignDepRefs))
		for k := range node.ForeignDepRefs {
			fkeys = append(fkeys, k)
		}
		sort.Strings(fkeys)

		for _, k := range fkeys {
			for _, r := range node.ForeignDepRefs[k] {

				if _, ok := seen[r.id]; ok {
					continue
				}

				seen[r.id] = struct{}{}
				addEdge(int(r.id), i)
			}
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

func finalizeNodes(e *BufferedEmitter, yield func(*Node)) []string {
	return finalizeNodesInOrder(e, finalizeOrder(e), yield)
}

func resolveAndUID(node *Node, uids []string, uidScratch *canonBuf) string {
	var insertionOrderDeps []string

	if len(node.DepRefs) > 0 {
		seen := make(map[string]struct{}, len(node.DepRefs))
		ordered := make([]string, 0, len(node.DepRefs))

		for _, r := range node.DepRefs {
			u := uids[r.id]
			if _, ok := seen[u]; ok {
				continue
			}

			seen[u] = struct{}{}
			ordered = append(ordered, u)
		}

		if node.KV["p"] == "LD" || node.KV["p"] == "AR" {
			insertionOrderDeps = ordered
			sorted := make([]string, len(ordered))
			copy(sorted, ordered)
			sort.Strings(sorted)
			node.Deps = sorted
		} else {
			sort.Strings(ordered)
			node.Deps = ordered
		}
	} else if node.Deps == nil {
		node.Deps = []string{}
	}

	if len(node.ForeignDepRefs) > 0 {
		fkeys := make([]string, 0, len(node.ForeignDepRefs))
		for k := range node.ForeignDepRefs {
			fkeys = append(fkeys, k)
		}
		sort.Strings(fkeys)

		resolved := make(map[string][]string, len(fkeys))
		for _, k := range fkeys {
			set := make(map[string]struct{})
			for _, r := range node.ForeignDepRefs[k] {
				set[uids[r.id]] = struct{}{}
			}

			if len(set) == 0 {
				continue
			}

			vals := make([]string, 0, len(set))
			for u := range set {
				vals = append(vals, u)
			}
			sort.Strings(vals)
			resolved[k] = vals
		}

		if len(resolved) > 0 {
			node.ForeignDeps = resolved
		}
	}

	node.Sandboxing = true

	u := nodeUIDWithBuf(node, uidScratch)
	node.UID = u

	node.SelfUID = u
	node.StatsUID = nodeStatsUID(node)

	if insertionOrderDeps != nil {
		node.Deps = insertionOrderDeps
	}

	node.DepRefs = nil
	node.ForeignDepRefs = nil

	return u
}

type StreamingEmitter struct {
	nodes         []*Node
	uids          []string
	pendingIdx    []int64
	pendingSet    map[int64]bool
	results       []int64
	onNode        func(*Node)
	finalized     bool
	readyCh       chan struct{}
	uidScratch    canonBuf
}

func NewStreamingEmitter(onNode func(*Node)) *StreamingEmitter {
	return &StreamingEmitter{
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
	e.uids = append(e.uids, "")

	if e.hasUnresolvedDeps(n) {
		e.pendingSet[id] = true
		e.pendingIdx = append(e.pendingIdx, id)
		return NodeRef{id: id}
	}

	e.uids[id] = resolveAndUID(n, e.uids, &e.uidScratch)
	if e.onNode != nil {
		e.onNode(n)
	}

	return NodeRef{id: id}
}

func (e *StreamingEmitter) hasUnresolvedDeps(n *Node) bool {
	for _, r := range n.DepRefs {
		if e.uids[r.id] == "" {
			return true
		}
	}

	for _, refs := range n.ForeignDepRefs {
		for _, r := range refs {
			if e.uids[r.id] == "" {
				return true
			}
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

func (e *StreamingEmitter) Finish() []string {
	if e.finalized {
		panic("StreamingEmitter.Finish called twice")
	}

	for _, id := range e.pendingIdx {
		n := e.nodes[id]
		e.uids[id] = resolveAndUID(n, e.uids, &e.uidScratch)
		if e.onNode != nil {
			e.onNode(n)
		}
	}

	e.finalized = true
	close(e.readyCh)

	results := make([]string, 0, len(e.results))
	seen := map[string]struct{}{}

	for _, rid := range e.results {
		u := e.uids[rid]
		if _, ok := seen[u]; ok {
			continue
		}

		seen[u] = struct{}{}
		results = append(results, u)
	}

	return results
}

func graphFromFinalizedEmitter(e *BufferedEmitter, uids []string) *Graph {
	n := len(e.nodes)

	out := &Graph{
		Conf:   map[string]interface{}{},
		Inputs: map[string]interface{}{},
		Graph:  make([]*Node, 0, n),
		Result: make([]string, 0, len(e.results)),
	}

	uidToNode := make(map[string]*Node, n)
	for i, node := range e.nodes {
		u := uids[i]
		if _, ok := uidToNode[u]; !ok {
			uidToNode[u] = node
		}
	}

	seenResult := map[string]struct{}{}

	for _, rid := range e.results {
		u := uids[rid]

		if _, ok := seenResult[u]; ok {
			continue
		}

		seenResult[u] = struct{}{}
		out.Result = append(out.Result, u)
	}

	seenNode := make(map[string]struct{}, n)

	var dfsVisit func(uid string)
	dfsVisit = func(uid string) {
		if _, ok := seenNode[uid]; ok {
			return
		}

		seenNode[uid] = struct{}{}
		node := uidToNode[uid]

		if node == nil {
			return
		}

		out.Graph = append(out.Graph, node)

		for _, depUID := range node.Deps {
			dfsVisit(depUID)
		}
	}

	for _, rootUID := range out.Result {
		dfsVisit(rootUID)
	}

	for i, node := range e.nodes {
		u := uids[i]
		if _, ok := seenNode[u]; !ok {
			seenNode[u] = struct{}{}
			out.Graph = append(out.Graph, node)
		}
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
