package main

// mortonGeneric interleaves the bits of p and s into one uint64 Z-order code:
// result bit 2i is s's bit i, bit 2i+1 is p's bit i. Unlike p<<32|s, it folds the
// low bits of BOTH ids into the key's low bits, so an identity-hashed
// open-addressing table (index = key & mask) spreads (p, s) pairs instead of
// clustering them by s alone. Bijective — distinct pairs never collide.
//
// This is the portable magic-number bit-spread; on amd64 morton() is the BMI2
// PDEP asm (morton_amd64.s), ~2.4x faster (0.8ns vs 2.0ns/op).
func mortonGeneric(p, s uint32) uint64 {
	return spreadEven(s) | spreadEven(p)<<1
}

// spreadEven scatters x's 32 bits into the even bit positions of a uint64
// (bit i -> bit 2i), zeroing the odd positions.
func spreadEven(x uint32) uint64 {
	v := uint64(x)
	v = (v | v<<16) & 0x0000FFFF0000FFFF
	v = (v | v<<8) & 0x00FF00FF00FF00FF
	v = (v | v<<4) & 0x0F0F0F0F0F0F0F0F
	v = (v | v<<2) & 0x3333333333333333
	v = (v | v<<1) & 0x5555555555555555

	return v
}
