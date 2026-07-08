//go:build amd64

package main

var useBucketHashAVX2 = cpuHasAVX2()

const bucketHashSIMDMin = 8

func cpuHasAVX2() bool

//go:noescape
func bucketAccumAVX2(p *VFS, n int) (sum, xr, sq, cb uint32)

func bucketHashPlatform(elems []VFS) (sum, xr, sq, cb uint32) {
	tail := elems

	if useBucketHashAVX2 && len(elems) >= bucketHashSIMDMin {
		k := len(elems) &^ 7

		sum, xr, sq, cb = bucketAccumAVX2(&elems[0], k)
		tail = elems[k:]
	}

	for _, v := range tail {
		x := uint32(v)

		sum += x
		xr ^= x
		sq += x * x
		cb += x * x * x
	}

	return sum, xr, sq, cb
}
