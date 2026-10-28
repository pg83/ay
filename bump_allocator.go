package main

import "unsafe"

const (
	bumpChunkInitial = 128
	bumpChunkBytes   = 1 << 18
)

type BumpAllocator[T any] struct {
	chunk  []T
	next   int
	strict bool
	open   bool
}

func (a *BumpAllocator[T]) markStrict() {
	a.strict = true
}

func newBumpAllocator[T any]() *BumpAllocator[T] {
	return &BumpAllocator[T]{next: bumpChunkInitial}
}

func (a *BumpAllocator[T]) alloc(n int) []T {
	if ownershipOn && a.strict {
		assert(!a.open, "bump: nested alloc on strict arena (open window)")

		a.open = true
	}

	if len(a.chunk) < n {
		var zero T

		limit := chaosBumpChunkBytes(bumpChunkBytes) / int(unsafe.Sizeof(zero))
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

type BumpSpan struct {
	chunk  uint32
	start  uint32
	length uint32
}

type IndexedBumpAllocator[T any] struct {
	BumpAllocator[T]
	chunks Vec[[]T]
}

func newIndexedBumpAllocator[T any]() *IndexedBumpAllocator[T] {
	return &IndexedBumpAllocator[T]{BumpAllocator: BumpAllocator[T]{next: bumpChunkInitial}}
}

func (a *IndexedBumpAllocator[T]) alloc(n int) []T {
	if len(a.chunk) < n {
		block := a.BumpAllocator.alloc(n)

		a.chunks.pushBack(block)

		return block
	}

	return a.BumpAllocator.alloc(n)
}

func (a *IndexedBumpAllocator[T]) commitSpan(n int) BumpSpan {
	i := len(a.chunks.s) - 1
	span := BumpSpan{chunk: uint32(i), start: uint32(len(a.chunks.s[i]) - len(a.chunk)), length: uint32(n)}

	a.commit(n)

	return span
}

func (a *IndexedBumpAllocator[T]) slice(span BumpSpan) []T {
	end := span.start + span.length

	return a.chunks.s[span.chunk][span.start:end:end]
}
