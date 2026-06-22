package main

// BitSet is a grow-on-demand set of dense uint32 ids backed by a bit vector. At
// 1 bit/id it is 32x smaller than an epoch-stamped IdSet, but has no reset; the
// zero value is empty. Suits set-once, never-cleared usage.
type BitSet struct {
	words []uint64
}

func (b *BitSet) has(v uint32) bool {
	w := v >> 6

	return w < uint32(len(b.words)) && b.words[w]&(uint64(1)<<(v&63)) != 0
}

func (b *BitSet) add(v uint32) {
	w := v >> 6

	if w >= uint32(len(b.words)) {
		grown := uint32(len(b.words)) * 2

		if grown <= w {
			grown = w + 1
		}

		next := make([]uint64, grown)
		copy(next, b.words)
		b.words = next
	}

	b.words[w] |= uint64(1) << (v & 63)
}

func (b *BitSet) remove(v uint32) {
	if w := v >> 6; w < uint32(len(b.words)) {
		b.words[w] &^= uint64(1) << (v & 63)
	}
}

// set adds v when on, removes it otherwise, so callers holding a bool need no
// branch.
func (b *BitSet) set(v uint32, on bool) {
	if on {
		b.add(v)
	} else {
		b.remove(v)
	}
}
