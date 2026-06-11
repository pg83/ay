package main

import "math/bits"

// uidVec is an append-only, index-addressable vector of UIDs keyed by node id.
//
// It is backed by 64 pages of geometric size: page p holds 1<<p entries (page 0
// holds 1, page 1 holds 2, …). Index i lives in page floor(log2(i+1)), which
// begins at index (1<<p)-1. Pages are allocated lazily on first write, so the
// footprint tracks the high-water id, and the 64-entry page table itself never
// grows or reallocates.
//
// Crucially a page never moves once allocated. That makes get/set lock-free
// across goroutines under the streaming-gen access pattern: the single gen
// goroutine set()s uids[id] before the node is emitted, and executor goroutines
// get(depId) only for deps resolved earlier (whose page is therefore already
// allocated and whose slot is already written). The `go fire` at emit establishes
// happens-before, and distinct ids touch distinct slots — so no lock is needed
// and the page table is never reallocated out from under a reader.
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
