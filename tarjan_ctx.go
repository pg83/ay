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

func (t *tarjanScratch) lowOf(v VFS) int32        { return t.low[uint32(v)] }
func (t *tarjanScratch) indexOf(v VFS) int32      { return t.index[uint32(v)] }
func (t *tarjanScratch) setLow(v VFS, x int32)    { t.low[uint32(v)] = x }
func (t *tarjanScratch) onStackOf(v VFS) bool     { return t.onStack[uint32(v)] }
func (t *tarjanScratch) setOnStack(v VFS, b bool) { t.onStack[uint32(v)] = b }
