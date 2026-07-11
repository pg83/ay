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

func bucketHashPlatform(elems []VFS) (sum, xr uint64) {
	if len(elems) >= bucketHashSIMDMin {
		if useBucketHashAVX512 {
			k := len(elems) &^ 7

			sum, xr = bucketMix64BlockAVX512(&elems[0], k)
			elems = elems[k:]
		} else if useBucketHashAVX2 {
			k := len(elems) &^ 3

			sum, xr = bucketMix64BlockAVX2(&elems[0], k)
			elems = elems[k:]
		}
	}

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
