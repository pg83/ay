package main

// bumpAllocator is a generic, append-free arena allocator.
//
// alloc(n) hands out a writable region of at least n elements; the caller writes by
// index, then commit(k) advances the boundary. Never write past len(region).
//
// Regions are address-stable: backing arrays are never moved, so a region stays valid
// for the arena's lifetime — safe to hand out long-lived slices into.
//
// Chunks grow geometrically (1.5x), and a chunk is always at least as large as the
// alloc that triggered it, so any single alloc fits in one chunk.
type BumpAllocator[T any] struct {
	chunks  [][]T
	off     int // bump boundary within the active (last) chunk
	next    int // size of the next chunk to allocate
	pending int // length handed out by the last alloc, not yet committed
}

func newBumpAllocator[T any](initial int) *BumpAllocator[T] {
	if initial < 1 {
		initial = 1
	}

	return &BumpAllocator[T]{next: initial}
}

// alloc returns a writable region of at least n elements, valid until the next alloc.
func (a *BumpAllocator[T]) alloc(n int) []T {
	if len(a.chunks) == 0 || a.off+n > len(a.chunks[len(a.chunks)-1]) {
		a.addChunk(n)
	}

	region := a.chunks[len(a.chunks)-1][a.off:]
	a.pending = len(region)

	return region
}

// commit advances the boundary by k, the number of elements written since alloc.
func (a *BumpAllocator[T]) commit(k int) {
	if k < 0 || k > a.pending {
		panic("bumpAllocator: commit out of range")
	}

	a.off += k
	a.pending = 0
}

// list copies vs into the arena and returns the committed block — an arena-backed
// replacement for a small slice literal.
func (a *BumpAllocator[T]) list(vs ...T) []T {
	n := len(vs)
	block := a.alloc(n)
	copy(block, vs)
	a.commit(n)

	return block[:n:n]
}

func (a *BumpAllocator[T]) addChunk(min int) {
	size := a.next

	if size < min {
		size = min
	}

	a.chunks = append(a.chunks, make([]T, size))
	a.off = 0

	a.next = size + size/2 // 1.5x growth
}
