package main

import "unsafe"

const bumpChunkBytes = 1 << 20

type BumpAllocator[T any] struct {
	chunks  [][]T
	off     int
	pending int
}

func bumpChunkElems[T any]() int {
	sz := int(unsafe.Sizeof(*new(T)))

	if sz < 1 {
		sz = 1
	}

	n := bumpChunkBytes / sz

	if n < 1 {
		n = 1
	}

	return n
}

func newBumpAllocator[T any](int) *BumpAllocator[T] {
	return &BumpAllocator[T]{}
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
	size := bumpChunkElems[T]()

	if size < min {
		size = min
	}

	a.chunks = append(a.chunks, make([]T, size))
	a.off = 0
}
