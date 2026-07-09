package main

import "unsafe"

const bumpChunkBytes = 1 << 21

type BumpAllocator[T any] struct {
	chunk  []T
	next   int
	strict bool
	open   bool
}

func (a *BumpAllocator[T]) markStrict() {
	a.strict = true
}

func newBumpAllocator[T any](hint int) *BumpAllocator[T] {
	return &BumpAllocator[T]{next: hint}
}

func (a *BumpAllocator[T]) alloc(n int) []T {
	if ownershipOn && a.strict {
		if a.open {
			throwFmt("bump: nested alloc on strict arena (open window)")
		}

		a.open = true
	}

	if len(a.chunk) < n {
		var zero T

		limit := bumpChunkBytes / int(unsafe.Sizeof(zero))
		size := a.next

		if size > limit {
			size = limit
		}

		if size < n {
			size = n
		}

		a.next = size * 2
		a.chunk = make([]T, size)

		if ownershipOn {
			registerOwnedSlice(a.chunk)
		}
	}

	return a.chunk
}

func (a *BumpAllocator[T]) commit(k int) {
	a.open = false
	a.chunk = a.chunk[k:]
}

func (a *BumpAllocator[T]) one() *T {
	block := a.alloc(1)
	p := &block[0]

	a.commit(1)

	return p
}

func (a *BumpAllocator[T]) list(vs ...T) []T {
	n := len(vs)
	block := a.alloc(n)

	copy(block, vs)
	a.commit(n)

	return block[:n:n]
}
