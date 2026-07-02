//go:build amd64

package main

import (
	"unsafe"

	"golang.org/x/sys/cpu"
)

var benchHasAVX512 = cpu.X86.HasAVX512F

//go:noescape
func spliceKernelAVX512(gen *uint32, epoch uint32, win *uint32, n int, block *uint32, k int) int

//go:noescape
func spliceKernelPrefetch(gen *uint32, epoch uint32, win *uint32, n int, block *uint32, k int) int

func benchAVX512(gen []uint32, epoch uint32, win []VFS, block []VFS, k int) int {
	n := len(win)

	if benchHasAVX512 && n >= 16 {
		k = spliceKernelAVX512(
			&gen[0],
			epoch,
			(*uint32)(unsafe.Pointer(&win[0])),
			n,
			(*uint32)(unsafe.Pointer(&block[0])),
			k,
		)

		return benchScalar(gen, epoch, win[n&^15:], block, k)
	}

	return benchScalar(gen, epoch, win, block, k)
}

func benchPrefetch(gen []uint32, epoch uint32, win []VFS, block []VFS, k int) int {
	n := len(win)

	if n == 0 {
		return k
	}

	return spliceKernelPrefetch(
		&gen[0],
		epoch,
		(*uint32)(unsafe.Pointer(&win[0])),
		n,
		(*uint32)(unsafe.Pointer(&block[0])),
		k,
	)
}
