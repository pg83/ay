package main

import (
	"container/heap"
	"sort"
)

// intHeap is a min-heap of ints used by Finalize's Kahn topo-sort.
// Previous linear-scan-for-min + slice-shift was O(N²) on real targets
// (235K nodes pre-dedup); heap is O(log N) per extraction, same
// tie-break (smallest buffer index wins), byte-exact preserved.
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

// emitter.go — Emitter interface, NodeRef placeholder, BufferedEmitter,
// Graph wrapper, and the Finalize Merkle pass.
//
// Rules call Emit(*Node) → NodeRef (opaque buffer index; the UID isn't
// known until every transitive dep has been hashed). Deps are wired by
// storing NodeRefs in DepRefs/ForeignDepRefs. Result(NodeRef) marks a
// final output; Graph.Result holds those UIDs in call order.
//
// Finalize topo-sorts buffered nodes, then walks dependency-first
// computing each UID from a canonicalised serialisation whose children's
// UIDs are already filled in (Merkle). Cycles throw. Resolved Deps and
// each ForeignDeps[key] are sorted alphabetically before canonicalisation
// so the hash is order-independent.

// NodeRef is the indirection that lets rules express "this node depends
// on that node" without knowing yet what the dependee's UID will be.
// The id is a monotonic index assigned by Emit.
type NodeRef struct {
	id int64
}

// Emitter is the interface rules use to publish nodes and mark results.
// OnReady returns a channel that closes when the referenced node's deps
// are resolved. BufferedEmitter returns one shared channel that closes at
// Finalize for any input ref. StreamingEmitter MUST close a per-ref
// channel as each node's deps resolve — the shared-channel shortcut is
// buffered-only.
type Emitter interface {
	Emit(n *Node) NodeRef
	Result(NodeRef)
	OnReady(NodeRef) <-chan struct{}
}

// BufferedEmitter accumulates nodes and result refs in memory;
// Finalize turns the buffer into a Graph.
type BufferedEmitter struct {
	nodes     []*Node
	results   []int64
	finalized bool

	// readyCh is shared across all OnReady calls — the buffered
	// model treats every node as "ready" only after Finalize, so
	// one channel that closes at Finalize covers every caller.
	readyCh chan struct{}
}

// NewBufferedEmitter constructs an empty BufferedEmitter.
func NewBufferedEmitter() *BufferedEmitter {
	return &BufferedEmitter{
		readyCh: make(chan struct{}),
	}
}

// OnReady returns a channel that closes when the node's deps are
// resolved. For BufferedEmitter every node becomes ready simultaneously
// at Finalize (single shared channel). Callers that `<-` before Finalize
// block; an out-of-range ref still returns the shared channel — Finalize's
// checkRef catches it, avoiding a silent deadlock on a bogus ref.
func (e *BufferedEmitter) OnReady(_ NodeRef) <-chan struct{} {
	return e.readyCh
}

// Emit appends n to the buffer and returns a NodeRef whose id is the
// node's index in the buffer. The same *Node pointer is retained — the
// rule may keep mutating it until Finalize is called, but that is bad
// practice and not relied upon.
func (e *BufferedEmitter) Emit(n *Node) NodeRef {
	if e.finalized {
		panic("BufferedEmitter.Emit called after Finalize")
	}

	id := int64(len(e.nodes))
	e.nodes = append(e.nodes, n)

	return NodeRef{id: id}
}

// Result marks the referenced node as a final output of the graph. The
// order of Result calls is preserved in the resulting Graph's `result`
// list (after Finalize translates ids to UIDs).
func (e *BufferedEmitter) Result(r NodeRef) {
	if e.finalized {
		panic("BufferedEmitter.Result called after Finalize")
	}

	e.results = append(e.results, r.id)
}

// Graph is the top-level on-disk shape: { conf, graph, inputs, result }.
// Field order matches the reference g.json (alphabetical) so encoding/json
// emits keys in the same order. Conf and Inputs are empty maps.
type Graph struct {
	Conf   map[string]interface{} `json:"conf"`
	Graph  []*Node                `json:"graph"`
	Inputs map[string]interface{} `json:"inputs"`
	Result []string               `json:"result"`
}

