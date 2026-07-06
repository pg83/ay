package main

import "unsafe"

const vecSlowGrowBytes = 1 << 20

type Vec[T any] struct {
	s []T
}

func (v *Vec[T]) len() int {
	return len(v.s)
}

func (v *Vec[T]) grow(need int) {
	var zero T

	c := cap(v.s) * 2

	if cap(v.s)*int(unsafe.Sizeof(zero)) >= vecSlowGrowBytes {
		c = cap(v.s) + cap(v.s)/2
	}

	if c < 8 {
		c = 8
	}

	if c < need {
		c = need
	}

	next := make([]T, len(v.s), c)

	copy(next, v.s)
	v.s = next
}

func (v *Vec[T]) reserve(n int) {
	if cap(v.s) < n {
		v.grow(n)
	}
}

func (v *Vec[T]) pushBack(x T) {
	if len(v.s) == cap(v.s) {
		v.grow(len(v.s) + 1)
	}

	v.s = append(v.s, x)
}

func (v *Vec[T]) ensureLen(n int) {
	if n <= len(v.s) {
		return
	}

	v.reserve(n)
	v.s = v.s[:n]
}
