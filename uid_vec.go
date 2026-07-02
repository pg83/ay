package main

import "math/bits"

type UidVec struct {
	pages [32][]UID
}

func pageOffset(id NodeRef) (page int, off int64) {
	n := uint64(id) + 1
	p := bits.Len64(n) - 1

	return p, int64(n - (uint64(1) << uint(p)))
}

func (v *UidVec) set(id NodeRef, u UID) {
	p, off := pageOffset(id)

	if v.pages[p] == nil {
		v.pages[p] = make([]UID, int64(1)<<uint(p))
	}

	v.pages[p][off] = u
}

func (v *UidVec) get(id NodeRef) UID {
	p, off := pageOffset(id)

	return v.pages[p][off]
}
