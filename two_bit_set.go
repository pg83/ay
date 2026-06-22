package main

// TwoBitSet is a grow-on-demand map of dense ids to 2-bit values, 32 cells per
// uint64 word. The zero value maps every id to 0, so 0 encodes "unset".
type TwoBitSet struct {
	words []uint64
}

func (b *TwoBitSet) get(v uint32) uint8 {
	w := v >> 5

	if w >= uint32(len(b.words)) {
		return 0
	}

	return uint8(b.words[w] >> ((v & 31) * 2) & 3)
}

func (b *TwoBitSet) set(v uint32, val uint8) {
	w := v >> 5

	if w >= uint32(len(b.words)) {
		grown := uint32(len(b.words)) * 2

		if grown <= w {
			grown = w + 1
		}

		next := make([]uint64, grown)
		copy(next, b.words)
		b.words = next
	}

	shift := (v & 31) * 2
	b.words[w] = b.words[w]&^(3<<shift) | uint64(val&3)<<shift
}
