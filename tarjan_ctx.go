package main

// tarjanScratch holds the per-node Tarjan SCC state, epoch-stamped so reset()
// is an O(1) epoch bump. Dense arrays are indexed by uint32(v).
type TarjanScratch struct {
	stamp   []uint32
	index   []int32
	low     []int32
	onStack BitSet
	epoch   uint32
}

// tarjanCtx bundles the SCC + closure-splice working state. One instance owned
// by genCtx is shared run-wide: gen is single-threaded with reset() before every
// use, so one instance avoids per-scanner duplicates of the dense arrays.
type TarjanCtx struct {
	scratch TarjanScratch
	stack   []VFS
	next    int32
	closure IdSet
}

func (t *TarjanScratch) reset(size uint32) {
	if uint32(len(t.stamp)) < size {
		grown := uint32(len(t.stamp)) * 2

		if grown < size {
			grown = size
		}

		t.stamp = make([]uint32, grown)
		t.index = make([]int32, grown)
		t.low = make([]int32, grown)
		t.onStack = BitSet{}
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

func (t *TarjanScratch) visited(v VFS) bool {
	id := uint32(v)

	return id < uint32(len(t.stamp)) && t.stamp[id] == t.epoch
}

func (t *TarjanScratch) discover(v VFS, idx int32) {
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
	}

	t.stamp[id] = t.epoch
	t.index[id] = idx
	t.low[id] = idx
	t.onStack.add(id)
}

func (t *TarjanScratch) onStackHas(v VFS) bool {
	return t.visited(v) && t.onStack.has(uint32(v))
}

func (t *TarjanScratch) lowOf(v VFS) int32 {
	return t.low[uint32(v)]
}

func (t *TarjanScratch) indexOf(v VFS) int32 {
	return t.index[uint32(v)]
}

func (t *TarjanScratch) setLow(v VFS, x int32) {
	t.low[uint32(v)] = x
}

func (t *TarjanScratch) onStackOf(v VFS) bool {
	return t.onStack.has(uint32(v))
}

func (t *TarjanScratch) setOnStack(v VFS, b bool) {
	t.onStack.set(uint32(v), b)
}

// closureSink is the scanner surface strongconnect needs. Kept narrow so the SCC
// algorithm does not depend on scanner internals.
type ClosureSink interface {
	forEachChild(v VFS, fn func(VFS))
	// cachedWindow returns the cached transitive closure of v, if one exists.
	cachedWindow(v VFS) (window []VFS, cached bool)
	// emitClosure reserves a block, runs fill (which writes the deduped closure
	// and returns the count), then commits, stores, and caches every member.
	emitClosure(members []VFS, fill func(block []VFS) int)
	// windowSubsumed reports whether v's whole window is already inside the
	// block being filled, so a splice loop can skip it.
	windowSubsumed(v VFS) bool
}

// runSCC resets the per-traversal Tarjan state and runs the SCC build rooted at
// the cycle entry dfs handed off. Returns the cached-child-edge count.
func (tc *TarjanCtx) runSCC(g ClosureSink, root VFS) uint64 {
	tc.scratch.reset(vfsBound())
	tc.stack = tc.stack[:0]
	tc.next = 0

	return tc.strongconnect(g, root)
}

// strongconnect is Tarjan's SCC step over the include graph. On each completed
// SCC it builds that SCC's transitive closure — members deduped, then non-member
// children's cached windows spliced in — into a block the sink hands it. Returns
// the cached-child-edge count so the caller bumps subgraphHits once.
func (tc *TarjanCtx) strongconnect(g ClosureSink, v VFS) (hits uint64) {
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

				if g.windowSubsumed(ch) {
					return
				}

				win, _ := g.cachedWindow(ch)
				k = tc.closure.spliceNew(win, block, k)
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
