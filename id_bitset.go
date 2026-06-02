package main

// idBitSet is a grow-on-demand set of ids (VFS values) backed by a bit vector —
// one bit per id, indexed by uint32(v). At 1 bit/id it is 32x smaller than an
// epoch-stamped idSet (which spends a uint32 per id). There is no epoch/reset:
// the zero value is an empty set, has reports membership, add inserts. It suits
// set-once, never-cleared usage (e.g. the dfs in-flight guard) where the dense
// idSet's per-id word would only waste memory.
type idBitSet struct {
	words []uint64
}

func (b *idBitSet) has(v VFS) bool {
	w := uint32(v) >> 6

	return w < uint32(len(b.words)) && b.words[w]&(uint64(1)<<(uint32(v)&63)) != 0
}

func (b *idBitSet) add(v VFS) {
	w := uint32(v) >> 6

	if w >= uint32(len(b.words)) {
		grown := uint32(len(b.words)) * 2

		if grown <= w {
			grown = w + 1
		}

		next := make([]uint64, grown)
		copy(next, b.words)
		b.words = next
	}

	b.words[w] |= uint64(1) << (uint32(v) & 63)
}
