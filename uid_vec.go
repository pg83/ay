package main

import "math/bits"

// UidVec is an append-only, index-addressable vector of UIDs keyed by node id.
//
// Backed by 64 geometric pages: page p holds 1<<p entries, index i lives in
// page floor(log2(i+1)). Pages allocate lazily on first write; the page table
// never reallocates.
//
// A page never moves once allocated, making get/set lock-free: the gen
// goroutine set()s before emit, executors get() only deps resolved earlier, and
// `go fire` at emit establishes happens-before — so no lock is needed.
type UidVec struct {
	pages [64][]UID
}

// pageOffset maps a node id to its page and offset.
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
