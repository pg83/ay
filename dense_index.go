package main

import "iter"

var denseIndexZero denseIndexPage

const denseIndexPageBits = 10

type denseIndexPage [1 << denseIndexPageBits]uint32

type DenseIndex struct {
	pages Vec[*denseIndexPage]
}

func (idx *DenseIndex) get(k uint32) uint32 {
	page := k >> denseIndexPageBits

	if int(page) < len(idx.pages.s) {
		return idx.pages.s[page][k&(1<<denseIndexPageBits-1)]
	}

	return 0
}

func (idx *DenseIndex) put(k, slot uint32) {
	page := k >> denseIndexPageBits

	if int(page) >= len(idx.pages.s) {
		old := len(idx.pages.s)

		idx.pages.ensureLen(int(page) + 1)

		for i := old; i < len(idx.pages.s); i++ {
			idx.pages.s[i] = &denseIndexZero
		}
	}

	p := idx.pages.s[page]

	if p == &denseIndexZero {
		p = new(denseIndexPage)
		idx.pages.s[page] = p
	}

	p[k&(1<<denseIndexPageBits-1)] = slot
}

func (idx *DenseIndex) each() iter.Seq2[uint32, uint32] {
	return func(yield func(uint32, uint32) bool) {
		for i, page := range idx.pages.s {
			if page == &denseIndexZero {
				continue
			}

			for j, slot := range page {
				if slot != 0 && !yield(uint32(i)<<denseIndexPageBits|uint32(j), slot) {
					return
				}
			}
		}
	}
}
