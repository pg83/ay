package main

type TarjanScratch struct {
	stamp   Vec[uint32]
	index   []int32
	low     []int32
	onStack BitSet
	epoch   uint32
}

type TarjanCtx struct {
	scratch TarjanScratch
	stack   []VFS
	next    int32
	closure IdSet
}

func (t *TarjanScratch) reset(size uint32) {
	if t.stamp.freshLen(int(size)) {
		t.index = make([]int32, t.stamp.len())
		t.low = make([]int32, t.stamp.len())
		t.onStack = BitSet{}
		t.epoch = 1

		return
	}

	t.epoch++

	if t.epoch == 0 {
		clear(t.stamp.s)

		t.epoch = 1
	}
}

func (t *TarjanScratch) visited(v VFS) bool {
	id := uint32(v)

	return id < uint32(t.stamp.len()) && t.stamp.s[id] == t.epoch
}

func (t *TarjanScratch) discover(v VFS, idx int32) {
	id := uint32(v)

	if int(id) >= t.stamp.len() {
		t.stamp.ensureLen(int(id) + 1)

		grown := make([]int32, t.stamp.len())

		copy(grown, t.index)
		t.index = grown
		grown = make([]int32, t.stamp.len())
		copy(grown, t.low)
		t.low = grown
	}

	t.stamp.s[id] = t.epoch
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

type ClosureSink interface {
	forEachChild(v VFS, fn func(VFS))

	cachedWindow(v VFS) (window Closure, cached bool)

	emitClosure(members []VFS, fill func(block []VFS) int)

	windowSubsumed(v VFS) bool
}

func (tc *TarjanCtx) runSCC(g ClosureSink, root VFS) uint64 {
	tc.scratch.reset(vfsBound())
	tc.stack = tc.stack[:0]
	tc.next = 0

	return tc.strongconnect(g, root)
}

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

				cl, _ := g.cachedWindow(ch)

				k = cl.spliceInto(&tc.closure, block, k)
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
