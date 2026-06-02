package main

// bumpAllocator is a generic, append-free arena allocator.
//
// alloc(n) hands out a contiguous, writable region of at least n elements
// carved from the current chunk's free tail. The caller writes into the region
// by index and then calls commit(k) with the number of elements it actually
// wrote (0 <= k <= len(region)); commit advances the allocation boundary by k,
// so the unwritten remainder of the region is handed out again by the next
// alloc. There is no append: the caller must not write past len(region).
//
// Regions are address-stable: chunk backing arrays are never reallocated or
// moved once created, so a region returned by alloc (and the prefix retained by
// commit) stays valid for the arena's lifetime. This makes the arena safe to
// hand out long-lived slices into.
//
// Chunks grow geometrically — each new chunk is 1.5x the previous one — without
// bound. Geometric growth keeps small arenas small (a tiny workload never
// allocates a large chunk) while keeping the number of chunks logarithmic in
// the total size. A chunk is always made at least as large as the alloc that
// triggered it, so any single alloc fits in one chunk.
type bumpAllocator[T any] struct {
	chunks  [][]T
	off     int // bump boundary within the active (last) chunk
	next    int // size of the next chunk to allocate
	pending int // length handed out by the last alloc, not yet committed
}

func newBumpAllocator[T any](initial int) *bumpAllocator[T] {
	if initial < 1 {
		initial = 1
	}

	return &bumpAllocator[T]{next: initial}
}

// alloc returns a writable region of at least n elements. The region is valid
// until the next alloc on this arena; call commit before allocating again.
func (a *bumpAllocator[T]) alloc(n int) []T {
	if len(a.chunks) == 0 || a.off+n > len(a.chunks[len(a.chunks)-1]) {
		a.addChunk(n)
	}

	region := a.chunks[len(a.chunks)-1][a.off:]
	a.pending = len(region)

	return region
}

// commit advances the allocation boundary by k, the number of elements actually
// written into the region returned by the preceding alloc.
func (a *bumpAllocator[T]) commit(k int) {
	if k < 0 || k > a.pending {
		panic("bumpAllocator: commit out of range")
	}

	a.off += k
	a.pending = 0
}

func (a *bumpAllocator[T]) addChunk(min int) {
	size := a.next

	if size < min {
		size = min
	}

	a.chunks = append(a.chunks, make([]T, size))
	a.off = 0

	a.next = size + size/2 // 1.5x growth
}
