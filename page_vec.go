package main

import (
	"math/bits"
	"unsafe"
)

type PageVec[T any] struct {
	pages [32][]T
	n     int
}

func (v *PageVec[T]) len() int {
	return v.n
}

func (v *PageVec[T]) push(x T) int {
	i := v.n
	m := uint64(i) + 1
	p := bits.Len64(m) - 1

	if v.pages[p] == nil {
		v.pages[p] = make([]T, uint64(1)<<uint(p))
	}

	v.pages[p][m-(uint64(1)<<uint(p))] = x
	v.n++

	return i
}

func (v *PageVec[T]) at(i int) *T {
	m := uint64(i) + 1
	p := (bits.Len64(m) - 1) & 31 // ids are uint32 → p ∈ [0,31]; the mask lets BCE drop the pages[p] check
	page := v.pages[p]
	off := m - (uint64(1) << uint(p))

	// off is always in [0, 2^p) and len(page)==2^p, so the index is in range
	// by construction: elide the bounds check and the len load.
	var zero T

	return (*T)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(page)), uintptr(off)*unsafe.Sizeof(zero)))
}
