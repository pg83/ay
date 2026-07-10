package main

type TarjanScratch struct {
	slots map[VFS]int
	nodes []tarjanNode
}

type tarjanNode struct {
	index   int32
	low     int32
	onStack bool
}

type TarjanCtx struct {
	scratch  TarjanScratch
	stack    []VFS
	next     int32
	closure  IdSet
	g        ClosureSink
	hits     uint64
	visitV   VFS
	emitK    int
	emitBuf  []VFS
	childFn  func(VFS)
	spliceFn func(VFS)
	emitFn   func([]VFS) int
}

func (t *TarjanScratch) reset(uint32) {
	if t.slots == nil {
		t.slots = make(map[VFS]int, 512)
	} else {
		clear(t.slots)
	}

	t.nodes = t.nodes[:0]
}

func (t *TarjanScratch) visited(v VFS) bool {
	_, ok := t.slots[v]

	return ok
}

func (t *TarjanScratch) discover(v VFS, idx int32) {
	t.slots[v] = len(t.nodes)
	t.nodes = append(t.nodes, tarjanNode{index: idx, low: idx, onStack: true})
}

func (t *TarjanScratch) onStackHas(v VFS) bool {
	i, ok := t.slots[v]

	return ok && t.nodes[i].onStack
}

func (t *TarjanScratch) lowOf(v VFS) int32 {
	return t.nodes[t.slots[v]].low
}

func (t *TarjanScratch) indexOf(v VFS) int32 {
	return t.nodes[t.slots[v]].index
}

func (t *TarjanScratch) setLow(v VFS, x int32) {
	t.nodes[t.slots[v]].low = x
}

func (t *TarjanScratch) onStackOf(v VFS) bool {
	return t.nodes[t.slots[v]].onStack
}

func (t *TarjanScratch) setOnStack(v VFS, b bool) {
	t.nodes[t.slots[v]].onStack = b
}

type ClosureSink interface {
	forEachChild(v VFS, fn func(VFS))

	cachedWindow(v VFS) (window Closure, cached bool)

	emitClosure(members []VFS, fill func(block []VFS) int)

	windowSubsumed(v VFS) bool
}

type TarjanPool struct {
	free []*TarjanCtx
}

func (p *TarjanPool) get() *TarjanCtx {
	if n := len(p.free); n > 0 {
		tc := p.free[n-1]

		p.free = p.free[:n-1]

		return tc
	}

	return &TarjanCtx{}
}

func (p *TarjanPool) put(tc *TarjanCtx) {
	p.free = append(p.free, tc)
}

var tarjans TarjanPool

func (tc *TarjanCtx) runSCC(g ClosureSink, root VFS) uint64 {
	if tc.g != nil {
		throwFmt("tarjan: nested runSCC (pending fire inside SCC walk)")
	}

	tc.scratch.reset(vfsBound())
	tc.stack = tc.stack[:0]
	tc.next = 0
	tc.g = g
	tc.hits = 0

	if tc.childFn == nil {
		tc.childFn = tc.visitChild
		tc.spliceFn = tc.spliceChild
		tc.emitFn = tc.emitMembers
	}

	tc.strongconnect(root)
	tc.g = nil

	return tc.hits
}

func (tc *TarjanCtx) visitChild(w VFS) {
	if _, cached := tc.g.cachedWindow(w); cached {
		tc.hits++

		return
	}

	v := tc.visitV

	if !tc.scratch.visited(w) {
		tc.strongconnect(w)

		tc.visitV = v

		if tc.scratch.lowOf(w) < tc.scratch.lowOf(v) {
			tc.scratch.setLow(v, tc.scratch.lowOf(w))
		}
	} else if tc.scratch.onStackOf(w) {
		if tc.scratch.indexOf(w) < tc.scratch.lowOf(v) {
			tc.scratch.setLow(v, tc.scratch.indexOf(w))
		}
	}
}

func (tc *TarjanCtx) spliceChild(ch VFS) {
	if tc.scratch.onStackHas(ch) {
		return
	}

	if tc.g.windowSubsumed(ch) {
		return
	}

	cl, _ := tc.g.cachedWindow(ch)

	tc.emitK = cl.spliceInto(&tc.closure, tc.emitBuf, tc.emitK)
}

func (tc *TarjanCtx) emitMembers(block []VFS) int {
	members := tc.emitMembersSlice()
	k := 0

	for _, u := range members {
		if !tc.closure.has(u) {
			tc.closure.add(u)
			block[k] = u
			k++
		}
	}

	tc.emitBuf = block
	tc.emitK = k

	for _, u := range members {
		tc.g.forEachChild(u, tc.spliceFn)
	}

	tc.emitBuf = nil

	return tc.emitK
}

func (tc *TarjanCtx) emitMembersSlice() []VFS {
	return tc.stack[tc.sccStart():]
}

func (tc *TarjanCtx) sccStart() int {
	v := tc.visitV
	sccStart := len(tc.stack) - 1

	for tc.stack[sccStart] != v {
		sccStart--
	}

	return sccStart
}

func (tc *TarjanCtx) strongconnect(v VFS) {
	tc.next++
	tc.scratch.discover(v, tc.next)
	tc.stack = append(tc.stack, v)
	tc.visitV = v

	tc.g.forEachChild(v, tc.childFn)

	if tc.scratch.lowOf(v) != tc.scratch.indexOf(v) {
		return
	}

	sccStart := tc.sccStart()
	members := tc.stack[sccStart:]

	tc.closure.reset(vfsBound())
	tc.g.emitClosure(members, tc.emitFn)

	for _, u := range members {
		tc.scratch.setOnStack(u, false)
	}

	tc.stack = tc.stack[:sccStart]
}
