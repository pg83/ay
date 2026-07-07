//go:build !amd64

package main

const bucketHashSIMDMin = 1 << 30

const useBucketHashAVX2 = false

func bucketAccumAVX2(p *VFS, n int) (sum, xr, sq uint32) {
	panic("bucketAccumAVX2 without amd64")
}