// Finalize converts a BufferedEmitter into *Graph: topo-sort, walk dep-
// first computing UID Merkle-style, populate Deps/ForeignDeps from
// resolved children's UIDs (sorted), clear internal *Refs, and return a
// Graph whose Result is the result-ref ids translated to UIDs in call
// order. Cycles or out-of-range NodeRefs throw via ThrowFmt.
//
// FinalizeStream is the streaming sibling: yields each finalized node
// (UID/SelfUID + resolved Deps/ForeignDeps populated, internal *Refs
// cleared) in dep-first topological order, then returns deduped root
// UIDs. Yielded *Node lives in the emitter's slice; not copied. Cannot
// run concurrently with Finalize on the same emitter.
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

// finalizeNodes is the shared implementation behind Finalize and
// FinalizeStream: validate buffer state, topo-sort, resolve Deps/
// ForeignDeps from DepRefs/ForeignDepRefs, compute per-node UID, yield
// via the optional callback in dep-first order. Returns uids[i] indexed
// by buffer position. Sets e.finalized on success; throws on
// re-finalize, pre-populated Deps/ForeignDeps, out-of-range NodeRefs,
// or cycles.
func finalizeNodes(e *BufferedEmitter, yield func(*Node)) []string {
	if e.finalized {
		ThrowFmt("finalize: emitter already finalized")
	}

	n := len(e.nodes)

	// Reject pre-populated Deps/ForeignDeps. Rules MUST use DepRefs/
	// ForeignDepRefs; pre-set public slices silently corrupt the Merkle
	// hash (overwritten when refs exist; participate without resolution
	// otherwise).
	for id, node := range e.nodes {
		if len(node.Deps) > 0 {
			ThrowFmt("finalize: node %d has pre-populated Deps; rules must use DepRefs only", id)
		}

		if len(node.ForeignDeps) > 0 {
			ThrowFmt("finalize: node %d has pre-populated ForeignDeps; rules must use ForeignDepRefs only", id)
		}
	}

	// Validate every NodeRef references a real buffered node.
	checkRef := func(owner int, r NodeRef) {
		if r.id < 0 || r.id >= int64(n) {
			ThrowFmt("node %d references out-of-range NodeRef id=%d (buffer size %d)", owner, r.id, n)
		}
	}

	for i, node := range e.nodes {
		for _, r := range node.DepRefs {
			checkRef(i, r)
		}

		// Iterate ForeignDepRefs in sorted-key order: this loop is
		// validation-only (no output bytes), but we keep the discipline
		// (D14) so reviewers don't have to second-guess.
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

	// Kahn topological sort by combined DepRefs + ForeignDepRefs edges
	// (child → parent). Ascending-in-degree scheduling emits leaves
	// first, which is what Merkle-style UID computation needs. Equal
	// in-degree breaks on buffer index for determinism.
	indeg := make([]int, n)
	// children[i] = nodes that depend on node i (i.e. nodes whose
	// DepRefs/ForeignDepRefs include i). When i is finalised, each
	// child's in-degree drops by one.
	children := make([][]int, n)
	addEdge := func(child, parent int) {
		// "parent depends on child" — edge from child to parent for
		// scheduling purposes. The parent's in-degree counts how many
		// of its dependencies are still unresolved.
		children[child] = append(children[child], parent)
		indeg[parent]++
	}

	for i, node := range e.nodes {
		// Use a set to dedupe: a node may legitimately list the same
		// child twice (e.g. through Deps and ForeignDeps), and we must
		// not double-count its in-degree or topo will deadlock.
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

	// Seed heap with every zero-in-degree node. Min-heap pop yields the
	// smallest buffer index — same tie-break as the prior linear scan, so
	// topo order is byte-exact-equivalent.
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
		// Find one node still with indeg > 0 to name in the error.
		for i, d := range indeg {
			if d > 0 {
				ThrowFmt("cycle detected involving node %d", i)
			}
		}

		ThrowFmt("cycle detected (could not order all %d nodes; ordered %d)", n, len(order))
	}

	// Walk in topo order, fill Deps/ForeignDeps from resolved children,
	// then hash. Because we go dependency-first, every child's UID is
	// known by the time we hash a parent.
	uids := make([]string, n)
	for _, i := range order {
		node := e.nodes[i]
		uids[i] = resolveAndUID(node, uids)

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

// resolveAndUID is the per-node finalize step: resolves DepRefs and
// ForeignDepRefs into Deps and ForeignDeps using already-known UIDs from
// preceding (dep-first) nodes, sets Sandboxing=true, hashes the node to
// compute UID/SelfUID, restores LD/AR insertion order, and clears the
// internal *Refs slots. Shared between buffered and streaming finalizes.
//
// LD/AR nodes preserve emit (insertion) order in their final Deps slice;
// the hash always sees the SORTED form for order-independence, then
// insertion order is restored.
func resolveAndUID(node *Node, uids []string) string {
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

	// Sandboxing is always true in the reference closure.
	node.Sandboxing = true

	u := nodeUID(node)
	node.UID = u
	// SelfUID is a PR-02 placeholder; pinned by t.Logf rather than t.Errorf.
	node.SelfUID = u
	node.StatsUID = ""

	if insertionOrderDeps != nil {
		node.Deps = insertionOrderDeps
	}

	node.DepRefs = nil
	node.ForeignDepRefs = nil

	return u
}

// StreamingEmitter finalizes each node inline at Emit time: resolves
// DepRefs from already-emitted nodes' UIDs, computes UID, fires
// onNode(n) synchronously. Used by `yatool make` so leaf compiles can
// start immediately. Gen emits in dep-first post-order DFS, so deps land
// before parents; out-of-order emits are parked and drained in Finish().
type StreamingEmitter struct {
	nodes      []*Node
	uids       []string
	pendingIdx []int64
	pendingSet map[int64]bool
	results    []int64
	onNode     func(*Node)
	finalized  bool
	readyCh    chan struct{}
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

	e.uids[id] = resolveAndUID(n, e.uids)
	if e.onNode != nil {
		e.onNode(n)
	}

	return NodeRef{id: id}
}

// hasUnresolvedDeps reports whether any DepRef/ForeignDepRef points at
// a not-yet-finalised peer. Should be false on the hot path: DFS
// post-order emit lands deps before parents.
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

// Finish drains the pending pool (out-of-order emits parked by
// hasUnresolvedDeps), runs resolveAndUID, fires onNode, and returns the
// deduped result UIDs in declaration order. Call once after Gen; further
// Emit/Result panics.
func (e *StreamingEmitter) Finish() []string {
	if e.finalized {
		panic("StreamingEmitter.Finish called twice")
	}

	for _, id := range e.pendingIdx {
		n := e.nodes[id]
		e.uids[id] = resolveAndUID(n, e.uids)
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

func Finalize(e *BufferedEmitter) *Graph {
	uids := finalizeNodes(e, nil)
	n := len(e.nodes)

	// Build output Graph. Graph[] is DFS preorder rooted at each Result
	// UID, visiting children in their as-emitted Deps order — matches
	// REF's graph[] array order. UIDs are deduplicated across all roots.
	out := &Graph{
		Conf:   map[string]interface{}{},
		Inputs: map[string]interface{}{},
		Graph:  make([]*Node, 0, n),
		Result: make([]string, 0, len(e.results)),
	}

	// Build a uid→node lookup for the DFS pass.
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

	// DFS preorder: visit each result root in result order, then recurse
	// into its Deps in their emitted order. Each node appears exactly once.
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

	// Disconnected subgraphs (from dedup collisions) are appended in
	// buffer (= emit) order — itself a topological order because emit
	// precedes each parent's emit.
	for i, node := range e.nodes {
		u := uids[i]
		if _, ok := seenNode[u]; !ok {
			seenNode[u] = struct{}{}
			out.Graph = append(out.Graph, node)
		}
	}

	return out
}
