package main

import (
	"syscall"
	"unsafe"
)

const (
	strOffBits     = 36
	strOffMask     = 1<<strOffBits - 1
	strLenMax      = 1<<(64-strOffBits) - 1
	strTextReserve = 1 << strOffBits
	strIDReserve   = 1 << 31
	strArenaStep   = 16 << 20
)

type ReservedArena struct {
	base     unsafe.Pointer
	used     int
	mapped   int
	reserved int
}

func reserveArena(size int) ReservedArena {
	for {
		mem, err := syscall.Mmap(-1, 0, size, syscall.PROT_NONE, syscall.MAP_PRIVATE|syscall.MAP_ANON)

		if err == nil {
			return ReservedArena{base: unsafe.Pointer(unsafe.SliceData(mem)), reserved: size}
		}

		if size <= strArenaStep {
			throwFmt("reserve %d bytes of address space: %v", size, err)
		}

		size >>= 1
	}
}

func (a *ReservedArena) grow(need int) {
	target := min((need+strArenaStep)&^(strArenaStep-1), a.reserved)

	if need > target {
		throwFmt("arena of %d bytes is exhausted (%d needed)", a.reserved, need)
	}

	if target <= a.mapped {
		return
	}

	throw(syscall.Mprotect(unsafe.Slice((*byte)(unsafe.Add(a.base, a.mapped)), target-a.mapped), syscall.PROT_READ|syscall.PROT_WRITE))

	a.mapped = target
}

func (a *ReservedArena) alloc(n int) []byte {
	if a.used+n >= a.mapped {
		a.grow(a.used + n)
	}

	return unsafe.Slice((*byte)(unsafe.Add(a.base, a.used)), a.mapped-a.used)
}

func (a *ReservedArena) commit(k int) {
	a.used += k
}
