package main

// splitMix64 mixes two dense 32-bit ids into one uniform 64-bit key for the
// identity-hashed IntMap (index = key & mask). The finalizer is a bijection, so
// distinct (p, s) never collide; it also scatters low bits so dense sequential
// ids don't cluster and blow the open-addressing probe chains.
func splitMix64(p, s uint32) uint64 {
	return mix64(uint64(p)<<32 | uint64(s))
}

// mix64 is the splitmix64 finalizer: a 64-bit bijection with full avalanche. It
// also chains sequence hashes order-sensitively, which a XOR fold would not.
func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31

	return x
}
