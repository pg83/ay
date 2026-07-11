//go:build amd64

package main

var (
	useBucketHashAVX2   = cpuHasAVX2()
	useBucketHashAVX512 = cpuHasAVX512()
)

const bucketHashSIMDMin = 16

func cpuHasAVX2() bool

func cpuHasAVX512() bool

//go:noescape
func bucketMix64BlockAVX2(p *VFS, n int) (sum, xr uint64)

//go:noescape
func bucketMix64BlockAVX512(p *VFS, n int) (sum, xr uint64)

func bucketMix64Scalar(elems []VFS) (sum, xr uint64) {
	var sum1, xor1 uint64

	for len(elems) >= 2 {
		z0 := mix64(uint64(elems[0]))
		z1 := mix64(uint64(elems[1]))

		sum += z0
		sum1 += z1
		xr ^= z0
		xor1 ^= z1
		elems = elems[2:]
	}

	for _, v := range elems {
		z := mix64(uint64(v))

		sum += z
		xr ^= z
	}

	return sum + sum1, xr ^ xor1
}

func bucketMix64AVX2(elems []VFS) (sum, xr uint64) {
	k := len(elems) &^ 3

	if k != 0 {
		sum, xr = bucketMix64BlockAVX2(&elems[0], k)
	}
	if k == len(elems) {
		return sum, xr
	}

	for _, v := range elems[k:] {
		z := mix64(uint64(v))

		sum += z
		xr ^= z
	}

	return sum, xr
}

func bucketMix64AVX512(elems []VFS) (sum, xr uint64) {
	k := len(elems) &^ 7

	if k != 0 {
		sum, xr = bucketMix64BlockAVX512(&elems[0], k)
	}
	if k == len(elems) {
		return sum, xr
	}

	for _, v := range elems[k:] {
		z := mix64(uint64(v))

		sum += z
		xr ^= z
	}

	return sum, xr
}

func bucketHashPlatform(elems []VFS) (sum, xr uint64) {
	if len(elems) >= bucketHashSIMDMin {
		if useBucketHashAVX512 {
			return bucketMix64AVX512(elems)
		}
		if useBucketHashAVX2 {
			return bucketMix64AVX2(elems)
		}
	}

	return bucketMix64Scalar(elems)
}
