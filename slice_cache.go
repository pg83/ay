package main

import (
	"unsafe"

	"github.com/zeebo/xxh3"
)

type SliceVal[T any] struct {
	verify uint64
	slice  []T
}

type SliceCache[T comparable] struct {
	pool     *BumpAllocator[T]
	table    *IntMap[SliceVal[T]]
	overflow *IntMap[SliceVal[T]]
}

func newSliceCache[T comparable](hint int) *SliceCache[T] {
	return &SliceCache[T]{
		pool:     newBumpAllocator[T](hint),
		table:    newIntMap[SliceVal[T]](hint),
		overflow: newIntMap[SliceVal[T]](1 << 4),
	}
}

func (c *SliceCache[T]) alloc(max int) []T {
	return c.pool.alloc(max)
}

func sliceBytes[T any](block []T) []byte {
	var zero T

	return unsafe.Slice((*byte)(unsafe.Pointer(&block[0])), len(block)*int(unsafe.Sizeof(zero)))
}

func (c *SliceCache[T]) intern(block []T) []T {
	if len(block) == 0 {
		return nil
	}

	sum := xxh3.Hash128(sliceBytes(block))
	h1, h2 := sum.Hi, sum.Lo

	if h1 == 0 {
		h1 = 1
	}

	if h2 == 0 {
		h2 = 1
	}

	cell, found := c.table.cell(h1)

	if found {
		if cell.verify == h2 {
			return cell.slice
		}

		cell2, found2 := c.overflow.cell(h2)

		if found2 {
			if cell2.verify != h1 {
				throwFmt("SliceCache: hash pair collision (h1=%#x h2=%#x, %d elems)", h1, h2, len(block))
			}

			return cell2.slice
		}

		slice := c.commit(block)

		*cell2 = SliceVal[T]{verify: h1, slice: slice}

		return slice
	}

	slice := c.commit(block)

	*cell = SliceVal[T]{verify: h2, slice: slice}

	return slice
}

func (c *SliceCache[T]) internCopy(xs []T) []T {
	if len(xs) == 0 {
		return nil
	}

	block := c.alloc(len(xs))

	copy(block, xs)

	return c.intern(block[:len(xs)])
}

func (c *SliceCache[T]) commit(block []T) []T {
	c.pool.commit(len(block))

	return block[:len(block):len(block)]
}

func dedupShared[T IdKey](c *SliceCache[T], lists ...[]T) []T {
	total := 0

	for _, l := range lists {
		total += len(l)
	}

	if total == 0 {
		return nil
	}

	deduper := dedupers.get()

	defer dedupers.put(deduper)

	block := c.alloc(total)
	k := 0

	for _, l := range lists {
		for _, x := range l {
			if deduper.add(x.strID()) {
				block[k] = x
				k++
			}
		}
	}

	return c.intern(block[:k])
}

func concatShared[T comparable](c *SliceCache[T], lists ...[]T) []T {
	total := 0
	nonEmpty := 0
	last := -1

	for i, l := range lists {
		if len(l) > 0 {
			total += len(l)
			nonEmpty++
			last = i
		}
	}

	if total == 0 {
		return nil
	}

	if nonEmpty == 1 {
		return lists[last]
	}

	block := c.alloc(total)
	k := 0

	for _, l := range lists {
		k += copy(block[k:], l)
	}

	return c.intern(block[:k])
}
