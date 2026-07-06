package main

type BitSet struct {
	words Vec[uint64]
}

func (b *BitSet) has(v uint32) bool {
	w := v >> 6

	return w < uint32(b.words.len()) && b.words.s[w]&(uint64(1)<<(v&63)) != 0
}

func (b *BitSet) add(v uint32) {
	w := v >> 6

	b.words.ensureLen(int(w) + 1)

	b.words.s[w] |= uint64(1) << (v & 63)
}

func (b *BitSet) remove(v uint32) {
	if w := v >> 6; w < uint32(b.words.len()) {
		b.words.s[w] &^= uint64(1) << (v & 63)
	}
}

func (b *BitSet) set(v uint32, on bool) {
	if on {
		b.add(v)
	} else {
		b.remove(v)
	}
}
