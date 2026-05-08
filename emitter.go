package main

import (
	"container/heap"
	"sort"
)

// intHeap is a min-heap of ints used by Finalize's Kahn topo-sort. A
// previous linear-scan-for-min + slice-shift implementation was O(N²)
// in the size of the ready queue and dominated gen wall-clock time on
// real targets (235K buffered nodes pre-dedup). A heap reduces the
// per-extraction cost to O(log N) without changing the tie-break
// (smallest buffer index wins), preserving byte-exact output.
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

// emitter.go — the Emitter interface, NodeRef placeholder, BufferedEmitter
// implementation, the Graph wrapper type, and the Finalize Merkle pass.
//
// Design (D6/D7):
//
//   - Rule authors call Emitter.Emit(*Node) and get back a NodeRef. The
//     ref is opaque — it carries an integer index into the emitter's
//     internal buffer, not the node's UID, because the UID isn't known
//     until every transitive dependency has been hashed.
//   - Rules wire dependencies by storing NodeRefs in a node's DepRefs /
//     ForeignDepRefs. The emitter never asks for `[]string` UIDs from a
//     rule.
//   - Result(NodeRef) marks a node as a final output; the resulting
//     Graph's `result` array contains those nodes' UIDs in the order
//     Result was called.
//   - Finalize topologically sorts the buffered nodes, then walks them
//     in dependency-first order computing each node's UID from a
//     canonicalised serialisation that already has its children's UIDs
//     filled in (a Merkle hash). Cycles are an error. Per D14 the
//     resolved Deps slice and each ForeignDeps[key] slice are sorted
//     alphabetically before the canonicalisation step so the hash is
//     order-independent.

// NodeRef is the indirection that lets rules express "this node depends
// on that node" without knowing yet what the dependee's UID will be.
// The id is a monotonic index assigned by Emit.
type NodeRef struct {
	id int64
}

// Emitter is the interface rules use to publish nodes and mark
// results.
//
// `OnReady` lands in PR-23 as part of the streaming-emitter contract
// (D37). It returns a channel that closes when the referenced node's
// dependencies are all resolved. `BufferedEmitter`'s implementation
// is a no-op — every channel returned closes immediately at
// `Finalize` time, because in the buffered model "all deps resolved"
// is equivalent to "Finalize ran". `StreamingEmitter` (M3) closes
// per-node as the topo wave reaches it. Locking the signature now
// means rule emitters that need to await readiness (PR-26+ parallel
// executor) can be written against `Emitter`, not the concrete
// type.
//
// BufferedEmitter returns one shared channel that closes at Finalize for any
// input ref. StreamingEmitter (M3) MUST close a per-ref channel as each node's
// deps resolve — the shared-channel shortcut is buffered-only.
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

// OnReady returns a channel that closes when the node's dependencies
// are all resolved. For BufferedEmitter that is at Finalize time —
// every node "becomes ready" simultaneously when the Merkle pass
// completes. The shared channel is closed by Finalize. Callers that
// `<-` before Finalize will block; that is correct semantics — a
// streaming caller would block on a streaming emitter too, just for
// a shorter duration.
//
// Per the brief, the ref is validated only loosely; an out-of-range
// ref will trip Finalize's checkRef when Finalize runs, not here.
// Returning a never-closing channel for a bogus ref would be a
// silent deadlock; the brief asks for "no-op" so we accept any ref
// and return the shared channel.
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
//
// Field order matches the reference g.json (also alphabetical), so that
// encoding/json emits keys in the same order as the upstream tool. For
// PR-02, Conf and Inputs are empty maps per D15 (later PRs may populate
// them).
type Graph struct {
	Conf   map[string]interface{} `json:"conf"`
	Graph  []*Node                `json:"graph"`
	Inputs map[string]interface{} `json:"inputs"`
	Result []string               `json:"result"`
}

