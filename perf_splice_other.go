//go:build !amd64

package main

func benchAVX512(gen []uint32, epoch uint32, win []VFS, block []VFS, k int) int {
	return benchScalar(gen, epoch, win, block, k)
}

func benchPrefetch(gen []uint32, epoch uint32, win []VFS, block []VFS, k int) int {
	return benchScalar(gen, epoch, win, block, k)
}
