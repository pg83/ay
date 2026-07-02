package main

const bumpChunkSize = 200_000

type BumpAllocator[T any] struct {
	chunk []T
}

func newBumpAllocator[T any](int) *BumpAllocator[T] {
	return &BumpAllocator[T]{}
}

func (a *BumpAllocator[T]) alloc(n int) []T {
	if len(a.chunk) < n {
		size := bumpChunkSize

		if size < n {
			size = n
		}

		a.chunk = make([]T, size)
	}

	return a.chunk
}

func (a *BumpAllocator[T]) commit(k int) {
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
