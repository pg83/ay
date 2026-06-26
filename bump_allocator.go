package main

const bumpChunkSize = 200_000

// BumpAllocator hands out subslices of a single fixed-size chunk. commit advances
// past the consumed prefix; when the chunk runs out a fresh one is made (sized up
// for a request larger than the default chunk). Old chunks stay alive only through
// the subslices callers keep, so no chunk list is needed.
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

func (a *BumpAllocator[T]) list(vs ...T) []T {
	n := len(vs)
	block := a.alloc(n)
	copy(block, vs)
	a.commit(n)

	return block[:n:n]
}
