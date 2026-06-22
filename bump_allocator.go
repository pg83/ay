package main

type BumpAllocator[T any] struct {
	chunks  [][]T
	off     int
	next    int
	pending int
}

func newBumpAllocator[T any](initial int) *BumpAllocator[T] {
	if initial < 1 {
		initial = 1
	}

	return &BumpAllocator[T]{next: initial}
}

func (a *BumpAllocator[T]) alloc(n int) []T {
	if len(a.chunks) == 0 || a.off+n > len(a.chunks[len(a.chunks)-1]) {
		a.addChunk(n)
	}

	region := a.chunks[len(a.chunks)-1][a.off:]
	a.pending = len(region)

	return region
}

func (a *BumpAllocator[T]) commit(k int) {
	if k < 0 || k > a.pending {
		panic("bumpAllocator: commit out of range")
	}

	a.off += k
	a.pending = 0
}

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

	a.next = size + size/2
}
