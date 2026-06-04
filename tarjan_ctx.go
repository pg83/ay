package main

// tarjanScratch holds the per-node Tarjan SCC state (stamp/index/low/onStack),
// epoch-stamped so reset() is an O(1) epoch bump rather than a clear. Every
// dense array is indexed by uint32(v) of the VFS node.
type tarjanScratch struct {
	stamp   []uint32
	index   []int32
	low     []int32
	onStack []bool
	epoch   uint32
}

// tarjanCtx bundles the SCC + closure-splice working state used by dfs/
// strongconnect. One instance is owned by genCtx and shared run-wide by the
// target and host scanners: scratch and closure hold vfsBound-sized dense
// arrays, and gen is single-threaded with reset() before every use (no nesting:
// dfs pass 1 finishes before pass 2, and strongconnect only reads cached child
// windows), so one instance avoids growing per-scanner duplicates. stack/next
// are SCC-local.
type tarjanCtx struct {
	scratch tarjanScratch
	stack   []VFS
	next    int32
	closure idSet
}

func (t *tarjanScratch) reset(size uint32) {
	if uint32(len(t.stamp)) < size {
		grown := uint32(len(t.stamp)) * 2

		if grown < size {
			grown = size
		}

		t.stamp = make([]uint32, grown)
		t.index = make([]int32, grown)
		t.low = make([]int32, grown)
		t.onStack = make([]bool, grown)
		t.epoch = 1

		return
	}

	t.epoch++

	if t.epoch == 0 {
		for i := range t.stamp {
			t.stamp[i] = 0
		}

		t.epoch = 1
	}
}

// tarjanScratch is keyed by VFS value; every dense array is indexed by uint32(v).
func (t *tarjanScratch) visited(v VFS) bool {
	id := uint32(v)

	return id < uint32(len(t.stamp)) && t.stamp[id] == t.epoch
}

func (t *tarjanScratch) discover(v VFS, idx int32) {
	id := uint32(v)

	if id >= uint32(len(t.stamp)) {
		grown := uint32(len(t.stamp)) * 2

		if grown <= id {
			grown = id + 1
		}

		stamp := make([]uint32, grown)
		copy(stamp, t.stamp)
		t.stamp = stamp
		index := make([]int32, grown)
		copy(index, t.index)
		t.index = index
		low := make([]int32, grown)
		copy(low, t.low)
		t.low = low
		onStack := make([]bool, grown)
		copy(onStack, t.onStack)
		t.onStack = onStack
	}

	t.stamp[id] = t.epoch
	t.index[id] = idx
	t.low[id] = idx
	t.onStack[id] = true
}

func (t *tarjanScratch) onStackHas(v VFS) bool {
	return t.visited(v) && t.onStack[uint32(v)]
}

func (t *tarjanScratch) lowOf(v VFS) int32 {
	return t.low[uint32(v)]
}

func (t *tarjanScratch) indexOf(v VFS) int32 {
	return t.index[uint32(v)]
}

func (t *tarjanScratch) setLow(v VFS, x int32) {
	t.low[uint32(v)] = x
}

func (t *tarjanScratch) onStackOf(v VFS) bool {
	return t.onStack[uint32(v)]
}

func (t *tarjanScratch) setOnStack(v VFS, b bool) {
	t.onStack[uint32(v)] = b
}

// closureSink is the scanner surface strongconnect needs: walk a node's
// resolved children, read an already-built child closure, and materialize a new
// one. The arena alloc/commit and the subgraphClosures/subgraphCache writes stay
// on the scanner side (emitClosure): strongconnect only fills the block it is
// handed. Kept narrow so the SCC algorithm does not depend on scanner internals.
type closureSink interface {
	forEachChild(v VFS, fn func(VFS))
	// cachedWindow returns the cached transitive closure of v, if one exists.
	cachedWindow(v VFS) (window []VFS, cached bool)
	// emitClosure reserves a block, runs fill (which writes the deduped closure
	// into it and returns the count), then commits + stores it and caches every
	// member against the resulting closure ref.
	emitClosure(members []VFS, fill func(block []VFS) int)
}

// runSCC resets the per-traversal Tarjan state (scratch epoch, stack, index
// counter) and runs the SCC build rooted at the cycle entry dfs handed off.
// Returns the number of already-cached child edges seen so the caller bumps
// subgraphHits once. The reset lives here, not at the call site, so the whole
// SCC machinery (state + lifecycle) stays inside tarjanCtx.
func (tc *tarjanCtx) runSCC(g closureSink, root VFS) uint64 {
	tc.scratch.reset(vfsBound())
	tc.stack = tc.stack[:0]
	tc.next = 0

	return tc.strongconnect(g, root)
}

// strongconnect is Tarjan's SCC step over the include graph (the cycle path that
// dfs hands off to when it re-enters an in-flight root). It owns the SCC
// mechanics — discover/low-link/stack/onStack on tc.scratch + tc.stack — and, on
// each completed SCC, builds that SCC's transitive closure (members deduped via
// tc.closure, then their non-member children's cached windows spliced in) into a
// block the sink hands it. Returns the number of already-cached child edges seen
// so the caller can bump subgraphHits once instead of per edge.
func (tc *tarjanCtx) strongconnect(g closureSink, v VFS) (hits uint64) {
	tc.next++
	tc.scratch.discover(v, tc.next)
	tc.stack = append(tc.stack, v)

	g.forEachChild(v, func(w VFS) {
		if _, cached := g.cachedWindow(w); cached {
			hits++

			return
		}

		if !tc.scratch.visited(w) {
			hits += tc.strongconnect(g, w)

			if tc.scratch.lowOf(w) < tc.scratch.lowOf(v) {
				tc.scratch.setLow(v, tc.scratch.lowOf(w))
			}
		} else if tc.scratch.onStackOf(w) {
			if tc.scratch.indexOf(w) < tc.scratch.lowOf(v) {
				tc.scratch.setLow(v, tc.scratch.indexOf(w))
			}
		}
	})

	if tc.scratch.lowOf(v) != tc.scratch.indexOf(v) {
		return hits
	}

	sccStart := len(tc.stack) - 1

	for tc.stack[sccStart] != v {
		sccStart--
	}

	members := tc.stack[sccStart:]

	tc.closure.reset(vfsBound())

	g.emitClosure(members, func(block []VFS) int {
		k := 0

		for _, u := range members {
			if !tc.closure.has(u) {
				tc.closure.add(u)
				block[k] = u
				k++
			}
		}

		for _, u := range members {
			g.forEachChild(u, func(ch VFS) {
				if tc.scratch.onStackHas(ch) {
					return
				}

				win, _ := g.cachedWindow(ch)

				for _, id := range win {
					if !tc.closure.has(id) {
						tc.closure.add(id)
						block[k] = id
						k++
					}
				}
			})
		}

		return k
	})

	for _, u := range members {
		tc.scratch.setOnStack(u, false)
	}

	tc.stack = tc.stack[:sccStart]

	return hits
}
