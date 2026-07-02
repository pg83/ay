package main

import "math/bits"

// PageVec is an append-only vector that never copies existing elements on
// growth: element i lives in page p = floor(log2(i+1)), which is allocated
// once at its final size 2^p. Access costs a bits.Len64 + one extra deref
// versus a flat slice, but append is memmove-free.
type PageVec[T any] struct {
	pages [64][]T
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
	p := bits.Len64(m) - 1

	return &v.pages[p][m-(uint64(1)<<uint(p))]
}
