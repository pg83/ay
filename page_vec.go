package main

import "math/bits"

type PageVec[T any] struct {
	pages [32][]T
}

func pageOffset(id uint32) (page int, off int64) {
	n := uint64(id) + 1
	p := bits.Len64(n) - 1

	return p, int64(n - (uint64(1) << uint(p)))
}

func (v *PageVec[T]) set(id uint32, x T) {
	p, off := pageOffset(id)

	if v.pages[p] == nil {
		v.pages[p] = make([]T, int64(1)<<uint(p))
	}

	v.pages[p][off] = x
}

func (v *PageVec[T]) get(id uint32) T {
	p, off := pageOffset(id)

	return v.pages[p][off]
}

func (v *PageVec[T]) getSafe(id uint32) T {
	p, off := pageOffset(id)

	if v.pages[p] == nil {
		var zero T

		return zero
	}

	return v.pages[p][off]
}
