//go:build !amd64

package main

func bucketHashPlatform(elems []VFS) (sum, xr, sq uint32) {
	for _, v := range elems {
		x := uint32(v)

		sum += x
		xr ^= x
		sq += x * x
	}

	return sum, xr, sq
}
