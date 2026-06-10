package main

// splitMix64 mixes two dense 32-bit ids into one uniformly distributed 64-bit
// key for the identity-hashed IntMap/IntValueMap (index = key & mask). It is the
// splitmix64 finalizer applied to p<<32|s: a bijection over 64 bits, so distinct
// (p, s) pairs never collide, while — unlike a Morton bit-interleave — it
// scatters the low bits, so dense sequential ids don't pile into one cluster and
// blow the open-addressing probe chains (Morton keying measured ~364 probes/Get
// on sourceUnderCache; splitMix64 brings it back to ~1.8).
func splitMix64(p, s uint32) uint64 {
	return mix64(uint64(p)<<32 | uint64(s))
}

// mix64 is the splitmix64 finalizer: a 64-bit bijection with full avalanche.
// Besides backing splitMix64, it chains sequence hashes (hashScanContext):
// h = mix64(h ^ next) is order-sensitive and never cancels repeated elements,
// which a bare XOR fold would.
func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31

	return x
}
