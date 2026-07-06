package main

type TwoBitSet struct {
	words Vec[uint64]
}

func (b *TwoBitSet) get(v uint32) uint8 {
	w := v >> 5

	if w >= uint32(b.words.len()) {
		return 0
	}

	return uint8(b.words.s[w] >> ((v & 31) * 2) & 3)
}

func (b *TwoBitSet) set(v uint32, val uint8) {
	w := v >> 5

	b.words.ensureLen(int(w) + 1)

	shift := (v & 31) * 2

	b.words.s[w] = b.words.s[w]&^(3<<shift) | uint64(val&3)<<shift
}
