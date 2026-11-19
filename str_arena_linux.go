//go:build linux

package main

import (
	"os"
	"syscall"
)

func (a *ReservedArena) mapFile(f *os.File, off uint64, size int) {
	page := os.Getpagesize()
	length := (size + page - 1) &^ (page - 1)

	if length != 0 {
		if _, _, errno := syscall.Syscall6(syscall.SYS_MMAP, uintptr(a.base), uintptr(length), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE|syscall.MAP_FIXED, f.Fd(), uintptr(off)); errno != 0 {
			throwFmt("map %s into the arena: %v", f.Name(), errno)
		}
	}

	a.used = size
	a.mapped = max(a.mapped, length)
}
