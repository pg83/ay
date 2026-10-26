package main

import (
	"syscall"
	"unsafe"
)

func openatRaw(dirFD int, path *byte, flags int) (int, syscall.Errno) {
	r1, _, errno := syscall.Syscall6(syscall.SYS_OPENAT, uintptr(dirFD), uintptr(unsafe.Pointer(path)), uintptr(flags), 0, 0, 0)

	return int(r1), errno
}

func fstatatRaw(dirFD int, path *byte, st *syscall.Stat_t) syscall.Errno {
	_, _, errno := syscall.Syscall6(sysFstatat, uintptr(dirFD), uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(st)), 0, 0, 0)

	return errno
}
