package main

import "math/bits"

// uidVec is an append-only, index-addressable vector of UIDs keyed by node id.
//
// It is backed by 64 pages of geometric size: page p holds 1<<p entries. Index i
// lives in page floor(log2(i+1)), which begins at index (1<<p)-1. Pages allocate
// lazily on first write, so footprint tracks the high-water id, and the page
// table itself never grows or reallocates.
//
// A page never moves once allocated, which makes get/set lock-free under the
// streaming-gen pattern: the single gen goroutine set()s uids[id] before emit,
// and executor goroutines get(depId) only for deps resolved earlier (already
// allocated and written). The `go fire` at emit establishes happens-before, and
// distinct ids touch distinct slots — so no lock is needed.
type UidVec struct {
	pages [64][]UID
}

// pageOffset maps a node id to its page and in-page offset.
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
