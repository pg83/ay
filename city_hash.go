package main

import (
	"encoding/binary"
	"math/bits"
)

const (
	cityK0  uint64 = 0xc3a5c85c97cb3127
	cityK1  uint64 = 0xb492b66fbe98f273
	cityK2  uint64 = 0x9ae16a3b2f90404f
	cityK3  uint64 = 0xc949d7c7509e6557
	cityMul uint64 = 0x9ddfea08eb382d69
)

func cityLoad64(s []byte, i int) uint64 {
	return binary.LittleEndian.Uint64(s[i:])
}

func cityLoad32(s []byte, i int) uint64 {
	return uint64(binary.LittleEndian.Uint32(s[i:]))
}

func cityRotate(v uint64, shift int) uint64 {
	return bits.RotateLeft64(v, -shift)
}

func cityShiftMix(v uint64) uint64 {
	return v ^ (v >> 47)
}

func cityHashLen16(u, v uint64) uint64 {
	a := (u ^ v) * cityMul

	a ^= a >> 47

	b := (v ^ a) * cityMul

	b ^= b >> 47

	return b * cityMul
}

func cityHashLen0to16(s []byte) uint64 {
	n := len(s)

	switch {
	case n > 8:
		a := cityLoad64(s, 0)
		b := cityLoad64(s, n-8)

		return cityHashLen16(a, cityRotate(b+uint64(n), n)) ^ b
	case n >= 4:
		a := cityLoad32(s, 0)

		return cityHashLen16(uint64(n)+(a<<3), cityLoad32(s, n-4))
	case n > 0:
		y := uint32(s[0]) + uint32(s[n>>1])<<8
		z := uint32(n) + uint32(s[n-1])<<2

		return cityShiftMix(uint64(y)*cityK2^uint64(z)*cityK3) * cityK2
	}

	return cityK2
}

func cityHashLen17to32(s []byte) uint64 {
	n := len(s)
	a := cityLoad64(s, 0) * cityK1
	b := cityLoad64(s, 8)
	c := cityLoad64(s, n-8) * cityK2
	d := cityLoad64(s, n-16) * cityK0

	return cityHashLen16(cityRotate(a-b, 43)+cityRotate(c, 30)+d, a+cityRotate(b^cityK3, 20)-c+uint64(n))
}

func cityWeakHashLen32(s []byte, i int, a, b uint64) (uint64, uint64) {
	w, x, y, z := cityLoad64(s, i), cityLoad64(s, i+8), cityLoad64(s, i+16), cityLoad64(s, i+24)

	a += w
	b = cityRotate(b+a+z, 21)

	c := a

	a += x
	a += y
	b += cityRotate(a, 44)

	return a + z, b + c
}

func cityHashLen33to64(s []byte) uint64 {
	n := len(s)
	z := cityLoad64(s, 24)
	a := cityLoad64(s, 0) + (uint64(n)+cityLoad64(s, n-16))*cityK0
	b := cityRotate(a+z, 52)
	c := cityRotate(a, 37)

	a += cityLoad64(s, 8)
	c += cityRotate(a, 7)
	a += cityLoad64(s, 16)

	vf := a + z
	vs := b + cityRotate(a, 31) + c

	a = cityLoad64(s, 16) + cityLoad64(s, n-32)
	z = cityLoad64(s, n-8)
	b = cityRotate(a+z, 52)
	c = cityRotate(a, 37)
	a += cityLoad64(s, n-24)
	c += cityRotate(a, 7)
	a += cityLoad64(s, n-16)

	wf := a + z
	ws := b + cityRotate(a, 31) + c
	r := cityShiftMix((vf+ws)*cityK2 + (wf+vs)*cityK0)

	return cityShiftMix(r*cityK0+vs) * cityK2
}

func cityHash64(s []byte) uint64 {
	n := len(s)

	switch {
	case n <= 16:
		return cityHashLen0to16(s)
	case n <= 32:
		return cityHashLen17to32(s)
	case n <= 64:
		return cityHashLen33to64(s)
	}

	x := cityLoad64(s, 0)
	y := cityLoad64(s, n-16) ^ cityK1
	z := cityLoad64(s, n-56) ^ cityK0
	v1, v2 := cityWeakHashLen32(s, n-64, uint64(n), y)
	w1, w2 := cityWeakHashLen32(s, n-32, uint64(n)*cityK1, cityK0)

	z += cityShiftMix(v2) * cityK1
	x = cityRotate(z+x, 39) * cityK1
	y = cityRotate(y, 33) * cityK1

	for i, rest := 0, (n-1)&^63; rest != 0; i, rest = i+64, rest-64 {
		x = cityRotate(x+y+v1+cityLoad64(s, i+16), 37) * cityK1
		y = cityRotate(y+v2+cityLoad64(s, i+48), 42) * cityK1
		x ^= w2
		y ^= v1
		z = cityRotate(z^w1, 33)
		v1, v2 = cityWeakHashLen32(s, i, v2*cityK1, x+w1)
		w1, w2 = cityWeakHashLen32(s, i+32, z+w2, y)
		z, x = x, z
	}

	return cityHashLen16(cityHashLen16(v1, w1)+cityShiftMix(y)*cityK1+z, cityHashLen16(v2, w2)+x)
}

func utilIntHash64(key uint64) uint64 {
	key += ^(key << 32)
	key ^= key >> 22
	key += ^(key << 13)
	key ^= key >> 8
	key += key << 3
	key ^= key >> 15
	key += ^(key << 27)
	key ^= key >> 31

	return key
}

func utilCombineHashes(l, r uint64) uint64 {
	return utilIntHash64(l) ^ r
}
