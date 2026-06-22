package main

func splitMix64(p, s uint32) uint64 {
	return mix64(uint64(p)<<32 | uint64(s))
}

func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31

	return x
}
