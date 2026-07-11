//go:build !amd64

package main

func bucketHashPlatform(elems []VFS) (sum, xr uint64) {
	for _, v := range elems {
		z := mix64(uint64(v))

		sum += z
		xr ^= z
	}

	return sum, xr
}