// Finalize converts a BufferedEmitter into a *Graph: topologically sorts
// the buffered nodes, walks them dependency-first computing each node's
// UID (Merkle-style; SelfUID gets the same algorithm; StatsUID is left
// empty for now), populates each node's serialised Deps/ForeignDeps
// slices from the resolved children's UIDs (sorted, per D14), clears the
// internal *Refs fields so they don't accidentally outlive Finalize, and
// returns a Graph whose `Result` array is the result-ref ids translated
// into UIDs in call order.
//
// Errors: a cycle in DepRefs/ForeignDepRefs throws an exception
// mentioning one of the offending node ids; a NodeRef pointing outside
// the buffer (id < 0 or id >= len(nodes)) also throws. None of these
// internal errors are discriminated by callers, so per STYLE.md we
// raise via ThrowFmt rather than returning an error. Callers that need
// to recover wrap the call in Try.
func Finalize(e *BufferedEmitter) *Graph {
	if e.finalized {
		ThrowFmt("finalize: emitter already finalized")
	}

	n := len(e.nodes)

	// Reject pre-populated Deps/ForeignDeps. Rules express dependencies via
	// DepRefs/ForeignDepRefs; allowing the public slices to be set up-front
	// would silently corrupt the Merkle hash because Finalize would either
	// overwrite them (for nodes with refs) or leave them to participate in
	// canonicalisation without ref-resolution (for nodes without refs).
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

	// Topological sort (Kahn's algorithm) by combined DepRefs +
	// ForeignDepRefs edges. Edge: child -> parent (i.e. an edge from a
	// dependee to its dependant). Scheduling by ascending in-degree
	// emits leaves first, which is what we need to compute UIDs Merkle-
	// style. Within equal in-degree we fall back to the buffer index
	// order — that keeps the topo order deterministic for any given
	// emit sequence.
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

	// Seed the heap with every zero-in-degree node. The heap pops the
	// smallest buffer index next — same tie-break the previous
	// linear-scan implementation produced — so the topo order is
	// byte-exact-equivalent. Seeding ascending is irrelevant to the
	// pop sequence (the heap re-orders) but cheap to write.
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

		// Resolve Deps. Dedupe + sort per D14 so the canonicalisation
		// is order-independent and the hash is stable across emit
		// orderings that produce the same set of edges.
		if len(node.DepRefs) > 0 {
			depSet := make(map[string]struct{}, len(node.DepRefs))
			for _, r := range node.DepRefs {
				depSet[uids[r.id]] = struct{}{}
			}

			deps := make([]string, 0, len(depSet))
			for u := range depSet {
				deps = append(deps, u)
			}
			sort.Strings(deps)
			node.Deps = deps
		} else if node.Deps == nil {
			// Ensure the field serialises as [] not null.
			node.Deps = []string{}
		}

		// Resolve ForeignDeps. Same dedupe+sort treatment per key. We
		// only emit the foreign_deps map at all if the rule author
		// actually populated ForeignDepRefs with at least one non-empty
		// key — that preserves the `omitempty` behaviour for nodes that
		// have no foreign deps and avoids serialising
		// `foreign_deps:{key:[]}` for empty-value keys.
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
					// Skip keys whose deduped+resolved slice is empty —
					// they would otherwise serialize as `key:[]`, which
					// is not what the reference output produces.
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
			// else: leave node.ForeignDeps nil so omitempty drops the field.
		}

		// Hash the (now child-resolved) node. The canonical form has
		// UID/SelfUID/StatsUID zeroed, ensuring hash-of-content not
		// hash-of-identity.
		canon := canonicalNodeBytes(node)
		u := computeUID(canon)
		node.UID = u
		// TODO(future-PR): SelfUID is currently set to the same value as UID
		// as a PR-02 placeholder. A future PR must compute a distinct value
		// (per ymake semantics, derived from this node's content only,
		// excluding child UIDs). Tests pin the placeholder behaviour with a
		// t.Logf rather than t.Errorf so this can be tightened later.
		node.SelfUID = u
		node.StatsUID = "" // explicit; refined in a later PR.
		uids[i] = u

		// Drop the internal *Refs so they do not leak past Finalize. The
		// emitter's `finalized` flag (set below) is the actual safety
		// net against re-Finalize; clearing the refs is hygiene only.
		node.DepRefs = nil
		node.ForeignDepRefs = nil
	}

	// Build the output Graph. `Graph` is the topo-ordered list of
	// nodes deduped by UID (two emits that hash to the same UID are
	// the same node and must appear once); `Result` is the UIDs
	// corresponding to the Result() refs in call order, also deduped
	// (duplicate Result(ref) calls are idempotent).
	out := &Graph{
		Conf:   map[string]interface{}{},
		Inputs: map[string]interface{}{},
		Graph:  make([]*Node, 0, n),
		Result: make([]string, 0, len(e.results)),
	}
	seenNode := map[string]struct{}{}

	for _, i := range order {
		u := uids[i]

		if _, ok := seenNode[u]; ok {
			continue
		}

		seenNode[u] = struct{}{}
		out.Graph = append(out.Graph, e.nodes[i])
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

	e.finalized = true

	// Signal every OnReady waiter (D37). The channel is shared
	// across all callers; closing it once releases everyone. If
	// the channel was never created (zero-value emitter), skip
	// the close — older callers that bypass NewBufferedEmitter
	// won't be affected.
	if e.readyCh != nil {
		close(e.readyCh)
	}

	return out
}
