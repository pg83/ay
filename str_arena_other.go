//go:build !linux

package main

import (
	"os"
	"unsafe"
)

func (a *ReservedArena) mapFile(f *os.File, off uint64, size int) {
	a.grow(size)
	throw2(f.ReadAt(unsafe.Slice((*byte)(a.base), size), int64(off)))

	a.used = size
}
