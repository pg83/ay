package main

import "math/bits"

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
	p := bits.Len64(m) - 1

	return &v.pages[p][m-(uint64(1)<<uint(p))]
}
