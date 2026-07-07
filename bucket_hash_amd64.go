//go:build amd64

package main

const bucketHashSIMDMin = 8

var useBucketHashAVX2 = cpuHasAVX2()

func cpuHasAVX2() bool

//go:noescape
func bucketAccumAVX2(p *VFS, n int) (sum, xr, sq uint32)
