//go:build amd64

package main

const bucketHashSIMDMin = 8

var useBucketHashAVX2 = cpuHasAVX2()

func cpuHasAVX2() bool

//go:noescape
func bucketAccumAVX2(p *VFS, n int) (sum, xr, sq uint32)

func bucketHashPlatform(elems []VFS) (sum, xr, sq uint32) {
	sum = uint32(len(elems))

	tail := elems

	if useBucketHashAVX2 && len(elems) >= bucketHashSIMDMin {
		k := len(elems) &^ 7
		s, x, q := bucketAccumAVX2(&elems[0], k)

		sum += s
		xr = x
		sq = q
		tail = elems[k:]
	}

	for _, v := range tail {
		x := uint32(v)

		sum += x
		xr ^= x
		sq += x * x
	}

	return sum, xr, sq
}
