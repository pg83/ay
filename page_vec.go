package main

import (
	"math/bits"
	"sync/atomic"
)

type PageVec[T any] struct {
	pages [32]atomic.Pointer[[]T]
}

func pageOffset(id uint32) (page int, off int64) {
	n := uint64(id) + 1
	p := bits.Len64(n) - 1

	return p, int64(n - (uint64(1) << uint(p)))
}

func (v *PageVec[T]) set(id uint32, x T) {
	p, off := pageOffset(id)

	if page := v.pages[p].Load(); page != nil {
		*unsafeAt(*page, uint64(off)) = x

		return
	}

	page := make([]T, int64(1)<<uint(p))

	*unsafeAt(page, uint64(off)) = x
	v.pages[p].Store(&page)
}

func (v *PageVec[T]) get(id uint32) T {
	p, off := pageOffset(id)

	return *unsafeAt(*v.pages[p].Load(), uint64(off))
}

func (v *PageVec[T]) getSafe(id uint32) T {
	p, off := pageOffset(id)
	page := v.pages[p].Load()

	if page == nil {
		var zero T

		return zero
	}

	return *unsafeAt(*page, uint64(off))
}
